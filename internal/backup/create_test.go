package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/testkit"
	"github.com/jackc/pgx/v5"
)

func TestCreateValidation(t *testing.T) {
	directory := privateDirectory(t)
	tool, err := exec.LookPath("pg_dump")
	if err != nil {
		t.Fatal("pg_dump is required for backup validation tests")
	}
	tool, err = filepath.Abs(tool)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{
		ID: "backup-validation", DatabaseURL: "postgres://user:password@127.0.0.1/db",
		DestinationDir: directory, PGDumpPath: tool, PGDumpVersion: pgToolVersion(t, tool, "pg_dump"),
		ModuleVersions: map[string]string{"identity": "1.0"}, MaxBytes: 1024, Timeout: time.Second,
	}

	tests := []struct {
		name   string
		change func(*Request)
	}{
		{name: "missing version pin", change: func(r *Request) { r.PGDumpVersion = "" }},
		{name: "unbounded output", change: func(r *Request) { r.MaxBytes = 0 }},
		{name: "unbounded process", change: func(r *Request) { r.Timeout = 0 }},
		{name: "process timeout exceeds cap", change: func(r *Request) { r.Timeout = maxBackupTimeout + time.Nanosecond }},
		{name: "missing module metadata", change: func(r *Request) { r.ModuleVersions = nil }},
		{name: "invalid schema", change: func(r *Request) { r.Schemas = []string{"bad; DROP SCHEMA public"} }},
		{name: "duplicate schema", change: func(r *Request) { r.Schemas = []string{"one", "one"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := request
			test.change(&candidate)
			if _, err := Create(context.Background(), candidate); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Create() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestConnectionFilesArePrivateAndEscaped(t *testing.T) {
	connectionURL := "postgres://backup:p%3Aa%5Css@localhost:5432/amos?sslmode=disable"
	config, err := pgx.ParseConfig(connectionURL)
	if err != nil {
		t.Fatal("parse synthetic fixture connection")
	}
	directory := privateDirectory(t)
	servicePath, passwordPath, cleanup, err := writeConnectionFiles(directory, config, connectionURL)
	if err != nil {
		t.Fatalf("write protected connection files: %v", err)
	}
	service, err := os.ReadFile(servicePath)
	if err != nil || !strings.Contains(string(service), "sslmode=disable\n") || strings.Contains(string(service), "'disable'") {
		t.Fatalf("service file did not preserve libpq parameter values: err=%v", err)
	}
	for _, path := range []string{servicePath, passwordPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("connection file mode = %04o, want 0600", info.Mode().Perm())
		}
	}
	passfile, err := os.ReadFile(passwordPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(passfile), `p\:a\\ss`) {
		t.Fatal("passfile did not escape colon and backslash characters")
	}
	t.Setenv("PGPASSWORD", "ambient-secret")
	for _, value := range childEnvironment(servicePath, passwordPath) {
		if strings.HasPrefix(value, "PGPASSWORD=") {
			t.Fatal("child environment inherited PGPASSWORD")
		}
	}
	if err := cleanup(); err != nil {
		t.Fatalf("remove temporary credentials: %v", err)
	}
	for _, path := range []string{servicePath, passwordPath} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("temporary connection file remains: %v", err)
		}
	}
}

func TestCreate(t *testing.T) {
	database, schema, databaseURL := createFixtureSchema(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := database.ExecContext(ctx, `CREATE TABLE backup_records (id integer PRIMARY KEY, value text NOT NULL)`); err != nil {
		t.Fatalf("create fixture table: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO backup_records (id, value) VALUES (1, 'fixture')`); err != nil {
		t.Fatalf("seed fixture table: %v", err)
	}
	directory := privateDirectory(t)
	request := fixtureRequest(t, databaseURL, directory, "complete", []string{schema})
	manifest, err := Create(ctx, request)
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	toolMajor, err := strconv.Atoi(strings.Split(request.PGDumpVersion, ".")[0])
	if err != nil {
		t.Fatal(err)
	}
	if manifest.State != "complete" || manifest.Scope != "schemas" || manifest.SchemaVersion != 1 || manifest.ServerMajor != toolMajor || len(manifest.Migrations) != 1 {
		t.Fatalf("unexpected backup recovery boundary: %+v", manifest)
	}
	if manifest.PGDumpVersion != request.PGDumpVersion || manifest.ConsistencyModel != "postgresql_exported_repeatable_read_snapshot" {
		t.Fatalf("unexpected tool/consistency metadata: %+v", manifest)
	}
	if got := manifest.Modules; len(got) != 1 || got[0] != (ModuleVersion{ID: "identity", Version: "1.0"}) {
		t.Fatalf("module metadata = %+v", got)
	}
	if manifest.ArchiveBytes <= 0 || len(manifest.ArchiveSHA256) != sha256.Size*2 {
		t.Fatalf("missing archive integrity fields: %+v", manifest)
	}
	backupDir := filepath.Join(directory, "backup-complete")
	archivePath := filepath.Join(backupDir, manifest.ArchiveName)
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read published archive: %v", err)
	}
	digest := sha256.Sum256(archive)
	if got := hex.EncodeToString(digest[:]); got != manifest.ArchiveSHA256 {
		t.Fatalf("archive digest = %s, manifest says %s", got, manifest.ArchiveSHA256)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(backupDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read completion marker: %v", err)
	}
	var decoded Manifest
	if err := json.Unmarshal(manifestBytes, &decoded); err != nil || decoded.State != "complete" || decoded.ArchiveSHA256 != manifest.ArchiveSHA256 {
		t.Fatalf("invalid completion manifest: %v; decoded=%+v", err, decoded)
	}
	for _, entry := range []string{".pg_service.conf", ".pgpass", "manifest.json.partial", "database.dump.partial"} {
		if _, err := os.Lstat(filepath.Join(backupDir, entry)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("private or incomplete artifact %s remains: %v", entry, err)
		}
	}
	if _, err := exec.CommandContext(ctx, pgRestorePath(t), "--list", archivePath).CombinedOutput(); err != nil {
		t.Fatalf("pg_restore rejected completed custom archive: %v", err)
	}
	restoreVersion := pgToolVersion(t, pgRestorePath(t), "pg_restore")
	restoreMajor, err := strconv.Atoi(strings.Split(restoreVersion, ".")[0])
	if err != nil || restoreMajor != manifest.ServerMajor {
		t.Fatalf("pg_restore version %q is incompatible with server major %d", restoreVersion, manifest.ServerMajor)
	}

	// A pre-existing empty directory must not be replaced by the completion
	// publication path; this catches os.Rename's replace-empty-directory case.
	collision := filepath.Join(directory, "backup-collision")
	if err := os.Mkdir(collision, 0o700); err != nil {
		t.Fatal(err)
	}
	request.ID = "collision"
	if _, err := Create(ctx, request); !errors.Is(err, ErrDestination) {
		t.Fatalf("Create() collision error = %v, want ErrDestination", err)
	}
	entries, err := os.ReadDir(collision)
	if err != nil || len(entries) != 0 {
		t.Fatalf("existing empty destination was altered: entries=%v err=%v", entries, err)
	}
}

func TestCreateCancellationDoesNotPublishPartialDump(t *testing.T) {
	database, schema, databaseURL := createFixtureSchema(t)
	ctx, setupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer setupCancel()
	if _, err := database.ExecContext(ctx, `CREATE TABLE cancellation_payload (id integer PRIMARY KEY, value text NOT NULL)`); err != nil {
		t.Fatalf("create cancellation fixture table: %v", err)
	}
	// Incompressible synthetic values keep the actual pg_dump process busy long
	// enough for the test to cancel after observing real partial archive bytes.
	if _, err := database.ExecContext(ctx, `INSERT INTO cancellation_payload (id, value)
		SELECT id, string_agg(md5(random()::text), '')
		FROM generate_series(1, 512) AS ids(id)
		CROSS JOIN generate_series(1, 2048) AS chunk
		GROUP BY id`); err != nil {
		t.Fatalf("seed cancellation fixture rows: %v", err)
	}
	directory := privateDirectory(t)
	request := fixtureRequest(t, databaseURL, directory, "cancel-midstream", []string{schema})
	request.MaxBytes = 64 << 20
	request.Timeout = 25 * time.Second
	backupCtx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := Create(backupCtx, request)
		result <- err
	}()
	partialPath := filepath.Join(directory, "backup-cancel-midstream", "database.dump.partial")
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	observedPartial := false
	for !observedPartial {
		select {
		case err := <-result:
			cancel()
			t.Fatalf("pg_dump finished before cancellation could interrupt it: %v", err)
		case <-timer.C:
			cancel()
			<-result
			t.Fatal("pg_dump did not produce partial archive bytes before timeout")
		case <-ticker.C:
			info, err := os.Stat(partialPath)
			if err == nil && info.Size() >= 64<<10 {
				observedPartial = true
			}
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, ErrCanceled) {
			t.Fatalf("Create() cancellation error = %v, want ErrCanceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Create() did not stop after canceling pg_dump")
	}
	backupDir := filepath.Join(directory, "backup-cancel-midstream")
	if _, err := os.Lstat(backupDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete backup is still selectable at %s: %v", backupDir, err)
	}
}

func TestCreateDatabaseFailureDoesNotPublishBackup(t *testing.T) {
	_, _, databaseURL := createFixtureSchema(t)
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("parse synthetic fixture connection")
	}
	parsed.Path = "/amos_backup_missing_database_fixture"
	directory := privateDirectory(t)
	request := fixtureRequest(t, parsed.String(), directory, "database-failure", nil)
	if _, err := Create(context.Background(), request); !errors.Is(err, ErrDumpFailed) {
		t.Fatalf("Create() database failure = %v, want ErrDumpFailed", err)
	}
	if _, err := os.Lstat(filepath.Join(directory, "backup-database-failure")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed database backup left a selectable artifact: %v", err)
	}
}

func TestCompletionManifestCancellationDoesNotPublish(t *testing.T) {
	directory := privateDirectory(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manifest := Manifest{Format: manifestFormat, State: "complete", ID: "cancelled"}
	if err := writeCompletionManifest(ctx, directory, manifest); !errors.Is(err, ErrCanceled) {
		t.Fatalf("writeCompletionManifest() error = %v, want ErrCanceled", err)
	}
	for _, name := range []string{"manifest.json", "manifest.json.partial"} {
		if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("canceled publication left %s: %v", name, err)
		}
	}
}

func TestDumpCommandWaitDelayBoundsInheritedOutputPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("synthetic inherited-pipe process fixture requires a Unix shell")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	command := commandWithWaitDelay(ctx, "/bin/sh", "-c", "(sleep 2) & wait")
	var output bytes.Buffer
	command.Stdout = &output
	started := time.Now()
	err := command.Run()
	if err == nil {
		t.Fatal("canceled dump command unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("inherited output pipe delayed cancellation for %s", elapsed)
	}
}

func TestCreateRejectsMismatchedToolPin(t *testing.T) {
	tool, err := exec.LookPath("pg_dump")
	if err != nil {
		t.Fatal("pg_dump is required for backup validation tests")
	}
	tool, err = filepath.Abs(tool)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{
		ID: "bad-pin", DatabaseURL: "postgres://user:password@127.0.0.1/db",
		DestinationDir: privateDirectory(t), PGDumpPath: tool, PGDumpVersion: "99.99",
		ModuleVersions: map[string]string{"identity": "1.0"}, MaxBytes: 1024, Timeout: time.Second,
	}
	if err := validateRequest(context.Background(), request); err != nil {
		toolInfo, _ := os.Stat(request.PGDumpPath)
		dirInfo, _ := os.Lstat(request.DestinationDir)
		t.Fatalf("test request failed validation: id=%t absTool=%t version=%t toolExecutable=%t privateDestination=%t modules=%d",
			backupIDPattern.MatchString(request.ID), filepath.IsAbs(request.PGDumpPath), validVersion(request.PGDumpVersion),
			toolInfo != nil && toolInfo.Mode().Perm()&0o111 != 0,
			dirInfo != nil && dirInfo.IsDir() && dirInfo.Mode().Perm()&0o077 == 0,
			len(request.ModuleVersions))
	}
	_, err = Create(context.Background(), request)
	if !errors.Is(err, ErrToolVersion) {
		t.Fatalf("Create() error = %v, want ErrToolVersion", err)
	}
	if strings.Contains(err.Error(), "password") {
		t.Fatalf("error leaked connection secret: %v", err)
	}
}

func createFixtureSchema(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	databaseURL, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	database, schema := testkit.NewPostgres(t)
	if _, err := database.Exec(`CREATE TABLE amos_schema_migrations (
		sequence bigint PRIMARY KEY, migration_id text NOT NULL, checksum bytea NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT transaction_timestamp())`); err != nil {
		t.Fatalf("create isolated migration ledger: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO amos_schema_migrations (sequence, migration_id, checksum) VALUES (1, '000001.test.fixture', decode(repeat('a', 64), 'hex'))`); err != nil {
		t.Fatalf("seed isolated migration ledger: %v", err)
	}
	return database, schema, databaseURL
}

func privateDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("protect temporary backup directory: %v", err)
	}
	return directory
}

func fixtureRequest(t *testing.T, databaseURL, directory, id string, schemas []string) Request {
	t.Helper()
	tool, err := exec.LookPath("pg_dump")
	if err != nil {
		t.Fatal("pg_dump is required for backup integration tests")
	}
	tool, err = filepath.Abs(tool)
	if err != nil {
		t.Fatal(err)
	}
	version := pgToolVersion(t, tool, "pg_dump")
	return Request{
		ID: id, DatabaseURL: databaseURL, DestinationDir: directory,
		PGDumpPath: tool, PGDumpVersion: version,
		ModuleVersions: map[string]string{"identity": "1.0"},
		Schemas:        schemas, MaxBytes: 32 << 20, Timeout: 15 * time.Second,
	}
}

func pgToolVersion(t *testing.T, path, name string) string {
	t.Helper()
	output, err := exec.Command(path, "--version").Output()
	if err != nil {
		t.Fatalf("read %s version: %v", name, err)
	}
	line := strings.TrimSpace(string(output))
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(name) + ` \(PostgreSQL\) (\d+(?:\.\d+){1,2})(?: .+)?$`)
	match := pattern.FindStringSubmatch(line)
	if len(match) != 2 {
		t.Fatalf("unexpected %s version output", name)
	}
	return match[1]
}

func pgRestorePath(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("pg_restore")
	if err != nil {
		t.Fatal("pg_restore is required for backup integration tests")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
