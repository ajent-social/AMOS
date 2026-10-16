package observability

import "github.com/ajent-social/amos/internal/telemetry"

// TraceConfig explicitly opts into copied, bounded, owner-selected tracing.
type TraceConfig = telemetry.TraceConfig
type TraceCollectorConfig = telemetry.TraceCollectorConfig
type TraceDestinationConfig = telemetry.TraceDestinationConfig
type TraceStats = telemetry.TraceStats

// Tracer owns its bounded worker. Hosts never close a shared tracer implicitly.
type Tracer = telemetry.Tracer
type TraceTransport = telemetry.TraceTransport

var ErrTraceConfig = telemetry.ErrTraceConfig
var ErrTraceClosed = telemetry.ErrTraceClosed

func NewTracer(cfg TraceConfig) (*Tracer, error) { return telemetry.NewTracer(cfg) }
