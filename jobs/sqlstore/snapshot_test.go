package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ajent-social/amos/jobs"
	"github.com/ajent-social/amos/observability"
	"github.com/google/uuid"
	"testing"
	"time"
)

type metricsConsumer struct{ resolution jobs.Resolution }

func (c metricsConsumer) Execute(context.Context, jobs.Job, uuid.UUID) jobs.Resolution {
	return c.resolution
}
func (c metricsConsumer) Reconcile(context.Context, jobs.Job, uuid.UUID) jobs.Resolution {
	return c.resolution
}
func TestMetricsSnapshotActualWorkerFailureAndRecovery(t *testing.T) {
	db, s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, _ := observability.New(observability.Config{WorkerNames: []string{"worker"}})
	observer, _ := c.WorkerObserver("worker")
	worker := jobs.Worker{Repository: s, Consumer: metricsConsumer{jobs.Resolution{Kind: jobs.ResolutionRetrySafe}}, Owner: "private-worker", Lease: time.Second, Observer: observer}
	first, err := s.Enqueue(ctx, intent(t, "retry", false))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if ok, err := worker.RunOne(ctx); !ok || err != nil {
			t.Fatalf("worker %v %v", ok, err)
		}
		snapshot, err := s.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_ = c.ObserveQueue("worker", snapshot, nil)
		if i < 2 && snapshot.Queued != 1 {
			t.Fatal("retry backlog missing", snapshot)
		}
		if i == 2 && (snapshot.Dead != 1 || snapshot.RetryAttempts != 2) {
			t.Fatal("dead/retry signal missing", snapshot)
		}
	}
	if c.Snapshot().Workers[0].Outcomes[6] != 2 || c.Snapshot().Workers[0].Outcomes[7] != 1 {
		t.Fatal("worker failure outcomes missing")
	}
	if _, err := s.Enqueue(ctx, intent(t, "success", false)); err != nil {
		t.Fatal(err)
	}
	worker.Consumer = metricsConsumer{jobs.Resolution{Kind: jobs.ResolutionSucceeded}}
	if _, err := worker.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	unknown, err := s.Enqueue(ctx, intent(t, "unknown", true))
	if err != nil {
		t.Fatal(err)
	}
	worker.Consumer = metricsConsumer{jobs.Resolution{Kind: jobs.ResolutionUnknown}}
	if _, err := worker.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := s.Get(ctx, unknown.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.Get(ctx, unknown.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.FenceToken != after.FenceToken || before.State != after.State || snapshot.Unknown != 1 || !snapshot.LastSuccessKnown || snapshot.LastSuccess.After(snapshot.ObservedAt) {
		t.Fatalf("snapshot mutated or misreported: %+v", snapshot)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM amos_jobs WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	reduced, err := s.Snapshot(ctx)
	if err != nil || reduced.RetryAttempts >= snapshot.RetryAttempts {
		t.Fatal("retry gauge claimed lifetime count", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := s.Snapshot(canceled); err == nil {
		t.Fatal("canceled snapshot healthy")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = s.Snapshot(ctx)
	if err == nil {
		t.Fatal("closed pool healthy")
	}
	_ = c.ObserveQueue("worker", snapshot, nil)
	_ = c.ObserveQueue("worker", jobs.Snapshot{}, err)
	if c.Snapshot().Workers[0].Queue.State != "stale" {
		t.Fatal("stale state missing")
	}
}
func TestMetricsSnapshotScopeAndReadOnly(t *testing.T) {
	db, all := testStore(t)
	ctx := context.Background()
	scope := newClaimScope(t)
	foreign := scope
	foreign.EnvironmentID = newClaimUUID(t)
	kind := claimScopeIntent(t, "other-kind", scope)
	kind.Kind = "other.kind"
	for _, in := range []jobs.Intent{claimScopeIntent(t, "wanted", scope), claimScopeIntent(t, "foreign", foreign), kind} {
		if _, err := all.Enqueue(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	cfg := all.config
	cfg.ClaimKinds = []string{"billing.webhook.reconcile"}
	cfg.ClaimScopes = []ClaimScope{scope}
	runnerCalls := 0
	scoped, err := NewWithTx(runnerFunc(func(ctx context.Context, options *sql.TxOptions, fn func(*sql.Tx) error) error {
		runnerCalls++
		if options == nil || !options.ReadOnly || options.Isolation != sql.LevelReadCommitted {
			t.Fatal("snapshot transaction not read only")
		}
		return (poolRunner{db: db}).WithTx(ctx, options, func(tx *sql.Tx) error {
			var mode string
			if err := tx.QueryRowContext(ctx, "SHOW transaction_read_only").Scan(&mode); err != nil {
				return err
			}
			if mode != "on" {
				t.Fatal("actual transaction writable")
			}
			return fn(tx)
		})
	}), cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := func() string {
		var v string
		if err := db.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(j) ORDER BY id)::text FROM amos_jobs j`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}()
	snapshot, err := scoped.Snapshot(ctx)
	if err != nil || snapshot.Queued != 1 || snapshot.LastSuccessKnown {
		t.Fatalf("scope %+v err %v", snapshot, err)
	}
	legacy, err := all.Snapshot(ctx)
	if err != nil || legacy.Queued != 3 {
		t.Fatalf("legacy %+v %v", legacy, err)
	}
	var after string
	if err := db.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(j) ORDER BY id)::text FROM amos_jobs j`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if runnerCalls != 1 {
		t.Fatal("snapshot replayed callback")
	}
	if before != after {
		t.Fatal("snapshot changed rows")
	}
	kindOnly := cfg
	kindOnly.ClaimScopes = nil
	byKind, err := New(db, kindOnly)
	if err != nil {
		t.Fatal(err)
	}
	v, err := byKind.Snapshot(ctx)
	if err != nil || v.Queued != 2 {
		t.Fatal("kind filter", v, err)
	}
	var nilStore *Store
	if _, err := nilStore.Snapshot(ctx); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal(err)
	}
}
