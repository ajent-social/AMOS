package telemetry

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type traceRecord struct {
	id             oteltrace.TraceID
	span, parent   oteltrace.SpanID
	start, end     uint64
	kind           oteltrace.SpanKind
	label, outcome string
}
type traceProcessor struct{ owner *Tracer }

func (traceProcessor) OnStart(context.Context, trace.ReadWriteSpan) {}
func (p traceProcessor) OnEnd(s trace.ReadOnlySpan) {
	t := p.owner
	traceAdd(&t.attempted, 1)
	r, ok := projectTraceSpan(s)
	if !ok {
		traceAdd(&t.dropped, 1)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		traceAdd(&t.dropped, 1)
		return
	}
	select {
	case t.queue <- r:
	default:
		traceAdd(&t.dropped, 1)
	}
}
func (traceProcessor) Shutdown(context.Context) error   { return nil }
func (traceProcessor) ForceFlush(context.Context) error { return nil }
func projectTraceSpan(s trace.ReadOnlySpan) (traceRecord, bool) {
	r := traceRecord{id: s.SpanContext().TraceID(), span: s.SpanContext().SpanID(), parent: s.Parent().SpanID(), start: uint64(s.StartTime().UnixNano()), end: uint64(s.EndTime().UnixNano()), kind: s.SpanKind()}
	if s.StartTime().UnixNano() <= 0 || s.EndTime().Before(s.StartTime()) || !s.SpanContext().IsSampled() {
		return traceRecord{}, false
	}
	expected := ""
	switch r.kind {
	case oteltrace.SpanKindServer:
		expected = "http.server"
	case oteltrace.SpanKindClient:
		expected = "http.client"
	case oteltrace.SpanKindInternal:
		expected = "database.transaction"
	default:
		return traceRecord{}, false
	}
	if s.Name() != expected {
		return traceRecord{}, false
	}
	seen := map[string]bool{}
	for _, a := range s.Attributes() {
		key := string(a.Key)
		if seen[key] || a.Value.Type() != attribute.STRING {
			return traceRecord{}, false
		}
		seen[key] = true
		switch key {
		case "amos.outcome":
			r.outcome = a.Value.AsString()
		case "amos.route":
			if r.kind != oteltrace.SpanKindServer {
				return traceRecord{}, false
			}
			r.label = a.Value.AsString()
		case "amos.destination":
			if r.kind != oteltrace.SpanKindClient {
				return traceRecord{}, false
			}
			r.label = a.Value.AsString()
		default:
			return traceRecord{}, false
		}
	}
	_, ok := traceProjection(r)
	return r, ok
}
func traceOrdinal(s string, prefix byte, max int) bool {
	if len(s) != 3 || s[0] != prefix || s[1] < '0' || s[1] > '9' || s[2] < '0' || s[2] > '9' {
		return false
	}
	return int(s[1]-'0')*10+int(s[2]-'0') < max
}
func traceProjection(r traceRecord) (*tracepb.Span, bool) {
	if !r.id.IsValid() || !r.span.IsValid() || r.start == 0 || r.end < r.start {
		return nil, false
	}
	name := ""
	var kind tracepb.Span_SpanKind
	labelkey := ""
	valid := false
	switch r.kind {
	case oteltrace.SpanKindServer:
		name = "http.server"
		kind = tracepb.Span_SPAN_KIND_SERVER
		labelkey = "amos.route"
		if r.label != "other" && r.label != "unmatched" && !traceOrdinal(r.label, 'r', 32) {
			return nil, false
		}
		valid = r.outcome == "ok" || r.outcome == "error" || r.outcome == "cancelled" || r.outcome == "panic"
	case oteltrace.SpanKindClient:
		name = "http.client"
		kind = tracepb.Span_SPAN_KIND_CLIENT
		labelkey = "amos.destination"
		if !traceOrdinal(r.label, 'o', 16) {
			return nil, false
		}
		valid = r.outcome == "ok" || r.outcome == "error" || r.outcome == "cancelled" || r.outcome == "unavailable"
	case oteltrace.SpanKindInternal:
		name = "database.transaction"
		kind = tracepb.Span_SPAN_KIND_INTERNAL
		if r.label != "" {
			return nil, false
		}
		valid = r.outcome == "commit" || r.outcome == "rollback" || r.outcome == "error" || r.outcome == "cancelled" || r.outcome == "panic" || r.outcome == "unavailable"
	}
	if !valid {
		return nil, false
	}
	s := &tracepb.Span{TraceId: bytes.Clone(r.id[:]), SpanId: bytes.Clone(r.span[:]), Name: name, Kind: kind, StartTimeUnixNano: r.start, EndTimeUnixNano: r.end, Flags: 1, Attributes: []*common.KeyValue{traceKV("amos.outcome", r.outcome)}}
	if r.parent.IsValid() {
		s.ParentSpanId = bytes.Clone(r.parent[:])
	}
	if labelkey != "" {
		s.Attributes = append(s.Attributes, traceKV(labelkey, r.label))
	}
	if r.outcome != "ok" && r.outcome != "commit" && r.outcome != "rollback" {
		s.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR}
	}
	if proto.Size(s) > 4096 {
		return nil, false
	}
	return s, true
}
func traceKV(k, v string) *common.KeyValue {
	return &common.KeyValue{Key: k, Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: v}}}
}
func (t *Tracer) runTraceWorker() {
	defer close(t.done)
	defer func() { _ = t.provider.Shutdown(context.Background()) }()
	closing := false
	closeSignal := t.closing
	for {
		var first traceRecord
		if closing {
			select {
			case first = <-t.queue:
			default:
				return
			}
		} else {
			select {
			case <-t.workerCtx.Done():
				t.discardTraceQueue()
				return
			case <-closeSignal:
				closing = true
				closeSignal = nil
				continue
			case first = <-t.queue:
			}
		}
		batch := []traceRecord{first}
		timer := time.NewTimer(100 * time.Millisecond)
	fill:
		for len(batch) < 16 {
			if closing {
				select {
				case r := <-t.queue:
					batch = append(batch, r)
					continue
				default:
					break fill
				}
			}
			select {
			case <-t.workerCtx.Done():
				timer.Stop()
				traceAdd(&t.dropped, uint64(len(batch)))
				t.discardTraceQueue()
				return
			case <-closeSignal:
				closing = true
				closeSignal = nil
			case r := <-t.queue:
				batch = append(batch, r)
			case <-timer.C:
				break fill
			}
		}
		timer.Stop()
		t.exportTraceBatch(batch)
		if t.workerCtx.Err() != nil {
			t.discardTraceQueue()
			return
		}
	}
}
func (t *Tracer) discardTraceQueue() {
	for {
		select {
		case <-t.queue:
			traceAdd(&t.dropped, 1)
		default:
			return
		}
	}
}
func (t *Tracer) exportTraceBatch(batch []traceRecord) {
	spans := make([]*tracepb.Span, 0, len(batch))
	for _, r := range batch {
		s, ok := traceProjection(r)
		if !ok {
			traceAdd(&t.dropped, 1)
			continue
		}
		spans = append(spans, s)
	}
	if len(spans) == 0 {
		return
	}
	count := uint64(len(spans))
	request := &collector.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{Resource: &resourcepb.Resource{Attributes: []*common.KeyValue{traceKV("service.name", "amos")}}, ScopeSpans: []*tracepb.ScopeSpans{{Scope: &common.InstrumentationScope{Name: "amos.tracing.v1"}, Spans: spans}}}}}
	success := false
	defer func() {
		if success {
			traceAdd(&t.exported, count)
		} else {
			traceAdd(&t.failed, count)
			traceAdd(&t.dropped, count)
		}
	}()
	if len(spans) > 16 || proto.Size(request) > 65536 {
		return
	}
	body, err := proto.Marshal(request)
	if err != nil || len(body) > 65536 {
		return
	}
	ctx, cancel := context.WithTimeout(t.workerCtx, 2*time.Second)
	defer cancel()
	transport := t.collector.transport(true)
	defer transport.CloseIdleConnections()
	// io.NopCloser intentionally prevents net/http from deriving GetBody/retries.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+t.collector.authority+"/v1/traces", io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Authorization", "Bearer "+t.token)
	resp, err := transport.RoundTrip(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		return
	}
	if len(resp.Header.Values("Content-Type")) != 1 {
		return
	}
	media, params, e := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if e != nil || media != "application/x-protobuf" || len(params) != 0 {
		return
	}
	encoding := resp.Header.Values("Content-Encoding")
	if len(encoding) > 1 || len(encoding) == 1 && encoding[0] != "" && !strings.EqualFold(encoding[0], "identity") {
		return
	}
	response, e := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if e != nil || len(response) > 65536 {
		return
	}
	ack := &collector.ExportTraceServiceResponse{}
	if e = proto.Unmarshal(response, ack); e != nil || len(ack.ProtoReflect().GetUnknown()) != 0 || ack.PartialSuccess != nil {
		return
	}
	success = true
}
