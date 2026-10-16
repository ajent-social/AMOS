// Package ops generates and evaluates finite, local-only operational alert definitions.
package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/ajent-social/amos/internal/opsconfig"
	"github.com/ajent-social/amos/observability"
)

const MaxDefinitionBytes = 64 << 10
const MaxSampleBytes = 1 << 20
const MaxSamples = 256
const Version = "amos-dashboard-alerts-v1"

var ErrInvalid = errors.New("invalid operations input")
var rolePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`)
var ids = [7]string{"service_errors", "service_latency", "disk_pressure", "database_pool", "queue_backlog", "backup_freshness", "certificate_expiry"}
var measurements = [7]string{"http_5xx_percent", "http_mean_latency_ms", "disk_percent", "pool_in_use_percent", "queued_depth_or_age_ms", "successful_backup_age_seconds", "certificate_remaining_seconds"}
var panelTitles = [7]string{"Service errors", "Service mean latency", "Disk pressure", "Database pool", "Queue backlog", "Backup freshness", "Certificate expiry"}
var panelUnits = [7]string{"percent", "milliseconds", "percent", "percent", "items_and_milliseconds", "seconds", "seconds"}
var comparisons = [7]string{"greater", "greater", "greater_or_equal", "greater_or_equal", "greater_or_age", "greater", "less_or_equal"}
var severities = [7]string{"critical", "warning", "critical", "warning", "warning", "critical", "warning"}
var runbookText = [7]string{
	"Inspect the private service error counters and readiness state. Identify the failing dependency using owner-local diagnostics. Apply the reviewed rollback or repair procedure. Confirm fresh error-rate windows stay at or below the configured threshold before closing the incident.",
	"Compare fresh mean latency windows with the configured threshold; this is not a percentile or SLO measurement. Inspect local resource and dependency contention. Apply the reviewed capacity or repair procedure and verify continuous recovery windows.",
	"Inspect free capacity and growth on the owner-selected data volume. Preserve databases, backups and authored data. Use the reviewed retention or capacity procedure; do not delete data automatically. Verify fresh disk usage remains below the threshold.",
	"Inspect configured maximum connections, in-use connections and wait counters. A zero or unknown maximum is unavailable. Find leaked or long-held transactions using private diagnostics. Repair the cause and verify fresh pool pressure remains below the threshold.",
	"Inspect queued depth and oldest queued age separately from unknown or leased work. Review bounded worker failure signals and reconciliation state. Preserve fencing and avoid replaying uncertain external effects. Verify both queued depth and age recover.",
	"Inspect the last successfully completed backup publication and its recovery manifest. Failed attempts and dump creation do not establish remote durability. Repair the backup path and complete a new verified publication. Verify freshness against the selected RPO; restore qualification remains separate.",
	"Inspect the owner-selected certificate expiry and renewal process. Use the reviewed renewal procedure without weakening identity or TLS checks. Confirm a fresh observed certificate exceeds the configured warning lead. An expired certificate remains adverse.",
}

// Config requires explicit owner targets. The CLI JSON adapter uses explicit duration strings.
// No destination or credential is accepted and no notification sender exists.
type Config struct {
	Targets opsconfig.Config `json:"targets"`
	Options Options          `json:"options"`
}
type Options struct {
	ErrorPercent            float64       `json:"error_percent"`
	MeanLatencyMilliseconds float64       `json:"mean_latency_ms"`
	PoolPercent             float64       `json:"pool_percent"`
	QueuedDepth             uint64        `json:"queued_depth"`
	QueuedAge               time.Duration `json:"queued_age"`
	CertificateLead         time.Duration `json:"certificate_lead"`
	Freshness               time.Duration `json:"freshness"`
	FailureHold             time.Duration `json:"failure_hold"`
	RecoveryHold            time.Duration `json:"recovery_hold"`
}
type Rule struct {
	ID                 string        `json:"id"`
	Measurement        string        `json:"measurement"`
	Comparison         string        `json:"comparison"`
	Threshold          float64       `json:"threshold"`
	SecondaryThreshold float64       `json:"secondary_threshold"`
	FailureHold        time.Duration `json:"failure_hold_ns"`
	Severity           string        `json:"severity"`
	OwnerRole          string        `json:"owner_role"`
	Runbook            string        `json:"runbook"`
	RecoveryCondition  string        `json:"recovery_condition"`
	RecoveryHold       time.Duration `json:"recovery_hold_ns"`
}
type Panel struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Measurement string `json:"measurement"`
	Unit        string `json:"unit"`
	RuleID      string `json:"rule_id"`
}
type Runbook struct {
	Path string `json:"path"`
	Text string `json:"text"`
}
type Bundle struct {
	Version              string            `json:"version"`
	Profile              opsconfig.Profile `json:"profile"`
	Activated            bool              `json:"activated"`
	NotificationsEnabled bool              `json:"notifications_enabled"`
	Freshness            time.Duration     `json:"freshness_ns"`
	Panels               [7]Panel          `json:"panels"`
	Rules                [7]Rule           `json:"rules"`
	Runbooks             [7]Runbook        `json:"runbooks"`
}

func validFloat(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func positive(v float64) bool   { return validFloat(v) && v > 0 }
func duration(v time.Duration, zero bool) bool {
	return v >= 0 && (zero || v > 0) && v <= 7*24*time.Hour
}
func Generate(c Config) (Bundle, error) {
	if c.Targets.Validate() != nil || !rolePattern.MatchString(c.Targets.AlertOwner) || !rolePattern.MatchString(c.Targets.RecoveryOwner) || !rolePattern.MatchString(c.Targets.IncidentOwner) {
		return Bundle{}, ErrInvalid
	}
	o := c.Options
	if !positive(o.ErrorPercent) || o.ErrorPercent >= 100 || !positive(o.MeanLatencyMilliseconds) || o.MeanLatencyMilliseconds > float64(7*24*time.Hour/time.Millisecond) || !positive(o.PoolPercent) || o.PoolPercent > 100 || o.QueuedDepth == 0 || o.QueuedDepth > 1<<53 || !duration(o.QueuedAge, false) || o.CertificateLead <= 0 || o.CertificateLead > 90*24*time.Hour || !duration(o.Freshness, false) || !duration(o.FailureHold, true) || !duration(o.RecoveryHold, true) {
		return Bundle{}, ErrInvalid
	}
	b := Bundle{Version: Version, Profile: c.Targets.Profile, Activated: c.Targets.Activated, Freshness: o.Freshness}
	thresholds := [7]float64{o.ErrorPercent, o.MeanLatencyMilliseconds, c.Targets.DiskPercent, o.PoolPercent, float64(o.QueuedDepth), c.Targets.RPO.Seconds(), o.CertificateLead.Seconds()}
	for i, id := range ids {
		b.Panels[i] = Panel{ID: id, Title: panelTitles[i], Measurement: measurements[i], Unit: panelUnits[i], RuleID: id}
		path := "runbooks/" + id + ".md"
		b.Rules[i] = Rule{ID: id, Measurement: measurements[i], Comparison: comparisons[i], Threshold: thresholds[i], FailureHold: o.FailureHold, Severity: severities[i], OwnerRole: c.Targets.AlertOwner, Runbook: path, RecoveryCondition: "not_adverse", RecoveryHold: o.RecoveryHold}
		b.Runbooks[i] = Runbook{Path: path, Text: runbookText[i]}
	}
	b.Rules[4].SecondaryThreshold = float64(o.QueuedAge) / float64(time.Millisecond)
	if Validate(b) != nil {
		return Bundle{}, ErrInvalid
	}
	return b, nil
}
func Validate(b Bundle) error {
	if b.Version != Version || (b.Profile != opsconfig.ProfileAWSManaged && b.Profile != opsconfig.ProfileAWSVM) || b.NotificationsEnabled || !duration(b.Freshness, false) {
		return ErrInvalid
	}
	for i, r := range b.Rules {
		if b.Panels[i] != (Panel{ID: ids[i], Title: panelTitles[i], Measurement: measurements[i], Unit: panelUnits[i], RuleID: ids[i]}) {
			return ErrInvalid
		}
		if r.ID != ids[i] || r.Measurement != measurements[i] || r.Comparison != comparisons[i] || !positive(r.Threshold) || !validFloat(r.SecondaryThreshold) || !duration(r.FailureHold, true) || !duration(r.RecoveryHold, true) || (r.Severity != "warning" && r.Severity != "critical") || !rolePattern.MatchString(r.OwnerRole) || r.Runbook != "runbooks/"+ids[i]+".md" || r.RecoveryCondition != "not_adverse" || b.Runbooks[i].Path != r.Runbook || b.Runbooks[i].Text != runbookText[i] {
			return ErrInvalid
		}
		switch i {
		case 0:
			if r.Threshold >= 100 {
				return ErrInvalid
			}
		case 1:
			if r.Threshold > float64(7*24*time.Hour/time.Millisecond) {
				return ErrInvalid
			}
		case 2, 3:
			if r.Threshold > 100 {
				return ErrInvalid
			}
		case 4:
			if r.Threshold > 1<<53 || math.Trunc(r.Threshold) != r.Threshold || !positive(r.SecondaryThreshold) || r.SecondaryThreshold > float64(7*24*time.Hour/time.Millisecond) {
				return ErrInvalid
			}
		case 5:
			if r.Threshold > time.Duration(math.MaxInt64).Seconds() {
				return ErrInvalid
			}
		case 6:
			if r.Threshold > 90*24*time.Hour.Seconds() {
				return ErrInvalid
			}
		}
		if i != 4 && r.SecondaryThreshold != 0 {
			return ErrInvalid
		}
	}
	body, err := json.Marshal(b)
	if err != nil || len(body) > MaxDefinitionBytes {
		return ErrInvalid
	}
	return nil
}
func Encode(b Bundle) ([]byte, error) {
	if Validate(b) != nil {
		return nil, ErrInvalid
	}
	return json.Marshal(b)
}

type wireTargets struct {
	Profile         opsconfig.Profile `json:"profile"`
	Activated       bool              `json:"activated"`
	AvailabilitySLO float64           `json:"availability_slo"`
	RPO             string            `json:"rpo"`
	RTO             string            `json:"rto"`
	Retention       string            `json:"retention"`
	BackupInterval  string            `json:"backup_interval"`
	CPUPercent      float64           `json:"cpu_percent"`
	MemoryPercent   float64           `json:"memory_percent"`
	DiskPercent     float64           `json:"disk_percent"`
	AlertOwner      string            `json:"alert_owner"`
	RecoveryOwner   string            `json:"recovery_owner"`
	IncidentOwner   string            `json:"incident_owner"`
}
type wireOptions struct {
	ErrorPercent            float64 `json:"error_percent"`
	MeanLatencyMilliseconds float64 `json:"mean_latency_ms"`
	PoolPercent             float64 `json:"pool_percent"`
	QueuedDepth             uint64  `json:"queued_depth"`
	QueuedAge               string  `json:"queued_age"`
	CertificateLead         string  `json:"certificate_lead"`
	Freshness               string  `json:"freshness"`
	FailureHold             string  `json:"failure_hold"`
	RecoveryHold            string  `json:"recovery_hold"`
}
type wireConfig struct {
	Targets wireTargets `json:"targets"`
	Options wireOptions `json:"options"`
}

// MarshalJSON emits a readable exact-key CLI configuration with duration strings.
func (c Config) MarshalJSON() ([]byte, error) {
	t, o := c.Targets, c.Options
	return json.Marshal(wireConfig{Targets: wireTargets{Profile: t.Profile, Activated: t.Activated, AvailabilitySLO: t.AvailabilitySLO, RPO: t.RPO.String(), RTO: t.RTO.String(), Retention: t.Retention.String(), BackupInterval: t.BackupInterval.String(), CPUPercent: t.CPUPercent, MemoryPercent: t.MemoryPercent, DiskPercent: t.DiskPercent, AlertOwner: t.AlertOwner, RecoveryOwner: t.RecoveryOwner, IncidentOwner: t.IncidentOwner}, Options: wireOptions{ErrorPercent: o.ErrorPercent, MeanLatencyMilliseconds: o.MeanLatencyMilliseconds, PoolPercent: o.PoolPercent, QueuedDepth: o.QueuedDepth, QueuedAge: o.QueuedAge.String(), CertificateLead: o.CertificateLead.String(), Freshness: o.Freshness.String(), FailureHold: o.FailureHold.String(), RecoveryHold: o.RecoveryHold.String()}})
}
func DecodeConfig(data []byte) (Config, error) {
	var wire wireConfig
	if strict(data, &wire, MaxDefinitionBytes) != nil {
		return Config{}, ErrInvalid
	}
	t, o := wire.Targets, wire.Options
	c := Config{Targets: opsconfig.Config{Profile: t.Profile, Activated: t.Activated, AvailabilitySLO: t.AvailabilitySLO, CPUPercent: t.CPUPercent, MemoryPercent: t.MemoryPercent, DiskPercent: t.DiskPercent, AlertOwner: t.AlertOwner, RecoveryOwner: t.RecoveryOwner, IncidentOwner: t.IncidentOwner}, Options: Options{ErrorPercent: o.ErrorPercent, MeanLatencyMilliseconds: o.MeanLatencyMilliseconds, PoolPercent: o.PoolPercent, QueuedDepth: o.QueuedDepth}}
	for _, pair := range []struct {
		input string
		out   *time.Duration
	}{{t.RPO, &c.Targets.RPO}, {t.RTO, &c.Targets.RTO}, {t.Retention, &c.Targets.Retention}, {t.BackupInterval, &c.Targets.BackupInterval}, {o.QueuedAge, &c.Options.QueuedAge}, {o.CertificateLead, &c.Options.CertificateLead}, {o.Freshness, &c.Options.Freshness}, {o.FailureHold, &c.Options.FailureHold}, {o.RecoveryHold, &c.Options.RecoveryHold}} {
		if len(pair.input) > 64 {
			return Config{}, ErrInvalid
		}
		v, err := time.ParseDuration(pair.input)
		if err != nil {
			return Config{}, ErrInvalid
		}
		*pair.out = v
	}
	if _, err := Generate(c); err != nil {
		return Config{}, ErrInvalid
	}
	return c, nil
}
func DecodeBundle(data []byte) (Bundle, error) {
	var b Bundle
	if strict(data, &b, MaxDefinitionBytes) != nil || Validate(b) != nil {
		return Bundle{}, ErrInvalid
	}
	return b, nil
}

// strict first validates the decoded exact grammar, including fixed array size,
// key aliases/duplicates, null and scalar types, before Go struct decoding.
func strict(data []byte, out any, limit int) error {
	if len(data) == 0 || len(data) > limit {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if grammar(d, reflect.TypeOf(out).Elem(), 0) != nil {
		return ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return ErrInvalid
	}
	return nil
}

var timeType = reflect.TypeOf(time.Time{})

func grammar(d *json.Decoder, t reflect.Type, depth int) error {
	if depth > 12 {
		return ErrInvalid
	}
	token, err := d.Token()
	if err != nil || token == nil {
		return ErrInvalid
	}
	if t == timeType {
		s, ok := token.(string)
		if !ok || len(s) > 35 || !strings.HasSuffix(s, "Z") {
			return ErrInvalid
		}
		if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
			return ErrInvalid
		}
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		if token != json.Delim('{') {
			return ErrInvalid
		}
		fields := make(map[string]reflect.Type, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "" {
				name = f.Name
			}
			fields[name] = f.Type
		}
		seen := make(map[string]bool, len(fields))
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return ErrInvalid
			}
			name, ok := key.(string)
			typ, exists := fields[name]
			if !ok || !exists || seen[name] {
				return ErrInvalid
			}
			seen[name] = true
			if grammar(d, typ, depth+1) != nil {
				return ErrInvalid
			}
		}
		if len(seen) != len(fields) {
			return ErrInvalid
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalid
		}
	case reflect.Array, reflect.Slice:
		if token != json.Delim('[') {
			return ErrInvalid
		}
		n := 0
		for d.More() {
			n++
			if n > MaxSamples || (t.Kind() == reflect.Array && n > t.Len()) {
				return ErrInvalid
			}
			if grammar(d, t.Elem(), depth+1) != nil {
				return ErrInvalid
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') || (t.Kind() == reflect.Array && n != t.Len()) {
			return ErrInvalid
		}
	case reflect.String:
		s, ok := token.(string)
		if !ok || len(s) > 4096 {
			return ErrInvalid
		}
	case reflect.Bool:
		if _, ok := token.(bool); !ok {
			return ErrInvalid
		}
	case reflect.Float64:
		if _, ok := token.(json.Number); !ok {
			return ErrInvalid
		}
	case reflect.Int64, reflect.Int, reflect.Uint64:
		if _, ok := token.(json.Number); !ok {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// Every observation is local input, not proof of its collection or authenticity.
type HTTPObservation struct {
	At                   time.Time `json:"at"`
	Available            bool      `json:"available"`
	Completed            uint64    `json:"completed"`
	ServerErrors         uint64    `json:"server_errors"`
	DurationCount        uint64    `json:"duration_count"`
	DurationMilliseconds uint64    `json:"duration_sum_ms"`
}
type PoolObservation struct {
	At        time.Time `json:"at"`
	Available bool      `json:"available"`
	InUse     uint64    `json:"in_use"`
	Max       uint64    `json:"max"`
}
type QueueObservation struct {
	At              time.Time `json:"at"`
	Available       bool      `json:"available"`
	Queued          uint64    `json:"queued"`
	AgeMilliseconds uint64    `json:"age_ms"`
	Unknown         uint64    `json:"unknown"`
	Leased          uint64    `json:"leased"`
}
type ValueObservation struct {
	At        time.Time `json:"at"`
	Available bool      `json:"available"`
	Value     float64   `json:"value"`
}
type TimeObservation struct {
	At        time.Time `json:"at"`
	Available bool      `json:"available"`
	Value     time.Time `json:"value"`
}
type Sample struct {
	At          time.Time        `json:"at"`
	HTTP        HTTPObservation  `json:"http"`
	Pool        PoolObservation  `json:"pool"`
	Queue       QueueObservation `json:"queue"`
	Disk        ValueObservation `json:"disk"`
	Backup      TimeObservation  `json:"backup"`
	Certificate TimeObservation  `json:"certificate"`
}
type Samples struct {
	Version string   `json:"version"`
	Samples []Sample `json:"samples"`
}

const SamplesVersion = "amos-alert-samples-v1"

func DecodeSamples(data []byte) (Samples, error) {
	var s Samples
	if strict(data, &s, MaxSampleBytes) != nil || s.Version != SamplesVersion || len(s.Samples) == 0 || len(s.Samples) > MaxSamples {
		return Samples{}, ErrInvalid
	}
	return s, nil
}

type Result struct {
	ID        string     `json:"id"`
	State     string     `json:"state"`
	Incident  bool       `json:"incident"`
	Available bool       `json:"available"`
	Values    [3]float64 `json:"values"`
}
type Evaluation struct {
	At      time.Time `json:"at"`
	Results [7]Result `json:"results"`
}
type Output struct {
	Version              string       `json:"version"`
	NotificationsEnabled bool         `json:"notifications_enabled"`
	Evaluations          []Evaluation `json:"evaluations"`
}
type incident struct {
	active                                       bool
	pending, recovery, lastEval, lastObservation time.Time
}

func fresh(at, now time.Time, available bool, limit time.Duration) bool {
	return available && !at.IsZero() && !at.After(now) && now.Sub(at) <= limit
}
func validateSample(s Sample) error {
	if s.At.IsZero() {
		return ErrInvalid
	}
	for _, at := range []time.Time{s.HTTP.At, s.Pool.At, s.Queue.At, s.Disk.At, s.Backup.At, s.Certificate.At} {
		if at.After(s.At) {
			return ErrInvalid
		}
	}
	if !validFloat(s.Disk.Value) || s.Disk.Value < 0 || s.Disk.Value > 100 || s.HTTP.ServerErrors > s.HTTP.Completed || s.Pool.InUse > s.Pool.Max && s.Pool.Max > 0 || s.Backup.Available && (s.Backup.Value.IsZero() || s.Backup.Value.After(s.Backup.At)) || s.Certificate.Available && s.Certificate.Value.IsZero() {
		return ErrInvalid
	}
	return nil
}
func Evaluate(b Bundle, input Samples) (Output, error) {
	if Validate(b) != nil || input.Version != SamplesVersion || len(input.Samples) < 1 || len(input.Samples) > MaxSamples {
		return Output{}, ErrInvalid
	}
	result := Output{Version: "amos-alert-evaluation-v1", Evaluations: make([]Evaluation, 0, len(input.Samples))}
	var states [7]incident
	var previous Sample
	for j, s := range input.Samples {
		if validateSample(s) != nil || j > 0 && !s.At.After(previous.At) {
			return Output{}, ErrInvalid
		}
		evaluation := Evaluation{At: s.At}
		for i, r := range b.Rules {
			values, observed, available := measure(i, s, previous, j > 0, b.Freshness)
			st := &states[i]
			out := Result{ID: r.ID, State: "disabled", Incident: st.active, Available: available, Values: values}
			if !b.Activated {
				evaluation.Results[i] = out
				continue
			}
			gap := !st.lastEval.IsZero() && (s.At.Sub(st.lastEval) > b.Freshness || observed.Sub(st.lastObservation) > b.Freshness)
			if gap {
				st.pending = time.Time{}
				st.recovery = time.Time{}
			}
			if !available || !st.lastObservation.IsZero() && !observed.After(st.lastObservation) {
				st.pending = time.Time{}
				st.recovery = time.Time{}
				out.State = "unavailable"
				out.Available = false
				evaluation.Results[i] = out
				continue
			}
			st.lastEval = s.At
			st.lastObservation = observed
			adverse := compare(r, values, s.Queue.Queued)
			if adverse {
				st.recovery = time.Time{}
				if st.active {
					out.State = "firing"
				} else {
					if st.pending.IsZero() {
						st.pending = s.At
					}
					out.State = "pending"
					if s.At.Sub(st.pending) >= r.FailureHold {
						st.active = true
						out.State = "firing"
					}
				}
			} else {
				st.pending = time.Time{}
				if !st.active {
					out.State = "healthy"
				} else {
					if st.recovery.IsZero() {
						st.recovery = s.At
					}
					out.State = "recovering"
					if s.At.Sub(st.recovery) >= r.RecoveryHold {
						st.active = false
						st.recovery = time.Time{}
						out.State = "recovered"
					}
				}
			}
			out.Incident = st.active
			evaluation.Results[i] = out
		}
		result.Evaluations = append(result.Evaluations, evaluation)
		previous = s
	}
	return result, nil
}
func compare(r Rule, v [3]float64, queued uint64) bool {
	switch r.Comparison {
	case "greater":
		return v[0] > r.Threshold
	case "greater_or_equal":
		return v[0] >= r.Threshold
	case "greater_or_age":
		// The generated depth threshold is validated as an exact uint64 integer.
		// Compare the source count before any float conversion used for display.
		return queued > uint64(r.Threshold) || v[1] > r.SecondaryThreshold
	case "less_or_equal":
		return v[0] <= r.Threshold
	}
	return false
}
func measure(i int, s, p Sample, hasPrevious bool, freshness time.Duration) ([3]float64, time.Time, bool) {
	var v [3]float64
	switch i {
	case 0, 1:
		h := s.HTTP
		old := p.HTTP
		ok := fresh(h.At, s.At, h.Available, freshness) && hasPrevious && fresh(old.At, s.At, old.Available, freshness) && h.At.After(old.At) && h.Completed >= old.Completed && h.ServerErrors >= old.ServerErrors && h.DurationCount >= old.DurationCount && h.DurationMilliseconds >= old.DurationMilliseconds
		if !ok {
			return v, h.At, false
		}
		if i == 0 {
			total := h.Completed - old.Completed
			fail := h.ServerErrors - old.ServerErrors
			if total == 0 || fail > total {
				return v, h.At, false
			}
			v[0] = 100 * float64(fail) / float64(total)
		} else {
			count := h.DurationCount - old.DurationCount
			if count == 0 {
				return v, h.At, false
			}
			v[0] = float64(h.DurationMilliseconds-old.DurationMilliseconds) / float64(count)
		}
		return v, h.At, true
	case 2:
		v[0] = s.Disk.Value
		return v, s.Disk.At, fresh(s.Disk.At, s.At, s.Disk.Available, freshness)
	case 3:
		if s.Pool.Max == 0 {
			return v, s.Pool.At, false
		}
		v[0] = 100 * float64(s.Pool.InUse) / float64(s.Pool.Max)
		return v, s.Pool.At, fresh(s.Pool.At, s.At, s.Pool.Available, freshness)
	case 4:
		v = [3]float64{float64(s.Queue.Queued), float64(s.Queue.AgeMilliseconds), float64(s.Queue.Unknown) + float64(s.Queue.Leased)}
		return v, s.Queue.At, fresh(s.Queue.At, s.At, s.Queue.Available, freshness)
	case 5:
		v[0] = s.At.Sub(s.Backup.Value).Seconds()
		return v, s.Backup.At, fresh(s.Backup.At, s.At, s.Backup.Available, freshness)
	case 6:
		v[0] = s.Certificate.Value.Sub(s.At).Seconds()
		return v, s.Certificate.At, fresh(s.Certificate.At, s.At, s.Certificate.Available, freshness)
	}
	return v, time.Time{}, false
}

// FromMetrics derives only HTTP/pool/queue signals. The caller supplies trusted
// local observation time and a configured worker ordinal; supplemental inputs
// remain unavailable until explicitly collected by the caller.
func FromMetrics(s observability.Snapshot, at time.Time, worker int) (Sample, error) {
	if s.Version != "amos-metrics-v1" || at.IsZero() || len(s.HTTP) > 34 || len(s.Workers) > 16 || worker < 0 || worker >= len(s.Workers) {
		return Sample{}, ErrInvalid
	}
	out := Sample{At: at, HTTP: HTTPObservation{At: at, Available: true}}
	add := func(a *uint64, b uint64) {
		if math.MaxUint64-*a <= b {
			out.HTTP.Available = false
			return
		}
		*a += b
	}
	for _, h := range s.HTTP {
		for i := 1; i <= 4; i++ {
			add(&out.HTTP.Completed, h.Status[i])
		}
		add(&out.HTTP.ServerErrors, h.Status[4])
		add(&out.HTTP.DurationCount, h.DurationCount)
		add(&out.HTTP.DurationMilliseconds, h.DurationMilliseconds)
	}
	p := s.Database
	if p.Open < 0 || p.InUse < 0 || p.Max < 0 {
		return Sample{}, ErrInvalid
	}
	out.Pool = PoolObservation{At: p.ObservedAt, Available: p.State == "observed", InUse: uint64(p.InUse), Max: uint64(p.Max)}
	q := s.Workers[worker].Queue
	out.Queue = QueueObservation{At: q.ObservedAt, Available: q.State == "observed", Queued: q.Queued, AgeMilliseconds: q.OldestQueuedMilliseconds, Unknown: q.Unknown, Leased: q.Leased}
	if validateSample(out) != nil {
		return Sample{}, ErrInvalid
	}
	return out, nil
}
