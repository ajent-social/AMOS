package ops

import (
	"database/sql"
	"encoding/json"
	"github.com/ajent-social/amos/internal/opsconfig"
	"github.com/ajent-social/amos/jobs"
	"github.com/ajent-social/amos/observability"
	"math"
	"strings"
	"testing"
	"time"
)

func fixtureConfig() Config {
	targets := opsconfig.Defaults()
	targets.Profile = opsconfig.ProfileAWSVM
	targets.Activated = true
	targets.AlertOwner = "example-alert-role"
	targets.RecoveryOwner = "example-recovery-role"
	targets.IncidentOwner = "example-incident-role"
	return Config{Targets: targets, Options: Options{ErrorPercent: 5, MeanLatencyMilliseconds: 100, PoolPercent: 80, QueuedDepth: 10, QueuedAge: time.Hour, CertificateLead: 7 * 24 * time.Hour, Freshness: time.Minute, FailureHold: 2 * time.Minute, RecoveryHold: time.Minute}}
}

var fixtureAt = time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)

func healthySample(at time.Time, n uint64) Sample {
	return Sample{At: at, HTTP: HTTPObservation{At: at, Available: true, Completed: n * 100, DurationCount: n * 100, DurationMilliseconds: n * 1000}, Pool: PoolObservation{At: at, Available: true, InUse: 2, Max: 10}, Queue: QueueObservation{At: at, Available: true, Unknown: 2, Leased: 3}, Disk: ValueObservation{At: at, Available: true, Value: 20}, Backup: TimeObservation{At: at, Available: true, Value: at.Add(-time.Hour)}, Certificate: TimeObservation{At: at, Available: true, Value: at.Add(30 * 24 * time.Hour)}}
}
func bundleFixture(t *testing.T) Bundle {
	t.Helper()
	b, err := Generate(fixtureConfig())
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func evaluateFixture(t *testing.T, b Bundle, s ...Sample) Output {
	t.Helper()
	out, err := Evaluate(b, Samples{Version: SamplesVersion, Samples: s})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestAlertsGenerateActualArtifactAndRunbooks(t *testing.T) {
	c := fixtureConfig()
	b, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Encode(b)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Generate(c)
	second, _ := Encode(again)
	if string(first) != string(second) || len(first) > MaxDefinitionBytes || b.NotificationsEnabled {
		t.Fatal("non-deterministic/oversized/enabled artifact")
	}
	decoded, err := DecodeBundle(first)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range decoded.Rules {
		if r.ID != ids[i] || r.OwnerRole != c.Targets.AlertOwner || r.RecoveryCondition == "" || r.Severity == "" || decoded.Panels[i].ID != r.ID || decoded.Panels[i].RuleID != r.ID || decoded.Panels[i].Measurement != r.Measurement || decoded.Runbooks[i].Path != r.Runbook || len(decoded.Runbooks[i].Text) < 100 {
			t.Fatal("not actionable", r)
		}
	}
	// The executed threshold comes from the generated definition, not hidden config.
	s := healthySample(fixtureAt, 1)
	s.Disk.Value = 50
	decoded.Rules[2].Threshold = 40
	decoded.Rules[2].FailureHold = 0
	if got := evaluateFixture(t, decoded, s).Evaluations[0].Results[2]; got.State != "firing" {
		t.Fatal("generated definition was not executed", got)
	}
	c.Targets.Activated = false
	disabled, _ := Generate(c)
	s.Disk.Value = 100
	out := evaluateFixture(t, disabled, s)
	for _, r := range out.Evaluations[0].Results {
		if r.State != "disabled" || r.Incident {
			t.Fatal("unactivated target active", r)
		}
	}
}
func TestAlertsAllSevenPendingFiringRecovery(t *testing.T) {
	b := bundleFixture(t)
	var samples []Sample
	for i := 0; i < 6; i++ {
		s := healthySample(fixtureAt.Add(time.Duration(i)*time.Minute), uint64(i+1))
		bad := i < 4
		s.HTTP.ServerErrors = uint64(min(i+1, 4)) * 50
		s.HTTP.DurationMilliseconds = uint64(min(i+1, 4))*50000 + uint64(max(i-3, 0))*1000
		if bad {
			s.Disk.Value = 90
			s.Pool.InUse = 9
			s.Queue.Queued = 20
			s.Queue.AgeMilliseconds = uint64(2 * time.Hour / time.Millisecond)
			s.Backup.Value = s.At.Add(-25 * time.Hour)
			s.Certificate.Value = s.At.Add(24 * time.Hour)
		}
		samples = append(samples, s)
	}
	out := evaluateFixture(t, b, samples...)
	for i := range ids {
		if got := out.Evaluations[1].Results[i]; got.State != "pending" {
			t.Fatalf("%s expected pending: %+v", ids[i], got)
		}
		if got := out.Evaluations[3].Results[i]; got.State != "firing" || !got.Incident {
			t.Fatalf("%s expected firing: %+v", ids[i], got)
		}
		if got := out.Evaluations[4].Results[i]; got.State != "recovering" || !got.Incident {
			t.Fatalf("%s expected recovering: %+v", ids[i], got)
		}
		if got := out.Evaluations[5].Results[i]; got.State != "recovered" || got.Incident {
			t.Fatalf("%s expected recovered: %+v", ids[i], got)
		}
	}
	if out.NotificationsEnabled {
		t.Fatal("send authority inferred")
	}
}
func TestAlertsBackupRPOIndependentOfDisk(t *testing.T) {
	b := bundleFixture(t)
	var samples []Sample
	for i := 0; i < 3; i++ {
		s := healthySample(fixtureAt.Add(time.Duration(i)*time.Minute), uint64(i+1))
		s.Backup.Value = fixtureAt.Add(-25 * time.Hour)
		samples = append(samples, s)
	}
	out := evaluateFixture(t, b, samples...)
	last := out.Evaluations[2]
	if last.Results[5].State != "firing" || last.Results[2].State != "healthy" {
		t.Fatal("RPO age did not fire independently", last.Results)
	}
	for i := 3; i < 5; i++ {
		s := healthySample(fixtureAt.Add(time.Duration(i)*time.Minute), uint64(i+1))
		samples = append(samples, s)
	}
	out = evaluateFixture(t, b, samples...)
	if out.Evaluations[4].Results[5].State != "recovered" {
		t.Fatal("successful publication did not recover")
	}
	// A bad disk with a fresh completed backup is the opposite independent signal.
	b.Rules[2].FailureHold = 0
	s := healthySample(fixtureAt, 1)
	s.Disk.Value = 95
	out = evaluateFixture(t, b, s)
	if out.Evaluations[0].Results[2].State != "firing" || out.Evaluations[0].Results[5].State != "healthy" {
		t.Fatal("disk/backup conflated")
	}
}
func TestAlertsGapsResetBothHoldsAndPreserveIncident(t *testing.T) {
	b := bundleFixture(t)
	b.Rules[2].FailureHold = 5 * time.Minute
	first := healthySample(fixtureAt, 1)
	first.Disk.Value = 90
	second := healthySample(fixtureAt.Add(time.Hour), 2)
	second.Disk.Value = 90
	out := evaluateFixture(t, b, first, second)
	if out.Evaluations[1].Results[2].State != "pending" {
		t.Fatal("unobserved hour counted toward failure hold")
	}
	b.Rules[2].FailureHold = 0
	b.Rules[2].RecoveryHold = 5 * time.Minute
	good1 := healthySample(fixtureAt.Add(time.Minute), 2)
	good2 := healthySample(fixtureAt.Add(time.Hour), 3)
	out = evaluateFixture(t, b, first, good1, good2)
	last := out.Evaluations[2].Results[2]
	if last.State != "recovering" || !last.Incident {
		t.Fatal("unobserved recovery hour resolved incident", last)
	}
	// A stale or repeated observation resets recovery and cannot resolve it.
	stale := healthySample(fixtureAt.Add(2*time.Hour), 4)
	stale.Disk.At = fixtureAt
	out = evaluateFixture(t, b, first, good1, stale)
	last = out.Evaluations[2].Results[2]
	if last.State != "unavailable" || !last.Incident {
		t.Fatal("stale input recovered incident", last)
	}
	repeated := healthySample(fixtureAt.Add(2*time.Minute), 3)
	repeated.Disk.At = good1.Disk.At
	out = evaluateFixture(t, b, first, good1, repeated)
	if out.Evaluations[2].Results[2].State != "unavailable" {
		t.Fatal("repeated observation counted toward hold")
	}
}
func TestAlertsUnknownResetAndRangeHandling(t *testing.T) {
	b := bundleFixture(t)
	first := healthySample(fixtureAt, 10)
	second := healthySample(fixtureAt.Add(time.Minute), 1)
	out := evaluateFixture(t, b, first, second)
	for _, i := range []int{0, 1} {
		if out.Evaluations[1].Results[i].State != "unavailable" {
			t.Fatal("reset fabricated health")
		}
	}
	second = healthySample(fixtureAt.Add(time.Minute), 10)
	out = evaluateFixture(t, b, first, second)
	if out.Evaluations[1].Results[0].State != "unavailable" || out.Evaluations[1].Results[1].State != "unavailable" {
		t.Fatal("zero traffic fabricated health")
	}
	second = healthySample(fixtureAt.Add(time.Minute), 11)
	second.Pool.Max = 0
	second.Backup.Available = false
	second.Certificate.Available = false
	second.Disk.Available = false
	out = evaluateFixture(t, b, first, second)
	for _, i := range []int{2, 3, 5, 6} {
		if out.Evaluations[1].Results[i].State != "unavailable" {
			t.Fatal("missing supplemental/max0 fabricated health", i)
		}
	}
	if out.Evaluations[1].Results[4].Values[2] != 5 {
		t.Fatal("unknown/leased backlog hidden")
	}
	for name, mutate := range map[string]func(*Sample){"future observation": func(s *Sample) { s.Disk.At = s.At.Add(time.Second) }, "nan": func(s *Sample) { s.Disk.Value = math.NaN() }, "range": func(s *Sample) { s.Disk.Value = 101 }, "negative": func(s *Sample) { s.Disk.Value = -1 }, "future backup": func(s *Sample) { s.Backup.Value = s.At.Add(time.Hour) }, "invalid pool": func(s *Sample) { s.Pool.InUse = 11 }, "invalid counters": func(s *Sample) { s.HTTP.ServerErrors = s.HTTP.Completed + 1 }} {
		t.Run(name, func(t *testing.T) {
			s := healthySample(fixtureAt, 1)
			mutate(&s)
			if _, err := Evaluate(b, Samples{Version: SamplesVersion, Samples: []Sample{s}}); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	if _, err := Evaluate(b, Samples{Version: SamplesVersion, Samples: []Sample{first, first}}); err == nil {
		t.Fatal("out of order accepted")
	}
}
func TestAlertsStrictGrammarAndBounds(t *testing.T) {
	c := fixtureConfig()
	config, _ := json.Marshal(c)
	b := bundleFixture(t)
	definition, _ := Encode(b)
	s := Samples{Version: SamplesVersion, Samples: []Sample{healthySample(fixtureAt, 1)}}
	samples, _ := json.Marshal(s)
	if _, err := DecodeConfig(config); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSamples(samples); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"case alias":            []byte(strings.Replace(string(config), `"targets":`, `"Targets":`, 1)),
		"escaped duplicate":     []byte(strings.Replace(string(config), `"error_percent":5`, `"error_percent":5,"\u0065rror_percent":1`, 1)),
		"null":                  []byte(strings.Replace(string(config), `"targets":{`, `"targets":null,"replacement":{`, 1)),
		"float duration":        []byte(strings.Replace(string(config), `"freshness":"1m0s"`, `"freshness":0.5`, 1)),
		"alias after":           []byte(strings.Replace(string(config), `"profile":"aws_vm"`, `"profile":"aws_vm","Profile":"aws_managed"`, 1)),
		"extra":                 append(append([]byte{}, config...), []byte(` {}`)...),
		"oversized":             []byte(strings.Repeat(" ", MaxDefinitionBytes+1)),
		"missing explicit hold": []byte(strings.Replace(string(config), `"failure_hold":"2m0s",`, "", 1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeConfig(data); err == nil {
				t.Fatal("ambiguous config accepted")
			}
		})
	}
	for name, change := range map[string]func(*Bundle){"notify": func(b *Bundle) { b.NotificationsEnabled = true }, "unknown comparator": func(b *Bundle) { b.Rules[0].Comparison = "raw query" }, "unknown measurement": func(b *Bundle) { b.Rules[0].Measurement = "secret@private" }, "runbook url": func(b *Bundle) { b.Runbooks[0].Path = "https://example.invalid/" }, "unbounded role": func(b *Bundle) { b.Rules[0].OwnerRole = strings.Repeat("x", 65) }, "zero threshold": func(b *Bundle) { b.Rules[0].Threshold = 0 }, "invalid recovery": func(b *Bundle) { b.Rules[0].RecoveryCondition = "always" }} {
		t.Run(name, func(t *testing.T) {
			bad := b
			change(&bad)
			encoded, _ := json.Marshal(bad)
			if _, err := DecodeBundle(encoded); err == nil {
				t.Fatal("invalid artifact accepted")
			}
		})
	}
	// Go fixed-array decoding would otherwise silently ignore the eighth member.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(definition, &raw); err != nil {
		t.Fatal(err)
	}
	var rules []json.RawMessage
	if err := json.Unmarshal(raw["rules"], &rules); err != nil {
		t.Fatal(err)
	}
	rules = append(rules, rules[0])
	raw["rules"], _ = json.Marshal(rules)
	extra, _ := json.Marshal(raw)
	if _, err := DecodeBundle(extra); err == nil {
		t.Fatal("extra fixed array member accepted")
	}
	alias := strings.Replace(string(samples), `"available":true`, `"available":true,"Available":false`, 1)
	if _, err := DecodeSamples([]byte(alias)); err == nil {
		t.Fatal("sample alias accepted")
	}
	oversized := s
	oversized.Samples = make([]Sample, MaxSamples+1)
	data, _ := json.Marshal(oversized)
	if _, err := DecodeSamples(data); err == nil {
		t.Fatal("sample count exceeded")
	}
	if _, err := DecodeSamples([]byte(strings.Repeat(" ", MaxSampleBytes+1))); err == nil {
		t.Fatal("sample byte cap exceeded")
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.Targets = opsconfig.Defaults() }, func(c *Config) { c.Targets.AlertOwner = "" }, func(c *Config) { c.Options.Freshness = 0 }, func(c *Config) { c.Options.ErrorPercent = 100 }, func(c *Config) { c.Options.MeanLatencyMilliseconds = math.Inf(1) }, func(c *Config) { c.Options.CertificateLead = 91 * 24 * time.Hour }, func(c *Config) { c.Options.FailureHold = -1 }} {
		bad := c
		mutate(&bad)
		if _, err := Generate(bad); err == nil {
			t.Fatal("invalid/unselected target accepted")
		}
	}
}
func TestAlertsAdapterUsesActualNumericMetricsWithoutBackupInference(t *testing.T) {
	c, err := observability.New(observability.Config{RouteTemplates: []string{"/private"}, WorkerNames: []string{"private-worker"}})
	if err != nil {
		t.Fatal(err)
	}
	c.ObserveHTTP("/private", 503, "dependency.unavailable", 30*time.Millisecond)
	c.ObserveDatabase(sql.DBStats{OpenConnections: 3, InUse: 2, MaxOpenConnections: 10}, nil)
	now := time.Now().UTC()
	if err := c.ObserveQueue("private-worker", jobs.Snapshot{ObservedAt: now, Queued: 3, Unknown: 2, Leased: 1, OldestQueuedAge: 2 * time.Minute, LastSuccessKnown: true, LastSuccess: now}, nil); err != nil {
		t.Fatal(err)
	}
	snap := c.Snapshot()
	at := time.Now().UTC()
	s, err := FromMetrics(snap, at, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.HTTP.Completed != 1 || s.HTTP.ServerErrors != 1 || s.HTTP.DurationMilliseconds != 30 || s.Pool.InUse != 2 || s.Pool.Max != 10 || s.Queue.Queued != 3 || s.Queue.Unknown != 2 || s.Queue.Leased != 1 || s.Backup.Available || s.Disk.Available || s.Certificate.Available {
		t.Fatal("adapter fabricated supplemental measurements", s)
	}
	c.ObserveHTTP("/private", 200, "", 10*time.Millisecond)
	next, err := FromMetrics(c.Snapshot(), at.Add(time.Second), 0)
	if err != nil {
		t.Fatal(err)
	}
	b := bundleFixture(t)
	out := evaluateFixture(t, b, s, next)
	if got := out.Evaluations[1].Results[1].Values[0]; got != 10 {
		t.Fatal("mean latency delta wrong", got)
	}
	snap.Version = "unsupported"
	if _, err := FromMetrics(snap, at, 0); err == nil {
		t.Fatal("unsupported source accepted")
	}
}

func TestAlertsMaximumArtifactAndInclusivePressureBounds(t *testing.T) {
	c := fixtureConfig()
	c.Targets.AlertOwner = "A" + strings.Repeat("x", 63)
	c.Targets.DiskPercent = 100
	c.Options.PoolPercent = 100
	c.Options.QueuedDepth = 1 << 53
	c.Options.QueuedAge = 7 * 24 * time.Hour
	c.Options.CertificateLead = 90 * 24 * time.Hour
	c.Options.Freshness = 7 * 24 * time.Hour
	c.Options.FailureHold = 0
	c.Options.RecoveryHold = 7 * 24 * time.Hour
	b, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	body, err := Encode(b)
	if err != nil || len(body) > MaxDefinitionBytes {
		t.Fatal("maximum artifact exceeds cap", err)
	}
	s := healthySample(fixtureAt, 1)
	s.Disk.Value = 100
	s.Pool.InUse = s.Pool.Max
	out := evaluateFixture(t, b, s)
	for _, i := range []int{2, 3} {
		if out.Evaluations[0].Results[i].State != "firing" {
			t.Fatal("100 percent pressure failed to fire at configured boundary")
		}
	}
}

func TestAlertsQueueComparisonPreservesLargeIntegerBoundary(t *testing.T) {
	c := fixtureConfig()
	c.Options.QueuedDepth = 1 << 53
	c.Options.FailureHold = 0
	b, err := Generate(c)
	if err != nil {
		t.Fatal(err)
	}
	s := healthySample(fixtureAt, 1)
	s.Queue.Queued = 1<<53 + 1
	out := evaluateFixture(t, b, s)
	if out.Evaluations[0].Results[4].State != "firing" {
		t.Fatal("integer queue threshold crossing rounded into healthy state")
	}
}
