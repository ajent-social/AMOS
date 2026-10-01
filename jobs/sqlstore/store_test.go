package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/jobs"
	"github.com/google/uuid"
)

func testStore(t *testing.T) (*sql.DB, *Store) {
	t.Helper()
	db, _ := testkit.NewPostgres(t)
	schema, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "jobs.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(schema), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("apply jobs migration fixture: %v", err)
		}
	}
	s, err := New(db, Config{MaxPayloadBytes: 4096, MaxAttempts: 3, MaxReconciliationAttempts: 4, MaxLease: time.Second, MaxRetryDelay: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return db, s
}

func intent(t *testing.T, key string, external bool) jobs.Intent {
	t.Helper()
	installation, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	application, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return jobs.Intent{InstallationID: installation, ApplicationID: application, Key: key, Kind: "email.send", Payload: []byte(`{"to":"synthetic@example.test"}`), ExternalEffect: external, Deadline: time.Now().UTC().Add(time.Hour)}
}

func TestEnqueueIsTransactionalIdempotentAndDurable(t *testing.T) {
	db, s := testStore(t)
	ctx := context.Background()
	in := intent(t, "idempotent-1", true)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rolled, err := s.EnqueueTx(ctx, tx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE test_domain_rows (id integer PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO test_domain_rows VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, rolled.ID); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("rolled back outbox row exists: %v", err)
	}
	var domainCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='test_domain_rows'`).Scan(&domainCount); err != nil {
		t.Fatal(err)
	}
	if domainCount != 0 {
		t.Fatal("domain write survived outbox rollback")
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE test_domain_rows (id integer PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO test_domain_rows VALUES (2)`); err != nil {
		t.Fatal(err)
	}
	created, err := s.EnqueueTx(ctx, tx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM test_domain_rows`).Scan(&domainCount); err != nil {
		t.Fatal(err)
	}
	if domainCount != 1 {
		t.Fatal("domain write did not commit with outbox intent")
	}
	restartStore, err := New(db, s.config)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := restartStore.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != created.ID || loaded.State != jobs.StateQueued {
		t.Fatalf("durable job changed: %#v", loaded)
	}
	replay, err := s.Enqueue(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != created.ID {
		t.Fatalf("same key/request created %s instead of %s", replay.ID, created.ID)
	}
	changed := in
	changed.Payload = []byte(`{"to":"different@example.test"}`)
	if _, err := s.Enqueue(ctx, changed); !errors.Is(err, jobs.ErrIdempotencyConflict) {
		t.Fatalf("changed request error=%v", err)
	}
}

func TestConcurrentWorkersClaimIntentOnce(t *testing.T) {
	_, s := testStore(t)
	ctx := context.Background()
	if _, ok, err := s.Claim(ctx, "empty-worker", 500*time.Millisecond); err != nil || ok {
		t.Fatalf("empty queue claim ok=%v err=%v", ok, err)
	}
	if _, err := s.Enqueue(ctx, intent(t, "concurrent-1", false)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	claimed := make(chan jobs.Job, 2)
	errs := make(chan error, 2)
	for _, owner := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			job, ok, err := s.Claim(ctx, owner, 500*time.Millisecond)
			if err != nil {
				errs <- err
			} else if ok {
				claimed <- job
			}
		}(owner)
	}
	wg.Wait()
	close(claimed)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	count := 0
	for range claimed {
		count++
	}
	if count != 1 {
		t.Fatalf("concurrent claims=%d, want 1", count)
	}
}

func TestExpiredLeaseFencesStaleCompletionAndReconciles(t *testing.T) {
	_, s := testStore(t)
	ctx := context.Background()
	in := intent(t, "lease-loss-1", true)
	if _, err := s.Enqueue(ctx, in); err != nil {
		t.Fatal(err)
	}
	stale, ok, err := s.Claim(ctx, "old-worker", 90*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("first claim ok=%v err=%v", ok, err)
	}
	time.Sleep(140 * time.Millisecond)
	current, ok, err := s.Claim(ctx, "new-worker", 500*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("recovery claim ok=%v err=%v", ok, err)
	}
	if current.Action != jobs.ActionReconcile || current.State != jobs.StateUnknown {
		t.Fatalf("expired external outcome was replayed: action=%s state=%s", current.Action, current.State)
	}
	if err := s.Resolve(ctx, stale, "old-worker", jobs.Resolution{Kind: jobs.ResolutionSucceeded}); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatalf("stale completion error=%v", err)
	}
	if err := s.Resolve(ctx, current, "new-worker", jobs.Resolution{Kind: jobs.ResolutionUnknown, RetryAfter: 100 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Get(ctx, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != jobs.StateUnknown || stored.LeaseOwner != "" {
		t.Fatalf("unresolved outcome state=%s owner=%q", stored.State, stored.LeaseOwner)
	}
}

func TestUnknownOutcomeSelectsReconciliationNotExecution(t *testing.T) {
	_, s := testStore(t)
	ctx := context.Background()
	if _, err := s.Enqueue(ctx, intent(t, "unknown-outcome-1", true)); err != nil {
		t.Fatal(err)
	}
	first, ok, err := s.Claim(ctx, "worker-a", 500*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if first.Action != jobs.ActionExecute {
		t.Fatalf("initial action=%s", first.Action)
	}
	if err := s.Resolve(ctx, first, "worker-a", jobs.Resolution{Kind: jobs.ResolutionUnknown}); err != nil {
		t.Fatal(err)
	}
	reconcile, ok, err := s.Claim(ctx, "worker-b", 500*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("reconcile claim ok=%v err=%v", ok, err)
	}
	if reconcile.Action != jobs.ActionReconcile {
		t.Fatalf("unknown remote outcome action=%s", reconcile.Action)
	}
}

func TestAttemptBudgetsAndUnknownReviewAreBounded(t *testing.T) {
	_, s := testStore(t)
	ctx := context.Background()
	if _, err := s.Enqueue(ctx, intent(t, "retry-budget-1", false)); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		job, ok, err := s.Claim(ctx, "retry-worker", 500*time.Millisecond)
		if err != nil || !ok {
			t.Fatalf("attempt %d claim ok=%v err=%v", attempt, ok, err)
		}
		if job.Attempt != attempt {
			t.Fatalf("attempt=%d want=%d", job.Attempt, attempt)
		}
		if err := s.Resolve(ctx, job, "retry-worker", jobs.Resolution{Kind: jobs.ResolutionRetrySafe}); err != nil {
			t.Fatal(err)
		}
	}
	var id uuid.UUID
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM amos_jobs WHERE idempotency_key='retry-budget-1'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	dead, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if dead.State != jobs.StateDead {
		t.Fatalf("exhausted job state=%s", dead.State)
	}
	if _, ok, err := s.Claim(ctx, "retry-worker", 500*time.Millisecond); err != nil || ok {
		t.Fatalf("dead job was claimable: ok=%v err=%v", ok, err)
	}

	if _, err := s.Enqueue(ctx, intent(t, "manual-review-1", true)); err != nil {
		t.Fatal(err)
	}
	job, ok, err := s.Claim(ctx, "effect-worker", 500*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("effect claim ok=%v err=%v", ok, err)
	}
	if err := s.Resolve(ctx, job, "effect-worker", jobs.Resolution{Kind: jobs.ResolutionUnknown}); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 4; attempt++ {
		job, ok, err = s.Claim(ctx, "reconcile-worker", 500*time.Millisecond)
		if err != nil || !ok {
			t.Fatalf("reconciliation %d claim ok=%v err=%v", attempt, ok, err)
		}
		if err := s.Resolve(ctx, job, "reconcile-worker", jobs.Resolution{Kind: jobs.ResolutionUnknown}); err != nil {
			t.Fatal(err)
		}
	}
	manual, err := s.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if manual.State != jobs.StateUnknown || !manual.ManualReview {
		t.Fatalf("exhausted reconciliation state=%s manual=%v", manual.State, manual.ManualReview)
	}
	if _, ok, err := s.Claim(ctx, "reconcile-worker", 500*time.Millisecond); err != nil || ok {
		t.Fatalf("manual-review job was claimable: ok=%v err=%v", ok, err)
	}
}

func TestClaimKindsPreservesOtherConsumersAndExpiredLeases(t *testing.T) {
	db, all := testStore(t)
	ctx := context.Background()
	billing := intent(t, "billing-kind", true)
	billing.Kind = "billing.webhook.reconcile"
	first, err := all.Enqueue(ctx, billing)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := all.Claim(ctx, "billing-owner", time.Second)
	if err != nil || !ok || claimed.ID != first.ID {
		t.Fatal("billing lease unavailable", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE amos_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", first.ID); err != nil {
		t.Fatal(err)
	}
	mail := intent(t, "mail-kind", true)
	mail.Kind = "email.send"
	second, err := all.Enqueue(ctx, mail)
	if err != nil {
		t.Fatal(err)
	}
	cfg := all.config
	cfg.ClaimKinds = []string{"email.send"}
	selected, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ClaimKinds[0] = "billing.webhook.reconcile"
	got, ok, err := selected.Claim(ctx, "mail-owner", time.Second)
	if err != nil || !ok || got.ID != second.ID {
		t.Fatal("wrong consumer claimed", err)
	}
	var state, owner string
	if err := db.QueryRowContext(ctx, "SELECT status,lease_owner FROM amos_jobs WHERE id=$1", first.ID).Scan(&state, &owner); err != nil {
		t.Fatal(err)
	}
	if state != "leased" || owner != "billing-owner" {
		t.Fatal("mail worker maintained another consumer lease")
	}
	if _, ok, err := selected.Claim(ctx, "mail-owner", time.Second); err != nil || ok {
		t.Fatal("mail worker claimed foreign work", err)
	}
}

func TestTxWriterEnqueueRollbackAndStableReplay(t *testing.T) {
	db, reader := testStore(t)
	writer, err := NewTxWriter(reader.config)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	in := intent(t, "transaction-only", false)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rolled, err := writer.EnqueueTx(ctx, tx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Get(ctx, rolled.ID); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatal("uncommitted outbox visible")
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := writer.EnqueueTx(ctx, tx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := writer.EnqueueTx(ctx, tx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatal("stable replay duplicated job")
	}
}
