package ops

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/backup"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// This required-service check seeds operational rows through the fixture owner.
// It verifies the concrete read-only adapter, not actual remote publication.
func TestBackupObservationRequiredPostgresSnapshotAge(t *testing.T) {
	path := os.Getenv("AMOS_BACKUP_SCHEDULE_TEST_PROFILE")
	if path == "" {
		t.Fatal("required backup TLS PostgreSQL profile is absent")
	}
	var p struct {
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
	b, e := os.ReadFile(path)
	if e != nil || json.Unmarshal(b, &p) != nil {
		t.Fatal("required private fixture profile unreadable")
	}
	c := backupScheduleFixture(t)
	c.Database.LedgerHost = p.Host
	c.Database.DumpHost = p.Host
	c.Database.LedgerPort = p.Port
	c.Database.DumpPort = p.Port
	c.Database.LedgerDatabase = p.LedgerDB
	c.Database.DumpDatabase = p.DumpDB
	c.Database.LedgerRootCAFile = p.CA
	c.Database.DumpRootCAFile = p.CA
	c.Database.LedgerWriteRole = p.Writer
	c.Database.LedgerReadRole = p.Reader
	c.Database.DumpRole = p.Dump
	normalized, e := backup.NormalizeScheduleDatabaseURL(c.Database, backup.ScheduleLedgerRead, p.ReadURL)
	if e != nil {
		t.Fatal("private fixture reader URL validation failed")
	}
	readerDB, e := sql.Open("pgx", normalized)
	if e != nil {
		t.Fatal("private fixture reader unavailable")
	}
	defer func() { _ = readerDB.Close() }()
	owner, e := sql.Open("pgx", p.OwnerURL)
	if e != nil {
		t.Fatal("private fixture owner unavailable")
	}
	defer func() { _ = owner.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var now time.Time
	if owner.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now) != nil {
		t.Fatal("database clock unavailable")
	}
	digest, e := backup.ScheduleConfigDigest(c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = owner.ExecContext(ctx, `INSERT INTO amos_backup_schedules(schedule_id,installation_id,application_id,environment,config_digest,clock_watermark) VALUES($1,$2,$3,$4,$5,$6)`, c.ScheduleID, c.InstallationID, c.ApplicationID, c.Environment, digest[:], now.Add(-time.Minute)); e != nil {
		t.Fatal("fixture stream insert failed")
	}
	r, e := backup.NewScheduleReader(readerDB, c)
	if e != nil {
		t.Fatal(e)
	}
	empty, e := ReadBackupObservation(ctx, r)
	if e != nil || empty.Available {
		t.Fatal("empty ledger fabricated freshness")
	}
	attempt, e := uuid.NewV7()
	if e != nil {
		t.Fatal(e)
	}
	snapshot := now.Add(-25 * time.Hour).UTC().Truncate(time.Microsecond)
	published := now.Add(-time.Minute).UTC().Truncate(time.Microsecond)
	if _, e = owner.ExecContext(ctx, `INSERT INTO amos_backup_attempts(attempt_id,schedule_id,slot_at,fence,started_at,finished_at,state,snapshot_at,published_at,archive_version_id,manifest_version_id) VALUES($1,$2,$3,1,$4,$3,'completed',$4,$3,'example-archive','example-manifest')`, attempt, c.ScheduleID, published, snapshot); e != nil {
		t.Fatal("fixture completed attempt insert failed")
	}
	if _, e = owner.ExecContext(ctx, `UPDATE amos_backup_schedules SET state='completed',last_success_attempt_id=$2,last_snapshot_at=$3,last_published_at=$4 WHERE schedule_id=$1`, c.ScheduleID, attempt, snapshot, published); e != nil {
		t.Fatal("fixture completed pointer update failed")
	}
	got, e := ReadBackupObservation(ctx, r)
	if e != nil || !got.Available || !got.Value.Equal(snapshot) || got.Value.Equal(published) || got.At.Sub(got.Value) < 24*time.Hour {
		t.Fatal("adapter reset RPO clock to acknowledgement")
	}
	if _, e = owner.ExecContext(ctx, `UPDATE amos_backup_schedules SET state='failed',failure_class='upload' WHERE schedule_id=$1`, c.ScheduleID); e != nil {
		t.Fatal("fixture failure update failed")
	}
	failed, e := ReadBackupObservation(ctx, r)
	if e != nil || !failed.Available || !failed.Value.Equal(snapshot) {
		t.Fatal("failed attempt changed successful snapshot age")
	}
	alerts := fixtureConfig()
	alerts.Options.FailureHold = 0
	definitions, e := Generate(alerts)
	if e != nil {
		t.Fatal(e)
	}
	sample := healthySample(failed.At, 1)
	sample.Backup = failed
	evaluated, e := Evaluate(definitions, Samples{Version: SamplesVersion, Samples: []Sample{sample}})
	if e != nil {
		t.Fatal(e)
	}
	if evaluated.Evaluations[0].Results[5].State != "firing" {
		t.Fatal("failed upload cleared actual backup freshness alert")
	}

	if _, e = owner.ExecContext(ctx, `UPDATE amos_backup_schedules SET clock_watermark=clock_timestamp()+interval '1 hour' WHERE schedule_id=$1`, c.ScheduleID); e != nil {
		t.Fatal("fixture clock update failed")
	}
	invalid, e := ReadBackupObservation(ctx, r)
	if e == nil || invalid.Available {
		t.Fatal("future clock fabricated health")
	}
	// Deliberately retain uniquely-owned synthetic rows for inspection; no cleanup
	// authority is inferred over other streams in this shared disposable database.
}
