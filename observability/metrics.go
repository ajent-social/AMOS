// Package observability provides finite, process-local private operator metrics.
package observability

import "github.com/ajent-social/amos/internal/telemetry"

// Config contains copied trusted route and worker names. Names never appear in output.
type Config = telemetry.MetricsConfig

// Collector is concurrency safe. Handler must use a separate operator-owned loopback listener.
type Collector = telemetry.Metrics
type Snapshot = telemetry.MetricsSnapshot

var ErrInvalidConfig = telemetry.ErrMetricsConfig

// New validates finite configuration before serving.
func New(cfg Config) (*Collector, error) { return telemetry.NewMetrics(cfg) }
