package runtime

import (
	"bytes"
	"github.com/ajent-social/amos/internal/telemetry"
	"github.com/ajent-social/amos/observability"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsTrustedOuterLoggingExactlyOnce(t *testing.T) {
	for _, mode := range []string{"nil", "inner", "same-outer"} {
		for _, panics := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "-success", true: "-panic"}[panics], func(t *testing.T) {
				var logs bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&logs, nil))
				var collector *observability.Collector
				if mode != "nil" {
					collector, _ = observability.New(observability.Config{RouteTemplates: []string{"/business"}})
				}
				rt, err := New(Options{Metrics: collector, Logger: logger, ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
				if err != nil {
					t.Fatal(err)
				}
				var trustedID string
				if err := rt.Register("GET", "/business", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					trustedID = telemetry.RequestID(r.Context())
					if trustedID == "" {
						t.Error("trusted outer ID lost")
					}
					if panics {
						panic("private-CANARY")
					}
					w.WriteHeader(204)
				})); err != nil {
					t.Fatal(err)
				}
				var outerMetrics *observability.Collector
				if mode == "same-outer" {
					outerMetrics = collector
				}
				outer := telemetry.LoggingWithMetrics(logger, func(*http.Request) string { return "/business" }, outerMetrics, rt.Handler())
				r := httptest.NewRequest("GET", "/business", nil)
				r.Header.Set("X-Request-ID", "caller-private")
				w := httptest.NewRecorder()
				outer.ServeHTTP(w, r)
				want := 204
				if panics {
					want = 500
				}
				if w.Code != want || w.Header().Get("X-Request-ID") != trustedID || strings.Count(logs.String(), "http request") != 1 {
					t.Fatalf("outer behavior changed: status%d logs%s", w.Code, logs.String())
				}
				if collector != nil {
					s := collector.Snapshot().HTTP[0]
					if s.DurationCount != 1 || s.Status[want/100-1] != 1 {
						t.Fatalf("missing or duplicate metrics: %+v", s)
					}
					if panics && s.Errors[3] != 1 {
						t.Fatal("panic not observed")
					}
				}
			})
		}
	}
}
