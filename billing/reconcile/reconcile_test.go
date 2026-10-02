package reconcile

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	billingprovider "github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

func TestT5_8_SchemaUsesCallerSequence(t *testing.T) {
	fragment, err := Schema(9)
	if err != nil {
		t.Fatal(err)
	}
	if fragment.Namespace != "billing_reconcile" || len(fragment.Migrations) != 1 || fragment.Migrations[0].Sequence != 9 || fragment.Migrations[0].Name != "reconciliation_work" {
		t.Fatalf("unexpected reconciliation schema fragment: %#v", fragment)
	}
	if _, err = Schema(0); err == nil {
		t.Fatal("zero migration sequence accepted")
	}
}

func TestT5_8_DirtyGenerationFencesWorkerAndCurrentSnapshotWins(t *testing.T) {
	ctx := context.Background()
	db, binding, _, accountID, customerID := newReconcileDB(t)
	markDirty(t, db, binding, "cus_current")
	first := claim(t, db)
	baselineAt := time.Now().UTC().Add(-3 * time.Minute)
	baseline := snapshot(binding, "cus_current", "rev-baseline", baselineAt, Subscription{Reference: "sub_old", Status: StatusActive, PriceKey: "price_monthly", Quantity: 1, PeriodStart: baselineAt.Add(-time.Hour), PeriodEnd: baselineAt.Add(29 * 24 * time.Hour)})
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		return s.applySnapshot(ctx, first, baseline, time.Minute, MaxEntitlementFreshness)
	}); err != nil {
		t.Fatalf("apply initial subscription snapshot: %v", err)
	}
	markDirty(t, db, binding, "cus_current")
	first = claim(t, db)
	// A signal arriving while the provider call is outstanding invalidates its
	// claim and prevents any stale projection from being written.
	markDirty(t, db, binding, "cus_current")
	old := snapshot(binding, "cus_current", "rev-old", time.Now().UTC().Add(-2*time.Minute), Subscription{Reference: "sub_old", Status: StatusCanceled, PriceKey: "price_monthly", Quantity: 1})
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		return s.applySnapshot(ctx, first, old, time.Minute, MaxEntitlementFreshness)
	}); !errors.Is(err, ErrStaleClaim) {
		t.Fatalf("dirty signal did not fence old provider response: %v", err)
	}
	second := claim(t, db)
	if second.Version == first.Version || second.Token == first.Token {
		t.Fatalf("new dirty generation did not produce fresh claim: first=%#v second=%#v", first, second)
	}
	newerAt := time.Now().UTC()
	newer := snapshot(binding, "cus_current", "rev-new", newerAt,
		Subscription{Reference: "sub_old", Status: StatusCanceled, PriceKey: "price_monthly", Quantity: 1, PeriodStart: baselineAt.Add(-time.Hour), PeriodEnd: baselineAt.Add(29 * 24 * time.Hour)},
		Subscription{Reference: "sub_new", Status: StatusActive, PriceKey: "price_monthly", Quantity: 3, PeriodStart: newerAt.Add(-time.Hour), PeriodEnd: newerAt.Add(30 * 24 * time.Hour)})
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		return s.applySnapshot(ctx, second, newer, time.Hour, MaxEntitlementFreshness)
	}); err != nil {
		t.Fatalf("apply current replacement snapshot: %v", err)
	}
	var gotState string
	var gotQty int64
	if err := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT state,quantity FROM billing_subscription_projections WHERE billing_account_id=$1 AND subscription_ref='sub_new'`, accountID).Scan(&gotState, &gotQty)
	}); err != nil {
		t.Fatal(err)
	}
	if gotState != "confirmed" || gotQty != 3 {
		t.Fatalf("current provider projection state=%s quantity=%d", gotState, gotQty)
	}
	var oldState string
	if err := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT state FROM billing_subscription_projections WHERE billing_account_id=$1 AND subscription_ref='sub_old'`, accountID).Scan(&oldState)
	}); err != nil {
		t.Fatal(err)
	}
	if oldState != "canceled" {
		t.Fatalf("superseded subscription state=%s, want canceled", oldState)
	}
	var rawStatus string
	if err := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT provider_status FROM billing_reconcile_projection_metadata WHERE billing_account_id=$1 AND subscription_ref='sub_new'`, accountID).Scan(&rawStatus)
	}); err != nil {
		t.Fatal(err)
	}
	if rawStatus != "active" {
		t.Fatalf("provider lifecycle metadata=%q, want active", rawStatus)
	}
	_ = customerID
	// Force the completed work due, obtain a newer claim, and deliver an older
	// provider result. The observation monotonicity guard must reject it.
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE billing_reconcile_work SET next_attempt_at=transaction_timestamp()-interval '1 second' WHERE billing_account_id=$1`, accountID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	third := claim(t, db)
	late := snapshot(binding, "cus_current", "rev-late", newerAt.Add(-time.Minute), Subscription{Reference: "sub_old", Status: StatusActive, PriceKey: "price_monthly", Quantity: 1, PeriodStart: newerAt.Add(-time.Hour), PeriodEnd: newerAt.Add(29 * 24 * time.Hour)})
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		return s.applySnapshot(ctx, third, late, time.Hour, MaxEntitlementFreshness)
	}); !errors.Is(err, ErrStaleSnapshot) {
		t.Fatalf("late old snapshot error=%v, want ErrStaleSnapshot", err)
	}
	var count int
	if err := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM billing_subscription_projections WHERE billing_account_id=$1 AND state='confirmed' AND subscription_ref='sub_new'`, accountID).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("late canceled/replaced subscription changed current projection: active new rows=%d", count)
	}
}

func TestT5_8_ExpiredDatabaseLeaseCannotApplySnapshot(t *testing.T) {
	ctx := context.Background()
	db, binding, _, accountID, _ := newReconcileDB(t)
	markDirty(t, db, binding, "cus_current")
	work := claim(t, db)
	observed := time.Now().UTC()
	current := snapshot(binding, "cus_current", "rev-expiry", observed)
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		// This transaction starts before the lease expiry. Its transaction
		// timestamp remains old even when the application reaches applySnapshot
		// after the database-clock lease has expired.
		var transactionStarted time.Time
		if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp()`).Scan(&transactionStarted); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE billing_reconcile_work SET lease_until=clock_timestamp()+interval '150 milliseconds' WHERE id=$1 AND claim_token=$2`, work.ID, work.Token); err != nil {
			return err
		}
		var leaseUntil time.Time
		if err := tx.QueryRowContext(ctx, `SELECT lease_until FROM billing_reconcile_work WHERE id=$1`, work.ID).Scan(&leaseUntil); err != nil {
			return err
		}
		if !transactionStarted.Before(leaseUntil) {
			return errors.New("test transaction did not start before lease expiry")
		}
		time.Sleep(250 * time.Millisecond)
		var databaseNow time.Time
		if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
			return err
		}
		if !databaseNow.After(leaseUntil) {
			return errors.New("test lease did not expire on database clock")
		}
		store, err := NewStore(tx)
		if err != nil {
			return err
		}
		return store.applySnapshot(ctx, work, current, time.Minute, MaxEntitlementFreshness)
	})
	if !errors.Is(err, ErrStaleClaim) {
		t.Fatalf("expired database-clock lease error=%v, want ErrStaleClaim", err)
	}
	var projections int
	if err := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM billing_subscription_projections WHERE billing_account_id=$1`, accountID).Scan(&projections)
	}); err != nil {
		t.Fatal(err)
	}
	if projections != 0 {
		t.Fatalf("expired lease wrote %d subscription projections", projections)
	}
}

func TestT5_8_PeriodicScanFindsMissedCustomerAndLinksUnknownReceipt(t *testing.T) {
	ctx := context.Background()
	db, binding, applicationID, accountID, customerID := newReconcileDB(t)
	var owner uuid.UUID
	if err := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT id FROM identity_persons WHERE installation_id=$1 AND application_id=$2`, uuid.MustParse(binding.InstallationID), applicationID).Scan(&owner)
	}); err != nil {
		t.Fatal(err)
	}
	organizationID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	membershipID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		ws, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		_, _, e = ws.CreateOrganizationWorkspace(ctx, workspacestore.CreateOrganizationInput{ID: organizationID, OwnerMembershipID: membershipID, Scope: workspacestore.Scope{InstallationID: uuid.MustParse(binding.InstallationID), ApplicationID: applicationID}, OwnerPersonID: owner})
		return e
	}); err != nil {
		t.Fatalf("create second scan workspace: %v", err)
	}
	secondBinding := binding
	secondBinding.WorkspaceID = organizationID.String()
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		bs, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		account, e := bs.EnsureWorkspaceAccount(ctx, newID(t), secondBinding)
		if e != nil {
			return e
		}
		_, e = bs.BindCustomer(ctx, newID(t), account.ID, secondBinding, "cus_second")
		return e
	}); err != nil {
		t.Fatalf("create second scan customer: %v", err)
	}
	var eventID uuid.UUID
	eventID, err = uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	payload := sha256.Sum256([]byte("normalized synthetic event metadata"))
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO billing_verified_webhook_ingress(id,installation_id,application_id,environment_id,provider,provider_account_id,account_mode,provider_event_ref,event_type,customer_ref,subscription_ref,payload_sha256,state,quarantine_reason) VALUES($1,$2,$3,$4,$5,$6,$7,'evt_unmatched','customer.subscription.updated','cus_current','sub_current',$8,'quarantined','unknown_customer')`, eventID, uuid.MustParse(binding.InstallationID), applicationID, uuid.MustParse(binding.EnvironmentID), binding.Provider, binding.AccountID, string(binding.AccountMode), payload[:])
		return e
	}); err != nil {
		t.Fatal(err)
	}
	checkpointID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	var first ScanResult
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		first, e = s.ScanPage(ctx, checkpointID, scanScope(binding, applicationID), 1)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if first.Bindings != 1 || first.UnmatchedEvents != 1 || first.Complete {
		t.Fatalf("first scan result %#v", first)
	}
	var secondPage ScanResult
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		secondPage, e = s.ScanPage(ctx, checkpointID, scanScope(binding, applicationID), 1)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if secondPage.Bindings != 1 || secondPage.Complete {
		t.Fatalf("resumed second keyset page %#v", secondPage)
	}
	var linked uuid.UUID
	if err = db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT work_id FROM billing_reconcile_ingress WHERE ingress_id=$1`, eventID).Scan(&linked)
	}); err != nil {
		t.Fatal(err)
	}
	if linked == uuid.Nil {
		t.Fatal("unmatched event linked to empty work identity")
	}
	var storedCustomer uuid.UUID
	if err = db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT customer_binding_id FROM billing_reconcile_work WHERE billing_account_id=$1`, accountID).Scan(&storedCustomer)
	}); err != nil {
		t.Fatal(err)
	}
	if storedCustomer != customerID {
		t.Fatalf("scan selected wrong customer binding: %s", storedCustomer)
	}
	// A later periodic sweep remains idempotent within its own cycle; it does not
	// relink or duplicate a previously resolved unmatched receipt.
	var second ScanResult
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		second, e = s.ScanPage(ctx, checkpointID, scanScope(binding, applicationID), 1)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if second.Bindings != 0 || !second.Complete {
		t.Fatalf("checkpoint did not complete scan cycle %#v", second)
	}
	var links int
	if err = db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM billing_reconcile_ingress WHERE ingress_id=$1`, eventID).Scan(&links)
	}); err != nil {
		t.Fatal(err)
	}
	if links != 1 || second.UnmatchedEvents != 0 || !second.Complete {
		t.Fatalf("duplicate unmatched processing links=%d result=%#v", links, second)
	}
	var works int
	if err = db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM billing_reconcile_work WHERE installation_id=$1 AND application_id=$2`, uuid.MustParse(binding.InstallationID), applicationID).Scan(&works)
	}); err != nil {
		t.Fatal(err)
	}
	if works != 2 {
		t.Fatalf("resumed full scan scheduled %d customers, want 2", works)
	}
}

func TestT5_8_RejectsAmbiguousAndUnknownPriceSnapshots(t *testing.T) {
	_, binding, applicationID, accountID, customerID := newReconcileDB(t)
	work := Work{ID: newID(t), BillingAccountID: accountID, CustomerBindingID: customerID, ApplicationID: applicationID, Binding: binding, CustomerRef: "cus_current", Version: 1, Token: newID(t), LeaseUntil: time.Now().Add(time.Minute), Attempts: 1}
	observed := time.Now().UTC()
	valid := snapshot(binding, "cus_current", "rev-1", observed, Subscription{Reference: "sub_a", Status: StatusActive, PriceKey: "price_monthly", Quantity: 1}, Subscription{Reference: "sub_b", Status: StatusTrialing, PriceKey: "price_monthly", Quantity: 1})
	if err := validateSnapshot(work, valid, observed, time.Minute, map[string]struct{}{"price_monthly": {}}); !errors.Is(err, ErrAmbiguousSnapshot) {
		t.Fatalf("multiple active subscriptions error=%v", err)
	}
	valid.Subscriptions = valid.Subscriptions[:1]
	valid.Subscriptions[0].PriceKey = "price_unconfigured"
	if err := validateSnapshot(work, valid, observed, time.Minute, map[string]struct{}{"price_monthly": {}}); !errors.Is(err, ErrUnknownPrice) {
		t.Fatalf("unknown price error=%v", err)
	}
}

func TestT5_8_SnapshotCompletenessAndClockMustFailClosed(t *testing.T) {
	db, binding, applicationID, accountID, customerID := newReconcileDB(t)
	work := Work{ID: newID(t), BillingAccountID: accountID, CustomerBindingID: customerID, ApplicationID: applicationID, Binding: binding, CustomerRef: "cus_current", Version: 1, Token: newID(t), LeaseUntil: time.Now().Add(time.Minute), Attempts: 1}
	now := time.Now().UTC()
	complete := snapshot(binding, "cus_current", "rev-complete", now, Subscription{Reference: "sub_current", Status: StatusActive, PriceKey: "price_monthly", Quantity: 1})
	if err := validateSnapshot(work, complete, now, MaxEntitlementFreshness, map[string]struct{}{"price_monthly": {}}); err != nil {
		t.Fatalf("complete current snapshot rejected: %v", err)
	}
	incomplete := complete
	incomplete.Complete = false
	if err := validateSnapshot(work, incomplete, now, MaxEntitlementFreshness, map[string]struct{}{"price_monthly": {}}); !errors.Is(err, ErrIncompleteSnapshot) {
		t.Fatalf("incomplete snapshot error=%v, want ErrIncompleteSnapshot", err)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		store, err := NewStore(tx)
		if err != nil {
			return err
		}
		return store.applySnapshot(context.Background(), work, incomplete, time.Minute, MaxEntitlementFreshness)
	}); !errors.Is(err, ErrIncompleteSnapshot) {
		t.Fatalf("storage accepted incomplete snapshot: %v", err)
	}
	future := complete
	future.ObservedAt = now.Add(time.Minute)
	if err := validateSnapshot(work, future, now, MaxEntitlementFreshness, map[string]struct{}{"price_monthly": {}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("future snapshot error=%v, want ErrInvalidInput", err)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		store, err := NewStore(tx)
		if err != nil {
			return err
		}
		return store.applySnapshot(context.Background(), work, future, time.Minute, MaxEntitlementFreshness)
	}); !errors.Is(err, ErrStaleSnapshot) {
		t.Fatalf("storage accepted future snapshot: %v", err)
	}
}

func TestT5_8_ReconcilerCannotExceedEntitlementFreshness(t *testing.T) {
	db, _, _, _, _ := newReconcileDB(t)
	cfg := workerConfig()
	cfg.Freshness = MaxEntitlementFreshness + time.Nanosecond
	if _, err := New(db, &testSnapshotSource{}, cfg); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("reconciler freshness over access bound error=%v, want ErrInvalidInput", err)
	}
}

func TestT5_8_WorkerAppliesBoundSnapshotAndReschedulesResponseLoss(t *testing.T) {
	ctx := context.Background()
	db, binding, _, accountID, _ := newReconcileDB(t)
	markDirty(t, db, binding, "cus_current")
	now := time.Now().UTC()
	good := snapshot(binding, "cus_current", "rev-worker", now, Subscription{Reference: "sub_worker", Status: StatusActive, PriceKey: "price_monthly", Quantity: 2, PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(30 * 24 * time.Hour)})
	source := &testSnapshotSource{snapshot: good}
	runner, err := New(db, source, workerConfig())
	if err != nil {
		t.Fatal(err)
	}
	worked, err := runner.RunOne(ctx)
	if err != nil || !worked {
		t.Fatalf("worker processed dirty customer: worked=%v err=%v", worked, err)
	}
	if source.calls != 1 || source.last.Binding != binding || source.last.CustomerRef != "cus_current" {
		t.Fatalf("provider snapshot request was not fully customer-bound: %#v calls=%d", source.last, source.calls)
	}
	var state string
	if err = db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT state FROM billing_subscription_projections WHERE billing_account_id=$1 AND subscription_ref='sub_worker'`, accountID).Scan(&state)
	}); err != nil {
		t.Fatal(err)
	}
	if state != "confirmed" {
		t.Fatalf("active and paid snapshot projection state=%q", state)
	}
	if worked, err = runner.RunOne(ctx); err != nil || worked {
		t.Fatalf("clean queue should have no immediate work: worked=%v err=%v", worked, err)
	}
	markDirty(t, db, binding, "cus_current")
	trialAt := time.Now().UTC()
	source.snapshot = snapshot(binding, "cus_current", "rev-trial", trialAt, Subscription{Reference: "sub_trial", Status: StatusTrialing, PriceKey: "price_monthly", Quantity: 1, PeriodStart: trialAt, PeriodEnd: trialAt.Add(14 * 24 * time.Hour)})
	if worked, err = runner.RunOne(ctx); err != nil || !worked {
		t.Fatalf("worker stores non-entitled trial lifecycle: worked=%v err=%v", worked, err)
	}
	var trialProjection, trialMetadata string
	if err = db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		if e := tx.QueryRowContext(ctx, `SELECT state FROM billing_subscription_projections WHERE billing_account_id=$1 AND subscription_ref='sub_trial'`, accountID).Scan(&trialProjection); e != nil {
			return e
		}
		return tx.QueryRowContext(ctx, `SELECT provider_status FROM billing_reconcile_projection_metadata WHERE billing_account_id=$1 AND subscription_ref='sub_trial'`, accountID).Scan(&trialMetadata)
	}); err != nil {
		t.Fatal(err)
	}
	if trialProjection != "incomplete" || trialMetadata != "trialing" {
		t.Fatalf("trial state was collapsed into entitlement: projection=%q metadata=%q", trialProjection, trialMetadata)
	}

	// An uncertain provider read leaves existing state intact and schedules a
	// durable bounded retry instead of treating the response loss as success.
	markDirty(t, db, binding, "cus_current")
	failing := &testSnapshotSource{err: errors.New("provider read response lost")}
	runner, err = New(db, failing, workerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if worked, err = runner.RunOne(ctx); err == nil || !worked {
		t.Fatalf("provider response loss did not surface and reschedule: worked=%v err=%v", worked, err)
	}
	if failing.calls != 1 {
		t.Fatalf("one bounded read failure caused %d provider attempts", failing.calls)
	}
	var attempts int
	var retryAt time.Time
	if err = db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT attempts,next_attempt_at FROM billing_reconcile_work WHERE billing_account_id=$1`, accountID).Scan(&attempts, &retryAt)
	}); err != nil {
		t.Fatal(err)
	}
	if attempts < 2 || !retryAt.After(time.Now().UTC()) {
		t.Fatalf("response-loss retry was not durably delayed: attempts=%d next=%s", attempts, retryAt)
	}
}

func TestT5_8_ScopedWorkerLeavesForeignApplicationWorkUntouched(t *testing.T) {
	ctx := context.Background()
	db, binding, applicationID, _, _ := newReconcileDB(t)
	foreignApplicationID := newID(t)
	var foreignWorkspaceID uuid.UUID
	person, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, person, binding.InstallationID, foreignApplicationID); e != nil {
			return e
		}
		ws, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		workspaceID, e := store.NewID()
		if e != nil {
			return e
		}
		foreignWorkspaceID = workspaceID
		_, e = ws.CreatePersonalWorkspace(ctx, workspacestore.CreatePersonalInput{ID: workspaceID, Scope: workspacestore.Scope{InstallationID: uuid.MustParse(binding.InstallationID), ApplicationID: foreignApplicationID}, OwnerPersonID: person})
		if e != nil {
			return e
		}
		foreignBinding := binding
		foreignBinding.WorkspaceID = workspaceID.String()
		bs, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		account, e := bs.EnsureWorkspaceAccount(ctx, newID(t), foreignBinding)
		if e != nil {
			return e
		}
		_, e = bs.BindCustomer(ctx, newID(t), account.ID, foreignBinding, "cus_foreign_application")
		return e
	}); err != nil {
		t.Fatalf("create valid foreign-application binding: %v", err)
	}
	foreignBinding := binding
	foreignBinding.WorkspaceID = foreignWorkspaceID.String()
	markDirty(t, db, foreignBinding, "cus_foreign_application")

	now := time.Now().UTC()
	source := &testSnapshotSource{snapshot: snapshot(binding, "cus_current", "rev_scoped", now, Subscription{Reference: "sub_scoped", Status: StatusActive, PriceKey: "price_monthly", Quantity: 1, PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(time.Hour)})}
	cfg := workerConfig()
	cfg.Scopes = []ScanScope{scanScope(binding, applicationID)}
	runner, err := New(db, source, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if worked, runErr := runner.RunOne(ctx); runErr != nil || worked {
		t.Fatalf("scoped worker claimed foreign application work: worked=%v err=%v", worked, runErr)
	}
	assertForeignWorkUntouched := func() {
		t.Helper()
		var attempts int
		var token sql.NullString
		var claimVersion sql.NullInt64
		var dirtyVersion int64
		if err := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT attempts,claim_token,claim_version,dirty_version FROM billing_reconcile_work WHERE customer_ref='cus_foreign_application'`).Scan(&attempts, &token, &claimVersion, &dirtyVersion)
		}); err != nil {
			t.Fatal(err)
		}
		if attempts != 0 || token.Valid || claimVersion.Valid || dirtyVersion != 1 {
			t.Fatalf("foreign work was changed: attempts=%d token=%v claimVersion=%v dirtyVersion=%d", attempts, token, claimVersion, dirtyVersion)
		}
	}
	assertForeignWorkUntouched()
	if source.calls != 0 {
		t.Fatalf("foreign-only queue triggered %d provider calls", source.calls)
	}

	markDirty(t, db, binding, "cus_current")
	if worked, runErr := runner.RunOne(ctx); runErr != nil || !worked {
		t.Fatalf("scoped worker did not process configured application: worked=%v err=%v", worked, runErr)
	}
	if source.calls != 1 || source.last.Binding != binding || source.last.CustomerRef != "cus_current" {
		t.Fatalf("provider call escaped exact configured binding: calls=%d request=%#v", source.calls, source.last)
	}
	assertForeignWorkUntouched()
}

func TestT5_8_ReconcilerCopiesAndBoundsConfiguredScopes(t *testing.T) {
	db, binding, applicationID, _, _ := newReconcileDB(t)
	scopes := []ScanScope{scanScope(binding, applicationID)}
	cfg := workerConfig()
	cfg.Scopes = scopes
	runner, err := New(db, &testSnapshotSource{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	scopes[0].ProviderAccountID = "acct_mutated"
	if runner.scopes[0].ProviderAccountID != binding.AccountID || len(runner.scopes) != 1 {
		t.Fatalf("reconciler retained caller-owned scope slice: %#v", runner.scopes)
	}
	cfg.Scopes = make([]ScanScope, maxReconciliationScopes+1)
	if _, err := New(db, &testSnapshotSource{}, cfg); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unbounded scope list error=%v, want ErrInvalidInput", err)
	}
	cfg = workerConfig()
	cfg.Scopes = []ScanScope{runner.scopes[0], runner.scopes[0]}
	if _, err := New(db, &testSnapshotSource{}, cfg); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate scope list error=%v, want ErrInvalidInput", err)
	}
}

func TestT5_8_RunOneBoundsProviderCallsBeforeClaiming(t *testing.T) {
	db, binding, applicationID, _, _ := newReconcileDB(t)
	var owner uuid.UUID
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT id FROM identity_persons WHERE installation_id=$1 AND application_id=$2`, uuid.MustParse(binding.InstallationID), applicationID).Scan(&owner)
	}); err != nil {
		t.Fatal(err)
	}
	organizationID, membershipID := newID(t), newID(t)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		ws, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		_, _, err = ws.CreateOrganizationWorkspace(context.Background(), workspacestore.CreateOrganizationInput{ID: organizationID, OwnerMembershipID: membershipID, Scope: workspacestore.Scope{InstallationID: uuid.MustParse(binding.InstallationID), ApplicationID: applicationID}, OwnerPersonID: owner})
		return err
	}); err != nil {
		t.Fatalf("create second customer workspace: %v", err)
	}
	secondBinding := binding
	secondBinding.WorkspaceID = organizationID.String()
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		store, err := billingstore.New(tx)
		if err != nil {
			return err
		}
		account, err := store.EnsureWorkspaceAccount(context.Background(), newID(t), secondBinding)
		if err != nil {
			return err
		}
		_, err = store.BindCustomer(context.Background(), newID(t), account.ID, secondBinding, "cus_second")
		return err
	}); err != nil {
		t.Fatalf("create second customer binding: %v", err)
	}
	markDirty(t, db, binding, "cus_current")
	markDirty(t, db, secondBinding, "cus_second")

	source := &blockingSnapshotSource{entered: make(chan SnapshotRequest, 4), release: make(chan struct{})}
	var releaseOnce sync.Once
	releaseSource := func() { releaseOnce.Do(func() { close(source.release) }) }
	defer releaseSource()
	runner, err := New(db, source, workerConfig())
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		worked bool
		err    error
	}
	firstDone := make(chan result, 1)
	go func() {
		worked, err := runner.RunOne(context.Background())
		firstDone <- result{worked: worked, err: err}
	}()
	select {
	case <-source.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first provider call did not begin")
	}

	waitCtx, cancelWait := context.WithCancel(context.Background())
	secondDone := make(chan result, 1)
	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		worked, err := runner.RunOne(waitCtx)
		secondDone <- result{worked: worked, err: err}
	}()
	<-secondStarted
	var secondRequest SnapshotRequest
	select {
	case secondRequest = <-source.entered:
	case <-time.After(100 * time.Millisecond):
	}
	if secondRequest.CustomerRef != "" {
		cancelWait()
		releaseSource()
		t.Fatalf("concurrent RunOne entered provider for %q while first call was blocked", secondRequest.CustomerRef)
	}
	var leased int
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM billing_reconcile_work WHERE claim_token IS NOT NULL`).Scan(&leased)
	}); err != nil {
		cancelWait()
		releaseSource()
		t.Fatal(err)
	}
	if leased != 1 {
		cancelWait()
		releaseSource()
		t.Fatalf("queued RunOne took a durable lease; claimed rows=%d, want 1", leased)
	}
	cancelWait()
	select {
	case got := <-secondDone:
		if got.worked || !errors.Is(got.err, context.Canceled) {
			releaseSource()
			t.Fatalf("cancelled queued RunOne result=%+v, want unworked context cancellation", got)
		}
	case <-time.After(3 * time.Second):
		releaseSource()
		t.Fatal("queued RunOne did not respond to cancellation")
	}
	releaseSource()
	select {
	case got := <-firstDone:
		if !got.worked || got.err != nil {
			t.Fatalf("first RunOne result=%+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first RunOne did not finish after provider release")
	}
	worked, err := runner.RunOne(context.Background())
	if err != nil || !worked {
		t.Fatalf("remaining queued work was not processable: worked=%v err=%v", worked, err)
	}
	if got := source.calls.Load(); got != 2 || source.maxInFlight.Load() != 1 {
		t.Fatalf("provider calls=%d max in-flight=%d, want 2 calls and 1 in-flight", got, source.maxInFlight.Load())
	}
}

type blockingSnapshotSource struct {
	entered     chan SnapshotRequest
	release     chan struct{}
	calls       atomic.Int32
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
}

func (s *blockingSnapshotSource) GetCustomerSnapshot(ctx context.Context, request SnapshotRequest) (CustomerSnapshot, error) {
	s.calls.Add(1)
	active := s.inFlight.Add(1)
	defer s.inFlight.Add(-1)
	for observed := s.maxInFlight.Load(); active > observed; observed = s.maxInFlight.Load() {
		if s.maxInFlight.CompareAndSwap(observed, active) {
			break
		}
	}
	select {
	case s.entered <- request:
	case <-ctx.Done():
		return CustomerSnapshot{}, ctx.Err()
	}
	select {
	case <-s.release:
	case <-ctx.Done():
		return CustomerSnapshot{}, ctx.Err()
	}
	return snapshot(request.Binding, request.CustomerRef, "rev-concurrent", time.Now().UTC()), nil
}

type testSnapshotSource struct {
	snapshot CustomerSnapshot
	err      error
	calls    int
	last     SnapshotRequest
}

func (s *testSnapshotSource) GetCustomerSnapshot(_ context.Context, request SnapshotRequest) (CustomerSnapshot, error) {
	s.calls++
	s.last = request
	return s.snapshot, s.err
}
func workerConfig() Config {
	return Config{Lease: time.Minute, RequestTimeout: 5 * time.Second, Freshness: MaxEntitlementFreshness, RefreshAfter: time.Hour, RetryBase: time.Second, RetryMax: time.Minute, MaxAttempts: 4, MinRequestInterval: time.Millisecond, PriceKeys: map[string]struct{}{"price_monthly": {}}}
}

func newReconcileDB(t *testing.T) (*storage.DB, billingprovider.Binding, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	_, schema := testkit.NewPostgres(t)
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	q.Set("search_path", schema)
	parsed.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatalf("open isolated Postgres: %v", err)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	ingress, err := migrations.BillingWebhookIngress(8)
	if err != nil {
		t.Fatal(err)
	}
	reconcileSchema, err := Schema(9)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := migrations.Core(ingress, reconcileSchema)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Migrate(ctx, db, registry); err != nil {
		t.Fatalf("apply billing reconciliation schema: %v", err)
	}
	installation, application, environment := newID(t), newID(t), newID(t)
	person, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, person, installation, application)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	workspaceID, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		ws, e := workspacestore.New(tx)
		if e != nil {
			return e
		}
		_, e = ws.CreatePersonalWorkspace(ctx, workspacestore.CreatePersonalInput{ID: workspaceID, Scope: workspacestore.Scope{InstallationID: installation, ApplicationID: application}, OwnerPersonID: person})
		return e
	}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	binding := billingprovider.Binding{InstallationID: installation.String(), EnvironmentID: environment.String(), WorkspaceID: workspaceID.String(), Provider: "stripe", AccountID: "acct_fixture", AccountMode: billingprovider.AccountTest}
	var accountID, customerID uuid.UUID
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		bs, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		account, e := bs.EnsureWorkspaceAccount(ctx, newID(t), binding)
		if e != nil {
			return e
		}
		accountID = account.ID
		customer, e := bs.BindCustomer(ctx, newID(t), accountID, binding, "cus_current")
		if e != nil {
			return e
		}
		customerID = customer.ID
		return nil
	}); err != nil {
		t.Fatalf("create billing binding: %v", err)
	}
	return db, binding, application, accountID, customerID
}

func markDirty(t *testing.T, db *storage.DB, binding billingprovider.Binding, customer string) {
	t.Helper()
	id := newID(t)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		return s.MarkDirty(context.Background(), id, binding, customer)
	}); err != nil {
		t.Fatalf("mark dirty: %v", err)
	}
}
func claim(t *testing.T, db *storage.DB) Work {
	t.Helper()
	token := newID(t)
	var w Work
	var claimed bool
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		w, claimed, e = s.ClaimDue(context.Background(), token, time.Minute)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("no reconciliation work was claimable")
	}
	return w
}
func snapshot(binding billingprovider.Binding, customer, revision string, observed time.Time, subs ...Subscription) CustomerSnapshot {
	return CustomerSnapshot{Binding: binding, CustomerRef: customer, Revision: revision, ObservedAt: observed, Subscriptions: subs, Complete: true}
}
func scanScope(b billingprovider.Binding, application uuid.UUID) ScanScope {
	return ScanScope{InstallationID: uuid.MustParse(b.InstallationID), ApplicationID: application, EnvironmentID: uuid.MustParse(b.EnvironmentID), Provider: b.Provider, ProviderAccountID: b.AccountID, AccountMode: b.AccountMode}
}
func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
