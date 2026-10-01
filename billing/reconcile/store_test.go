package reconcile

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestT5_8_RetryRejectsExpiredLeaseUsingDatabaseClock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, binding, _, accountID, _ := newReconcileDB(t)
	markDirty(t, db, binding, "cus_current")
	work := claim(t, db)

	var leaseDeadline, scheduledBefore time.Time
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `UPDATE billing_reconcile_work
			SET lease_until=clock_timestamp()+interval '250 milliseconds',
				next_attempt_at=clock_timestamp()+interval '2 hours'
			WHERE id=$1 AND claim_token=$2 AND claim_version=$3 AND dirty_version=$3
			RETURNING lease_until,next_attempt_at`, work.ID, work.Token, work.Version).Scan(&leaseDeadline, &scheduledBefore)
	}); err != nil {
		t.Fatalf("set short committed test lease: %v", err)
	}

	var transactionStarted, transactionAfterWait, databaseAfterWait time.Time
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp(),lease_until FROM billing_reconcile_work WHERE id=$1`, work.ID).Scan(&transactionStarted, &leaseDeadline); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_sleep(0.4)`); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp(),clock_timestamp()`).Scan(&transactionAfterWait, &databaseAfterWait); err != nil {
			return err
		}
		if !transactionStarted.Before(leaseDeadline) || !transactionAfterWait.Equal(transactionStarted) || !databaseAfterWait.After(leaseDeadline) {
			return errors.New("database did not cross the lease deadline within the earlier transaction")
		}
		store, err := NewStore(tx)
		if err != nil {
			return err
		}
		return store.Retry(ctx, work, time.Now().UTC().Add(3*time.Hour))
	})
	if !errors.Is(err, ErrStaleClaim) {
		t.Fatalf("retry after lease expiry = %v, want ErrStaleClaim", err)
	}

	var token uuid.UUID
	var version, dirtyVersion int64
	var leaseAfter, scheduledAfter time.Time
	if err := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT claim_token,claim_version,dirty_version,lease_until,next_attempt_at
			FROM billing_reconcile_work WHERE billing_account_id=$1`, accountID).Scan(&token, &version, &dirtyVersion, &leaseAfter, &scheduledAfter)
	}); err != nil {
		t.Fatal(err)
	}
	if token != work.Token || version != work.Version || dirtyVersion != work.Version || !leaseAfter.Equal(leaseDeadline) || !scheduledAfter.Equal(scheduledBefore) {
		t.Fatalf("stale retry changed scheduling or claim: token=%s version=%d dirty=%d lease=%s scheduled=%s", token, version, dirtyVersion, leaseAfter, scheduledAfter)
	}
}
