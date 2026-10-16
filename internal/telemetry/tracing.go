package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var ErrTraceConfig = errors.New("invalid tracing configuration")
var ErrTraceClosed = errors.New("tracing closed")
var errTraceExport = errors.New("trace export unavailable")

type TraceConfig struct {
	Enabled            bool
	SamplePerMillion   uint32
	RouteTemplates     []string
	Collector          TraceCollectorConfig
	Outbound           []TraceDestinationConfig
	TrustedIngressSPKI [][]byte
}
type TraceCollectorConfig struct {
	Authority, DialIP string
	CAPEM             []byte
	AuthToken         string
}
type TraceDestinationConfig struct {
	Authority, DialIP                                string
	CAPEM, ClientCertificatePEM, ClientPrivateKeyPEM []byte
}
type TraceStats struct {
	Attempted, Exported, Dropped, Failed uint64
	Active, Queued                       uint32
}

// Tracer owns a private SDK and one bounded exporter. The owner must Close it
// after stopping hosts; tracing is never an application readiness dependency.
type Tracer struct {
	mu                                   sync.Mutex
	shutdownTimer                        *time.Timer
	closed                               bool
	provider                             *sdktrace.TracerProvider
	sdk                                  trace.Tracer
	routes                               map[string]string
	ratio                                uint32
	ingress                              [][32]byte
	collector                            traceEndpoint
	token                                string
	destinations                         []*TraceTransport
	queue                                chan traceRecord
	closing                              chan struct{}
	done                                 chan struct{}
	workerCtx                            context.Context
	cancel                               context.CancelFunc
	entropy                              func(bool, uint32) (trace.TraceID, trace.SpanID, bool, error)
	active                               atomic.Uint32
	attempted, exported, dropped, failed atomic.Uint64
}

func NewTracer(cfg TraceConfig) (*Tracer, error) {
	if !cfg.Enabled {
		if cfg.SamplePerMillion != 0 || len(cfg.RouteTemplates) != 0 || cfg.Collector.Authority != "" || cfg.Collector.DialIP != "" || len(cfg.Collector.CAPEM) != 0 || cfg.Collector.AuthToken != "" || len(cfg.Outbound) != 0 || len(cfg.TrustedIngressSPKI) != 0 {
			return nil, ErrTraceConfig
		}
		return &Tracer{}, nil
	}
	for _, e := range os.Environ() {
		if len(e) >= 5 && e[:5] == "OTEL_" {
			return nil, ErrTraceConfig
		}
	}
	if cfg.SamplePerMillion > 1000000 || len(cfg.RouteTemplates) > 32 || len(cfg.Outbound) > 16 || len(cfg.TrustedIngressSPKI) > 16 || !validTraceToken(cfg.Collector.AuthToken) {
		return nil, ErrTraceConfig
	}
	collector, err := newTraceEndpoint(cfg.Collector.Authority, cfg.Collector.DialIP, cfg.Collector.CAPEM, nil, nil, false)
	if err != nil {
		return nil, ErrTraceConfig
	}
	t := &Tracer{routes: make(map[string]string), ratio: cfg.SamplePerMillion, collector: collector, token: cfg.Collector.AuthToken, queue: make(chan traceRecord, 256), closing: make(chan struct{}), done: make(chan struct{})}
	for i, s := range cfg.RouteTemplates {
		if !canonicalMetricRoute(s) {
			return nil, ErrTraceConfig
		}
		if _, ok := t.routes[s]; ok {
			return nil, ErrTraceConfig
		}
		t.routes[s] = fmt.Sprintf("r%02d", i)
	}
	for _, p := range cfg.TrustedIngressSPKI {
		if len(p) != 32 {
			return nil, ErrTraceConfig
		}
		var digest [32]byte
		copy(digest[:], p)
		for _, old := range t.ingress {
			if digest == old {
				return nil, ErrTraceConfig
			}
		}
		t.ingress = append(t.ingress, digest)
	}
	seen := make(map[string]bool)
	for i, d := range cfg.Outbound {
		if seen[d.Authority] {
			return nil, ErrTraceConfig
		}
		seen[d.Authority] = true
		ep, e := newTraceEndpoint(d.Authority, d.DialIP, d.CAPEM, d.ClientCertificatePEM, d.ClientPrivateKeyPEM, true)
		if e != nil {
			return nil, ErrTraceConfig
		}
		t.destinations = append(t.destinations, newTraceTransport(t, ep, i))
	}
	t.entropy = traceEntropy
	t.workerCtx, t.cancel = context.WithCancel(context.Background())
	t.provider = sdktrace.NewTracerProvider(sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "amos"))), sdktrace.WithIDGenerator(traceIDGenerator{}), sdktrace.WithSampler(traceSampler{}), sdktrace.WithoutPanicRecording(), sdktrace.WithRawSpanLimits(sdktrace.SpanLimits{AttributeValueLengthLimit: 16, AttributeCountLimit: 2, EventCountLimit: 0, LinkCountLimit: 0, AttributePerEventCountLimit: 0, AttributePerLinkCountLimit: 0}), sdktrace.WithSpanProcessor(traceProcessor{t}))
	t.sdk = t.provider.Tracer("amos.tracing.v1")
	go t.runTraceWorker()
	return t, nil
}
func validTraceToken(s string) bool {
	if len(s) < 1 || len(s) > 512 {
		return false
	}
	for _, b := range []byte(s) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}
func traceAdd(a *atomic.Uint64, n uint64) {
	for {
		old := a.Load()
		if a.CompareAndSwap(old, saturate(old, n)) {
			return
		}
	}
}
func (t *Tracer) Stats() TraceStats {
	if t == nil {
		return TraceStats{}
	}
	return TraceStats{Attempted: t.attempted.Load(), Exported: t.exported.Load(), Dropped: t.dropped.Load(), Failed: t.failed.Load(), Active: t.active.Load(), Queued: uint32(len(t.queue))}
}
func (t *Tracer) stopped() bool {
	if t == nil || t.sdk == nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// Close stops admission immediately. The first caller establishes a shared
// deadline of at most five seconds. Later callers may force cancellation, never
// extend the deadline. No exporter diagnostic is returned.
func (t *Tracer) Close(ctx context.Context) error {
	if t == nil || t.sdk == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t.mu.Lock()
	if !t.closed {
		t.closed = true
		close(t.closing)
		budget := 5 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			budget = min(budget, time.Until(deadline))
		}
		t.shutdownTimer = time.AfterFunc(max(budget, 0), t.cancel)
	}
	t.mu.Unlock()
	if ctx.Err() != nil {
		select {
		case <-t.done:
			return nil
		default:
			t.cancel()
		}
	}
	for _, d := range t.destinations {
		d.CloseIdleConnections()
	}
	select {
	case <-ctx.Done():
		t.cancel()
		<-t.done
		return ErrTraceClosed
	case <-t.done:
		if t.workerCtx.Err() != nil {
			return ErrTraceClosed
		}
		return nil
	}
}

type traceScopeKey struct{}
type traceTicketKey struct{}
type traceScope struct {
	owner   *Tracer
	sc      trace.SpanContext
	sampled bool
	live    atomic.Bool
}
type traceTicket struct {
	traceID trace.TraceID
	spanID  trace.SpanID
	sampled bool
	valid   bool
}
type traceIDGenerator struct{}

func (traceIDGenerator) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	p, _ := ctx.Value(traceTicketKey{}).(*traceTicket)
	if p == nil || !p.valid {
		return trace.TraceID{}, trace.SpanID{}
	}
	return p.traceID, p.spanID
}
func (traceIDGenerator) NewSpanID(ctx context.Context, id trace.TraceID) trace.SpanID {
	p, _ := ctx.Value(traceTicketKey{}).(*traceTicket)
	if p == nil || !p.valid || id != p.traceID {
		return trace.SpanID{}
	}
	return p.spanID
}

type traceSampler struct{}

func (traceSampler) Description() string { return "amos.private" }
func (traceSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	ticket, _ := p.ParentContext.Value(traceTicketKey{}).(*traceTicket)
	if ticket != nil && ticket.valid && ticket.traceID == p.TraceID && ticket.sampled {
		return sdktrace.SamplingResult{Decision: sdktrace.RecordAndSample}
	}
	return sdktrace.SamplingResult{Decision: sdktrace.Drop}
}

// traceEntropy is private: no caller-selected generator or weak fallback exists.
func traceEntropy(root bool, ratio uint32) (trace.TraceID, trace.SpanID, bool, error) {
	return readTraceEntropy(rand.Read, root, ratio)
}
func readTraceEntropy(read func([]byte) (int, error), root bool, ratio uint32) (trace.TraceID, trace.SpanID, bool, error) {
	var tid trace.TraceID
	var sid trace.SpanID
	var buf [56]byte
	n, err := read(buf[:])
	if err != nil || n != len(buf) {
		return tid, sid, false, errTraceExport
	}
	copy(sid[:], buf[16:24])
	if !sid.IsValid() {
		return tid, sid, false, errTraceExport
	}
	if !root {
		return tid, sid, false, nil
	}
	copy(tid[:], buf[:16])
	if !tid.IsValid() {
		return tid, sid, false, errTraceExport
	}
	if ratio == 0 {
		return tid, sid, false, nil
	}
	if ratio == 1000000 {
		return tid, sid, true, nil
	}
	// Eight bounded draws, rejecting the short tail, give an unbiased ppm draw.
	for i := 24; i < len(buf); i += 4 {
		v := binary.BigEndian.Uint32(buf[i : i+4])
		if uint64(v) < uint64(1<<32)/1000000*1000000 {
			return tid, sid, v%1000000 < ratio, nil
		}
	}
	return tid, sid, false, errTraceExport
}
func ownedTraceScope(ctx context.Context) *traceScope {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(traceScopeKey{}).(*traceScope)
	if p == nil || !p.live.Load() || p.owner.stopped() {
		return nil
	}
	return p
}

func inertTraceContext(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(trace.ContextWithSpanContext(ctx, trace.SpanContext{}), traceScopeKey{}, (*traceScope)(nil))
}

func (t *Tracer) startTrace(ctx context.Context, name string, kind trace.SpanKind, parent trace.SpanContext, decision *bool, labels ...attribute.KeyValue) (context.Context, *traceScope, func(string)) {
	noop := func(string) {}
	if ctx == nil || t == nil || t.sdk == nil {
		return inertTraceContext(ctx), nil, noop
	}
	root := decision == nil
	tid, sid, sampled, err := t.entropy(root, t.ratio)
	if err != nil {
		traceAdd(&t.dropped, 1)
		return inertTraceContext(ctx), nil, noop
	}
	if decision != nil {
		sampled = *decision
	}
	if parent.IsValid() {
		tid = parent.TraceID()
		if root && parent.IsRemote() && !parent.IsSampled() {
			sampled = false
		}
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		traceAdd(&t.dropped, 1)
		return inertTraceContext(ctx), nil, noop
	}
	if sampled && t.active.Load() >= 256 {
		t.mu.Unlock()
		traceAdd(&t.dropped, 1)
		return inertTraceContext(ctx), nil, noop
	}
	if sampled {
		t.active.Add(1)
	}
	t.mu.Unlock()
	// Mask any caller SDK span. SDK handles and the private ticket never escape.
	ticket := &traceTicket{traceID: tid, spanID: sid, sampled: sampled, valid: true}
	private := trace.ContextWithSpanContext(ctx, parent)
	private = context.WithValue(private, traceTicketKey{}, ticket)
	_, span := t.sdk.Start(private, name, trace.WithSpanKind(kind), trace.WithAttributes(labels...))
	sc := span.SpanContext()
	if !sc.IsValid() || sc.TraceID() != tid || sc.SpanID() != sid {
		span.End()
		if sampled {
			t.active.Add(^uint32(0))
		}
		traceAdd(&t.dropped, 1)
		return inertTraceContext(ctx), nil, noop
	}
	scope := &traceScope{owner: t, sc: sc, sampled: sampled}
	scope.live.Store(true)
	out := trace.ContextWithSpanContext(ctx, sc)
	out = context.WithValue(out, traceScopeKey{}, scope)
	var once sync.Once
	end := func(outcome string) {
		once.Do(func() {
			scope.live.Store(false)
			if sampled {
				defer t.active.Add(^uint32(0))
				span.SetAttributes(attribute.String("amos.outcome", outcome))
				if outcome != "ok" && outcome != "commit" && outcome != "rollback" {
					span.SetStatus(codes.Error, "")
				}
				span.End()
			}
		})
	}
	// Fixed labels are attached internally only; caller context cannot mutate span.
	if kind == trace.SpanKindServer { // populated by the server wrapper before ending
		return out, scope, func(outcome string) { end(outcome) }
	}
	return out, scope, end
}

// TransactionTraceOutcome is a finite completion classification, never an error.
type TransactionTraceOutcome uint8

const (
	TransactionTraceCommit TransactionTraceOutcome = iota
	TransactionTraceRollback
	TransactionTraceError
	TransactionTraceCancelled
	TransactionTracePanic
	TransactionTraceUnavailable
)

// BeginTransactionTrace preserves nil/unowned contexts and does no work for them.
// The returned child context is for private driver use; callbacks retain their
// original server context, so outbound work remains a sibling of the transaction.
func BeginTransactionTrace(ctx context.Context) (context.Context, func(TransactionTraceOutcome)) {
	parent := ownedTraceScope(ctx)
	if parent == nil {
		return ctx, func(TransactionTraceOutcome) {}
	}
	child, _, end := parent.owner.startTrace(ctx, "database.transaction", trace.SpanKindInternal, parent.sc, &parent.sampled)
	return child, func(outcome TransactionTraceOutcome) {
		v := "error"
		switch outcome {
		case TransactionTraceCommit:
			v = "commit"
		case TransactionTraceRollback:
			v = "rollback"
		case TransactionTraceCancelled:
			v = "cancelled"
		case TransactionTracePanic:
			v = "panic"
		case TransactionTraceUnavailable:
			v = "unavailable"
		}
		if v != "panic" && ctx.Err() != nil {
			v = "cancelled"
		}
		end(v)
	}
}
