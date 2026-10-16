package observability_test

import (
	"github.com/ajent-social/amos/observability"
	"testing"
)

func TestMetricsPublicFacade(t *testing.T) {
	c, err := observability.New(observability.Config{})
	if err != nil || c.Snapshot().Version != "amos-metrics-v1" {
		t.Fatal("public collector unavailable", err)
	}
	if _, err := observability.New(observability.Config{WorkerNames: []string{"private@name"}}); err == nil {
		t.Fatal("invalid config accepted")
	}
}
