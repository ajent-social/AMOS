package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type scheduleFixtureProfile struct {
	Host      string `json:"host"`
	Port      uint16 `json:"port"`
	CA        string `json:"ca_path"`
	LedgerDB  string `json:"ledger_database"`
	DumpDB    string `json:"dump_database"`
	Writer    string `json:"ledger_write_role"`
	Reader    string `json:"ledger_read_role"`
	Dump      string `json:"dump_role"`
	WriteURL  string `json:"ledger_write_url"`
	ReadURL   string `json:"ledger_read_url"`
	DumpURL   string `json:"dump_url"`
	OwnerURL  string `json:"owner_url"`
	PGDump    string `json:"pg_dump_path"`
	PGVersion string `json:"pg_dump_version"`
}
type scheduleFixtureWriter struct {
	*memoryObjectClient
	fail bool
}

func (*scheduleFixtureWriter) validateScheduleRuntime(ScheduleAWSConfig, time.Duration) error {
	return nil
}
func (w *scheduleFixtureWriter) PutObject(ctx context.Context, in PutObjectInput) (PutObjectOutput, error) {
	if w.fail {
		return PutObjectOutput{}, errors.New("synthetic publication unavailable")
	}
	return w.memoryObjectClient.PutObject(ctx, in)
}
func scheduleFixture(t *testing.T) (*Scheduler, *ScheduleReader, *scheduleFixtureWriter, scheduleFixtureProfile) {
	t.Helper()
	path := os.Getenv("AMOS_BACKUP_SCHEDULE_TEST_PROFILE")
	if path == "" {
		t.Fatal("AMOS_BACKUP_SCHEDULE_TEST_PROFILE required for real TLS scheduling tests")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal("private fixture profile unavailable")
	}
	var p scheduleFixtureProfile
	if json.Unmarshal(b, &p) != nil {
		t.Fatal("private fixture profile invalid")
	}
	cfg := scheduleTestConfig(t)
	cfg.ScheduleID, _ = uuid.NewV7()
	cfg.ApplicationID, _ = uuid.NewV7()
	cfg.InstallationID, _ = uuid.NewV7()
	cfg.AWS.Prefix = "installations/" + cfg.InstallationID.String() + "/development"
	cfg.Database.LedgerHost = p.Host
	cfg.Database.DumpHost = p.Host
	cfg.Database.LedgerPort = p.Port
	cfg.Database.DumpPort = p.Port
	cfg.Database.LedgerDatabase = p.LedgerDB
	cfg.Database.DumpDatabase = p.DumpDB
	cfg.Database.LedgerWriteRole = p.Writer
	cfg.Database.LedgerReadRole = p.Reader
	cfg.Database.DumpRole = p.Dump
	cfg.Database.LedgerRootCAFile = p.CA
	cfg.Database.DumpRootCAFile = p.CA
	cfg.Create.PGDumpPath = p.PGDump
	cfg.Create.PGDumpVersion = p.PGVersion
	open := func(raw string) *sql.DB {
		db, e := sql.Open("pgx", raw)
		if e != nil {
			t.Fatal("fixture open")
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	db := open(p.WriteURL)
	readDB := open(p.ReadURL)
	w := &scheduleFixtureWriter{memoryObjectClient: newMemoryObjectClient()}
	store, e := NewStore(w, cfg.AWS.Bucket, cfg.AWS.Prefix, cfg.AWS.KMSKeyARN, cfg.Create.MaxBytes)
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewScheduler(db, cfg, store, p.DumpURL)
	if e != nil {
		t.Fatal(e)
	}
	reader, e := NewScheduleReader(readDB, cfg)
	if e != nil {
		t.Fatal(e)
	}
	return s, reader, w, p
}
func TestScheduleActualDumpPublicationAndRestart(t *testing.T) {
	s, r, w, _ := scheduleFixture(t)
	ctx := context.Background()
	before, e := r.Snapshot(ctx)
	if e != ErrScheduleUnavailable || before.PublicationKnown {
		t.Fatal("initial stream unexpectedly fresh")
	}
	home := t.TempDir()
	if os.Mkdir(filepath.Join(home, ".postgresql"), 0700) != nil {
		t.Fatal("home")
	}
	for _, name := range []string{"postgresql.crt", "postgresql.key"} {
		if os.WriteFile(filepath.Join(home, ".postgresql", name), []byte("synthetic invalid identity"), 0600) != nil {
			t.Fatal("home auth")
		}
	}
	t.Setenv("HOME", home)
	if e := s.preflightDump(ctx); e != nil {
		t.Fatal("dump identity preflight", e)
	}
	if ok, e := s.stagingCapacity(ctx); e != nil || !ok {
		info, _ := os.Lstat(s.cfg.Create.StagingDir)
		entries, _ := os.ReadDir(s.cfg.Create.StagingDir)
		t.Fatalf("staging capacity mode=%v owned=%v entries=%d err=%v", info.Mode(), scheduleOwned(info, true), len(entries), e)
	}
	result, e := s.RunDue(ctx)
	if e != nil || result != ScheduleCompleted {
		t.Fatalf("real scheduled dump/publication failed: result=%s", result)
	}
	snap, e := r.Snapshot(ctx)
	if e != nil || !snap.PublicationKnown || snap.State != "completed" || snap.LastSnapshotAt.IsZero() || snap.LastSnapshotAt.After(snap.LastPublishedAt) || snap.RetainedAttempts != 1 || len(w.objects) != 2 {
		t.Fatal("acknowledged snapshot did not become sole freshness authority")
	}
	for _, name := range []string{"postgresql.crt", "postgresql.key"} {
		b, e := os.ReadFile(filepath.Join(home, ".postgresql", name))
		if e != nil || string(b) != "synthetic invalid identity" {
			t.Fatal("default client identity modified")
		}
	}
	restarted, e := NewScheduler(s.db, s.cfg, s.store, s.dumpURL)
	if e != nil {
		t.Fatal(e)
	}
	result, e = restarted.RunDue(ctx)
	if e != nil || result != ScheduleNotDue || len(w.objects) != 2 {
		t.Fatal("restart replayed spent slot")
	}
	bad := s.cfg
	bad.UploadTimeout += time.Second
	other, e := NewScheduler(s.db, bad, s.store, s.dumpURL)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.RunDue(ctx); e == nil {
		t.Fatal("immutable policy binding rebound")
	}
}
func TestScheduleFailedUploadNeverFresh(t *testing.T) {
	s, r, w, _ := scheduleFixture(t)
	w.fail = true
	result, e := s.RunDue(context.Background())
	if e == nil || result != ScheduleUnknown {
		t.Fatal("failed publication accepted")
	}
	snap, e := r.Snapshot(context.Background())
	if e != nil || snap.PublicationKnown || !snap.LastSnapshotAt.IsZero() || snap.RetainedAttempts != 1 || snap.State != "unknown" {
		t.Fatal("failed upload promoted false freshness")
	}
	result, e = s.RunDue(context.Background())
	if e != nil || result != ScheduleNotDue {
		t.Fatal("failed slot retried")
	}
}
func TestScheduleLeaseOverlapExpiryAndFencing(t *testing.T) {
	s, r, _, _ := scheduleFixture(t)
	ctx := context.Background()
	lease, result, e := s.admit(ctx, false)
	if e != nil || result != "" {
		t.Fatal("admission")
	}
	_, result, e = s.admit(ctx, false)
	if e != nil || result != ScheduleBusy {
		t.Fatal("overlap not fenced")
	}
	s.clock = func(now time.Time) time.Time { return now.Add(2 * time.Minute) }
	next, result, e := s.admit(ctx, false)
	if e != nil || result != "" || next.id == lease.id || next.fence <= lease.fence {
		t.Fatal("expired lease did not advance independent identity")
	}
	if s.finish(ctx, lease, nil, "dump", time.Second) == nil {
		t.Fatal("old fence completed")
	}
	snap, e := r.Snapshot(ctx)
	if e == nil && snap.PublicationKnown {
		t.Fatal("lease loss promoted freshness")
	}
	if s.finish(ctx, next, nil, "dump", time.Second) != nil {
		t.Fatal("new lease cleanup")
	}
}
func TestScheduleCapacityAndScopeIsolation(t *testing.T) {
	s, r, _, _ := scheduleFixture(t)
	s.cfg.MaxRetainedAttempts = 1
	s.digest, _ = ScheduleConfigDigest(s.cfg)
	r.cfg = s.cfg
	r.digest = s.digest
	lease, _, e := s.admit(context.Background(), false)
	if e != nil {
		t.Fatal("admit")
	}
	if s.finish(context.Background(), lease, nil, "dump", 0) != nil {
		t.Fatal("finish")
	}
	s.clock = func(now time.Time) time.Time { return now.Add(2 * time.Minute) }
	_, result, e := s.admit(context.Background(), false)
	if e != ErrScheduleFailed || result != ScheduleCapacity {
		t.Fatal("retained cap exceeded")
	}
	foreign := s.cfg
	foreign.ScheduleID, _ = uuid.NewV7()
	other, e := NewScheduleReader(r.db, foreign)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.Snapshot(context.Background()); e == nil {
		t.Fatal("foreign schedule scope adopted")
	}
	if os.WriteFile(filepath.Join(s.cfg.Create.StagingDir, "foreign"), []byte("x"), 0600) != nil {
		t.Fatal("fixture")
	}
	if _, e = s.stagingCapacity(context.Background()); e == nil {
		t.Fatal("foreign staging entry accepted")
	}
}
func TestScheduleClockRegressionAndReaderAuthority(t *testing.T) {
	s, r, _, _ := scheduleFixture(t)
	ctx := context.Background()
	lease, _, e := s.admit(ctx, false)
	if e != nil {
		t.Fatal("admit")
	}
	if s.finish(ctx, lease, nil, "dump", 0) != nil {
		t.Fatal("finish")
	}
	if _, e = r.db.ExecContext(ctx, `UPDATE public.amos_backup_schedules SET state='idle' WHERE schedule_id=$1`, s.cfg.ScheduleID); e == nil {
		t.Fatal("read authority could mutate ledger")
	}
	s.clock = func(now time.Time) time.Time { return now.Add(-time.Hour) }
	_, result, e := s.admit(ctx, false)
	if e != ErrScheduleUnavailable || result != ScheduleUnknown {
		t.Fatal("backward clock accepted")
	}
	snapshot, e := s.Snapshot(ctx)
	if e == nil || snapshot.PublicationKnown || snapshot.State != "clock_invalid" {
		t.Fatal("clock anomaly healthy")
	}
}
func TestScheduleCancelledCallerCreatesNoAttempt(t *testing.T) {
	s, r, _, _ := scheduleFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := s.RunDue(ctx); e == nil {
		t.Fatal("cancelled caller admitted")
	}
	snap, e := r.Snapshot(context.Background())
	if e != ErrScheduleUnavailable || snap.PublicationKnown {
		t.Fatal("cancelled admission wrote ledger")
	}
}
func TestSchedulePreviousPublicationSurvivesLaterFailure(t *testing.T) {
	s, _, w, _ := scheduleFixture(t)
	ctx := context.Background()
	if result, e := s.RunDue(ctx); e != nil || result != ScheduleCompleted {
		t.Fatal("initial actual publication")
	}
	before, e := s.Snapshot(ctx)
	if e != nil || !before.PublicationKnown {
		t.Fatal("initial snapshot")
	}
	s.clock = func(now time.Time) time.Time { return now.Add(2 * time.Minute) }
	w.fail = true
	if result, e := s.RunDue(ctx); e == nil || result != ScheduleUnknown {
		t.Fatal("later failure")
	}
	after, e := s.Snapshot(ctx)
	if e != nil || !after.PublicationKnown || !after.LastSnapshotAt.Equal(before.LastSnapshotAt) || !after.LastPublishedAt.Equal(before.LastPublishedAt) || after.RetainedAttempts != 2 {
		t.Fatal("failed attempt changed acknowledged snapshot authority")
	}
}
func TestScheduleKnownEmptyCrashDirectory(t *testing.T) {
	s, _, _, _ := scheduleFixture(t)
	lease, _, e := s.admit(context.Background(), false)
	if e != nil {
		t.Fatal("admit")
	}
	if os.Mkdir(filepath.Join(s.cfg.Create.StagingDir, "backup-"+lease.id.String()), 0700) != nil {
		t.Fatal("owned empty crash fixture")
	}
	if s.finish(context.Background(), lease, nil, "dump", 0) != nil {
		t.Fatal("finish")
	}
	if ok, e := s.stagingCapacity(context.Background()); e != nil || !ok {
		t.Fatal("known empty retained crash directory rejected")
	}
}
func TestScheduleConcurrentSQLAdmission(t *testing.T) {
	s, _, _, profile := scheduleFixture(t)
	secondPool, e := sql.Open("pgx", profile.WriteURL)
	if e != nil {
		t.Fatal("independent writer pool")
	}
	defer func() { _ = secondPool.Close() }()
	second, e := NewScheduler(secondPool, s.cfg, s.store, s.dumpURL)
	if e != nil {
		t.Fatal(e)
	}
	if second == s || second.db == s.db {
		t.Fatal("scheduler instances share writer pool")
	}
	type outcome struct {
		lease  scheduleLease
		result ScheduleResult
		err    error
	}
	out := make(chan outcome, 2)
	start := make(chan struct{})
	for _, instance := range []*Scheduler{s, second} {
		go func() { <-start; l, r, e := instance.admit(context.Background(), false); out <- outcome{l, r, e} }()
	}
	close(start)
	var winner scheduleLease
	busy := 0
	for range 2 {
		o := <-out
		if o.err != nil {
			t.Fatal("concurrent admission error")
		}
		switch o.result {
		case ScheduleBusy:
			busy++
		case "":
			winner = o.lease
		default:
			t.Fatal("unexpected concurrent result")
		}
	}
	if busy != 1 || winner.id == uuid.Nil {
		t.Fatal("overlapping SQL admission was not serialized")
	}
	if s.finish(context.Background(), winner, nil, "dump", 0) != nil {
		t.Fatal("winner cleanup")
	}
}

func TestScheduleActualDumpCancellation(t *testing.T) {
	s, r, _, _ := scheduleFixture(t)
	raw, e := os.ReadFile(os.Getenv("AMOS_BACKUP_SCHEDULE_TEST_PROFILE"))
	if e != nil {
		t.Fatal("profile")
	}
	var p map[string]any
	if json.Unmarshal(raw, &p) != nil {
		t.Fatal("profile")
	}
	owner, e := sql.Open("pgx", p["target_owner_url"].(string))
	if e != nil {
		t.Fatal("owner fixture")
	}
	defer func() { _ = owner.Close() }()
	lock, e := owner.BeginTx(context.Background(), nil)
	if e != nil {
		t.Fatal("lock fixture")
	}
	defer func() { _ = lock.Rollback() }()
	if _, e = lock.Exec(`LOCK TABLE public.backup_fixture_payload IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal("lock fixture")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := s.RunDue(ctx); done <- e }()
	deadline := time.Now().Add(3 * time.Second)
	started := false
	for time.Now().Before(deadline) {
		files, _ := filepath.Glob(filepath.Join(s.cfg.Create.StagingDir, "backup-*", "database.dump.partial"))
		if len(files) > 0 {
			started = true
			break
		}
		select {
		case <-done:
			t.Fatal("dump exited before real blocked child")
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if !started {
		t.Fatal("real pg_dump child did not start")
	}
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled dump completed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled real dump did not return")
	}
	snap, e := r.Snapshot(context.Background())
	if e != nil || snap.PublicationKnown || snap.RetainedAttempts != 1 {
		t.Fatal("cancelled actual dump promoted freshness")
	}
}

// Version acknowledgments here are a finite fixture for the SQL completion
// boundary; they make no claim about live object-provider behavior.
func TestScheduleDelayedSuccessfulAcknowledgmentCannotCrossFence(t *testing.T) {
	s, _, _, _ := scheduleFixture(t)
	ctx := context.Background()
	receipt := func(l scheduleLease) *StoredBackup {
		return &StoredBackup{Manifest: Manifest{SnapshotTransactionStartedAt: l.started}, ArchiveVersionID: "fixture-archive-" + l.id.String(), ManifestVersionID: "fixture-manifest-" + l.id.String()}
	}
	prior, result, e := s.admit(ctx, false)
	if e != nil || result != "" {
		t.Fatal("initial admission")
	}
	if s.finish(ctx, prior, receipt(prior), "", time.Millisecond) != nil {
		t.Fatal("prior acknowledgment")
	}
	s.clock = func(now time.Time) time.Time { return now.Add(2 * time.Minute) }
	delayed, result, e := s.admit(ctx, false)
	if e != nil || result != "" {
		t.Fatal("delayed admission")
	}
	s.clock = func(now time.Time) time.Time { return now.Add(4 * time.Minute) }
	successor, result, e := s.admit(ctx, false)
	if e != nil || result != "" || successor.fence <= delayed.fence {
		t.Fatal("successor admission")
	}
	type truth struct {
		state, success, active string
		fence                  int64
		snapshot, published    time.Time
	}
	read := func() truth {
		var v truth
		if s.db.QueryRowContext(ctx, `SELECT state,last_success_attempt_id::text,coalesce(active_attempt_id::text,''),fence,last_snapshot_at,last_published_at FROM public.amos_backup_schedules WHERE schedule_id=$1`, s.cfg.ScheduleID).Scan(&v.state, &v.success, &v.active, &v.fence, &v.snapshot, &v.published) != nil {
			t.Fatal("read fenced truth")
		}
		return v
	}
	assertAttempts := func() {
		var oldState, newState string
		if s.db.QueryRowContext(ctx, `SELECT state FROM public.amos_backup_attempts WHERE schedule_id=$1 AND attempt_id=$2`, s.cfg.ScheduleID, delayed.id).Scan(&oldState) != nil || s.db.QueryRowContext(ctx, `SELECT state FROM public.amos_backup_attempts WHERE schedule_id=$1 AND attempt_id=$2`, s.cfg.ScheduleID, successor.id).Scan(&newState) != nil || oldState != "unknown" || newState != "running" {
			t.Fatal("stale acknowledgment changed attempt outcomes")
		}
	}
	before := read()
	if before.success != prior.id.String() || before.active != successor.id.String() {
		t.Fatal("expected prior success and active successor")
	}
	if s.finish(ctx, delayed, receipt(delayed), "", time.Second) == nil {
		t.Fatal("stale successful acknowledgment crossed fence")
	}
	if read() != before {
		t.Fatal("stale acknowledgment changed prior success or successor state")
	}
	assertAttempts()
	if s.finish(ctx, successor, receipt(successor), "", time.Millisecond) != nil {
		t.Fatal("successor acknowledgment")
	}
	completed := read()
	if completed.success != successor.id.String() || completed.state != "completed" {
		t.Fatal("successor did not complete")
	}
	if s.finish(ctx, delayed, receipt(delayed), "", time.Second) == nil || read() != completed {
		t.Fatal("late acknowledgment replaced successor success")
	}
}
