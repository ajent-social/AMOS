package telemetry

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ajent-social/amos/jobs"
	"math"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMetricsConfig(t *testing.T) {
	for _, cfg := range []MetricsConfig{{RouteTemplates: []string{"/x", "/x"}}, {RouteTemplates: []string{"//x"}}, {RouteTemplates: []string{"/x?secret"}}, {RouteTemplates: []string{"/../x"}}, {RouteTemplates: []string{"/x/*/y"}}, {WorkerNames: []string{"secret@private"}}, {WorkerNames: []string{"a", "a"}}, {WorkerNames: make([]string, 17)}, {RouteTemplates: make([]string, 33)}} {
		if _, err := NewMetrics(cfg); err == nil {
			t.Fatalf("accepted invalid config %#v", cfg)
		}
	}
	cfg := MetricsConfig{RouteTemplates: []string{"/private/*"}, WorkerNames: []string{"private-worker"}}
	c, err := NewMetrics(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RouteTemplates[0] = "/changed"
	cfg.WorkerNames[0] = "changed"
	c.ObserveHTTP("/private/*", 200, "", time.Millisecond)
	if c.Snapshot().HTTP[0].Status[1] != 1 {
		t.Fatal("configuration not copied")
	}
	if _, err := c.WorkerObserver("private-worker"); err != nil {
		t.Fatal(err)
	}
}
func TestMetricsCardinalityPrivacyAndConcurrency(t *testing.T) {
	c, _ := NewMetrics(MetricsConfig{RouteTemplates: []string{"/private/*"}, WorkerNames: []string{"secret-worker"}})
	var wg sync.WaitGroup
	for j := 0; j < 8; j++ {
		wg.Add(1)
		go func(j int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				c.ObserveHTTP(fmt.Sprintf("/private-CANARY/%d/%d?email=secret@example.test", j, i), 700+i, "private-error-CANARY", time.Second)
			}
		}(j)
	}
	wg.Wait()
	s := c.Snapshot()
	if len(s.HTTP) != 3 || s.HTTP[1].DurationCount != 8000 {
		t.Fatalf("cardinality expanded or observations lost: series=%d other_count=%d", len(s.HTTP), s.HTTP[1].DurationCount)
	}
	b, _ := json.Marshal(s)
	for _, secret := range []string{"CANARY", "private", "secret-worker", "example.test"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	if len(b) > MaxMetricsBytes {
		t.Fatal("size bound")
	}
	if _, err := c.WorkerObserver("not-configured"); err == nil {
		t.Fatal("unknown worker accepted")
	}
	if err := c.ObserveQueue("not-configured", jobs.Snapshot{}, nil); err == nil {
		t.Fatal("unknown snapshot accepted")
	}
}
func TestMetricsSaturationHistogramsAndSafeCodes(t *testing.T) {
	c, _ := NewMetrics(MetricsConfig{})
	c.ObserveHTTP("", 200, "", -1)
	c.ObserveHTTP("", 500, "handler.panic", 5*time.Millisecond+1)
	s := c.Snapshot().HTTP[1]
	if s.Duration[0] != 1 || s.Duration[1] != 2 || s.DurationCount != 2 || s.Errors[3] != 1 {
		t.Fatalf("histogram: %+v", s)
	}
	c.snapshot.HTTP[1].Status[1] = math.MaxUint64
	c.snapshot.HTTP[1].DurationMilliseconds = math.MaxUint64
	c.ObserveHTTP("", 200, "unknown", time.Second)
	s = c.Snapshot().HTTP[1]
	if s.Status[1] != math.MaxUint64 || s.DurationMilliseconds != math.MaxUint64 {
		t.Fatal("wrapped")
	}
	for i, code := range metricCodes {
		c.ObserveHTTP("", 100*(i%5+1), code, 0)
	}
	c.ObserveHTTP("", 99, "secret", 0)
	if c.Snapshot().HTTP[1].Status[5] != 1 {
		t.Fatal("status class")
	}
}
func TestMetricsPoolQueueStatesAndCopies(t *testing.T) {
	c, _ := NewMetrics(MetricsConfig{WorkerNames: []string{"mail"}})
	if c.Snapshot().Database.State != "unavailable" {
		t.Fatal("initial health fabricated")
	}
	c.ObserveDatabase(sql.DBStats{}, errors.New("private"))
	_ = c.ObserveQueue("mail", jobs.Snapshot{}, errors.New("private"))
	if c.Snapshot().Workers[0].Queue.State != "unavailable" {
		t.Fatal("missing snapshot healthy")
	}
	c.ObserveDatabase(sql.DBStats{OpenConnections: 3, InUse: 2, Idle: 1, WaitCount: 4, WaitDuration: time.Second}, nil)
	now := time.Now().UTC()
	_ = c.ObserveQueue("mail", jobs.Snapshot{ObservedAt: now, Queued: 4, Unknown: 2, RetryAttempts: 5, LastSuccessKnown: true, LastSuccess: now}, nil)
	obs, _ := c.WorkerObserver("mail")
	obs(jobs.Event{Outcome: jobs.OutcomeRetry, Elapsed: time.Second})
	obs(jobs.Event{Outcome: "raw-CANARY", Elapsed: time.Second})
	c.ObserveDatabase(sql.DBStats{}, errors.New("unavailable"))
	_ = c.ObserveQueue("mail", jobs.Snapshot{}, errors.New("unavailable"))
	s := c.Snapshot()
	if s.Database.State != "stale" || s.Database.Open != 3 || s.Workers[0].Queue.State != "stale" || s.Workers[0].Queue.Queued != 4 || s.Workers[0].Outcomes[6] != 1 {
		t.Fatalf("stale data lost: %+v", s)
	}
	c.snapshot.Database.State = "observed"
	c.snapshot.Database.ObservedAt = now.Add(-MetricsFreshness - time.Second)
	c.snapshot.Workers[0].Queue.State = "observed"
	c.snapshot.Workers[0].Queue.ObservedAt = now.Add(-MetricsFreshness - time.Second)
	if c.Snapshot().Database.State != "stale" || c.Snapshot().Workers[0].Queue.State != "stale" {
		t.Fatal("old data healthy")
	}
	s.HTTP[0].Route = "mutation"
	s.Workers[0].Worker = "mutation"
	if c.Snapshot().HTTP[0].Route == "mutation" || c.Snapshot().Workers[0].Worker == "mutation" {
		t.Fatal("snapshot aliases collector")
	}
}
func TestMetricsHandlerBoundary(t *testing.T) {
	c, _ := NewMetrics(MetricsConfig{})
	var body string
	for _, tt := range []struct {
		method, peer string
		status       int
	}{{"GET", "127.0.0.1:123", 200}, {"HEAD", "[::1]:123", 200}, {"POST", "127.0.0.1:123", 405}, {"GET", "192.0.2.1:123", 403}, {"GET", "", 403}, {"GET", "localhost:123", 403}, {"GET", "127.0.0.1", 403}, {"GET", "127.0.0.1:", 403}, {"GET", "127.0.0.1:bad", 403}, {"GET", "[::1]:65536", 403}} {
		r := httptest.NewRequest(tt.method, "/metrics", nil)
		r.RemoteAddr = tt.peer
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, r)
		if w.Code != tt.status || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%+v: %d", tt, w.Code)
		}
		if tt.method == "GET" && tt.status == 200 {
			body = w.Body.String()
		}
		if tt.method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
	}
	if !strings.Contains(body, "amos-metrics-v1") {
		t.Fatal("version")
	}
	cfg := MetricsConfig{}
	for i := 0; i < 32; i++ {
		cfg.RouteTemplates = append(cfg.RouteTemplates, fmt.Sprintf("/private/%d", i))
	}
	for i := 0; i < 16; i++ {
		cfg.WorkerNames = append(cfg.WorkerNames, fmt.Sprintf("worker%d", i))
	}
	maxc, _ := NewMetrics(cfg)
	for i := range maxc.snapshot.HTTP {
		h := &maxc.snapshot.HTTP[i]
		h.DurationCount = math.MaxUint64
		h.DurationMilliseconds = math.MaxUint64
		for j := range h.Status {
			h.Status[j] = math.MaxUint64
		}
		for j := range h.Errors {
			h.Errors[j] = math.MaxUint64
		}
		for j := range h.Duration {
			h.Duration[j] = math.MaxUint64
		}
	}
	for i := range maxc.snapshot.Workers {
		for j := range maxc.snapshot.Workers[i].Outcomes {
			maxc.snapshot.Workers[i].Outcomes[j] = math.MaxUint64
		}
	}
	b, _ := json.Marshal(maxc.Snapshot())
	if len(b) > MaxMetricsBytes {
		t.Fatal("maximum configured output exceeds limit")
	}
}

func TestMetricsActualLoopbackListener(t *testing.T) {
	c, _ := NewMetrics(MetricsConfig{})
	server := httptest.NewServer(c.Handler())
	defer server.Close()
	get, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = get.Body.Close() }()
	head, err := server.Client().Head(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = head.Body.Close() }()
	if get.StatusCode != 200 || head.StatusCode != 200 || head.Header.Get("Content-Length") != get.Header.Get("Content-Length") || head.Header.Get("Content-Length") == "" {
		t.Fatal("actual GET/HEAD mismatch")
	}
}
