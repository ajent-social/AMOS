package apphost

import (
	"context"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajent-social/amos/app"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/observability"
	"github.com/ajent-social/amos/storage"
	"go.opentelemetry.io/otel/trace"
)

func TestLocalTracingCallerOwnership(t *testing.T) {
	collector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer dummy" {
			w.WriteHeader(401)
			return
		}
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error("collector read failed")
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer collector.Close()
	authority, err := url.Parse(collector.URL)
	if err != nil {
		t.Fatal(err)
	}
	roots := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: collector.Certificate().Raw})
	tracer, err := observability.NewTracer(observability.TraceConfig{Enabled: true, SamplePerMillion: 1000000, RouteTemplates: []string{"/trace-probe"}, Collector: observability.TraceCollectorConfig{Authority: authority.Host, DialIP: "127.0.0.1", CAPEM: roots, AuthToken: "dummy"}})
	if err != nil {
		t.Fatal("tracer unavailable")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if tracer.Close(ctx) != nil {
			t.Error("owner tracer close failed")
		}
	}()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	config := localConfig(t, localDatabase(t), origin)
	config.Tracing = tracer
	var observed atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if !span.SpanContext().IsValid() || span.IsRecording() {
			t.Error("host did not forward private tracer")
		}
		observed.Add(1)
		w.WriteHeader(202)
	})
	config.Business = func(*storage.DB, *session.Service) ([]Route, error) {
		return []Route{{Method: "GET", Pattern: "/trace-probe", Handler: handler}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	host, err := NewLocal(ctx, config)
	if err != nil {
		_ = listener.Close()
		cancel()
		t.Fatal("local host construction failed")
	}
	done := make(chan error, 1)
	go func() { done <- host.Serve(ctx, listener) }()
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(origin + "/trace-probe")
	if err != nil {
		cancel()
		_ = host.Close()
		t.Fatal("host request failed")
	}
	if response.Body.Close() != nil || response.StatusCode != 202 {
		t.Error("host response invalid")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Error("host stop failed")
		}
	case <-time.After(5 * time.Second):
		_ = listener.Close()
		t.Fatal("host stop exceeded bound")
	}
	if host.Close() != nil {
		t.Fatal("host close failed")
	}
	// Reuse after Host.Close proves the caller still owns a live tracer.
	independent, err := app.New(app.Options{Tracing: tracer, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if independent.RegisterBusinessRoute("GET", "/trace-probe", handler) != nil {
		t.Fatal("independent route failed")
	}
	recorder := httptest.NewRecorder()
	independent.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/trace-probe", nil))
	if recorder.Code != 202 || observed.Load() != 2 {
		t.Fatal("host closed caller-owned tracer")
	}
}
