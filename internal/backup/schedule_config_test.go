package backup

import (
	"bytes"
	"context"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/opsconfig"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func scheduleTestConfig(t *testing.T) ScheduleConfig {
	t.Helper()
	targets := opsconfig.Defaults()
	targets.Profile = opsconfig.ProfileAWSVM
	targets.Activated = true
	targets.BackupInterval = time.Minute
	targets.AlertOwner = "operator"
	targets.RecoveryOwner = "recovery"
	targets.IncidentOwner = "incident"
	installation := uuid.MustParse("01890f3e-7c00-7000-8000-000000000001")
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal("protect staging fixture")
	}
	return ScheduleConfig{Version: ScheduleConfigVersion, ScheduleID: uuid.MustParse("01890f3e-7c00-7000-8000-000000000002"), InstallationID: installation, ApplicationID: uuid.MustParse("01890f3e-7c00-7000-8000-000000000003"), Environment: "development", Targets: targets, Database: ScheduleDatabaseConfig{LedgerWriteRef: "env://AMOS_LEDGER_WRITE", LedgerReadRef: "env://AMOS_LEDGER_READ", DumpRef: "env://AMOS_DUMP", LedgerWriteRole: "backup_writer", LedgerReadRole: "backup_reader", DumpRole: "backup_dump", LedgerHost: "127.0.0.1", DumpHost: "127.0.0.1", LedgerDatabase: "backup_ops", DumpDatabase: "backup_target", LedgerRootCAFile: filepath.Join(root, "ca.pem"), DumpRootCAFile: filepath.Join(root, "ca.pem"), LedgerPort: 5432, DumpPort: 5432}, AWS: ScheduleAWSConfig{CredentialKind: "imds_v2", AccountID: "123456789012", Region: "us-east-1", RuntimeRoleARN: "arn:aws:iam::123456789012:role/backup-writer", BucketAccountID: "123456789012", Bucket: "example-backups", Prefix: "installations/" + installation.String() + "/development", KMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789abc"}, Create: ScheduleCreateConfig{StagingDir: root, PGDumpPath: "/usr/bin/pg_dump", PGDumpVersion: "16.15", ModuleVersions: map[string]string{"identity": "1.0"}, MaxBytes: 16 << 20, DumpTimeout: 5 * time.Second}, UploadTimeout: 5 * time.Second, RunTimeout: 30 * time.Second, MaxRetainedAttempts: 64, MaxStagingArtifacts: 64, MaxStagingBytes: 128 << 20}
}
func TestScheduleConfigStrictWireAndDigest(t *testing.T) {
	cfg := scheduleTestConfig(t)
	encoded, e := EncodeScheduleConfig(cfg)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := DecodeScheduleConfig(bytes.NewReader(encoded))
	if e != nil {
		t.Fatal(e)
	}
	want, _ := ScheduleConfigDigest(cfg)
	got, _ := ScheduleConfigDigest(decoded)
	if want != got {
		t.Fatal("wire digest changed")
	}
	for name, mut := range map[string]func([]byte) []byte{
		"alias":             func(b []byte) []byte { return bytes.Replace(b, []byte(`"run_timeout"`), []byte(`"Run_Timeout"`), 1) },
		"duplicate":         func(b []byte) []byte { return append([]byte(`{"version":"amos-backup-config-v1",`), b[1:]...) },
		"escaped_duplicate": func(b []byte) []byte { return append([]byte(`{"\u0076ersion":"amos-backup-config-v1",`), b[1:]...) },
		"nested_unknown": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"targets":{`), []byte(`"targets":{"extra":true,`), 1)
		},
		"missing": func(b []byte) []byte { return bytes.Replace(b, []byte(`"max_retained_attempts":64,`), nil, 1) },
		"numeric_duration": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"run_timeout":"30s"`), []byte(`"run_timeout":30000000000`), 1)
		},
		"bad_uuid": func(b []byte) []byte {
			return bytes.Replace(b, []byte(cfg.ScheduleID.String()), []byte(strings.ToUpper(cfg.ScheduleID.String())), 1)
		},
		"null": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"activated":true`), []byte(`"activated":null`), 1)
		},
		"array":    func(b []byte) []byte { return bytes.Replace(b, []byte(`"database":{`), []byte(`"database":[`), 1) },
		"trailing": func(b []byte) []byte { return append(b, []byte(` {}`)...) },
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := DecodeScheduleConfig(bytes.NewReader(mut(bytes.Clone(encoded)))); e != ErrScheduleConfig {
				t.Fatal("malformed wire accepted")
			}
		})
	}
	if _, e := DecodeScheduleConfig(strings.NewReader(strings.Repeat(" ", MaxScheduleConfigBytes+1))); e != ErrScheduleConfig {
		t.Fatal("oversize accepted")
	}
	cfg.Create.ModuleVersions = map[string]string{"workspace": "2", "identity": "1"}
	a, _ := ScheduleConfigDigest(cfg)
	cfg.Create.ModuleVersions = map[string]string{"identity": "1", "workspace": "2"}
	b, _ := ScheduleConfigDigest(cfg)
	if a != b {
		t.Fatal("map order changed digest")
	}
	cfg.UploadTimeout += time.Second
	c, _ := ScheduleConfigDigest(cfg)
	if c == b {
		t.Fatal("policy omitted from digest")
	}
	cfg.Targets.Activated = false
	if ValidateScheduleConfig(cfg) != nil {
		t.Fatal("inactive render rejected")
	}
	s := &Scheduler{cfg: cfg}
	if _, e := s.RunDue(context.Background()); e != ErrScheduleConfig {
		t.Fatal("inactive run accepted")
	}
}
func TestScheduleDatabaseURLNoAmbientOrHomeAuthority(t *testing.T) {
	cfg := scheduleTestConfig(t)
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if os.WriteFile(cfg.Database.DumpRootCAFile, ca, 0600) != nil {
		t.Fatal("CA fixture")
	}
	u := &url.URL{Scheme: "postgresql", Host: net.JoinHostPort(cfg.Database.DumpHost, "5432"), Path: "/" + cfg.Database.DumpDatabase, User: url.UserPassword(cfg.Database.DumpRole, "dummy")}
	q := url.Values{"sslmode": {"verify-full"}, "sslrootcert": {cfg.Database.DumpRootCAFile}}
	u.RawQuery = q.Encode()
	raw := u.String()
	home := t.TempDir()
	if e := os.Mkdir(filepath.Join(home, ".postgresql"), 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"postgresql.crt", "postgresql.key"} {
		if e := os.WriteFile(filepath.Join(home, ".postgresql", name), []byte("synthetic invalid client identity"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	t.Setenv("HOME", home)
	normalized, e := NormalizeScheduleDatabaseURL(cfg.Database, ScheduleDump, raw)
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := pgx.ParseConfig(normalized)
	if e != nil || parsed.TLSConfig == nil || len(parsed.TLSConfig.Certificates) != 0 {
		t.Fatal("default client identity was consulted")
	}
	for _, bad := range []string{raw + "&sslmode=verify-full", raw + "&service=other", raw + "&sslcert=/invalid", strings.Replace(raw, "verify-full", "disable", 1), strings.Replace(raw, ":5432", ":05432", 1), strings.Replace(raw, "backup_dump", "other_role", 1)} {
		if ValidateScheduleDatabaseURL(cfg.Database, ScheduleDump, bad) != ErrScheduleConfig {
			t.Fatal("ambiguous target/auth accepted")
		}
	}
	t.Setenv("PGPASSWORD", "")
	if ValidateScheduleDatabaseURL(cfg.Database, ScheduleDump, raw) != ErrScheduleConfig {
		t.Fatal("empty ambient selector accepted")
	}
}
