package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/backup"
	"github.com/ajent-social/amos/internal/opsconfig"
	"github.com/ajent-social/amos/internal/scaffold/ops"
	"github.com/google/uuid"
)

func backupCLITestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("fixture directory protection failed")
	}
	return dir
}
func backupCLIConfig(t *testing.T) backup.ScheduleConfig {
	t.Helper()
	targets := opsconfig.Defaults()
	targets.Profile = opsconfig.ProfileAWSVM
	targets.AlertOwner = "test-alert"
	targets.RecoveryOwner = "test-recovery"
	targets.IncidentOwner = "test-incident"
	installation := uuid.MustParse("019a0000-0000-7000-8000-000000000001")
	return backup.ScheduleConfig{Version: "amos-backup-config-v1", ScheduleID: uuid.MustParse("019a0000-0000-7000-8000-000000000002"), InstallationID: installation, ApplicationID: uuid.MustParse("019a0000-0000-7000-8000-000000000003"), Environment: "development", Targets: targets, Database: backup.ScheduleDatabaseConfig{LedgerWriteRef: "env://AMOS_TEST_LEDGER_WRITE", LedgerReadRef: "env://AMOS_TEST_LEDGER_READ", DumpRef: "env://AMOS_TEST_DUMP", LedgerWriteRole: "backup_writer", LedgerReadRole: "backup_reader", DumpRole: "backup_dump", LedgerHost: "ledger.example.test", LedgerPort: 5432, LedgerDatabase: "operations", LedgerRootCAFile: "/etc/amos/ledger-ca.pem", DumpHost: "application.example.test", DumpPort: 5432, DumpDatabase: "application", DumpRootCAFile: "/etc/amos/dump-ca.pem"}, AWS: backup.ScheduleAWSConfig{CredentialKind: "imds_v2", AccountID: "123456789012", Region: "us-east-1", RuntimeRoleARN: "arn:aws:iam::123456789012:role/amos-runtime-test", BucketAccountID: "123456789012", Bucket: "backup-test-fixture", Prefix: "installations/" + installation.String() + "/development", KMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/test"}, Create: backup.ScheduleCreateConfig{StagingDir: backupCLITestDir(t), PGDumpPath: "/usr/bin/pg_dump", PGDumpVersion: "17.6", ModuleVersions: map[string]string{"amos": "1.0.0"}, MaxBytes: 1 << 20, DumpTimeout: 15 * time.Second}, UploadTimeout: 15 * time.Second, RunTimeout: time.Minute, MaxRetainedAttempts: 64, MaxStagingArtifacts: 64, MaxStagingBytes: 8 << 20}
}
func writeBackupCLIConfig(t *testing.T, cfg backup.ScheduleConfig) string {
	t.Helper()
	body, err := backup.EncodeScheduleConfig(cfg)
	if err != nil {
		t.Fatal("valid fixture configuration rejected")
	}
	path := filepath.Join(backupCLITestDir(t), "backup.json")
	if os.WriteFile(path, body, 0600) != nil {
		t.Fatal("configuration fixture write failed")
	}
	return path
}
func TestBackupCLIInertRunAndStrictInput(t *testing.T) {
	config := backupCLIConfig(t)
	path := writeBackupCLIConfig(t, config)
	t.Setenv("AWS_PROFILE", "CANARY_PRIVATE_VALUE")
	t.Setenv("PGPASSWORD", "CANARY_PRIVATE_VALUE")
	for _, name := range []string{"AMOS_TEST_LEDGER_WRITE", "AMOS_TEST_LEDGER_READ", "AMOS_TEST_DUMP"} {
		t.Setenv(name, "CANARY_PRIVATE_VALUE")
	}
	var out, diagnostics bytes.Buffer
	if runBackupIO(context.Background(), []string{"run", "--config", path}, &out, &diagnostics) == 0 || out.Len() != 0 || strings.Contains(diagnostics.String(), "CANARY") || strings.Contains(diagnostics.String(), path) {
		t.Fatal("inactive run accepted or leaked private input")
	}
	for _, args := range [][]string{nil, {"unknown"}, {"status"}, {"run", "--config", path, "--assets-config", path}, {"render", "--config", path}, {"run", "--unsupported", "CANARY_PRIVATE_VALUE"}} {
		out.Reset()
		diagnostics.Reset()
		if runBackupIO(context.Background(), args, &out, &diagnostics) == 0 || strings.Contains(diagnostics.String(), "CANARY_PRIVATE_VALUE") {
			t.Fatal("invalid command accepted or echoed input")
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"case-alias": append([]byte(`{"Version":"CANARY_PRIVATE_VALUE",`), body[1:]...), "duplicate": append([]byte(`{"version":"CANARY_PRIVATE_VALUE",`), body[1:]...), "trailing": append(append([]byte{}, body...), []byte("{}")...)} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(backupCLITestDir(t), "invalid.json")
			if os.WriteFile(file, data, 0600) != nil {
				t.Fatal("fixture write failed")
			}
			out.Reset()
			diagnostics.Reset()
			if runBackupIO(context.Background(), []string{"status", "--config", file}, &out, &diagnostics) == 0 || out.Len() != 0 || strings.Contains(diagnostics.String(), "CANARY") {
				t.Fatal("malformed configuration accepted or echoed")
			}
		})
	}
}
func TestBackupCLIProtectedConfiguration(t *testing.T) {
	dir := backupCLITestDir(t)
	path := filepath.Join(dir, "config.json")
	if os.WriteFile(path, []byte("{}"), 0644) != nil {
		t.Fatal("fixture write failed")
	}
	if _, err := readBackupConfiguration(path); err != nil {
		t.Fatal("protected owner read-only configuration rejected")
	}
	if os.Chmod(path, 0664) != nil {
		t.Fatal("fixture chmod failed")
	}
	if _, err := readBackupConfiguration(path); err == nil {
		t.Fatal("group-writable configuration accepted")
	}
	if os.Chmod(path, 0600) != nil {
		t.Fatal("fixture chmod failed")
	}
	link := filepath.Join(dir, "link.json")
	if os.Symlink(path, link) != nil {
		t.Fatal("fixture link failed")
	}
	if _, err := readBackupConfiguration(link); err == nil {
		t.Fatal("symlink configuration accepted")
	}
	ancestor := filepath.Join(backupCLITestDir(t), "linked-directory")
	if os.Symlink(dir, ancestor) != nil {
		t.Fatal("ancestor fixture link failed")
	}
	if _, err := readBackupConfiguration(filepath.Join(ancestor, "config.json")); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
	if os.WriteFile(path, []byte(strings.Repeat("x", 65537)), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	if _, err := readBackupConfiguration(path); err == nil {
		t.Fatal("oversized configuration accepted")
	}
	if os.Chmod(dir, 0777) != nil {
		t.Fatal("fixture chmod failed")
	}
	if _, err := readBackupConfiguration(path); err == nil {
		t.Fatal("replaceable parent accepted")
	}
}

func TestBackupCLIRenderOffline(t *testing.T) {
	cfg := backupCLIConfig(t)
	path := writeBackupCLIConfig(t, cfg)
	assets := ops.BackupAssetsConfig{Version: ops.BackupAssetsVersion, Profile: opsconfig.ProfileAWSVM, VM: &ops.BackupVMAssets{Executable: "/opt/amos/bin/amos", ExpectedExecutableSHA256: strings.Repeat("a", 64), ConfigFile: "/etc/amos/backup.json", ExpectedConfigSHA256: strings.Repeat("b", 64), WriteEnvironmentFile: "/etc/amos/backup-write.env", ReadEnvironmentFile: "/etc/amos/backup-read.env", RunnerUser: "backup_runner", RunnerGroup: "backup_runner", ReaderUser: "backup_reader", ReaderGroup: "backup_reader", RunService: "amos-backup-run.service", RunTimer: "amos-backup-run.timer", CheckService: "amos-backup-check.service", CheckTimer: "amos-backup-check.timer"}}
	body, err := json.Marshal(assets)
	if err != nil {
		t.Fatal(err)
	}
	assetsPath := filepath.Join(backupCLITestDir(t), "assets.json")
	if os.WriteFile(assetsPath, body, 0600) != nil {
		t.Fatal("assets fixture write failed")
	}
	for _, name := range []string{"AWS_PROFILE", "PGPASSWORD", "AMOS_TEST_LEDGER_WRITE", "AMOS_TEST_LEDGER_READ", "AMOS_TEST_DUMP"} {
		t.Setenv(name, "CANARY_PRIVATE_VALUE")
	}
	var first, second, diagnostics bytes.Buffer
	for _, out := range []*bytes.Buffer{&first, &second} {
		if runBackupIO(context.Background(), []string{"render", "--config", path, "--assets-config", assetsPath}, out, &diagnostics) != 0 {
			t.Fatal("offline rendering failed")
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) || diagnostics.Len() != 0 || strings.Contains(first.String(), "CANARY_PRIVATE_VALUE") || !strings.Contains(first.String(), "Declared, unverified") || !strings.Contains(first.String(), "backup run --config") || !strings.Contains(first.String(), "backup check --config") {
		t.Fatal("rendering was nondeterministic, resolved secrets, or omitted command wiring")
	}
}

func TestBackupCLIActualExecutableReadOnlyTLSLedger(t *testing.T) {
	profilePath := os.Getenv("AMOS_BACKUP_SCHEDULE_TEST_PROFILE")
	if profilePath == "" {
		t.Fatal("AMOS_BACKUP_SCHEDULE_TEST_PROFILE required for actual CLI TLS ledger checks")
	}
	body, err := os.ReadFile(profilePath)
	if err != nil || len(body) > 64<<10 {
		t.Fatal("private CLI fixture profile unavailable")
	}
	var profile struct {
		Host     string `json:"host"`
		Port     uint16 `json:"port"`
		CA       string `json:"ca_path"`
		LedgerDB string `json:"ledger_database"`
		DumpDB   string `json:"dump_database"`
		Writer   string `json:"ledger_write_role"`
		Reader   string `json:"ledger_read_role"`
		Dump     string `json:"dump_role"`
		ReadURL  string `json:"ledger_read_url"`
		OwnerURL string `json:"owner_url"`
	}
	if json.Unmarshal(body, &profile) != nil || profile.ReadURL == "" || profile.OwnerURL == "" {
		t.Fatal("private CLI fixture profile invalid")
	}
	cfg := backupCLIConfig(t)
	for _, id := range []*uuid.UUID{&cfg.ScheduleID, &cfg.InstallationID, &cfg.ApplicationID} {
		*id, err = uuid.NewV7()
		if err != nil {
			t.Fatal("fixture identifier unavailable")
		}
	}
	cfg.AWS.Prefix = "installations/" + cfg.InstallationID.String() + "/development"
	cfg.Database.LedgerHost, cfg.Database.DumpHost = profile.Host, profile.Host
	cfg.Database.LedgerPort, cfg.Database.DumpPort = profile.Port, profile.Port
	cfg.Database.LedgerDatabase, cfg.Database.DumpDatabase = profile.LedgerDB, profile.DumpDB
	cfg.Database.LedgerWriteRole, cfg.Database.LedgerReadRole, cfg.Database.DumpRole = profile.Writer, profile.Reader, profile.Dump
	cfg.Database.LedgerRootCAFile, cfg.Database.DumpRootCAFile = profile.CA, profile.CA
	path := writeBackupCLIConfig(t, cfg)
	digest, err := backup.ScheduleConfigDigest(cfg)
	if err != nil {
		t.Fatal("fixture configuration digest rejected")
	}
	owner, err := sql.Open("pgx", profile.OwnerURL)
	if err != nil {
		t.Fatal("fixture owner connection unavailable")
	}
	defer func() { _ = owner.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := owner.ExecContext(ctx, `INSERT INTO public.amos_backup_schedules(schedule_id,installation_id,application_id,environment,config_digest) VALUES($1,$2,$3,$4,$5)`, cfg.ScheduleID, cfg.InstallationID, cfg.ApplicationID, cfg.Environment, digest[:]); err != nil {
		t.Fatal("idle read-only fixture seed failed")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_, _ = owner.ExecContext(cleanup, `DELETE FROM public.amos_backup_schedules WHERE schedule_id=$1`, cfg.ScheduleID)
	}()
	binary := filepath.Join(backupCLITestDir(t), "amos")
	build := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-p", "1", "-o", binary, ".")
	if err := build.Run(); err != nil {
		t.Fatal("actual CLI executable build failed")
	}
	env := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "PG") || strings.HasPrefix(name, "AMOS_TEST_LEDGER_") || name == "AMOS_TEST_DUMP" || name == "AWS_PROFILE" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "AMOS_TEST_LEDGER_READ="+profile.ReadURL, "AMOS_TEST_LEDGER_WRITE=CANARY_PRIVATE_VALUE", "AMOS_TEST_DUMP=CANARY_PRIVATE_VALUE", "AWS_PROFILE=CANARY_PRIVATE_VALUE")
	for _, command := range []string{"status", "check"} {
		cmd := exec.CommandContext(ctx, binary, "backup", command, "--config", path)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if err != nil || len(output) > 64<<10 || strings.Contains(string(output), "CANARY") || strings.Contains(string(output), profile.ReadURL) {
			t.Fatal("actual read-only command failed or leaked private inputs")
		}
		if command == "status" {
			var snapshot backup.ScheduleSnapshot
			if json.Unmarshal(output, &snapshot) != nil || snapshot.Version != backup.ScheduleStatusVersion || snapshot.State != "idle" || snapshot.PublicationKnown || snapshot.ObservedAt.IsZero() {
				t.Fatal("actual CLI status invented publication or omitted database clock")
			}
		} else {
			var check struct {
				Version     string              `json:"version"`
				State       string              `json:"state"`
				Observation ops.TimeObservation `json:"observation"`
			}
			if json.Unmarshal(output, &check) != nil || check.Version != "amos-backup-check-v1" || check.State != "unavailable" || check.Observation.Available || check.Observation.At.IsZero() {
				t.Fatal("actual CLI check invented backup freshness")
			}
		}
	}
	var schedules, attempts int
	if owner.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM public.amos_backup_schedules WHERE schedule_id=$1 AND state='idle' AND fence=0 AND retained_attempts=0),(SELECT count(*) FROM public.amos_backup_attempts WHERE schedule_id=$1)`, cfg.ScheduleID).Scan(&schedules, &attempts) != nil || schedules != 1 || attempts != 0 {
		t.Fatal("read-only CLI mutated scheduling state")
	}
	cmd := exec.CommandContext(ctx, binary, "backup", "status", "--config", path)
	cmd.Env = append(append([]string{}, env...), "PGPASSWORD=CANARY_PRIVATE_VALUE")
	if output, err := cmd.CombinedOutput(); err == nil || strings.Contains(string(output), "CANARY") {
		t.Fatal("ambient PostgreSQL selector accepted or echoed")
	}
}
