package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ajent-social/amos/internal/opsconfig"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/google/uuid"
)

const MaxScheduleConfigBytes = 64 << 10

var scheduleRolePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
var scheduleOwnerPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`)
var scheduleEnvPattern = regexp.MustCompile(`^env://[A-Z_][A-Z0-9_]{0,63}$`)
var scheduleAWSRolePattern = regexp.MustCompile(`^role/[A-Za-z0-9+=,.@_-]{1,64}$`)
var scheduleRegionPattern = regexp.MustCompile(`^(?:[a-z]{2}-[a-z]+-[1-9][0-9]?|[a-z]{2}-gov-[a-z]+-[1-9][0-9]?)$`)
var scheduleAccountPattern = regexp.MustCompile(`^[0-9]{12}$`)

// ValidateScheduleConfig performs structural checks only, including inactive
// render configurations. RunDue separately requires explicit activation.
func ValidateScheduleConfig(c ScheduleConfig) error {
	if c.Version != ScheduleConfigVersion || !scheduleUUID(c.ScheduleID) || !scheduleUUID(c.InstallationID) || !scheduleUUID(c.ApplicationID) || (c.Environment != "development" && c.Environment != "staging" && c.Environment != "production") || c.Targets.Validate() != nil {
		return ErrScheduleConfig
	}
	if !scheduleOwnerPattern.MatchString(c.Targets.AlertOwner) || !scheduleOwnerPattern.MatchString(c.Targets.RecoveryOwner) || !scheduleOwnerPattern.MatchString(c.Targets.IncidentOwner) || c.Targets.BackupInterval < time.Minute || c.Targets.BackupInterval > 7*24*time.Hour || c.Targets.BackupInterval%time.Minute != 0 {
		return ErrScheduleConfig
	}
	if c.RunTimeout <= 0 || c.RunTimeout > time.Hour || c.Create.DumpTimeout <= 0 || c.UploadTimeout <= 0 || c.Create.DumpTimeout > c.RunTimeout || c.UploadTimeout > c.RunTimeout || c.Create.DumpTimeout+c.UploadTimeout+15*time.Second > c.RunTimeout {
		return ErrScheduleConfig
	}
	if c.Create.MaxBytes < 1 || c.Create.MaxBytes > 5<<30 || c.MaxRetainedAttempts < 1 || c.MaxRetainedAttempts > 4096 || c.MaxStagingArtifacts < 1 || c.MaxStagingArtifacts > 4096 || c.MaxStagingBytes < c.Create.MaxBytes+2<<20 || c.MaxStagingBytes > 1<<40 {
		return ErrScheduleConfig
	}
	if !schedulePath(c.Create.StagingDir) || !schedulePath(c.Create.PGDumpPath) || !validVersion(c.Create.PGDumpVersion) || len(c.Create.PGDumpVersion) > 32 || len(c.Create.ModuleVersions) < 1 || len(c.Create.ModuleVersions) > 64 {
		return ErrScheduleConfig
	}
	for k, v := range c.Create.ModuleVersions {
		if !validModuleID(k) || len(v) < 1 || len(v) > 128 || strings.TrimSpace(v) != v || strings.ContainsAny(v, "\r\n\x00") {
			return ErrScheduleConfig
		}
	}
	if ValidateScheduleAWSConfig(c.AWS) != nil {
		return ErrScheduleConfig
	}
	expectedKind := "imds_v2"
	if c.Targets.Profile == opsconfig.ProfileAWSManaged {
		expectedKind = "ecs_task"
	}
	if c.AWS.CredentialKind != expectedKind || c.AWS.Prefix != "installations/"+c.InstallationID.String()+"/"+c.Environment {
		return ErrScheduleConfig
	}
	d := c.Database
	refs := []string{d.LedgerWriteRef, d.LedgerReadRef, d.DumpRef}
	roles := []string{d.LedgerWriteRole, d.LedgerReadRole, d.DumpRole}
	for i, r := range refs {
		if !scheduleEnvPattern.MatchString(r) || strings.HasPrefix(strings.TrimPrefix(r, "env://"), "PG") {
			return ErrScheduleConfig
		}
		for j := 0; j < i; j++ {
			if r == refs[j] {
				return ErrScheduleConfig
			}
		}
	}
	for i, r := range roles {
		if !scheduleRolePattern.MatchString(r) {
			return ErrScheduleConfig
		}
		for j := 0; j < i; j++ {
			if r == roles[j] {
				return ErrScheduleConfig
			}
		}
	}
	if !scheduleHost(d.LedgerHost) || !scheduleHost(d.DumpHost) || d.LedgerPort == 0 || d.DumpPort == 0 || !scheduleRolePattern.MatchString(d.LedgerDatabase) || !scheduleRolePattern.MatchString(d.DumpDatabase) || d.LedgerDatabase == d.DumpDatabase || !schedulePath(d.LedgerRootCAFile) || !schedulePath(d.DumpRootCAFile) {
		return ErrScheduleConfig
	}
	return nil
}
func scheduleUUID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
func schedulePath(s string) bool {
	if len(s) < 2 || len(s) > 512 || !filepath.IsAbs(s) || filepath.Clean(s) != s {
		return false
	}
	for _, c := range []byte(s) {
		allowed := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '/' || c == '.' || c == '_' || c == '-'
		if !allowed {
			return false
		}
	}
	return true
}
func scheduleHost(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	if a, e := netip.ParseAddr(s); e == nil {
		return a.String() == s && !a.Is4In6() && a.Zone() == "" && (a.IsGlobalUnicast() || a.IsLoopback() || a.IsLinkLocalUnicast())
	}
	if strings.Trim(s, "0123456789.") == "" || s != strings.ToLower(s) {
		return false
	}
	for _, p := range strings.Split(s, ".") {
		if len(p) == 0 || len(p) > 63 || p[0] == '-' || p[len(p)-1] == '-' {
			return false
		}
		for _, b := range []byte(p) {
			allowed := b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-'
			if !allowed {
				return false
			}
		}
	}
	return true
}
func ValidateScheduleAWSConfig(c ScheduleAWSConfig) error {
	if c.CredentialKind != "imds_v2" && c.CredentialKind != "ecs_task" || !scheduleAccountPattern.MatchString(c.AccountID) || c.BucketAccountID != c.AccountID || !scheduleRegionPattern.MatchString(c.Region) || !validStoreBucket(c.Bucket) || !validStorePrefix(c.Prefix) || strings.Trim(c.Prefix, "/") != c.Prefix || len(c.Prefix) > 512 || len(c.RuntimeRoleARN) > 2048 || len(c.KMSKeyARN) > 2048 {
		return ErrScheduleConfig
	}
	role, e := arn.Parse(c.RuntimeRoleARN)
	if e != nil || role.Service != "iam" || role.Region != "" || role.AccountID != c.AccountID || !scheduleAWSRolePattern.MatchString(role.Resource) {
		return ErrScheduleConfig
	}
	kms, e := arn.Parse(c.KMSKeyARN)
	if e != nil || kms.Service != "kms" || kms.AccountID != c.BucketAccountID || kms.Region != c.Region || !storeKMSKeyPattern.MatchString(c.KMSKeyARN) || kms.Partition != role.Partition {
		return ErrScheduleConfig
	}
	switch role.Partition {
	case "aws":
		if strings.HasPrefix(c.Region, "cn-") || strings.Contains(c.Region, "-gov-") {
			return ErrScheduleConfig
		}
	case "aws-cn":
		if !strings.HasPrefix(c.Region, "cn-") {
			return ErrScheduleConfig
		}
	case "aws-us-gov":
		if !strings.Contains(c.Region, "-gov-") {
			return ErrScheduleConfig
		}
	default:
		return ErrScheduleConfig
	}
	return nil
}

type ScheduleDatabaseKind string

const (
	ScheduleLedgerWrite ScheduleDatabaseKind = "ledger_write"
	ScheduleLedgerRead  ScheduleDatabaseKind = "ledger_read"
	ScheduleDump        ScheduleDatabaseKind = "dump"
)

// ValidateScheduleDatabaseURL rejects ambient PostgreSQL selection and requires
// a complete explicit target/authentication/TLS URL. No values enter diagnostics.
func ValidateScheduleDatabaseURL(c ScheduleDatabaseConfig, kind ScheduleDatabaseKind, raw string) error {
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PG") {
			return ErrScheduleConfig
		}
	}
	if len(raw) == 0 || len(raw) > 65536 {
		return ErrScheduleConfig
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "postgres" && u.Scheme != "postgresql" || u.Opaque != "" || u.Fragment != "" || u.User == nil {
		return ErrScheduleConfig
	}
	host, port, db, role, ca := c.LedgerHost, c.LedgerPort, c.LedgerDatabase, c.LedgerWriteRole, c.LedgerRootCAFile
	switch kind {
	case ScheduleLedgerWrite:
	case ScheduleLedgerRead:
		role = c.LedgerReadRole
	case ScheduleDump:
		host, port, db, role, ca = c.DumpHost, c.DumpPort, c.DumpDatabase, c.DumpRole, c.DumpRootCAFile
	default:
		return ErrScheduleConfig
	}
	if u.Host != net.JoinHostPort(host, strconv.Itoa(int(port))) || u.Path != "/"+db || u.User.Username() != role {
		return ErrScheduleConfig
	}
	pwd, ok := u.User.Password()
	if !ok || pwd == "" || strings.ContainsAny(pwd, "\r\n\x00") {
		return ErrScheduleConfig
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return ErrScheduleConfig
	}
	for k, v := range q {
		if len(v) != 1 || k != "sslmode" && k != "sslrootcert" && k != "connect_timeout" && k != "sslcert" && k != "sslkey" {
			return ErrScheduleConfig
		}
	}
	if _, ok := q["sslcert"]; ok {
		if q.Get("sslcert") != "" || q.Get("sslkey") != "" || len(q["sslkey"]) != 1 {
			return ErrScheduleConfig
		}
	} else if _, ok := q["sslkey"]; ok {
		return ErrScheduleConfig
	}
	if q.Get("sslmode") != "verify-full" || q.Get("sslrootcert") != ca {
		return ErrScheduleConfig
	}
	if v := q.Get("connect_timeout"); v != "" && v != "1" && v != "2" {
		return ErrScheduleConfig
	}
	return nil
}

type scheduleWireTargets struct {
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
type scheduleWireDatabase struct {
	LedgerWriteRef   string `json:"ledger_write_ref"`
	LedgerReadRef    string `json:"ledger_read_ref"`
	DumpRef          string `json:"dump_ref"`
	LedgerWriteRole  string `json:"ledger_write_role"`
	LedgerReadRole   string `json:"ledger_read_role"`
	DumpRole         string `json:"dump_role"`
	LedgerHost       string `json:"ledger_host"`
	LedgerDatabase   string `json:"ledger_database"`
	LedgerRootCAFile string `json:"ledger_root_ca_file"`
	DumpHost         string `json:"dump_host"`
	DumpDatabase     string `json:"dump_database"`
	DumpRootCAFile   string `json:"dump_root_ca_file"`
	LedgerPort       uint16 `json:"ledger_port"`
	DumpPort         uint16 `json:"dump_port"`
}
type scheduleWireAWS struct {
	CredentialKind  string `json:"credential_kind"`
	AccountID       string `json:"account_id"`
	Region          string `json:"region"`
	RuntimeRoleARN  string `json:"runtime_role_arn"`
	BucketAccountID string `json:"bucket_account_id"`
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"`
	KMSKeyARN       string `json:"kms_key_arn"`
}
type scheduleWireCreate struct {
	StagingDir     string            `json:"staging_dir"`
	PGDumpPath     string            `json:"pg_dump_path"`
	PGDumpVersion  string            `json:"pg_dump_version"`
	ModuleVersions map[string]string `json:"module_versions"`
	MaxBytes       int64             `json:"max_bytes"`
	DumpTimeout    string            `json:"dump_timeout"`
}
type scheduleWire struct {
	Version             string               `json:"version"`
	ScheduleID          string               `json:"schedule_id"`
	InstallationID      string               `json:"installation_id"`
	ApplicationID       string               `json:"application_id"`
	Environment         string               `json:"environment"`
	Targets             scheduleWireTargets  `json:"targets"`
	Database            scheduleWireDatabase `json:"database"`
	AWS                 scheduleWireAWS      `json:"aws"`
	Create              scheduleWireCreate   `json:"create"`
	UploadTimeout       string               `json:"upload_timeout"`
	RunTimeout          string               `json:"run_timeout"`
	MaxRetainedAttempts uint32               `json:"max_retained_attempts"`
	MaxStagingArtifacts uint32               `json:"max_staging_artifacts"`
	MaxStagingBytes     int64                `json:"max_staging_bytes"`
}

func scheduleToWire(c ScheduleConfig) scheduleWire {
	t, d, a, r := c.Targets, c.Database, c.AWS, c.Create
	return scheduleWire{Version: c.Version, ScheduleID: c.ScheduleID.String(), InstallationID: c.InstallationID.String(), ApplicationID: c.ApplicationID.String(), Environment: c.Environment, Targets: scheduleWireTargets{t.Profile, t.Activated, t.AvailabilitySLO, t.RPO.String(), t.RTO.String(), t.Retention.String(), t.BackupInterval.String(), t.CPUPercent, t.MemoryPercent, t.DiskPercent, t.AlertOwner, t.RecoveryOwner, t.IncidentOwner}, Database: scheduleWireDatabase(d), AWS: scheduleWireAWS(a), Create: scheduleWireCreate{r.StagingDir, r.PGDumpPath, r.PGDumpVersion, r.ModuleVersions, r.MaxBytes, r.DumpTimeout.String()}, UploadTimeout: c.UploadTimeout.String(), RunTimeout: c.RunTimeout.String(), MaxRetainedAttempts: c.MaxRetainedAttempts, MaxStagingArtifacts: c.MaxStagingArtifacts, MaxStagingBytes: c.MaxStagingBytes}
}
func ScheduleConfigDigest(c ScheduleConfig) ([32]byte, error) {
	if ValidateScheduleConfig(c) != nil {
		return [32]byte{}, ErrScheduleConfig
	}
	b, e := json.Marshal(scheduleToWire(c))
	if e != nil {
		return [32]byte{}, ErrScheduleConfig
	}
	return sha256.Sum256(b), nil
}

// EncodeScheduleConfig produces the exact duration-string public wire shape.
func EncodeScheduleConfig(c ScheduleConfig) ([]byte, error) {
	if ValidateScheduleConfig(c) != nil {
		return nil, ErrScheduleConfig
	}
	b, e := json.Marshal(scheduleToWire(c))
	if e != nil || len(b) > MaxScheduleConfigBytes {
		return nil, ErrScheduleConfig
	}
	return b, nil
}
func DecodeScheduleConfig(r io.Reader) (ScheduleConfig, error) {
	if r == nil {
		return ScheduleConfig{}, ErrScheduleConfig
	}
	data, e := io.ReadAll(io.LimitReader(r, MaxScheduleConfigBytes+1))
	if e != nil || len(data) > MaxScheduleConfigBytes || !scheduleExactJSON(data) {
		return ScheduleConfig{}, ErrScheduleConfig
	}
	var w scheduleWire
	if json.Unmarshal(data, &w) != nil {
		return ScheduleConfig{}, ErrScheduleConfig
	}
	c := ScheduleConfig{Version: w.Version, Environment: w.Environment, Database: ScheduleDatabaseConfig(w.Database), AWS: ScheduleAWSConfig(w.AWS), Create: ScheduleCreateConfig{StagingDir: w.Create.StagingDir, PGDumpPath: w.Create.PGDumpPath, PGDumpVersion: w.Create.PGDumpVersion, ModuleVersions: w.Create.ModuleVersions, MaxBytes: w.Create.MaxBytes}, MaxRetainedAttempts: w.MaxRetainedAttempts, MaxStagingArtifacts: w.MaxStagingArtifacts, MaxStagingBytes: w.MaxStagingBytes}
	for _, p := range []struct {
		raw  string
		dest *uuid.UUID
	}{{w.ScheduleID, &c.ScheduleID}, {w.InstallationID, &c.InstallationID}, {w.ApplicationID, &c.ApplicationID}} {
		v, err := uuid.Parse(p.raw)
		if err != nil || v.String() != p.raw || !scheduleUUID(v) {
			return ScheduleConfig{}, ErrScheduleConfig
		}
		*p.dest = v
	}
	t := w.Targets
	c.Targets = opsconfig.Config{Profile: t.Profile, Activated: t.Activated, AvailabilitySLO: t.AvailabilitySLO, CPUPercent: t.CPUPercent, MemoryPercent: t.MemoryPercent, DiskPercent: t.DiskPercent, AlertOwner: t.AlertOwner, RecoveryOwner: t.RecoveryOwner, IncidentOwner: t.IncidentOwner}
	for _, p := range []struct {
		raw  string
		dest *time.Duration
	}{{t.RPO, &c.Targets.RPO}, {t.RTO, &c.Targets.RTO}, {t.Retention, &c.Targets.Retention}, {t.BackupInterval, &c.Targets.BackupInterval}, {w.Create.DumpTimeout, &c.Create.DumpTimeout}, {w.UploadTimeout, &c.UploadTimeout}, {w.RunTimeout, &c.RunTimeout}} {
		v, err := time.ParseDuration(p.raw)
		if err != nil {
			return ScheduleConfig{}, ErrScheduleConfig
		}
		*p.dest = v
	}
	if ValidateScheduleConfig(c) != nil {
		return ScheduleConfig{}, ErrScheduleConfig
	}
	return c, nil
}

// The schema walk rejects duplicate decoded keys and aliases before Go's JSON
// struct decoding; config contains no arrays, recursive maps or untyped payloads.
func scheduleExactJSON(data []byte) bool {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var object func(string) bool
	keys := map[string][]string{
		"":         {"version", "schedule_id", "installation_id", "application_id", "environment", "targets", "database", "aws", "create", "upload_timeout", "run_timeout", "max_retained_attempts", "max_staging_artifacts", "max_staging_bytes"},
		"targets":  {"profile", "activated", "availability_slo", "rpo", "rto", "retention", "backup_interval", "cpu_percent", "memory_percent", "disk_percent", "alert_owner", "recovery_owner", "incident_owner"},
		"database": {"ledger_write_ref", "ledger_read_ref", "dump_ref", "ledger_write_role", "ledger_read_role", "dump_role", "ledger_host", "ledger_database", "ledger_root_ca_file", "dump_host", "dump_database", "dump_root_ca_file", "ledger_port", "dump_port"},
		"aws":      {"credential_kind", "account_id", "region", "runtime_role_arn", "bucket_account_id", "bucket", "prefix", "kms_key_arn"},
		"create":   {"staging_dir", "pg_dump_path", "pg_dump_version", "module_versions", "max_bytes", "dump_timeout"},
	}
	object = func(name string) bool {
		tok, e := d.Token()
		if e != nil || tok != json.Delim('{') {
			return false
		}
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			k, ok := key.(string)
			if e != nil || !ok || seen[k] {
				return false
			}
			seen[k] = true
			if name == "module_versions" {
				if len(seen) > 64 || !validModuleID(k) {
					return false
				}
				v, e := d.Token()
				s, ok := v.(string)
				if e != nil || !ok || len(s) > 128 {
					return false
				}
				continue
			}
			found := false
			for _, allowed := range keys[name] {
				if k == allowed {
					found = true
					break
				}
			}
			if !found {
				return false
			}
			if name == "" && (k == "targets" || k == "database" || k == "aws" || k == "create") || name == "create" && k == "module_versions" {
				if !object(k) {
					return false
				}
			} else {
				v, e := d.Token()
				if e != nil || v == nil {
					return false
				}
				if _, bad := v.(json.Delim); bad {
					return false
				}
			}
		}
		tok, e = d.Token()
		return e == nil && tok == json.Delim('}') && (name == "module_versions" || len(seen) == len(keys[name]))
	}
	if !object("") {
		return false
	}
	_, e := d.Token()
	return e == io.EOF
}

// NormalizeScheduleDatabaseURL disables pgx's default HOME client identity. Empty
// certificate/key settings override home-derived defaults in the pinned parser.
func NormalizeScheduleDatabaseURL(c ScheduleDatabaseConfig, kind ScheduleDatabaseKind, raw string) (string, error) {
	if ValidateScheduleDatabaseURL(c, kind, raw) != nil {
		return "", ErrScheduleConfig
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	q.Set("sslcert", "")
	q.Set("sslkey", "")
	u.RawQuery = q.Encode()
	return u.String(), nil
}
