package reconcile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/jobs"
	jobsqlstore "github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type webhookJobsFixture struct {
	db          *storage.DB
	raw         *sql.DB
	binding     provider.Binding
	application uuid.UUID
	account     uuid.UUID
	scope       ScanScope
	consumer    *WebhookJobConsumer
	writer      *jobsqlstore.TxWriter
	repository  *jobsqlstore.Store
}

type webhookJobsPayload struct {
	IngressID         uuid.UUID `json:"ingress_id"`
	EnvironmentID     uuid.UUID `json:"environment_id"`
	Provider          string    `json:"provider"`
	ProviderAccountID string    `json:"provider_account_id"`
	AccountMode       string    `json:"account_mode"`
}

func newWebhookJobsFixture(t *testing.T) *webhookJobsFixture {
	t.Helper()
	db, binding, applicationID, accountID, _ := newReconcileDB(t)
	var schema string
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT current_schema()`).Scan(&schema)
	}); err != nil {
		t.Fatalf("read isolated PostgreSQL schema: %v", err)
	}
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	pgConfig, err := pgx.ParseConfig(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	raw := stdlib.OpenDB(*pgConfig)
	raw.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = raw.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := raw.PingContext(ctx); err != nil {
		t.Fatalf("open isolated jobs SQL pool: %v", err)
	}
	scope := ScanScope{
		InstallationID: uuid.MustParse(binding.InstallationID), ApplicationID: applicationID,
		EnvironmentID: uuid.MustParse(binding.EnvironmentID), Provider: binding.Provider,
		ProviderAccountID: binding.AccountID, AccountMode: binding.AccountMode,
	}
	config := jobsqlstore.Config{
		ClaimKinds: []string{webhookReconcileJobKind}, MaxPayloadBytes: maxWebhookReconcilePayload,
		MaxAttempts: 5, MaxReconciliationAttempts: 5, MaxLease: time.Minute, MaxRetryDelay: time.Hour,
	}
	consumer, err := NewWebhookJobConsumer(WebhookJobConfig{Database: db, Endpoints: []ScanScope{scope}})
	if err != nil {
		t.Fatalf("construct webhook job consumer: %v", err)
	}
	writer, err := jobsqlstore.NewTxWriter(config)
	if err != nil {
		t.Fatalf("construct jobs transaction writer: %v", err)
	}
	repository, err := jobsqlstore.New(raw, config)
	if err != nil {
		t.Fatalf("construct billing-only jobs repository: %v", err)
	}
	return &webhookJobsFixture{db: db, raw: raw, binding: binding, application: applicationID, account: accountID, scope: scope, consumer: consumer, writer: writer, repository: repository}
}

func (f *webhookJobsFixture) enqueueReceiptJob(t *testing.T, customerRef, quarantineReason string) (uuid.UUID, jobs.Job) {
	t.Helper()
	ctx := context.Background()
	ingressID := newID(t)
	endpoint := billingstore.EndpointScope{
		InstallationID: f.scope.InstallationID, ApplicationID: f.scope.ApplicationID, EnvironmentID: f.scope.EnvironmentID,
		Provider: f.scope.Provider, ProviderAccountID: f.scope.ProviderAccountID, AccountMode: f.scope.AccountMode,
	}
	var job jobs.Job
	err := f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		store, err := billingstore.New(tx)
		if err != nil {
			return err
		}
		input := billingstore.VerifiedWebhookInput{
			ID: ingressID, Endpoint: endpoint, EventID: "evt_" + ingressID.String(),
			EventType: "customer.subscription.updated", CustomerRef: customerRef,
			SubscriptionRef: "sub_reference", PayloadSHA256: sha256.Sum256([]byte("normalized test receipt")),
			QuarantineReason: quarantineReason,
		}
		if quarantineReason == "" {
			input.Binding = f.binding
		}
		receipt, err := store.PersistVerifiedWebhook(ctx, input)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(webhookJobsPayload{
			IngressID: receipt.ID, EnvironmentID: f.scope.EnvironmentID, Provider: f.scope.Provider,
			ProviderAccountID: f.scope.ProviderAccountID, AccountMode: string(f.scope.AccountMode),
		})
		if err != nil {
			return err
		}
		job, err = f.writer.EnqueueTx(ctx, tx, jobs.Intent{
			InstallationID: f.scope.InstallationID, ApplicationID: f.scope.ApplicationID,
			Key: "billing:reconcile:" + receipt.ID.String(), Kind: webhookReconcileJobKind,
			Payload: payload, ExternalEffect: false, Deadline: receipt.ReceivedAt.Add(7 * 24 * time.Hour),
		})
		return err
	})
	if err != nil {
		t.Fatalf("persist synthetic receipt and reconciliation job: %v", err)
	}
	return ingressID, job
}

func (f *webhookJobsFixture) enqueueEmailJob(t *testing.T) string {
	t.Helper()
	key := "email:test:" + newID(t).String()
	err := f.db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := f.writer.EnqueueTx(context.Background(), tx, jobs.Intent{
			InstallationID: f.scope.InstallationID, ApplicationID: f.scope.ApplicationID,
			Key: key, Kind: "email.send", Payload: []byte(`{"to":"synthetic@example.invalid"}`),
			ExternalEffect: true, Deadline: time.Now().UTC().Add(24 * time.Hour),
		})
		return err
	})
	if err != nil {
		t.Fatalf("enqueue unrelated email job: %v", err)
	}
	return key
}

func TestT5_8_WebhookJobWorkerClaimsOnlyBillingKind(t *testing.T) {
	f := newWebhookJobsFixture(t)
	emailKey := f.enqueueEmailJob(t)
	ingressID, enqueued := f.enqueueReceiptJob(t, "cus_current", "")
	worker := jobs.Worker{Repository: f.repository, Consumer: f.consumer, Owner: "billing-worker-test", Lease: time.Minute}
	worked, err := worker.RunOne(context.Background())
	if err != nil || !worked {
		t.Fatalf("billing worker result worked=%v err=%v", worked, err)
	}
	var emailState, billingState string
	if err := f.raw.QueryRow(`SELECT status FROM amos_jobs WHERE idempotency_key=$1`, emailKey).Scan(&emailState); err != nil {
		t.Fatal(err)
	}
	if err := f.raw.QueryRow(`SELECT status FROM amos_jobs WHERE id=$1`, enqueued.ID).Scan(&billingState); err != nil {
		t.Fatal(err)
	}
	if emailState != string(jobs.StateQueued) || billingState != string(jobs.StateSucceeded) {
		t.Fatalf("billing-only worker changed unrelated job: email=%s billing=%s", emailState, billingState)
	}
	var dirtyVersion int64
	var customerRef string
	if err := f.raw.QueryRow(`SELECT dirty_version,customer_ref FROM billing_reconcile_work WHERE billing_account_id=$1`, f.account).Scan(&dirtyVersion, &customerRef); err != nil {
		t.Fatal(err)
	}
	var linkedWork uuid.UUID
	if err := f.raw.QueryRow(`SELECT work_id FROM billing_reconcile_ingress WHERE ingress_id=$1`, ingressID).Scan(&linkedWork); err != nil {
		t.Fatal(err)
	}
	if dirtyVersion != 1 || customerRef != "cus_current" || linkedWork == uuid.Nil {
		t.Fatalf("receipt did not queue current customer: dirty=%d customer=%s work=%s", dirtyVersion, customerRef, linkedWork)
	}
}

func TestT5_8_WebhookJobDuplicateDoesNotDirtyOrInvalidateWorkLease(t *testing.T) {
	f := newWebhookJobsFixture(t)
	ingressID, _ := f.enqueueReceiptJob(t, "cus_current", "")
	ctx := context.Background()
	job, ok, err := f.repository.Claim(ctx, "billing-duplicate-test", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim webhook job: ok=%v err=%v", ok, err)
	}
	first := f.consumer.Execute(ctx, job, job.ID)
	if first.Kind != jobs.ResolutionSucceeded {
		t.Fatalf("first local dispatch result=%s", first.Kind)
	}
	workClaim := Work{}
	workClaimed := false
	if err := f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		reconcileStore, err := NewStore(tx)
		if err != nil {
			return err
		}
		workClaim, workClaimed, err = reconcileStore.ClaimDue(ctx, newID(t), time.Minute)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !workClaimed {
		t.Fatal("dirty customer was not claimable")
	}
	second := f.consumer.Reconcile(ctx, job, job.ID)
	if second.Kind != jobs.ResolutionSucceeded {
		t.Fatalf("duplicate local dispatch result=%s", second.Kind)
	}
	var dirtyVersion int64
	var currentToken sql.NullString
	var currentVersion sql.NullInt64
	var currentLease sql.NullTime
	if err := f.raw.QueryRow(`SELECT dirty_version,claim_token,claim_version,lease_until FROM billing_reconcile_work WHERE id=$1`, workClaim.ID).Scan(&dirtyVersion, &currentToken, &currentVersion, &currentLease); err != nil {
		t.Fatal(err)
	}
	if dirtyVersion != workClaim.Version || !currentToken.Valid || currentToken.String != workClaim.Token.String() || !currentVersion.Valid || currentVersion.Int64 != workClaim.Version || !currentLease.Valid || !currentLease.Time.Equal(workClaim.LeaseUntil) {
		t.Fatalf("duplicate receipt invalidated work lease: dirty=%d token=%s version=%d lease=%s", dirtyVersion, currentToken.String, currentVersion.Int64, currentLease.Time)
	}
	var links int
	if err := f.raw.QueryRow(`SELECT count(*) FROM billing_reconcile_ingress WHERE ingress_id=$1`, ingressID).Scan(&links); err != nil || links != 1 {
		t.Fatalf("duplicate receipt link count=%d err=%v", links, err)
	}
	if err := f.repository.Resolve(ctx, job, job.LeaseOwner, jobs.Resolution{Kind: jobs.ResolutionSucceeded}); err != nil {
		t.Fatalf("resolve webhook job after duplicate check: %v", err)
	}
}

func TestT5_8_WebhookJobRejectsForeignAndMalformedEnvelopes(t *testing.T) {
	f := newWebhookJobsFixture(t)
	ingressID, _ := f.enqueueReceiptJob(t, "cus_current", "")
	job, ok, err := f.repository.Claim(context.Background(), "billing-envelope-test", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim webhook job: ok=%v err=%v", ok, err)
	}
	t.Run("malformed payload", func(t *testing.T) {
		bad := job
		bad.Payload = []byte(`{"ingress_id":`)
		if got := f.consumer.Execute(context.Background(), bad, bad.ID); got.Kind != jobs.ResolutionTerminal {
			t.Fatalf("malformed payload resolution=%s", got.Kind)
		}
	})
	t.Run("foreign application", func(t *testing.T) {
		bad := job
		bad.ApplicationID = newID(t)
		if got := f.consumer.Execute(context.Background(), bad, bad.ID); got.Kind != jobs.ResolutionTerminal {
			t.Fatalf("foreign application resolution=%s", got.Kind)
		}
	})
	t.Run("foreign endpoint in strict payload", func(t *testing.T) {
		bad := job
		var payload webhookJobsPayload
		if err := json.Unmarshal(bad.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		payload.ProviderAccountID = "acct_foreign"
		bad.Payload, err = json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if got := f.consumer.Execute(context.Background(), bad, bad.ID); got.Kind != jobs.ResolutionTerminal {
			t.Fatalf("foreign endpoint resolution=%s", got.Kind)
		}
	})
	t.Run("external effect assertion", func(t *testing.T) {
		bad := job
		bad.ExternalEffect = true
		if got := f.consumer.Execute(context.Background(), bad, bad.ID); got.Kind != jobs.ResolutionTerminal {
			t.Fatalf("external effect job resolution=%s", got.Kind)
		}
	})
	t.Run("wrong idempotency argument", func(t *testing.T) {
		if got := f.consumer.Execute(context.Background(), job, newID(t)); got.Kind != jobs.ResolutionTerminal {
			t.Fatalf("wrong idempotency argument resolution=%s", got.Kind)
		}
	})
	var works, links int
	if err := f.raw.QueryRow(`SELECT count(*) FROM billing_reconcile_work WHERE billing_account_id=$1`, f.account).Scan(&works); err != nil {
		t.Fatal(err)
	}
	if err := f.raw.QueryRow(`SELECT count(*) FROM billing_reconcile_ingress WHERE ingress_id=$1`, ingressID).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if works != 0 || links != 0 {
		t.Fatalf("rejected envelopes wrote local work: rows=%d links=%d", works, links)
	}
}

func TestT5_8_WebhookJobExpiryInsideEarlierTransactionDoesNotQueue(t *testing.T) {
	f := newWebhookJobsFixture(t)
	ingressID, _ := f.enqueueReceiptJob(t, "cus_current", "")
	ctx := context.Background()
	job, ok, err := f.repository.Claim(ctx, "billing-expiry-test", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim webhook job: ok=%v err=%v", ok, err)
	}
	shortLease, err := f.raw.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := shortLease.QueryRowContext(ctx, `UPDATE amos_jobs SET lease_until=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING lease_until`, job.ID).Scan(&job.LeaseUntil); err != nil {
		_ = shortLease.Rollback()
		t.Fatalf("set short job lease: %v", err)
	}
	if err := shortLease.Commit(); err != nil {
		t.Fatal(err)
	}
	payload, ok := parseWebhookReconcilePayload(job.Payload)
	if !ok {
		t.Fatal("claimed payload did not parse")
	}
	var transactionStarted, observedTransactionStart, databaseNow time.Time
	err = f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp()`).Scan(&transactionStarted); err != nil {
			return err
		}
		if err := lockWebhookJob(ctx, tx, job); err != nil {
			return err
		}
		if !transactionStarted.Before(job.LeaseUntil) {
			return errors.New("test transaction did not start before durable job lease expiry")
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_sleep(1.25)`); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp(),clock_timestamp()`).Scan(&observedTransactionStart, &databaseNow); err != nil {
			return err
		}
		if !observedTransactionStart.Equal(transactionStarted) || !databaseNow.After(job.LeaseUntil) {
			return errors.New("database transaction did not cross the durable lease deadline")
		}
		_, err := f.consumer.processWebhookReceipt(ctx, tx, job, payload, f.scope)
		return err
	})
	if !errors.Is(err, errWebhookJobRejected) {
		t.Fatalf("processing with expired job lease = %v, want rejection", err)
	}
	var works, links int
	if err := f.raw.QueryRow(`SELECT count(*) FROM billing_reconcile_work WHERE billing_account_id=$1`, f.account).Scan(&works); err != nil {
		t.Fatal(err)
	}
	if err := f.raw.QueryRow(`SELECT count(*) FROM billing_reconcile_ingress WHERE ingress_id=$1`, ingressID).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if works != 0 || links != 0 {
		t.Fatalf("expired durable job changed reconciliation queue: rows=%d links=%d", works, links)
	}
}

func TestT5_8_UnknownWebhookJobCompletesAndPeriodicScanLaterMatches(t *testing.T) {
	f := newWebhookJobsFixture(t)
	ingressID, _ := f.enqueueReceiptJob(t, "cus_later", "unknown_customer")
	ctx := context.Background()
	job, ok, err := f.repository.Claim(ctx, "billing-unknown-test", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim unmatched webhook job: ok=%v err=%v", ok, err)
	}
	if got := f.consumer.Execute(ctx, job, job.ID); got.Kind != jobs.ResolutionSucceeded {
		t.Fatalf("unknown customer job resolution=%s, want successful no-effect", got.Kind)
	}
	if err := f.repository.Resolve(ctx, job, job.LeaseOwner, jobs.Resolution{Kind: jobs.ResolutionSucceeded}); err != nil {
		t.Fatalf("finish unknown-customer local job: %v", err)
	}
	var workCount int
	if err := f.raw.QueryRow(`SELECT count(*) FROM billing_reconcile_work WHERE billing_account_id=$1`, f.account).Scan(&workCount); err != nil {
		t.Fatal(err)
	}
	if workCount != 0 {
		t.Fatalf("unknown customer scheduled provider work before binding: %d", workCount)
	}

	var active billingstore.CustomerBinding
	if err := f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		store, err := billingstore.New(tx)
		if err != nil {
			return err
		}
		active, err = store.FindCustomerBindingByRef(ctx, billingstore.CustomerLookupInput{
			Endpoint:    billingstore.EndpointScope{InstallationID: f.scope.InstallationID, ApplicationID: f.scope.ApplicationID, EnvironmentID: f.scope.EnvironmentID, Provider: f.scope.Provider, ProviderAccountID: f.scope.ProviderAccountID, AccountMode: f.scope.AccountMode},
			CustomerRef: "cus_current",
		})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE billing_customer_bindings SET state='retired',retired_at=transaction_timestamp() WHERE id=$1 AND state='active'`, active.ID); err != nil {
			return err
		}
		_, err = store.BindCustomer(ctx, newID(t), f.account, f.binding, "cus_later")
		return err
	}); err != nil {
		t.Fatalf("bind customer after unmatched receipt: %v", err)
	}
	checkpointID := newID(t)
	var result ScanResult
	if err := f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		store, err := NewStore(tx)
		if err != nil {
			return err
		}
		result, err = store.ScanPage(ctx, checkpointID, f.scope, 20)
		return err
	}); err != nil {
		t.Fatalf("periodic scan after customer binding: %v", err)
	}
	if result.UnmatchedEvents != 1 {
		t.Fatalf("periodic scan matched %d unmatched receipts, want 1", result.UnmatchedEvents)
	}
	var customerRef string
	if err := f.raw.QueryRow(`SELECT customer_ref FROM billing_reconcile_work WHERE billing_account_id=$1`, f.account).Scan(&customerRef); err != nil {
		t.Fatal(err)
	}
	var links int
	if err := f.raw.QueryRow(`SELECT count(*) FROM billing_reconcile_ingress WHERE ingress_id=$1`, ingressID).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if customerRef != "cus_later" || links != 1 {
		t.Fatalf("later periodic match customer=%q links=%d", customerRef, links)
	}
}

func TestT5_8_WebhookJobConsumerRequiresAllowedEndpoint(t *testing.T) {
	f := newWebhookJobsFixture(t)
	foreign := f.scope
	foreign.ProviderAccountID = "acct_not_configured"
	consumer, err := NewWebhookJobConsumer(WebhookJobConfig{Database: f.db, Endpoints: []ScanScope{f.scope}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewWebhookJobConsumer(WebhookJobConfig{Database: f.db, Endpoints: []ScanScope{foreign}}); err != nil {
		t.Fatalf("otherwise valid endpoint configuration failed construction: %v", err)
	}
	ingressID := newID(t)
	payload, err := json.Marshal(webhookJobsPayload{
		IngressID: ingressID, EnvironmentID: foreign.EnvironmentID, Provider: foreign.Provider,
		ProviderAccountID: foreign.ProviderAccountID, AccountMode: string(foreign.AccountMode),
	})
	if err != nil {
		t.Fatal(err)
	}
	job := jobs.Job{
		ID: newID(t), InstallationID: foreign.InstallationID, ApplicationID: foreign.ApplicationID,
		Key: "billing:reconcile:" + ingressID.String(), Kind: webhookReconcileJobKind, Payload: payload,
		State: jobs.StateLeased, Action: jobs.ActionExecute, LeaseOwner: "synthetic", FenceToken: 1, LeaseUntil: time.Now().Add(time.Minute), Deadline: time.Now().Add(time.Hour),
	}
	if got := consumer.Execute(context.Background(), job, job.ID); got.Kind != jobs.ResolutionTerminal {
		t.Fatalf("unconfigured provider account resolution=%s", got.Kind)
	}
}

func TestT5_8_WebhookJobStrictPayloadRejectsAmbiguousJSON(t *testing.T) {
	validID := newID(t)
	payload, err := json.Marshal(webhookJobsPayload{
		IngressID: validID, EnvironmentID: newID(t), Provider: "stripe",
		ProviderAccountID: "acct_test", AccountMode: string(provider.AccountTest),
	})
	if err != nil {
		t.Fatal(err)
	}
	if valid, ok := parseWebhookReconcilePayload(payload); !ok || valid.IngressID != validID {
		t.Fatalf("canonical payload parse=%+v valid=%v", valid, ok)
	}
	cases := map[string][]byte{
		"duplicate key": []byte(`{"ingress_id":"` + validID.String() + `","ingress_id":"` + validID.String() + `","environment_id":"` + uuid.NewString() + `","provider":"stripe","provider_account_id":"acct_test","account_mode":"test"}`),
		"unknown key":   append(append([]byte(nil), payload[:len(payload)-1]...), []byte(`,"workspace_id":"ignored"}`)...),
		"trailing data": append(append([]byte(nil), payload...), []byte(` {}`)...),
		"wrong type":    []byte(`{"ingress_id":1,"environment_id":"` + uuid.NewString() + `","provider":"stripe","provider_account_id":"acct_test","account_mode":"test"}`),
		"over bound":    append(append([]byte(nil), payload...), bytes.Repeat([]byte(" "), maxWebhookReconcilePayload)...),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := parseWebhookReconcilePayload(raw); ok {
				t.Fatal("non-strict webhook job payload was accepted")
			}
		})
	}
}

func TestT5_8_WebhookJobRejectsReceiptBoundToDifferentWorkspace(t *testing.T) {
	f := newWebhookJobsFixture(t)
	ingressID, _ := f.enqueueReceiptJob(t, "cus_current", "")
	foreignAccount, foreignWorkspace := createWebhookJobForeignWorkspace(t, f)
	if _, err := f.raw.Exec(`UPDATE billing_verified_webhook_ingress SET billing_account_id=$2,workspace_id=$3 WHERE id=$1`, ingressID, foreignAccount, foreignWorkspace); err != nil {
		t.Fatalf("install cross-workspace receipt regression: %v", err)
	}
	job, ok, err := f.repository.Claim(context.Background(), "billing-scope-test", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim scoped webhook job: ok=%v err=%v", ok, err)
	}
	if got := f.consumer.Execute(context.Background(), job, job.ID); got.Kind != jobs.ResolutionTerminal {
		t.Fatalf("receipt with a foreign workspace binding resolution=%s", got.Kind)
	}
	var works, links int
	if err := f.raw.QueryRow(`SELECT count(*) FROM billing_reconcile_work WHERE billing_account_id=$1`, f.account).Scan(&works); err != nil {
		t.Fatal(err)
	}
	if err := f.raw.QueryRow(`SELECT count(*) FROM billing_reconcile_ingress WHERE ingress_id=$1`, ingressID).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if works != 0 || links != 0 {
		t.Fatalf("foreign receipt binding changed queue: rows=%d links=%d", works, links)
	}
}

func createWebhookJobForeignWorkspace(t *testing.T, f *webhookJobsFixture) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var owner uuid.UUID
	if err := f.raw.QueryRow(`SELECT id FROM identity_persons WHERE installation_id=$1 AND application_id=$2 LIMIT 1`, f.scope.InstallationID, f.scope.ApplicationID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	workspaceID, membershipID := newID(t), newID(t)
	foreignBinding := f.binding
	foreignBinding.WorkspaceID = workspaceID.String()
	var accountID uuid.UUID
	err := f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		workspaces, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		_, _, err = workspaces.CreateOrganizationWorkspace(ctx, workspacestore.CreateOrganizationInput{
			ID: workspaceID, OwnerMembershipID: membershipID,
			Scope:         workspacestore.Scope{InstallationID: f.scope.InstallationID, ApplicationID: f.scope.ApplicationID},
			OwnerPersonID: owner,
		})
		if err != nil {
			return err
		}
		billing, err := billingstore.New(tx)
		if err != nil {
			return err
		}
		account, err := billing.EnsureWorkspaceAccount(ctx, newID(t), foreignBinding)
		if err != nil {
			return err
		}
		accountID = account.ID
		_, err = billing.BindCustomer(ctx, newID(t), accountID, foreignBinding, "cus_foreign")
		return err
	})
	if err != nil {
		t.Fatalf("create alternate workspace billing binding: %v", err)
	}
	return accountID, workspaceID
}
