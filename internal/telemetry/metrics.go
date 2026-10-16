package telemetry

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ajent-social/amos/jobs"
)

// MetricsConfig is finite trusted operator configuration. Names are never exported.
type MetricsConfig struct {
	RouteTemplates []string
	WorkerNames    []string
}

var ErrMetricsConfig = errors.New("invalid metrics configuration")

const MaxMetricsBytes = 256 << 10
const MetricsFreshness = 5 * time.Second

var durationBounds = [...]uint64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, math.MaxUint64}
var metricCodes = [...]string{"none", "auth.denied", "dependency.unavailable", "handler.panic", "method.not_allowed", "path.invalid", "route.not_found", "route.unavailable", "http.error"}
var workerOutcomes = [...]jobs.Outcome{jobs.OutcomeIdle, jobs.OutcomeInvalid, jobs.OutcomeCanceled, jobs.OutcomeClaimFailed, jobs.OutcomeResolveFailed, jobs.OutcomeSucceeded, jobs.OutcomeRetry, jobs.OutcomeDead, jobs.OutcomeUnknown, jobs.OutcomePanic}

type HTTPMetrics struct {
	Route                string     `json:"route"`
	Status               [6]uint64  `json:"status_counts"`    // 1xx through 5xx, then other.
	Errors               [9]uint64  `json:"error_counts"`     // metricCodes order.
	Duration             [12]uint64 `json:"duration_buckets"` // cumulative upper bounds, final infinity.
	DurationCount        uint64     `json:"duration_count"`
	DurationMilliseconds uint64     `json:"duration_sum_ms"`
}
type PoolMetrics struct {
	State           string    `json:"state"`
	ObservedAt      time.Time `json:"observed_at"`
	Open            int       `json:"open"`
	InUse           int       `json:"in_use"`
	Idle            int       `json:"idle"`
	Max             int       `json:"max"`
	WaitCount       int64     `json:"wait_count"`
	WaitNanoseconds int64     `json:"wait_nanoseconds"`
}
type QueueMetrics struct {
	State                    string    `json:"state"`
	ObservedAt               time.Time `json:"observed_at"`
	Queued                   uint64    `json:"queued"`
	Unknown                  uint64    `json:"unknown"`
	Leased                   uint64    `json:"leased"`
	OldestQueuedMilliseconds uint64    `json:"oldest_queued_ms"`
	RetryAttempts            uint64    `json:"retry_attempts"`
	Dead                     uint64    `json:"dead"`
	LastSuccess              time.Time `json:"last_success"`
	LastSuccessKnown         bool      `json:"last_success_known"`
}
type WorkerMetrics struct {
	Worker              string       `json:"worker"`
	Outcomes            [10]uint64   `json:"outcome_counts"`
	DurationNanoseconds uint64       `json:"duration_sum_ns"`
	Queue               QueueMetrics `json:"queue"`
}
type MetricsSnapshot struct {
	Version                    string           `json:"version"`
	StatusClasses              [6]string        `json:"status_classes"`
	ErrorCodes                 [9]string        `json:"error_codes"`
	WorkerOutcomes             [10]jobs.Outcome `json:"worker_outcomes"`
	DurationBoundsMilliseconds [12]uint64       `json:"duration_bounds_ms"`
	HTTP                       []HTTPMetrics    `json:"http"`
	Database                   PoolMetrics      `json:"database"`
	Workers                    []WorkerMetrics  `json:"workers"`
}

// Metrics stores only a fixed number of numeric series. Its zero value is not a configured collector.
type Metrics struct {
	mu       sync.Mutex
	routes   map[string]int
	workers  map[string]int
	snapshot MetricsSnapshot
}

func NewMetrics(cfg MetricsConfig) (*Metrics, error) {
	if len(cfg.RouteTemplates) > 32 || len(cfg.WorkerNames) > 16 {
		return nil, ErrMetricsConfig
	}
	c := &Metrics{routes: make(map[string]int), workers: make(map[string]int)}
	c.snapshot = MetricsSnapshot{Version: "amos-metrics-v1", StatusClasses: [6]string{"1xx", "2xx", "3xx", "4xx", "5xx", "other"}, ErrorCodes: metricCodes, WorkerOutcomes: workerOutcomes, DurationBoundsMilliseconds: durationBounds, Database: PoolMetrics{State: "unavailable"}}
	for i, s := range cfg.RouteTemplates {
		if !canonicalMetricRoute(s) {
			return nil, ErrMetricsConfig
		}
		if _, ok := c.routes[s]; ok {
			return nil, ErrMetricsConfig
		}
		c.routes[s] = i
		c.snapshot.HTTP = append(c.snapshot.HTTP, HTTPMetrics{Route: fmt.Sprintf("r%02d", i)})
	}
	c.snapshot.HTTP = append(c.snapshot.HTTP, HTTPMetrics{Route: "other"}, HTTPMetrics{Route: "unmatched"})
	for i, s := range cfg.WorkerNames {
		if len(s) < 1 || len(s) > 64 || strings.IndexFunc(s, func(r rune) bool {
			valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
			return !valid
		}) >= 0 {
			return nil, ErrMetricsConfig
		}
		if _, ok := c.workers[s]; ok {
			return nil, ErrMetricsConfig
		}
		c.workers[s] = i
		c.snapshot.Workers = append(c.snapshot.Workers, WorkerMetrics{Worker: fmt.Sprintf("w%02d", i), Queue: QueueMetrics{State: "unavailable"}})
	}
	return c, nil
}
func canonicalMetricRoute(s string) bool {
	if len(s) < 1 || len(s) > 256 || !strings.HasPrefix(s, "/") || strings.ContainsAny(s, "%?#\\") {
		return false
	}
	base := strings.TrimSuffix(s, "/*")
	return base != "" && path.Clean(base) == base && !strings.Contains(base, "*") && validRoute.MatchString(base) && !strings.Contains(base, "..")
}
func saturate(a, b uint64) uint64 {
	if math.MaxUint64-a < b {
		return math.MaxUint64
	}
	return a + b
}

// ObserveHTTP accepts only a server-selected registered template. Unrecognized values never allocate labels.
func (c *Metrics) ObserveHTTP(template string, status int, code string, elapsed time.Duration) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapshot.Version == "" {
		return
	}
	i, ok := c.routes[template]
	if !ok {
		i = len(c.snapshot.HTTP) - 2
		if template == "" || template == "unmatched" {
			i++
		}
	}
	h := &c.snapshot.HTTP[i]
	s := 5
	if status >= 100 && status < 600 {
		s = status/100 - 1
	}
	h.Status[s] = saturate(h.Status[s], 1)
	e := 8
	if code == "" {
		e = 0
	} else {
		for j, v := range metricCodes {
			if v == code {
				e = j
				break
			}
		}
	}
	h.Errors[e] = saturate(h.Errors[e], 1)
	if elapsed < 0 {
		elapsed = 0
	}
	ms := uint64(elapsed / time.Millisecond)
	for j, b := range durationBounds {
		if b == math.MaxUint64 || elapsed <= time.Duration(b)*time.Millisecond {
			h.Duration[j] = saturate(h.Duration[j], 1)
		}
	}
	h.DurationCount = saturate(h.DurationCount, 1)
	h.DurationMilliseconds = saturate(h.DurationMilliseconds, ms)
}

// ObserveDatabase pushes a trusted pool snapshot, or marks last-good values stale on error.
func (c *Metrics) ObserveDatabase(stats sql.DBStats, err error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	p := &c.snapshot.Database
	if err != nil {
		p.State = failedState(p.ObservedAt)
		return
	}
	*p = PoolMetrics{State: "observed", ObservedAt: time.Now().UTC(), Open: nonnegative(stats.OpenConnections), InUse: nonnegative(stats.InUse), Idle: nonnegative(stats.Idle), Max: nonnegative(stats.MaxOpenConnections), WaitCount: max(stats.WaitCount, 0), WaitNanoseconds: max(int64(stats.WaitDuration), 0)}
}
func nonnegative(v int) int { return max(v, 0) }
func failedState(t time.Time) string {
	if t.IsZero() {
		return "unavailable"
	}
	return "stale"
}
func (c *Metrics) WorkerObserver(name string) (jobs.Observer, error) {
	if c == nil {
		return nil, ErrMetricsConfig
	}
	c.mu.Lock()
	i, ok := c.workers[name]
	c.mu.Unlock()
	if !ok {
		return nil, ErrMetricsConfig
	}
	return func(e jobs.Event) {
		c.mu.Lock()
		defer c.mu.Unlock()
		for j, v := range workerOutcomes {
			if e.Outcome == v {
				w := &c.snapshot.Workers[i]
				w.Outcomes[j] = saturate(w.Outcomes[j], 1)
				w.DurationNanoseconds = saturate(w.DurationNanoseconds, uint64(max(e.Elapsed, 0)))
				break
			}
		}
	}, nil
}
func (c *Metrics) ObserveQueue(name string, s jobs.Snapshot, err error) error {
	if c == nil {
		return ErrMetricsConfig
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	i, ok := c.workers[name]
	if !ok {
		return ErrMetricsConfig
	}
	q := &c.snapshot.Workers[i].Queue
	if err != nil || s.ObservedAt.IsZero() {
		q.State = failedState(q.ObservedAt)
		return nil
	}
	*q = QueueMetrics{State: "observed", ObservedAt: s.ObservedAt.UTC(), Queued: s.Queued, Unknown: s.Unknown, Leased: s.Leased, OldestQueuedMilliseconds: uint64(max(s.OldestQueuedAge, 0) / time.Millisecond), RetryAttempts: s.RetryAttempts, Dead: s.Dead, LastSuccess: s.LastSuccess.UTC(), LastSuccessKnown: s.LastSuccessKnown}
	return nil
}
func (c *Metrics) Snapshot() MetricsSnapshot {
	if c == nil {
		return MetricsSnapshot{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.snapshot
	s.HTTP = append([]HTTPMetrics{}, s.HTTP...)
	s.Workers = append([]WorkerMetrics{}, s.Workers...)
	now := time.Now()
	if s.Database.State == "observed" && now.Sub(s.Database.ObservedAt) > MetricsFreshness {
		s.Database.State = "stale"
	}
	for i := range s.Workers {
		q := &s.Workers[i].Queue
		if q.State == "observed" && now.Sub(q.ObservedAt) > MetricsFreshness {
			q.State = "stale"
		}
	}
	return s
}

// Handler must be served on a distinct operator-owned loopback listener. It performs no collection I/O.
func (c *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		host, port, err := net.SplitHostPort(r.RemoteAddr)
		portNumber, portErr := strconv.Atoi(port)
		ip := net.ParseIP(host)
		if err != nil || portErr != nil || portNumber < 1 || portNumber > 65535 || ip == nil || !ip.IsLoopback() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		snapshot := c.Snapshot()
		if snapshot.Version == "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		b, err := json.Marshal(snapshot)
		if err != nil || len(b) > MaxMetricsBytes {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(b)
		}
	})
}
