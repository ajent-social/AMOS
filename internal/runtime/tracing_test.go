package runtime

import (
	"bytes"
	"context"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/telemetry"
	"github.com/ajent-social/amos/observability"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestTracingBothRuntimeDispatchPaths(t *testing.T) {
	for _, outer := range []bool{false, true} {
		name := "owned_logger"
		if outer {
			name = "outer_logger"
		}
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			count := 0
			collectorServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				var request collector.ExportTraceServiceRequest
				if err != nil || r.Header.Get("Authorization") != "Bearer dummy" || proto.Unmarshal(body, &request) != nil {
					t.Error("invalid actual collector request")
					w.WriteHeader(400)
					return
				}
				mu.Lock()
				for _, resource := range request.ResourceSpans {
					for _, scope := range resource.ScopeSpans {
						for _, span := range scope.Spans {
							if span.Name != "http.server" {
								t.Error("unexpected runtime wire span")
							}
							count++
						}
					}
				}
				mu.Unlock()
				w.Header().Set("Content-Type", "application/x-protobuf")
			}))
			defer collectorServer.Close()
			authority, err := url.Parse(collectorServer.URL)
			if err != nil {
				t.Fatal(err)
			}
			roots := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: collectorServer.Certificate().Raw})
			tracer, err := observability.NewTracer(observability.TraceConfig{Enabled: true, SamplePerMillion: 1000000, RouteTemplates: []string{"/probe"}, Collector: observability.TraceCollectorConfig{Authority: authority.Host, DialIP: "127.0.0.1", CAPEM: roots, AuthToken: "dummy"}})
			if err != nil {
				t.Fatal("tracer unavailable")
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = tracer.Close(ctx)
			}()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			application, err := New(Options{Tracing: tracer, Logger: logger, ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if application.RegisterBusinessRoute("GET", "/probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })) != nil {
				t.Fatal("route unavailable")
			}
			handler := application.Handler()
			if outer {
				handler = telemetry.Logging(logger, application.routeTemplate, handler)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/probe", nil))
			if recorder.Code != 202 || bytes.Count(logs.Bytes(), []byte("\n")) != 1 {
				t.Fatal("tracing changed status or duplicate logger records")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if tracer.Close(ctx) != nil {
				t.Fatal("tracer close failed")
			}
			mu.Lock()
			defer mu.Unlock()
			if count != 1 {
				t.Fatalf("runtime dispatch emitted %d server spans want1", count)
			}
		})
	}
}
