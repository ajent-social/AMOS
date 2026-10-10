package restore_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/backup"
	"github.com/ajent-social/amos/internal/restore"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/jackc/pgx/v5"
)

func TestDatabaseRestore(t *testing.T) {
	raw := os.Getenv("AMOS_RESTORE_TEST_ADMIN_URL")
	if raw == "" {
		t.Fatal("AMOS_RESTORE_TEST_ADMIN_URL must name the isolated task-owned PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatalf("connect required PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := admin.Close(context.Background()); closeErr != nil {
			t.Errorf("close admin connection: %v", closeErr)
		}
	})

	var b [6]byte
	if _, err = rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(b[:])
	sourceName := "restore_source_" + suffix
	targetName := "restore_target_" + suffix
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{sourceName}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dropDB(sourceName) })
	sourceURL := databaseURL(t, raw, sourceName)

	registry, err := migrations.Core()
	if err != nil {
		t.Fatalf("load registered AMOS core migrations: %v", err)
	}
	expectedMigrations := migrationVersions(registry)
	// This immutable module contract is defined by internal/initializer's supportedModules.
	modules := map[string]string{"identity": "1.0", "workspace": "1.0", "billing": "1.0"}
	expectedModules := []backup.ModuleVersion{{ID: "billing", Version: "1.0"}, {ID: "identity", Version: "1.0"}, {ID: "workspace", Version: "1.0"}}

	database, err := storage.Open(ctx, sourceURL)
	if err != nil {
		t.Fatalf("open isolated AMOS source database: %v", err)
	}
	if err = storage.Migrate(ctx, database, registry); err != nil {
		_ = database.Close()
		t.Fatalf("apply registered AMOS core migrations: %v", err)
	}
	if err = database.Close(); err != nil {
		t.Fatalf("close migrated AMOS source: %v", err)
	}
	source, err := pgx.Connect(ctx, sourceURL)
	if err != nil {
		t.Fatal(err)
	}
	seedRegisteredRecords(t, ctx, source)
	if err = source.Close(context.Background()); err != nil {
		t.Fatalf("close seeded AMOS source: %v", err)
	}

	pgDump := tool(t, "pg_dump")
	pgRestore := tool(t, "pg_restore")
	dumpVersion := toolVersion(t, pgDump)
	backupRoot := t.TempDir()
	if err = os.Chmod(backupRoot, 0700); err != nil {
		t.Fatal(err)
	}
	backupRequest := backup.Request{
		ID: "backup-" + suffix, DatabaseURL: sourceURL, DestinationDir: backupRoot,
		PGDumpPath: pgDump, PGDumpVersion: dumpVersion,
		ModuleVersions: modules, MaxBytes: 64 << 20, Timeout: time.Minute,
	}
	manifest, err := backup.Create(ctx, backupRequest)
	if err != nil {
		t.Fatalf("create backup from registered AMOS schema: %v", err)
	}
	if !equalMigrationVersions(manifest.Migrations, expectedMigrations) || !equalModuleVersions(manifest.Modules, expectedModules) {
		t.Fatalf("backup metadata differs from trusted AMOS registry/module versions")
	}

	backupDir := filepath.Join(backupRoot, "backup-"+backupRequest.ID)
	archive := filepath.Join(backupDir, "database.dump")
	restoreRequest := restore.Request{
		BackupDir: backupDir, AdminURL: raw, Target: targetName,
		PGRestorePath: pgRestore, TempDir: t.TempDir(),
		CurrentMigrations: expectedMigrations, CurrentModules: expectedModules,
		MaxBytes: 64 << 20, Timeout: time.Minute,
	}
	originalArchive, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, ctx, admin, targetName)
	corruptArchive := append([]byte(nil), originalArchive...)
	corruptArchive[len(corruptArchive)/2] ^= 1
	if err = os.WriteFile(archive, corruptArchive, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = restore.Restore(ctx, restoreRequest); !errors.Is(err, restore.ErrIntegrity) {
		t.Fatalf("corrupt backup accepted: %v", err)
	}
	assertAbsent(t, ctx, admin, targetName)
	if err = os.WriteFile(archive, originalArchive, 0600); err != nil {
		t.Fatal(err)
	}
	restoreRequest.CurrentMigrations = append([]backup.MigrationVersion(nil), expectedMigrations...)
	restoreRequest.CurrentMigrations[0].SHA256 = strings.Repeat("b", 64)
	if _, err = restore.Preflight(ctx, restoreRequest); !errors.Is(err, restore.ErrIncompatible) {
		t.Fatalf("incompatible registered migration accepted: %v", err)
	}
	restoreRequest.CurrentMigrations = expectedMigrations

	// Create a real registered AMOS archive whose database ledger is one row
	// shorter than its manifest claims. Exact restored-ledger validation must
	// reject it and remove the isolated target before returning.
	shortSourceName := "restore_short_source_" + suffix
	shortTargetName := "restore_short_target_" + suffix
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{shortSourceName}.Sanitize()); err != nil {
		t.Fatalf("create isolated shorter-ledger source: %v", err)
	}
	defer dropDB(shortSourceName)
	shortSourceURL := databaseURL(t, raw, shortSourceName)
	registered := registry.Migrations()
	shortFragments := make([]migrations.Fragment, 0, len(registered)-1)
	for _, migration := range registered[:len(registered)-1] {
		shortFragments = append(shortFragments, migrations.Fragment{Namespace: migration.Namespace, Migrations: []migrations.Migration{migration}})
	}
	shortRegistry, err := migrations.NewRegistry(shortFragments...)
	if err != nil {
		t.Fatalf("build valid registered migration prefix: %v", err)
	}
	shortDatabase, err := storage.Open(ctx, shortSourceURL)
	if err != nil {
		t.Fatalf("open shorter-ledger source: %v", err)
	}
	if err = storage.Migrate(ctx, shortDatabase, shortRegistry); err != nil {
		_ = shortDatabase.Close()
		t.Fatalf("apply registered migration prefix: %v", err)
	}
	if err = shortDatabase.Close(); err != nil {
		t.Fatalf("close shorter-ledger source: %v", err)
	}
	shortBackupRoot := filepath.Join(backupRoot, "short-ledger")
	if err = os.Mkdir(shortBackupRoot, 0700); err != nil {
		t.Fatal(err)
	}
	shortBackupRequest := backupRequest
	shortBackupRequest.ID = "short-ledger-" + suffix
	shortBackupRequest.DatabaseURL = shortSourceURL
	shortBackupRequest.DestinationDir = shortBackupRoot
	shortManifest, err := backup.Create(ctx, shortBackupRequest)
	if err != nil {
		t.Fatalf("create shorter-ledger AMOS archive: %v", err)
	}
	shortBackupDir := filepath.Join(shortBackupRoot, "backup-"+shortBackupRequest.ID)
	shortManifest.Migrations = expectedMigrations
	shortManifest.SchemaVersion = expectedMigrations[len(expectedMigrations)-1].Sequence
	shortManifestBytes, err := json.Marshal(shortManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(shortBackupDir, "manifest.json"), shortManifestBytes, 0600); err != nil {
		t.Fatal(err)
	}
	shortRestoreRequest := restoreRequest
	shortRestoreRequest.BackupDir = shortBackupDir
	shortRestoreRequest.Target = shortTargetName
	shortPrepared, shortErr := restore.Restore(ctx, shortRestoreRequest)
	if shortErr != nil && !errors.Is(shortErr, restore.ErrIncompatible) {
		t.Fatalf("short restored migration ledger returned unexpected error: %v", shortErr)
	}
	if shortPrepared != nil {
		if discardErr := shortPrepared.Discard(context.Background()); discardErr != nil {
			t.Fatalf("discard unexpectedly accepted shorter-ledger restore: %v", discardErr)
		}
	}
	if shortErr == nil {
		t.Fatal("short restored migration ledger accepted")
	}
	assertAbsent(t, ctx, admin, shortTargetName)
	assertNoConnectableStagingDatabase(t, ctx, admin)

	prepared, err := restore.Restore(ctx, restoreRequest)
	if err != nil {
		t.Fatalf("restore registered AMOS schema: %v", err)
	}
	_, elapsed := prepared.Evidence()
	if elapsed <= 0 || prepared.RecoveryPointLag() <= 0 {
		t.Fatalf("missing local recovery evidence: elapsed=%v lag=%v", elapsed, prepared.RecoveryPointLag())
	}
	t.Cleanup(func() {
		if discardErr := prepared.Discard(context.Background()); discardErr != nil {
			t.Errorf("discard prepared restore: %v", discardErr)
		}
	})
	assertAbsent(t, ctx, admin, targetName)
	assertNoConnectableStagingDatabase(t, ctx, admin)

	// Fail the second provider-state check after sessions were revoked.
	gate := &checkState{err: errors.New("provider state changed"), failOn: 2}
	if err = prepared.Activate(ctx, gate); !errors.Is(err, restore.ErrActivation) {
		t.Fatalf("activation with changed provider state: %v", err)
	}
	assertAbsent(t, ctx, admin, targetName)
	assertNoConnectableStagingDatabase(t, ctx, admin)

	// A post-rename barrier failure must leave the renamed database closed;
	// retrying the same Prepared must complete without trying to rename it again.
	gate = &checkState{err: errors.New("provider state changed after rename"), failOn: 3}
	if err = prepared.Activate(ctx, gate); !errors.Is(err, restore.ErrActivation) {
		t.Fatalf("post-rename activation failure: %v", err)
	}
	assertClosedDatabase(t, ctx, admin, targetName)
	gate = &checkState{}
	if err = prepared.Activate(ctx, gate); err != nil {
		t.Fatalf("retry activation after post-rename failure: %v", err)
	}
	assertDatabaseConnectable(t, ctx, admin, targetName)
	if gate.calls != 3 {
		t.Fatalf("retry provider checks=%d, want 3", gate.calls)
	}
	repeatedGate := &checkState{}
	if err = prepared.Activate(ctx, repeatedGate); err != nil {
		t.Fatalf("repeated activation: %v", err)
	}
	if repeatedGate.calls != 1 {
		t.Fatalf("repeated activation provider checks=%d, want one current-state check", repeatedGate.calls)
	}

	// PUBLIC must not gain a path into the restored database.
	probeRole := "restore_probe_" + suffix
	if _, err = admin.Exec(ctx, "CREATE ROLE "+pgx.Identifier{probeRole}.Sanitize()+" LOGIN"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+pgx.Identifier{probeRole}.Sanitize())
	}()
	probeURL := databaseURL(t, raw, targetName)
	probe, err := url.Parse(probeURL)
	if err != nil {
		t.Fatal(err)
	}
	probe.User = url.User(probeRole)
	if probeDB, probeErr := pgx.Connect(ctx, probe.String()); probeErr == nil {
		if closeErr := probeDB.Close(context.Background()); closeErr != nil {
			t.Errorf("close unexpected probe connection: %v", closeErr)
		}
		t.Fatal("restored database allowed an unprivileged role to connect")
	}

	target, err := pgx.Connect(ctx, databaseURL(t, raw, targetName))
	if err != nil {
		t.Fatalf("connect activated AMOS database as owner: %v", err)
	}
	targetClosed := false
	t.Cleanup(func() {
		if !targetClosed {
			if closeErr := target.Close(context.Background()); closeErr != nil {
				t.Errorf("close restored target: %v", closeErr)
			}
		}
	})
	assertAMOSRecords(t, ctx, target, suffix, true)

	source, err = pgx.Connect(ctx, sourceURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := source.Close(context.Background()); closeErr != nil {
			t.Errorf("close original source: %v", closeErr)
		}
	})
	assertAMOSRecords(t, ctx, source, suffix, false)
	if gate.calls != 3 {
		t.Fatalf("provider checks=%d, want 3", gate.calls)
	}
	if err = target.Close(context.Background()); err != nil {
		t.Fatalf("close activated target before discard: %v", err)
	}
	targetClosed = true
	if err = prepared.Discard(ctx); err != nil {
		t.Fatalf("discard activated restore: %v", err)
	}
	assertAbsent(t, ctx, admin, targetName)
	if err = prepared.Discard(ctx); err != nil {
		t.Fatalf("repeat discard: %v", err)
	}
	if err = prepared.Activate(ctx, &checkState{}); !errors.Is(err, restore.ErrActivation) {
		t.Fatalf("activation after discard was not rejected: %v", err)
	}
}

func seedRegisteredRecords(t *testing.T, ctx context.Context, source *pgx.Conn) {
	t.Helper()
	const (
		installation = "018f0000-0000-7000-8000-000000000001"
		application  = "018f0000-0000-7000-8000-000000000002"
		person       = "018f0000-0000-7000-8000-000000000003"
		workspace    = "018f0000-0000-7000-8000-000000000004"
		environment  = "018f0000-0000-7000-8000-000000000005"
		billing      = "018f0000-0000-7000-8000-000000000006"
		session      = "018f0000-0000-7000-8000-000000000007"
		job          = "018f0000-0000-7000-8000-000000000008"
		subscription = "018f0000-0000-7000-8000-00000000000a"
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := source.Exec(ctx, "INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')", person, installation, application); err != nil {
		t.Fatalf("seed registered identity person: %v", err)
	}
	if _, err := source.Exec(ctx, "INSERT INTO workspaces(id,installation_id,application_id,kind,state,personal_owner_id) VALUES($1,$2,$3,'personal','active',$4)", workspace, installation, application, person); err != nil {
		t.Fatalf("seed registered personal workspace: %v", err)
	}
	if _, err := source.Exec(ctx, "INSERT INTO billing_workspace_accounts(id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,state) VALUES($1,$2,$3,$4,$5,'fixture','fixture-account','test','active')", billing, installation, application, environment, workspace); err != nil {
		t.Fatalf("seed registered flat billing account: %v", err)
	}
	if _, err := source.Exec(ctx, "INSERT INTO billing_subscription_projections(id,billing_account_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,subscription_ref,state,price_key,quantity,observed_at) VALUES($1,$2,$3,$4,$5,$6,'fixture','fixture-account','test','synthetic-flat-subscription','confirmed','synthetic-flat-price',1,$7)", subscription, billing, installation, application, environment, workspace, now); err != nil {
		t.Fatalf("seed registered flat billing projection: %v", err)
	}
	expires := now.Add(time.Hour)
	idle := now.Add(30 * time.Minute)
	if _, err := source.Exec(ctx, "INSERT INTO identity_sessions(id,person_id,installation_id,application_id,environment_id,token_digest,security_epoch,authentication_method,authenticated_at,issued_at,expires_at,idle_expires_at) VALUES($1,$2,$3,$4,$5,decode(repeat('a',64),'hex'),0,'email_password',$6,$6,$7,$8)", session, person, installation, application, environment, now, expires, idle); err != nil {
		t.Fatalf("seed registered recovered session: %v", err)
	}
	if _, err := source.Exec(ctx, "INSERT INTO amos_jobs(id,installation_id,application_id,idempotency_key,request_hash,kind,payload,external_effect,status,attempt_count,max_attempts,reconciliation_attempt_count,max_reconciliation_attempts,available_at,next_reconciliation_at,deadline_at) VALUES($1,$2,$3,'restore-fixture',decode(repeat('b',64),'hex'),'restore.fixture','{}',false,'queued',0,3,0,3,$4,$4,$5)", job, installation, application, now, expires); err != nil {
		t.Fatalf("seed registered non-effect job: %v", err)
	}
}

func assertAMOSRecords(t *testing.T, ctx context.Context, db *pgx.Conn, suffix string, wantSessionRevoked bool) {
	t.Helper()
	const person = "018f0000-0000-7000-8000-000000000003"
	const workspace = "018f0000-0000-7000-8000-000000000004"
	const session = "018f0000-0000-7000-8000-000000000007"
	const job = "018f0000-0000-7000-8000-000000000008"
	var kind, state string
	if err := db.QueryRow(ctx, "SELECT kind,state FROM workspaces WHERE id=$1", workspace).Scan(&kind, &state); err != nil || kind != "personal" || state != "active" {
		t.Fatalf("personal workspace invariant failed for fixture %s: %v", suffix, err)
	}
	var owner string
	if err := db.QueryRow(ctx, "SELECT personal_owner_id::text FROM workspaces WHERE id=$1", workspace).Scan(&owner); err != nil || owner != person {
		t.Fatalf("personal workspace owner invariant failed for fixture %s: %v", suffix, err)
	}
	var price, billingState string
	if err := db.QueryRow(ctx, "SELECT price_key,state FROM billing_subscription_projections WHERE subscription_ref='synthetic-flat-subscription'").Scan(&price, &billingState); err != nil || price != "synthetic-flat-price" || billingState != "confirmed" {
		t.Fatalf("synthetic flat-price projection invariant failed for fixture %s: %v", suffix, err)
	}
	var externalEffect bool
	var jobState string
	if err := db.QueryRow(ctx, "SELECT external_effect,status FROM amos_jobs WHERE id=$1", job).Scan(&externalEffect, &jobState); err != nil || externalEffect || jobState != "queued" {
		t.Fatalf("restored job is not effect-disabled: %v", err)
	}
	var revoked sql.NullTime
	if err := db.QueryRow(ctx, "SELECT revoked_at FROM identity_sessions WHERE id=$1", session).Scan(&revoked); err != nil {
		t.Fatalf("read restored session: %v", err)
	}
	if wantSessionRevoked != revoked.Valid {
		t.Fatalf("session revocation=%v, want %v", revoked.Valid, wantSessionRevoked)
	}
}

func migrationVersions(registry migrations.Registry) []backup.MigrationVersion {
	registered := registry.Migrations()
	out := make([]backup.MigrationVersion, 0, len(registered))
	for _, migration := range registered {
		sum := sha256.Sum256([]byte(migration.SQL))
		out = append(out, backup.MigrationVersion{
			Sequence: migration.Sequence, ID: migration.ID(), SHA256: hex.EncodeToString(sum[:]),
		})
	}
	return out
}

func equalMigrationVersions(a, b []backup.MigrationVersion) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func equalModuleVersions(a, b []backup.ModuleVersion) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func assertDatabaseConnectable(t *testing.T, ctx context.Context, admin *pgx.Conn, name string) {
	t.Helper()
	var allowed bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1 AND datallowconn)", name).Scan(&allowed); err != nil || !allowed {
		t.Fatalf("activated database is not connectable: %v %v", allowed, err)
	}
}
func assertClosedDatabase(t *testing.T, ctx context.Context, admin *pgx.Conn, name string) {
	t.Helper()
	var closed bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1 AND NOT datallowconn)", name).Scan(&closed); err != nil || !closed {
		t.Fatalf("database is not present but connection-fenced: %v %v", closed, err)
	}
}
func assertNoConnectableStagingDatabase(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	var count int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM pg_database WHERE datname LIKE 'amos_restore_%' AND datallowconn").Scan(&count); err != nil || count != 0 {
		t.Fatalf("staging DB accepts connections: count=%d err=%v", count, err)
	}
}

type checkState struct {
	err    error
	calls  int
	failOn int
}

func (c *checkState) CheckCurrentProviderState(context.Context) error {
	c.calls++
	if c.calls == c.failOn {
		return c.err
	}
	return nil
}
func tool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("%s required", name)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
func toolVersion(t *testing.T, path string) string {
	t.Helper()
	output, err := exec.Command(path, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(output))
	if len(fields) < 3 {
		t.Fatal("invalid pg_dump version")
	}
	return fields[2]
}
func databaseURL(t *testing.T, raw, name string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}
func assertAbsent(t *testing.T, ctx context.Context, admin *pgx.Conn, name string) {
	t.Helper()
	var exists bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", name).Scan(&exists); err != nil || exists {
		t.Fatalf("target DB exists before activation: %v %v", exists, err)
	}
}
func dropDB(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if conn, err := pgx.Connect(ctx, os.Getenv("AMOS_RESTORE_TEST_ADMIN_URL")); err == nil {
		_, _ = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize())
		_ = conn.Close(context.Background())
	}
}
