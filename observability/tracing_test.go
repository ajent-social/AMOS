package observability_test

import (
	"context"
	"testing"

	"github.com/ajent-social/amos/observability"
)

func TestTracingPublicDefaultOff(t *testing.T) {
	tracer, err := observability.NewTracer(observability.TraceConfig{})
	if err != nil || tracer.Stats() != (observability.TraceStats{}) {
		t.Fatal("default-off construction failed")
	}
	if err = tracer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = tracer.Transport(0); err != observability.ErrTraceConfig {
		t.Fatal("unconfigured destination accepted")
	}
	var absent *observability.Tracer
	var nilCtx context.Context
	if absent.Stats() != (observability.TraceStats{}) || absent.Close(nilCtx) != nil {
		t.Fatal("nil compatibility failed")
	}
	if _, err = observability.NewTracer(observability.TraceConfig{SamplePerMillion: 1}); err != observability.ErrTraceConfig {
		t.Fatal("populated disabled config accepted")
	}
}
