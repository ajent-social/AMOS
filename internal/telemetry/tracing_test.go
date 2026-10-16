package telemetry

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type tracingPKI struct {
	ca, clientPEM, clientKey []byte
	server, client           tls.Certificate
	roots                    *x509.CertPool
}

func newTracingPKI(t *testing.T) tracingPKI {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic tracing CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	root, e = x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	roots := x509.NewCertPool()
	roots.AddCert(root)
	leaf := func(serial int64, client bool) (tls.Certificate, []byte, []byte) {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		usage := x509.ExtKeyUsageServerAuth
		if client {
			usage = x509.ExtKeyUsageClientAuth
		}
		c := &x509.Certificate{SerialNumber: big.NewInt(serial), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"collector.example.test"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		d, err := x509.CreateCertificate(rand.Reader, c, root, &k.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		kd, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			t.Fatal(err)
		}
		cp := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: d})
		kp := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kd})
		pair, err := tls.X509KeyPair(cp, kp)
		if err != nil {
			t.Fatal(err)
		}
		pair.Leaf, err = x509.ParseCertificate(d)
		if err != nil {
			t.Fatal(err)
		}
		return pair, cp, kp
	}
	server, _, _ := leaf(2, false)
	client, cp, kp := leaf(3, true)
	return tracingPKI{ca: ca, roots: roots, server: server, client: client, clientPEM: cp, clientKey: kp}
}

type tracingCollectorFixture struct {
	server             *httptest.Server
	pki                tracingPKI
	wires              chan *collector.ExportTraceServiceRequest
	calls, connections atomic.Uint64
	bad                atomic.Bool
}

func newTracingCollector(t *testing.T, respond func(http.ResponseWriter, *http.Request)) *tracingCollectorFixture {
	t.Helper()
	f := &tracingCollectorFixture{pki: newTracingPKI(t), wires: make(chan *collector.ExportTraceServiceRequest, 2048)}
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Method != "POST" || r.URL.RequestURI() != "/v1/traces" || r.ProtoMajor != 1 || r.ProtoMinor != 1 || r.Header.Get("Content-Type") != "application/x-protobuf" || r.Header.Get("Authorization") != "Bearer dummy" || r.Header.Get("Content-Encoding") != "" || r.Header.Get("Accept-Encoding") != "" {
			f.bad.Store(true)
		}
		body, e := io.ReadAll(io.LimitReader(r.Body, 65537))
		if e != nil || len(body) > 65536 {
			f.bad.Store(true)
		}
		msg := &collector.ExportTraceServiceRequest{}
		if proto.Unmarshal(body, msg) != nil {
			f.bad.Store(true)
		}
		select {
		case f.wires <- msg:
		default:
			f.bad.Store(true)
		}
		if respond != nil {
			respond(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	s.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{f.pki.server}, NextProtos: []string{"http/1.1"}}
	s.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			f.connections.Add(1)
		}
	}
	s.StartTLS()
	f.server = s
	t.Cleanup(s.Close)
	return f
}
func (f *tracingCollectorFixture) config() TraceConfig {
	return TraceConfig{Enabled: true, SamplePerMillion: 1000000, RouteTemplates: []string{"/synthetic/*"}, Collector: TraceCollectorConfig{Authority: strings.TrimPrefix(f.server.URL, "https://"), DialIP: "127.0.0.1", CAPEM: bytes.Clone(f.pki.ca), AuthToken: "dummy"}}
}
func newFixtureTracer(t *testing.T, cfg TraceConfig) *Tracer {
	t.Helper()
	tr, e := NewTracer(cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tr.Close(ctx)
	})
	return tr
}
func traceRequest(h http.Handler) {
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "https://application.example.test/synthetic/secret?token=secret", nil))
}
func collectTraceSpans(t *testing.T, f *tracingCollectorFixture) []*tracepb.Span {
	t.Helper()
	var out []*tracepb.Span
	for {
		select {
		case msg := <-f.wires:
			checkTraceWire(t, msg)
			for _, r := range msg.ResourceSpans {
				for _, s := range r.ScopeSpans {
					out = append(out, s.Spans...)
				}
			}
		default:
			if f.bad.Load() {
				t.Fatal("invalid collector protocol")
			}
			return out
		}
	}
}
func checkTraceWire(t *testing.T, msg *collector.ExportTraceServiceRequest) {
	t.Helper()
	if len(msg.ProtoReflect().GetUnknown()) != 0 || len(msg.ResourceSpans) != 1 || proto.Size(msg) > 65536 {
		t.Fatal("invalid envelope")
	}
	r := msg.ResourceSpans[0]
	wantResource := &resourcepb.Resource{Attributes: []*common.KeyValue{traceKV("service.name", "amos")}}
	if !proto.Equal(r.Resource, wantResource) || r.SchemaUrl != "" || len(r.ProtoReflect().GetUnknown()) != 0 || len(r.ScopeSpans) != 1 {
		t.Fatal("resource leak")
	}
	scope := r.ScopeSpans[0]
	if !proto.Equal(scope.Scope, &common.InstrumentationScope{Name: "amos.tracing.v1"}) || scope.SchemaUrl != "" || len(scope.ProtoReflect().GetUnknown()) != 0 || len(scope.Spans) > 16 {
		t.Fatal("scope leak")
	}
	for _, s := range scope.Spans {
		var r traceRecord
		copy(r.id[:], s.TraceId)
		copy(r.span[:], s.SpanId)
		copy(r.parent[:], s.ParentSpanId)
		r.start = s.StartTimeUnixNano
		r.end = s.EndTimeUnixNano
		switch s.Kind {
		case tracepb.Span_SPAN_KIND_SERVER:
			r.kind = trace.SpanKindServer
		case tracepb.Span_SPAN_KIND_CLIENT:
			r.kind = trace.SpanKindClient
		case tracepb.Span_SPAN_KIND_INTERNAL:
			r.kind = trace.SpanKindInternal
		}
		for _, a := range s.Attributes {
			switch a.Key {
			case "amos.outcome":
				r.outcome = a.Value.GetStringValue()
			case "amos.route", "amos.destination":
				r.label = a.Value.GetStringValue()
			}
		}
		want, ok := traceProjection(r)
		if !ok || !proto.Equal(s, want) {
			t.Fatalf("nonfinite span: %v", s.Name)
		}
	}
}

func TestTracingDefaultOffAndStrictConfig(t *testing.T) {
	var nilCtx context.Context
	off, e := NewTracer(TraceConfig{})
	if e != nil || off.sdk != nil || off.queue != nil || off.Stats() != (TraceStats{}) {
		t.Fatal("disabled allocated state")
	}
	if e = off.Close(nilCtx); e != nil {
		t.Fatal(e)
	}
	ctx, end := BeginTransactionTrace(nilCtx)
	if ctx != nil {
		t.Fatal("nil changed")
	}
	end(TransactionTraceCommit)
	f := newTracingCollector(t, nil)
	base := f.config()
	cases := []TraceConfig{{RouteTemplates: []string{"/x"}}}
	add := func(mut func(*TraceConfig)) {
		c := base
		c.RouteTemplates = append([]string(nil), base.RouteTemplates...)
		mut(&c)
		cases = append(cases, c)
	}
	add(func(c *TraceConfig) { c.SamplePerMillion = 1000001 })
	add(func(c *TraceConfig) { c.RouteTemplates = []string{"/x", "/x"} })
	add(func(c *TraceConfig) { c.RouteTemplates = []string{"/x?secret"} })
	add(func(c *TraceConfig) { c.RouteTemplates = make([]string, 33) })
	config := struct{ crlf, oversized string }{"dummy\r\n", strings.Repeat("x", 513)}
	add(func(c *TraceConfig) { c.Collector.AuthToken = config.crlf })
	add(func(c *TraceConfig) { c.Collector.AuthToken = config.oversized })
	add(func(c *TraceConfig) { c.Collector.Authority = "https://secret.example.test:443" })
	add(func(c *TraceConfig) { c.Collector.Authority = "127.000.0.1:443" })
	add(func(c *TraceConfig) { c.Collector.Authority = "EXAMPLE.test:443" })
	add(func(c *TraceConfig) { c.Collector.Authority = "example.test:0443" })
	add(func(c *TraceConfig) { c.Collector.DialIP = "0.0.0.0" })
	add(func(c *TraceConfig) { c.Collector.DialIP = "224.0.0.1" })
	add(func(c *TraceConfig) {
		c.Collector.CAPEM = append(bytes.Clone(c.Collector.CAPEM), []byte("malformed")...)
	})
	add(func(c *TraceConfig) { c.Collector.CAPEM = make([]byte, 65537) })
	add(func(c *TraceConfig) { c.TrustedIngressSPKI = [][]byte{make([]byte, 31)} })
	add(func(c *TraceConfig) { c.TrustedIngressSPKI = [][]byte{make([]byte, 32), make([]byte, 32)} })
	add(func(c *TraceConfig) { c.Outbound = make([]TraceDestinationConfig, 17) })
	for i, c := range cases {
		if tr, err := NewTracer(c); err != ErrTraceConfig || tr != nil {
			t.Fatalf("case %d accepted", i)
		}
	}
	t.Setenv("OTEL_SYNTHETIC", "")
	if _, err := NewTracer(base); err != ErrTraceConfig {
		t.Fatal("empty ambient OTEL accepted")
	}
	if f.calls.Load() != 0 {
		t.Fatal("constructor dialed")
	}
}
func TestTracingFiniteWirePrivateContextGraphAndCopies(t *testing.T) {
	f := newTracingCollector(t, nil)
	cfg := f.config()
	tr := newFixtureTracer(t, cfg)
	cfg.RouteTemplates[0] = "/changed"
	clear(cfg.Collector.CAPEM)
	canary := "synthetic-secret-SQL-password-session-body"
	seen := false
	h := ObserveTracing(tr, func(*http.Request) string { return "/synthetic/*" }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = true
		s := trace.SpanFromContext(r.Context())
		if s.IsRecording() {
			t.Error("writable SDK span escaped")
		}
		s.SetName(canary)
		s.SetAttributes(attribute.String(canary, canary))
		s.RecordError(errors.New(canary))
		s.AddEvent(canary)
		s.AddLink(trace.Link{SpanContext: s.SpanContext(), Attributes: []attribute.KeyValue{attribute.String(canary, canary)}})
		s.SetStatus(codes.Error, canary)
		s.End()
		_, end := BeginTransactionTrace(r.Context())
		end(TransactionTraceCommit)
		_, end = BeginTransactionTrace(r.Context())
		end(TransactionTraceRollback)
		w.WriteHeader(201)
	}))
	traceRequest(h)
	if !seen {
		t.Fatal("business not called")
	}
	if e := tr.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	spans := collectTraceSpans(t, f)
	if len(spans) != 3 {
		t.Fatalf("spans=%d stats=%+v", len(spans), tr.Stats())
	}
	var server *tracepb.Span
	for _, s := range spans {
		if s.Name == "http.server" {
			server = s
		}
	}
	if server == nil {
		t.Fatal("missing server")
	}
	for _, s := range spans {
		if !bytes.Equal(s.TraceId, server.TraceId) {
			t.Fatal("trace mismatch")
		}
		if s != server && !bytes.Equal(s.ParentSpanId, server.SpanId) {
			t.Fatal("wrong parent")
		}
		b, _ := proto.Marshal(s)
		if bytes.Contains(b, []byte(canary)) || bytes.Contains(b, []byte(base64.StdEncoding.EncodeToString([]byte(canary)))) {
			t.Fatal("privacy canary")
		}
	}
	if tr.Stats().Active != 0 || tr.Stats().Queued != 0 || tr.Stats().Exported != 3 {
		t.Fatalf("stats %+v", tr.Stats())
	}
	before := f.connections.Load()
	time.Sleep(140 * time.Millisecond)
	if f.connections.Load() != before {
		t.Fatal("idle reconnect")
	}
}

func TestTracingSamplingInheritanceAndPublicForgery(t *testing.T) {
	for _, ratio := range []uint32{0, 300000, 1000000} {
		t.Run(fmt.Sprint(ratio), func(t *testing.T) {
			f := newTracingCollector(t, nil)
			cfg := f.config()
			cfg.SamplePerMillion = ratio
			tr := newFixtureTracer(t, cfg)
			var sampled, unsampled int
			foreign := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
			h := ObserveTracing(tr, nil, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				parent := trace.SpanContextFromContext(r.Context())
				child, end := BeginTransactionTrace(r.Context())
				sc := trace.SpanContextFromContext(child)
				if sc.IsSampled() != parent.IsSampled() || sc.TraceID() != parent.TraceID() {
					t.Error("child did not inherit")
				}
				if sc.IsSampled() {
					sampled++
				} else {
					unsampled++
				}
				end(TransactionTraceCommit)
				if parent.TraceID() == foreign.TraceID() {
					t.Error("caller authority promoted")
				}
			}))
			for range 48 {
				r := httptest.NewRequest("GET", "https://application.example.test/", nil).WithContext(trace.ContextWithSpanContext(context.Background(), foreign))
				r.Header.Set("Traceparent", "00-01000000000000000000000000000000-0200000000000000-01")
				h.ServeHTTP(httptest.NewRecorder(), r)
			}
			_ = tr.Close(context.Background())
			if ratio == 0 && sampled != 0 || ratio == 1000000 && unsampled != 0 || ratio == 300000 && (sampled == 0 || unsampled == 0) {
				t.Fatalf("sampled=%d unsampled=%d", sampled, unsampled)
			}
			spans := collectTraceSpans(t, f)
			if len(spans) != sampled*2 {
				t.Fatalf("records=%d expected=%d", len(spans), sampled*2)
			}
		})
	}
}

func TestTracingPanicCancellationAndOutcomes(t *testing.T) {
	f := newTracingCollector(t, nil)
	tr := newFixtureTracer(t, f.config())
	marker := &struct{ private string }{"synthetic panic secret"}
	h := ObserveTracing(tr, nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(marker) }))
	func() {
		defer func() {
			if recover() != marker {
				t.Error("panic identity changed")
			}
		}()
		traceRequest(h)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h = ObserveTracing(tr, nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "https://application.example.test/", nil).WithContext(ctx))
	_ = tr.Close(context.Background())
	spans := collectTraceSpans(t, f)
	if len(spans) != 2 {
		t.Fatalf("spans %d", len(spans))
	}
	outcomes := map[string]bool{}
	for _, s := range spans {
		for _, a := range s.Attributes {
			if a.Key == "amos.outcome" {
				outcomes[a.Value.GetStringValue()] = true
			}
		}
	}
	if !outcomes["panic"] || !outcomes["cancelled"] {
		t.Fatal(outcomes)
	}
}

func TestTracingEntropyFailureAndUnbiasedBounds(t *testing.T) {
	for _, read := range []func([]byte) (int, error){func([]byte) (int, error) { return 0, errors.New("synthetic secret") }, func(p []byte) (int, error) { clear(p); return len(p), nil }, func(p []byte) (int, error) {
		for i := range p {
			p[i] = 255
		}
		return len(p), nil
	}} {
		_, _, _, e := readTraceEntropy(read, true, 300000)
		if e == nil {
			t.Fatal("entropy failure accepted")
		}
	}
	for range 100 {
		tid, sid, _, e := traceEntropy(true, 500000)
		if e != nil || !tid.IsValid() || !sid.IsValid() {
			t.Fatal("cryptographic generation failed")
		}
	}
	var counter atomic.Uint64
	counter.Store(math.MaxUint64 - 1)
	traceAdd(&counter, 3)
	if counter.Load() != math.MaxUint64 {
		t.Fatal("counter wrapped")
	}
	if sc, _, _, _ := traceEntropy(false, 0); sc.IsValid() {
		t.Fatal("child generated trace ID")
	}
}

func TestTracingPrivateSDKLimitsAndPoisonedProjection(t *testing.T) {
	f := newTracingCollector(t, nil)
	tr := newFixtureTracer(t, f.config())
	tid, sid, _, _ := traceEntropy(true, 1000000)
	ticket := &traceTicket{traceID: tid, spanID: sid, sampled: true, valid: true}
	ctx := context.WithValue(context.Background(), traceTicketKey{}, ticket)
	_, span := tr.sdk.Start(ctx, "http.server", trace.WithSpanKind(trace.SpanKindServer))
	span.SetAttributes(attribute.String("amos.route", "r00"), attribute.String("amos.outcome", "ok"))
	span.SetAttributes(attribute.String("private-extra", "private-extra"))
	span.AddEvent("private-event")
	span.AddLink(trace.Link{SpanContext: span.SpanContext()})
	ro := span.(sdktrace.ReadOnlySpan)
	if len(ro.Attributes()) != 2 || len(ro.Events()) != 0 || len(ro.Links()) != 0 {
		t.Fatal("SDK raw limits not enforced")
	}
	span.SetStatus(codes.Error, "private status")
	span.End()
	// Unknown names/labels/outcomes must fail the finite reconstruction.
	for _, r := range []traceRecord{{id: tid, span: sid, start: 1, end: 2, kind: trace.SpanKindServer, label: "secret", outcome: "ok"}, {id: tid, span: sid, start: 1, end: 2, kind: trace.SpanKindClient, label: "o16", outcome: "ok"}, {id: tid, span: sid, start: 1, end: 2, kind: trace.SpanKindInternal, outcome: "private-error"}} {
		if _, ok := traceProjection(r); ok {
			t.Fatal("poisoned record accepted")
		}
	}
	_ = resource.NewSchemaless(attribute.String("host.name", "private-host")) // never used by the wire projection
	_ = tr.Close(context.Background())
	spans := collectTraceSpans(t, f)
	if len(spans) != 1 || spans[0].Status != nil {
		t.Fatal("SDK status escaped allowlisted outcome projection")
	}
}

func TestTracingCollectorRejectsAndNeverRetries(t *testing.T) {
	cases := []struct {
		name    string
		respond func(http.ResponseWriter, *http.Request)
	}{
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", r.URL.String())
			w.WriteHeader(307)
		}},
		{"overload", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }},
		{"unauthorized", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) }},
		{"ambiguous", func(w http.ResponseWriter, _ *http.Request) {
			c, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				_ = c.Close()
			}
		}},
		{"wrong-type", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(200)
		}},
		{"params", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-protobuf; unsupported=value")
			w.WriteHeader(200)
		}},
		{"compression", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.Header().Set("Content-Encoding", "gzip")
			w.WriteHeader(200)
		}},
		{"malformed", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-protobuf")
			_, _ = w.Write([]byte{255})
		}},
		{"unknown", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-protobuf")
			_, _ = w.Write([]byte{0x10, 0x01})
		}},
		{"partial", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-protobuf")
			b, _ := proto.Marshal(&collector.ExportTraceServiceResponse{PartialSuccess: &collector.ExportTracePartialSuccess{}})
			_, _ = w.Write(b)
		}},
		{"large-body", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-protobuf")
			_, _ = w.Write(make([]byte, 65537))
		}},
		{"large-header", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.Header().Set("X-Synthetic", strings.Repeat("x", 9000))
			w.WriteHeader(200)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newTracingCollector(t, c.respond)
			tr := newFixtureTracer(t, f.config())
			traceRequest(ObserveTracing(tr, nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
			_ = tr.Close(context.Background())
			stats := tr.Stats()
			if stats.Failed != 1 || stats.Dropped != 1 || stats.Exported != 0 || f.calls.Load() != 1 || f.connections.Load() != 1 {
				t.Fatalf("retry/acceptance %+v calls=%d conns=%d", stats, f.calls.Load(), f.connections.Load())
			}
			collectTraceSpans(t, f)
			before := f.connections.Load()
			time.Sleep(30 * time.Millisecond)
			if f.connections.Load() != before {
				t.Fatal("idle reconnect")
			}
		})
	}
}

func TestTracingBoundedOverloadNonblockingAndClose(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	entered := make(chan struct{}, 1)
	f := newTracingCollector(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	})
	tr := newFixtureTracer(t, f.config())
	h := ObserveTracing(tr, nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for range 16 {
		traceRequest(h)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("collector not entered")
	}
	done := make(chan struct{})
	started := time.Now()
	go func() {
		for range 1200 {
			traceRequest(h)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1500 * time.Millisecond):
		releaseOnce.Do(func() { close(release) })
		t.Fatal("overload backpressured request completion")
	}
	if time.Since(started) > 1500*time.Millisecond {
		t.Fatal("request latency exceeded fixture bound")
	}
	s := tr.Stats()
	if s.Queued > 256 || s.Active > 256 || s.Dropped == 0 {
		t.Fatalf("bounds %+v", s)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started = time.Now()
	if e := tr.Close(ctx); e == nil {
		t.Fatal("cancelled close success")
	}
	if time.Since(started) > time.Second {
		t.Fatal("close exceeded bound")
	}
	releaseOnce.Do(func() { close(release) })
	if tr.Stats().Queued != 0 || tr.Stats().Active != 0 {
		t.Fatal(tr.Stats())
	}
	before := f.connections.Load()
	traceRequest(h)
	time.Sleep(30 * time.Millisecond)
	if f.connections.Load() != before {
		t.Fatal("late restart")
	}
}

func TestTracingActiveAdmissionAndConcurrentClose(t *testing.T) {
	f := newTracingCollector(t, nil)
	tr := newFixtureTracer(t, f.config())
	entered := make(chan struct{}, 300)
	release := make(chan struct{})
	h := ObserveTracing(tr, nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { entered <- struct{}{}; <-release }))
	var wg sync.WaitGroup
	for range 300 {
		wg.Go(func() { traceRequest(h) })
	}
	for range 300 {
		<-entered
	}
	if s := tr.Stats(); s.Active != 256 || s.Dropped != 44 {
		t.Fatalf("admission %+v", s)
	}
	var closers sync.WaitGroup
	for range 8 {
		closers.Go(func() { _ = tr.Close(context.Background()) })
	}
	closers.Wait()
	close(release)
	wg.Wait()
	if s := tr.Stats(); s.Active != 0 || s.Queued != 0 || s.Dropped < 300 {
		t.Fatalf("late ends %+v", s)
	}
}

func newTracingBackend(t *testing.T, pki tracingPKI, h http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(h)
	s.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pki.roots, Certificates: []tls.Certificate{pki.server}, NextProtos: []string{"http/1.1"}}
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}
func tracingDestination(s *httptest.Server, pki tracingPKI) TraceDestinationConfig {
	return TraceDestinationConfig{Authority: strings.TrimPrefix(s.URL, "https://"), DialIP: "127.0.0.1", CAPEM: bytes.Clone(pki.ca), ClientCertificatePEM: bytes.Clone(pki.clientPEM), ClientPrivateKeyPEM: bytes.Clone(pki.clientKey)}
}
func TestTracingActualMTLSOutboundAndContinuation(t *testing.T) {
	f := newTracingCollector(t, nil)
	cfg := f.config()
	pki := newTracingPKI(t)
	digest := sha256.Sum256(pki.client.Leaf.RawSubjectPublicKeyInfo)
	cfg.TrustedIngressSPKI = [][]byte{bytes.Clone(digest[:])}
	var backend http.Handler
	var calls atomic.Uint64
	server := newTracingBackend(t, pki, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); backend.ServeHTTP(w, r) }))
	cfg.Outbound = []TraceDestinationConfig{tracingDestination(server, pki)}
	tr := newFixtureTracer(t, cfg)
	backend = ObserveTracing(tr, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.TLS.Version != tls.VersionTLS13 || len(r.TLS.VerifiedChains) == 0 {
			t.Error("not verified mTLS")
		}
		if r.Header.Get("Tracestate") != "" || r.Header.Get("Baggage") != "" {
			t.Error("raw propagation escaped")
		}
		w.WriteHeader(202)
	}))
	transport, e := tr.Transport(0)
	if e != nil {
		t.Fatal(e)
	}
	same, e := tr.Transport(0)
	if e != nil || same != transport {
		t.Fatal("transport not cached")
	}
	if _, e = tr.Transport(1); e == nil {
		t.Fatal("unlisted index")
	}
	frontend := ObserveTracing(tr, nil, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		req, e := http.NewRequestWithContext(r.Context(), "GET", server.URL+"/secret?private=query", nil)
		if e != nil {
			t.Fatal(e)
		}
		req.Header["traceparent"] = []string{"raw-secret"}
		req.Header["TRACEPARENT"] = []string{"other-secret"}
		req.Header.Set("Tracestate", "private-state")
		req.Header.Set("Baggage", "private-baggage")
		req.Header.Set("Authorization", "synthetic-business-secret")
		original := req.Header.Clone()
		resp, e := transport.RoundTrip(req)
		if e != nil {
			t.Error(e)
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if !reflect.DeepEqual(req.Header, original) {
			t.Error("caller headers mutated")
		}
	}))
	traceRequest(frontend)
	_ = tr.Close(context.Background())
	spans := collectTraceSpans(t, f)
	if len(spans) != 3 || calls.Load() != 1 {
		t.Fatalf("spans=%d calls=%d stats=%+v", len(spans), calls.Load(), tr.Stats())
	}
	var client, outer, inner *tracepb.Span
	for _, s := range spans {
		if s.Name == "http.client" {
			client = s
		} else if len(s.ParentSpanId) == 0 {
			outer = s
		} else {
			inner = s
		}
	}
	if client == nil || outer == nil || inner == nil || !bytes.Equal(client.ParentSpanId, outer.SpanId) || !bytes.Equal(inner.ParentSpanId, client.SpanId) || !bytes.Equal(inner.TraceId, outer.TraceId) {
		t.Fatal("wrong actual mTLS graph")
	}
	if _, e = transport.RoundTrip(httptest.NewRequest("GET", server.URL, nil)); e != ErrTraceClosed {
		t.Fatal("closed transport accepted")
	}
}
func TestTracingTrustedNarrowingAndMalformedPropagation(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		ratio     uint32
		allow     bool
		headers   []string
		sample    bool
		continued bool
	}{
		{"trusted", 1000000, true, []string{"00-01000000000000000000000000000000-0200000000000000-01"}, true, true},
		{"narrow", 1000000, true, []string{"00-01000000000000000000000000000000-0200000000000000-00"}, false, true},
		{"cannot-escalate", 0, true, []string{"00-01000000000000000000000000000000-0200000000000000-01"}, false, true},
		{"unlisted-peer", 1000000, false, []string{"00-01000000000000000000000000000000-0200000000000000-01"}, true, false},
		{"duplicate", 1000000, true, []string{"00-01000000000000000000000000000000-0200000000000000-01", "00-01000000000000000000000000000000-0200000000000000-01"}, true, false},
		{"future", 1000000, true, []string{"01-01000000000000000000000000000000-0200000000000000-01"}, true, false},
		{"unknown-flags", 1000000, true, []string{"00-01000000000000000000000000000000-0200000000000000-03"}, true, false},
		{"uppercase", 1000000, true, []string{"00-AA000000000000000000000000000000-0200000000000000-01"}, true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newTracingCollector(t, nil)
			cfg := f.config()
			cfg.SamplePerMillion = scenario.ratio
			pki := newTracingPKI(t)
			if scenario.allow {
				hash := sha256.Sum256(pki.client.Leaf.RawSubjectPublicKeyInfo)
				cfg.TrustedIngressSPKI = [][]byte{hash[:]}
			}
			tr := newFixtureTracer(t, cfg)
			h := ObserveTracing(tr, nil, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				sc := trace.SpanContextFromContext(r.Context())
				if sc.IsSampled() != scenario.sample {
					t.Error("incoming flags escalated/local decision lost")
				}
				continued := sc.TraceID() == (trace.TraceID{1})
				if continued != scenario.continued {
					t.Error("continuation trust failed")
				}
				child, end := BeginTransactionTrace(r.Context())
				if trace.SpanContextFromContext(child).IsSampled() != scenario.sample {
					t.Error("child resampled")
				}
				end(TransactionTraceCommit)
			}))
			server := newTracingBackend(t, pki, h)
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pki.roots, Certificates: []tls.Certificate{pki.client}}}, Timeout: time.Second}
			defer client.CloseIdleConnections()
			req, _ := http.NewRequest("GET", server.URL, nil)
			req.Header["Traceparent"] = scenario.headers
			resp, e := client.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			_ = resp.Body.Close()
			_ = tr.Close(context.Background())
			spans := collectTraceSpans(t, f)
			if scenario.sample && len(spans) != 2 || !scenario.sample && len(spans) != 0 {
				t.Fatalf("records=%d stats=%+v", len(spans), tr.Stats())
			}
		})
	}
}
func TestTracingOutboundDenialCancellationTimeoutAndNoOwner(t *testing.T) {
	f := newTracingCollector(t, nil)
	pki := newTracingPKI(t)
	var calls atomic.Uint64
	release := make(chan struct{})
	defer close(release)
	server := newTracingBackend(t, pki, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Baggage") != "" || r.Header.Get("Traceparent") != "" {
			t.Error("unowned propagation leaked")
		}
		if r.URL.Path == "/slow" {
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		w.WriteHeader(200)
	}))
	cfg := f.config()
	cfg.Outbound = []TraceDestinationConfig{tracingDestination(server, pki)}
	tr := newFixtureTracer(t, cfg)
	transport, _ := tr.Transport(0)
	clear(cfg.Outbound[0].CAPEM)
	clear(cfg.Outbound[0].ClientPrivateKeyPEM)
	for _, url := range []string{"http://" + transport.endpoint.authority, "https://other.example.test:443", "https://user:secret@" + transport.endpoint.authority, "https://" + transport.endpoint.authority + "/#fragment"} {
		req, _ := http.NewRequest("GET", url, nil)
		if _, e := transport.RoundTrip(req); e == nil {
			t.Fatal("invalid destination accepted")
		}
	}
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Host = "other.example.test"
	if _, e := transport.RoundTrip(req); e == nil || calls.Load() != 0 {
		t.Fatal("host mismatch dialed")
	}
	req, _ = http.NewRequest("GET", server.URL, nil)
	req.Header.Set("Traceparent", "raw-secret")
	req.Header.Set("Baggage", "raw-secret")
	resp, e := transport.RoundTrip(req)
	if e != nil {
		t.Fatal(e)
	}
	_ = resp.Body.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ = http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if _, e = transport.RoundTrip(req); e == nil {
		t.Fatal("cancel ignored")
	}
	started := time.Now()
	req, _ = http.NewRequest("GET", server.URL+"/slow", nil)
	if _, e = transport.RoundTrip(req); e == nil || time.Since(started) > 3*time.Second {
		t.Fatal("request timeout not bounded")
	}
	_ = tr.Close(context.Background())
	if tr.Stats().Attempted != 0 {
		t.Fatal("unowned transport recorded")
	}
}
func TestTracingWrongTrustAndAuthorityFailClosed(t *testing.T) {
	for _, wrong := range []string{"CA", "authority", "token"} {
		t.Run(wrong, func(t *testing.T) {
			f := newTracingCollector(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
			cfg := f.config()
			switch wrong {
			case "CA":
				cfg.Collector.CAPEM = newTracingPKI(t).ca
			case "authority":
				_, port, _ := net.SplitHostPort(cfg.Collector.Authority)
				cfg.Collector.Authority = "wrong.example.test:" + port
			case "token":
				cfg.Collector.AuthToken = "test"
			}
			tr := newFixtureTracer(t, cfg)
			traceRequest(ObserveTracing(tr, nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
			_ = tr.Close(context.Background())
			if tr.Stats().Exported != 0 || tr.Stats().Failed != 1 {
				t.Fatal("wrong trust accepted")
			}
			if wrong != "token" && f.calls.Load() != 0 {
				t.Fatal("TLS failed to protect collector")
			}
		})
	}
}

func TestTracingAbandonedResponseEndsAtOwnedDeadline(t *testing.T) {
	f := newTracingCollector(t, nil)
	pki := newTracingPKI(t)
	server := newTracingBackend(t, pki, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	cfg := f.config()
	cfg.Outbound = []TraceDestinationConfig{tracingDestination(server, pki)}
	tr := newFixtureTracer(t, cfg)
	transport, _ := tr.Transport(0)
	var abandoned io.ReadCloser
	h := ObserveTracing(tr, nil, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequestWithContext(r.Context(), "GET", server.URL, nil)
		req.Header = nil
		resp, e := transport.RoundTrip(req)
		if e != nil {
			t.Error(e)
			return
		}
		abandoned = resp.Body
		if req.Header != nil {
			t.Error("nil caller Header mutated")
		}
	}))
	started := time.Now()
	traceRequest(h)
	if abandoned == nil {
		t.Fatal("response missing")
	}
	defer func() { _ = abandoned.Close() }()
	if tr.Stats().Active != 1 {
		t.Fatalf("expected unclosed client span %+v", tr.Stats())
	}
	deadline := time.Now().Add(3 * time.Second)
	for tr.Stats().Active != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if tr.Stats().Active != 0 || time.Since(started) > 3*time.Second {
		t.Fatal("abandoned body retained recording admission")
	}
	_ = tr.Close(context.Background())
	spans := collectTraceSpans(t, f)
	if len(spans) != 2 {
		t.Fatalf("spans=%d", len(spans))
	}
	found := false
	for _, s := range spans {
		if s.Name == "http.client" {
			for _, a := range s.Attributes {
				if a.Key == "amos.outcome" && a.Value.GetStringValue() == "cancelled" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("timeout did not end cancelled client span")
	}
}

func TestTracingFailureMasksCallerSpanAndOldScope(t *testing.T) {
	for _, failure := range []string{"entropy", "capacity", "closed"} {
		t.Run(failure, func(t *testing.T) {
			f := newTracingCollector(t, nil)
			tr := newFixtureTracer(t, f.config())
			type authKey struct{}
			base := context.WithValue(context.Background(), authKey{}, "synthetic-auth-state")
			parent, _, parentEnd := tr.startTrace(base, "http.server", trace.SpanKindServer, trace.SpanContext{}, nil, attribute.String("amos.route", "r00"))
			defer parentEnd("ok")
			other := sdktrace.NewTracerProvider()
			_, writable := other.Tracer("synthetic-external").Start(context.Background(), "private-caller-span")
			defer func() { writable.End(); _ = other.Shutdown(context.Background()) }()
			ctx := trace.ContextWithSpan(parent, writable)
			var ends []func(string)
			switch failure {
			case "entropy":
				tr.entropy = func(bool, uint32) (trace.TraceID, trace.SpanID, bool, error) {
					return trace.TraceID{}, trace.SpanID{}, false, errors.New("synthetic entropy failure")
				}
			case "capacity":
				for range 255 {
					_, _, end := tr.startTrace(base, "http.server", trace.SpanKindServer, trace.SpanContext{}, nil, attribute.String("amos.route", "r00"))
					ends = append(ends, end)
				}
			case "closed":
				_ = tr.Close(context.Background())
			}
			called := false
			h := ObserveTracing(tr, nil, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				called = true
				c := r.Context()
				if trace.SpanFromContext(c).IsRecording() || trace.SpanContextFromContext(c).IsValid() || ownedTraceScope(c) != nil {
					t.Error("failed ingress exposed recording authority")
				}
				if c.Value(authKey{}) != "synthetic-auth-state" || c.Done() != ctx.Done() {
					t.Error("business context altered")
				}
				child, end := BeginTransactionTrace(c)
				if child != c {
					t.Error("inert context started child")
				}
				end(TransactionTraceCommit)
			}))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "https://application.example.test/", nil).WithContext(ctx))
			if !called {
				t.Fatal("business skipped")
			}
			_ = tr.Close(context.Background())
			for _, end := range ends {
				end("ok")
			}
		})
	}
}
func TestTracingUnicastAndDestinationIdentityValidation(t *testing.T) {
	f := newTracingCollector(t, nil)
	cfg := f.config()
	for _, ip := range []string{"255.255.255.255", "::", netip.AddrFrom16([16]byte{0xff, 0x02, 15: 1}).String(), "::ffff:127.0.0.1", netip.AddrFrom16([16]byte{0xfe, 0x80, 15: 1}).WithZone("synthetic").String()} {
		bad := cfg
		bad.Collector.DialIP = ip
		if _, e := NewTracer(bad); e != ErrTraceConfig {
			t.Fatal("noncanonical/nonunicast dial accepted")
		}
	}
	pki := newTracingPKI(t)
	server := newTracingBackend(t, pki, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	other := newTracingPKI(t)
	cfg.Outbound = []TraceDestinationConfig{tracingDestination(server, pki)}
	for _, key := range []bool{false, true} {
		bad := cfg
		bad.Outbound = append([]TraceDestinationConfig(nil), cfg.Outbound...)
		if key {
			bad.Outbound[0].ClientPrivateKeyPEM = append(bytes.Clone(pki.clientKey), []byte("malformed")...)
		} else {
			bad.Outbound[0].ClientCertificatePEM = append([]byte("malformed"), pki.clientPEM...)
		}
		if _, e := NewTracer(bad); e != ErrTraceConfig {
			t.Fatal("malformed PEM material accepted")
		}
	}
	cfg.Outbound[0].ClientCertificatePEM = other.clientPEM
	cfg.Outbound[0].ClientPrivateKeyPEM = other.clientKey
	tr := newFixtureTracer(t, cfg)
	transport, _ := tr.Transport(0)
	req, _ := http.NewRequest("GET", server.URL, nil)
	if resp, e := transport.RoundTrip(req); e == nil {
		_ = resp.Body.Close()
		t.Fatal("foreign client identity accepted")
	}
	_ = tr.Close(context.Background())
	serverDER := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pki.server.Certificate[0]})
	keyDER, e := x509.MarshalPKCS8PrivateKey(pki.server.PrivateKey)
	if e != nil {
		t.Fatal(e)
	}
	cfg.Outbound[0].ClientCertificatePEM = serverDER
	cfg.Outbound[0].ClientPrivateKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if _, e = NewTracer(cfg); e != ErrTraceConfig {
		t.Fatal("server-only client certificate accepted")
	}
}

func TestTracingPoisonedSDKResourceScopeEventsAndStatus(t *testing.T) {
	f := newTracingCollector(t, nil)
	tr := newFixtureTracer(t, f.config())
	canary := "synthetic-private-SQL-session-credential"
	provider := sdktrace.NewTracerProvider(sdktrace.WithResource(resource.NewSchemaless(attribute.String("host.name", canary))), sdktrace.WithSpanProcessor(traceProcessor{tr}))
	_, s := provider.Tracer(canary, trace.WithInstrumentationVersion(canary), trace.WithSchemaURL("https://private.example.test/schema")).Start(context.Background(), "http.server", trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("amos.route", "r00"), attribute.String("amos.outcome", "ok")))
	s.AddEvent(canary, trace.WithAttributes(attribute.String("private", canary)))
	s.AddLink(trace.Link{SpanContext: s.SpanContext(), Attributes: []attribute.KeyValue{attribute.String("private", canary)}})
	s.SetStatus(codes.Error, canary)
	s.End()
	_ = provider.Shutdown(context.Background())
	_ = tr.Close(context.Background())
	spans := collectTraceSpans(t, f)
	if len(spans) != 1 {
		t.Fatalf("projection lost valid finite observation %+v", tr.Stats())
	}
	b, _ := proto.Marshal(spans[0])
	if bytes.Contains(b, []byte(canary)) {
		t.Fatal("poisoned SDK metadata exported")
	}
}
func TestTracingCachedDestinationConnectionLimit(t *testing.T) {
	f := newTracingCollector(t, nil)
	pki := newTracingPKI(t)
	var active, maxActive atomic.Int64
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	server := newTracingBackend(t, pki, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		for {
			old := maxActive.Load()
			if n <= old || maxActive.CompareAndSwap(old, n) {
				break
			}
		}
		defer active.Add(-1)
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(200)
	}))
	cfg := f.config()
	cfg.Outbound = []TraceDestinationConfig{tracingDestination(server, pki)}
	tr := newFixtureTracer(t, cfg)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			transport, e := tr.Transport(0)
			if e != nil {
				t.Error(e)
				return
			}
			req, _ := http.NewRequest("GET", server.URL, nil)
			resp, e := transport.RoundTrip(req)
			if e != nil {
				t.Error(e)
				return
			}
			_ = resp.Body.Close()
		})
	}
	for range 4 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("first four connections unavailable")
		}
	}
	select {
	case <-entered:
		t.Error("destination exceeded shared connection cap")
	case <-time.After(50 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	wg.Wait()
	if maxActive.Load() > 4 {
		t.Fatal("connection cap exceeded")
	}
}

func TestTracingStatusWriterPreservesHeadersAndCapabilities(t *testing.T) {
	f := newTracingCollector(t, nil)
	tr := newFixtureTracer(t, f.config())
	h := ObserveTracing(tr, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "synthetic-existing-id")
		if _, ok := w.(http.Hijacker); ok {
			t.Error("unsupported hijack advertised")
		}
		if _, ok := w.(http.Pusher); ok {
			t.Error("unsupported push advertised")
		}
		if e := http.NewResponseController(w).Flush(); e != nil {
			t.Error(e)
		}
		_, _ = w.Write([]byte("synthetic-private-response"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "https://application.example.test/", nil))
	if rec.Header().Get("X-Request-ID") != "synthetic-existing-id" || rec.Body.String() != "synthetic-private-response" {
		t.Fatal("tracing changed response")
	}
	_ = tr.Close(context.Background())
	collectTraceSpans(t, f)
}
