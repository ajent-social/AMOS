package telemetryconsumer_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajent-social/amos/app"
	"github.com/ajent-social/amos/observability"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func TestTracingPublicComposition(t *testing.T) {
	for name, open := range map[string]func(*testing.T) tracingDatabase{"development": plainTracingDatabase, "runtime_tls_restricted": runtimeTracingDatabase} {
		t.Run(name, func(t *testing.T) { exerciseTracingComposition(t, open(t)) })
	}
}
func exerciseTracingComposition(t *testing.T, database tracingDatabase) {
	t.Helper()
	ca := newTracingCA(t)
	collectorPair, _, _, _ := ca.identity(t, "collector.test", false)
	backendPair, _, _, _ := ca.identity(t, "backend.test", false)
	_, clientCert, clientKey, clientSPKI := ca.identity(t, "synthetic-client.test", true)
	sink := &tracingCollector{}
	collectorAddress, _ := serveTracingTLS(t, collectorPair, nil, sink)
	var backend atomic.Pointer[app.App]
	backendAddress, backendServer := serveTracingTLS(t, backendPair, ca.pem, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		application := backend.Load()
		if application == nil {
			http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
			return
		}
		application.Handler().ServeHTTP(w, r)
	}))
	config := observability.TraceConfig{Enabled: true, SamplePerMillion: 1000000, RouteTemplates: []string{"/work", "/private", "/rollback", "/panic", "/cancel", "/begin", "/outbound-failure", "/noop", "/callback-commit"}, Collector: observability.TraceCollectorConfig{Authority: tracingAuthority(collectorAddress, "collector.test"), DialIP: "127.0.0.1", CAPEM: ca.pem, AuthToken: "dummy"}, Outbound: []observability.TraceDestinationConfig{{Authority: tracingAuthority(backendAddress, "backend.test"), DialIP: "127.0.0.1", CAPEM: ca.pem, ClientCertificatePEM: clientCert, ClientPrivateKeyPEM: clientKey}}, TrustedIngressSPKI: [][]byte{clientSPKI}}
	tracer, err := observability.NewTracer(config)
	if err != nil {
		t.Fatal("public tracing constructor failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if tracer.Close(ctx) != nil {
			t.Error("tracer close failed")
		}
	})
	transport, err := tracer.Transport(0)
	if err != nil {
		t.Fatal("owned transport unavailable")
	}
	outbound := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	backendApp, err := app.New(app.Options{Tracing: tracer, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	backend.Store(backendApp)
	if backendApp.RegisterBusinessRoute("GET", "/private", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sc := trace.SpanContextFromContext(r.Context())
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || !sc.IsValid() || !sc.IsSampled() {
			http.Error(w, "verified continuity absent", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(204)
	})) != nil {
		t.Fatal("backend route unavailable")
	}
	logs := &logSink{}
	frontend, err := app.New(app.Options{Tracing: tracer, Logger: slog.New(slog.NewJSONHandler(logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	marker := errors.New(tracingCanary)
	panicMarker := &struct{ value string }{tracingCanary}
	var observedPanic atomic.Bool
	id := uuid.NewString()
	rollbackID := uuid.NewString()
	callbackCommitID := uuid.NewString()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = database.runner.WithTx(ctx, nil, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "DELETE FROM "+database.table+" WHERE id IN ($1,$2,$3)", id, rollbackID, callbackCommitID)
			return err
		})
	})
	runOutbound := func(w http.ResponseWriter, r *http.Request) {
		forged := trace.ContextWithSpanContext(r.Context(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled, Remote: true}))
		req, err := http.NewRequestWithContext(forged, "GET", "https://"+config.Outbound[0].Authority+"/private?private="+tracingCanary, nil)
		if err != nil {
			http.Error(w, "fixture request unavailable", http.StatusInternalServerError)
			return
		}
		req.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
		req.Header.Set("baggage", "private="+tracingCanary)
		req.Header.Set("tracestate", tracingCanary)
		req.Header.Set("Authorization", tracingCanary)
		response, err := outbound.Do(req)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != 204 {
			w.WriteHeader(502)
			return
		}
		w.WriteHeader(200)
	}
	register := func(path string, handler http.HandlerFunc) {
		t.Helper()
		if frontend.RegisterBusinessRoute("GET", path, handler) != nil {
			t.Fatal("frontend route unavailable")
		}
	}
	register("/work", func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if span.IsRecording() {
			t.Error("writable SDK span escaped to business context")
		}
		span.SetName(tracingCanary)
		span.SetAttributes(attribute.String("credential", tracingCanary))
		span.AddEvent(tracingCanary, trace.WithAttributes(attribute.String("payload", tracingCanary)))
		span.RecordError(fmt.Errorf("%s", tracingCanary))
		span.SetStatus(codes.Error, tracingCanary)
		span.AddLink(trace.Link{SpanContext: trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled}), Attributes: []attribute.KeyValue{attribute.String("source", tracingCanary)}})
		err := database.runner.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(r.Context(), "INSERT INTO "+database.table+"(id,value) VALUES($1,$2)", id, tracingCanary)
			return err
		})
		if err != nil {
			w.WriteHeader(500)
			return
		}
		runOutbound(w, r)
	})
	register("/rollback", func(w http.ResponseWriter, r *http.Request) {
		err := database.runner.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(r.Context(), "INSERT INTO "+database.table+"(id,value) VALUES($1,$2)", rollbackID, tracingCanary); err != nil {
				return err
			}
			return marker
		})
		if err != marker {
			t.Error("callback error identity changed")
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	})
	register("/callback-commit", func(w http.ResponseWriter, r *http.Request) {
		err := database.runner.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(r.Context(), "INSERT INTO "+database.table+"(id,value) VALUES($1,$2)", callbackCommitID, tracingCanary); err != nil {
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			return marker
		})
		if err != marker {
			t.Error("already-completed callback result changed")
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	})
	register("/panic", func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				observedPanic.Store(recovered == panicMarker)
				panic(recovered)
			}
		}()
		_ = database.runner.WithTx(r.Context(), nil, func(*sql.Tx) error { panic(panicMarker) })
	})
	register("/cancel", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		timer := time.AfterFunc(25*time.Millisecond, cancel)
		defer timer.Stop()
		err := database.runner.WithTx(ctx, nil, func(tx *sql.Tx) error { _, _ = tx.ExecContext(ctx, "SELECT pg_sleep(2)"); return ctx.Err() })
		if !errors.Is(err, context.Canceled) {
			t.Error("transaction cancellation identity changed")
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	})
	register("/begin", func(w http.ResponseWriter, r *http.Request) {
		called := false
		err := database.runner.WithTx(r.Context(), &sql.TxOptions{Isolation: sql.IsolationLevel(99)}, func(*sql.Tx) error { called = true; return nil })
		if err == nil || called {
			t.Error("failed begin behavior changed")
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	})
	register("/outbound-failure", runOutbound)
	register("/noop", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })
	server := httptest.NewServer(frontend.Handler())
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	request := func(path string, want int) {
		t.Helper()
		req, err := http.NewRequest("GET", server.URL+path+"?private="+tracingCanary, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
		req.Header.Set("baggage", "private="+tracingCanary)
		req.Header.Set("Authorization", tracingCanary)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("real App request failed")
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != want {
			t.Fatalf("App request status=%d want=%d", response.StatusCode, want)
		}
	}
	request("/work", 200)
	spans := sink.spans(t, 4)
	assertTracingGraph(t, spans)
	request("/rollback", 200)
	request("/callback-commit", 200)
	request("/panic", 500)
	if !observedPanic.Load() {
		t.Fatal("original panic identity changed")
	}
	request("/cancel", 200)
	request("/begin", 200)
	if err := backendServer.Close(); err != nil {
		t.Fatal("owned backend close failed")
	}
	transport.CloseIdleConnections()
	request("/outbound-failure", 502)
	spans = sink.spans(t, 16)
	assertTracingPrivacy(t, sink)
	outcomes := map[string]bool{}
	for _, span := range spans {
		if span.Name == "database.transaction" {
			for _, item := range span.Attributes {
				if item.Key == "amos.outcome" {
					outcomes[item.Value.GetStringValue()] = true
				}
			}
		}
	}
	for _, want := range []string{"commit", "rollback", "panic", "cancelled", "unavailable", "error"} {
		if !outcomes[want] {
			t.Fatalf("required actual database outcome %s absent", want)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := database.runner.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var count int
		err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+database.table+" WHERE id=$1", rollbackID).Scan(&count)
		if err != nil {
			return err
		}
		if count != 0 {
			return errors.New("rollback persisted data")
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+database.table+" WHERE id IN ($1,$2) AND value=$3", id, callbackCommitID, tracingCanary).Scan(&count); err != nil {
			return err
		}
		if count != 2 {
			return errors.New("actual commits did not persist")
		}
		return nil
	}); err != nil {
		t.Fatal("actual rollback persistence invalid")
	}
	records(t, logs, 7)
	if tracer.Close(ctx) != nil {
		t.Fatal("caller-owned tracer close failed")
	}
	before := tracer.Stats()
	request("/noop", 202)
	after := tracer.Stats()
	if after.Exported != before.Exported {
		t.Fatal("closed tracer exported application request")
	}
	records(t, logs, 8)
}
func TestTracingNilCompatibility(t *testing.T) {
	logs := &logSink{}
	application, err := app.New(app.Options{Logger: slog.New(slog.NewJSONHandler(logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if application.RegisterBusinessRoute("GET", "/nil", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })) != nil {
		t.Fatal("nil route unavailable")
	}
	request := httptest.NewRequest("GET", "/nil?private="+tracingCanary, nil)
	request.Header.Set("traceparent", tracingCanary)
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != 202 {
		t.Fatal("nil tracer changed application behavior")
	}
	records(t, logs, 1)
	var closed *storage.DB
	marker := errors.New("synthetic callback identity")
	if closed.WithTx(context.Background(), nil, func(*sql.Tx) error { return marker }) != storage.ErrClosed {
		t.Fatal("nil storage result changed")
	}
}
