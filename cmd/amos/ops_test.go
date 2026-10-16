package main

import (
	"bytes"
	"encoding/json"
	"github.com/ajent-social/amos/internal/opsconfig"
	"github.com/ajent-social/amos/internal/scaffold/ops"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func opsConfigFixture() ops.Config {
	targets := opsconfig.Defaults()
	targets.Profile = opsconfig.ProfileAWSVM
	targets.Activated = true
	targets.AlertOwner = "example-alert-role"
	targets.RecoveryOwner = "example-recovery-role"
	targets.IncidentOwner = "example-incident-role"
	return ops.Config{Targets: targets, Options: ops.Options{ErrorPercent: 5, MeanLatencyMilliseconds: 100, PoolPercent: 80, QueuedDepth: 10, QueuedAge: time.Hour, CertificateLead: 7 * 24 * time.Hour, Freshness: time.Minute, FailureHold: 2 * time.Minute, RecoveryHold: time.Minute}}
}
func opsSampleFixture(at time.Time, n uint64) ops.Sample {
	return ops.Sample{At: at, HTTP: ops.HTTPObservation{At: at, Available: true, Completed: n * 100, DurationCount: n * 100, DurationMilliseconds: n * 1000}, Pool: ops.PoolObservation{At: at, Available: true, InUse: 1, Max: 10}, Queue: ops.QueueObservation{At: at, Available: true}, Disk: ops.ValueObservation{At: at, Available: true, Value: 20}, Backup: ops.TimeObservation{At: at, Available: true, Value: at.Add(-time.Hour)}, Certificate: ops.TimeObservation{At: at, Available: true, Value: at.Add(30 * 24 * time.Hour)}}
}
func opsWriteFixture(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestAlertsCLIActualRenderEvaluateStates(t *testing.T) {
	cfg := opsConfigFixture()
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"rpo":"24h0m0s"`) || !strings.Contains(string(body), `"freshness":"1m0s"`) {
		t.Fatal("duration units not explicit")
	}
	config := opsWriteFixture(t, "owner.json", body)
	var output, diagnostics bytes.Buffer
	if code := runOpsIO([]string{"render", "--config", config}, &output, &diagnostics); code != 0 || diagnostics.Len() != 0 {
		t.Fatal(code, diagnostics.String())
	}
	definition := opsWriteFixture(t, "definition.json", output.Bytes())
	bundle, err := ops.DecodeBundle(output.Bytes())
	if err != nil || bundle.NotificationsEnabled {
		t.Fatal("actual render invalid", err)
	}
	base := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name    string
		times   []time.Duration
		disk    []float64
		backups []time.Duration
		want    []string
	}{
		{"healthy", []time.Duration{0, time.Minute}, []float64{20, 20}, []time.Duration{time.Hour, time.Hour}, []string{"healthy", "healthy"}},
		{"pending firing recovering recovered", []time.Duration{0, time.Minute, 2 * time.Minute, 3 * time.Minute, 4 * time.Minute}, []float64{90, 90, 90, 20, 20}, []time.Duration{25 * time.Hour, 25 * time.Hour, 25 * time.Hour, time.Hour, time.Hour}, []string{"pending", "pending", "firing", "recovering", "recovered"}},
		{"gap", []time.Duration{0, time.Hour}, []float64{90, 90}, []time.Duration{25 * time.Hour, 25 * time.Hour}, []string{"pending", "pending"}},
		{"recovery gap", []time.Duration{0, time.Minute, 2 * time.Minute, 3 * time.Minute, time.Hour}, []float64{90, 90, 90, 20, 20}, []time.Duration{25 * time.Hour, 25 * time.Hour, 25 * time.Hour, time.Hour, time.Hour}, []string{"pending", "pending", "firing", "recovering", "recovering"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var samples []ops.Sample
			for i, delta := range tt.times {
				s := opsSampleFixture(base.Add(delta), uint64(i+1))
				s.Disk.Value = tt.disk[i]
				s.Backup.Value = s.At.Add(-tt.backups[i])
				samples = append(samples, s)
			}
			data, _ := json.Marshal(ops.Samples{Version: ops.SamplesVersion, Samples: samples})
			file := opsWriteFixture(t, "samples.json", data)
			output.Reset()
			diagnostics.Reset()
			if code := runOpsIO([]string{"evaluate", "--definition", definition, "--samples", file}, &output, &diagnostics); code != 0 {
				t.Fatal(code, diagnostics.String())
			}
			var result ops.Output
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			for i, want := range tt.want {
				if result.Evaluations[i].Results[2].State != want || result.Evaluations[i].Results[5].State != want {
					t.Fatalf("disk/backup state %d: %+v", i, result.Evaluations[i].Results)
				}
			}
			if result.NotificationsEnabled || strings.Contains(output.String(), "example-alert-role") || strings.Contains(output.String(), config) {
				t.Fatal("evaluation echoed owner/input or enabled sends")
			}
		})
	}
	// Disabled targets, counter reset, unknown supplements and max-open zero use actual CLI.
	cfg.Targets.Activated = false
	body, _ = json.Marshal(cfg)
	config = opsWriteFixture(t, "disabled.json", body)
	output.Reset()
	if runOpsIO([]string{"render", "--config", config}, &output, &diagnostics) != 0 {
		t.Fatal("disabled render")
	}
	disabled := opsWriteFixture(t, "disabled-definition.json", output.Bytes())
	s := opsSampleFixture(base, 1)
	s.Disk.Value = 99
	data, _ := json.Marshal(ops.Samples{Version: ops.SamplesVersion, Samples: []ops.Sample{s}})
	samplePath := opsWriteFixture(t, "disabled-sample.json", data)
	output.Reset()
	if runOpsIO([]string{"evaluate", "--definition", disabled, "--samples", samplePath}, &output, &diagnostics) != 0 {
		t.Fatal("disabled evaluate")
	}
	if strings.Count(output.String(), `"state":"disabled"`) != 7 {
		t.Fatal("targets activated implicitly")
	}
	a := opsSampleFixture(base, 10)
	b := opsSampleFixture(base.Add(time.Minute), 1)
	b.Backup.Available = false
	b.Pool.Max = 0
	b.Pool.InUse = 0
	data, _ = json.Marshal(ops.Samples{Version: ops.SamplesVersion, Samples: []ops.Sample{a, b}})
	samplePath = opsWriteFixture(t, "unknown-sample.json", data)
	output.Reset()
	if runOpsIO([]string{"evaluate", "--definition", definition, "--samples", samplePath}, &output, &diagnostics) != 0 {
		t.Fatal("reset/unknown evaluate")
	}
	var result ops.Output
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, 1, 3, 5} {
		if result.Evaluations[1].Results[i].State != "unavailable" {
			t.Fatal("reset/unknown became healthy")
		}
	}
}
func TestAlertsCLIGenericErrorsAndClosedInputs(t *testing.T) {
	var output, diagnostics bytes.Buffer
	cfg := opsConfigFixture()
	valid, _ := json.Marshal(cfg)
	good := opsWriteFixture(t, "good.json", valid)
	secret := opsWriteFixture(t, "private-CANARY.json", []byte(`{"secret":"PRIVATE-CANARY"}`))
	b, _ := ops.Generate(cfg)
	definition, _ := ops.Encode(b)
	def := opsWriteFixture(t, "definition.json", definition)
	for _, args := range [][]string{{}, {"unknown"}, {"render", "--config", secret}, {"render", "--config", good, "--secret-CANARY", "credential"}, {"render", "--config", good, "unexpected-CANARY"}, {"evaluate", "--definition", def, "--samples", secret}, {"evaluate", "--definition", def}, {"render", "--config", "missing-CANARY"}, {"render", "--config", good, "--definition", def}} {
		output.Reset()
		diagnostics.Reset()
		if code := runOpsIO(args, &output, &diagnostics); code == 0 || output.Len() != 0 || diagnostics.String() != "amos ops: invalid input or operation unavailable\n" {
			t.Fatalf("unsafe CLI failure: code=%d output=%q diagnostics=%q", code, output.String(), diagnostics.String())
		}
	}
	malformed := strings.Replace(string(valid), `"rpo":"24h0m0s"`, `"rpo":86400000000000`, 1)
	path := opsWriteFixture(t, "implicit-units.json", []byte(malformed))
	output.Reset()
	diagnostics.Reset()
	if runOpsIO([]string{"render", "--config", path}, &output, &diagnostics) == 0 {
		t.Fatal("implicit duration units accepted")
	}
	// Exercise main command dispatch without initializer fallback.
	if run([]string{"ops", "unknown"}) == 0 {
		t.Fatal("main dispatch missing")
	}
}
