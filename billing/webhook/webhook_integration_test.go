package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/jobs"
	jobsqlstore "github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var errInjectedBeforeCommit = errors.New("injected failure before PostgreSQL commit")

type failBeforeCommitRunner struct{ db TxRunner }

func (r failBeforeCommitRunner) WithTx(ctx context.Context, options *sql.TxOptions, fn func(*sql.Tx) error) error {
	return r.db.WithTx(ctx, options, func(tx *sql.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		return errInjectedBeforeCommit
	})
}

func TestT5_7_RealPostgresDurableQuarantineDuplicateConflictAndRollback(t *testing.T) {
	db, queryDB := newIngressDatabase(t)
	storeFactory := func(tx *sql.Tx) (TransactionStore, error) { return store.New(tx) }
	jobWriter, err := jobsqlstore.NewTxWriter(jobsqlstore.Config{MaxPayloadBytes: 2048, MaxAttempts: 5, MaxReconciliationAttempts: 5, MaxLease: time.Minute, MaxRetryDelay: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := NewSQLInbox(db, storeFactory, jobWriter)
	if err != nil {
		t.Fatal(err)
	}
	router := testRouter()
	cfg := Config{Scope: testScope(), EventAccountID: "acct_other", Secrets: []SigningSecret{{Value: currentSecret}}, Router: router, Inbox: inbox}
	h, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	body := eventBody(t)
	w := serveSigned(t, h, body, currentSecret, time.Now())
	if w.Code != http.StatusAccepted {
		t.Fatalf("durable quarantine response = %d, body=%s", w.Code, w.Body.String())
	}
	if got := countIngress(t, queryDB, "evt_test_0001"); got != 1 {
		t.Fatalf("durable quarantine rows=%d, want 1", got)
	}
	firstJob := getReconcileJob(t, queryDB, "evt_test_0001")
	if firstJob.Kind != reconcileJobKind || firstJob.ExternalEffect || firstJob.MaxAttempts != 5 || firstJob.Deadline.IsZero() {
		t.Fatalf("reconcile intent policy mismatch: %+v", firstJob)
	}
	var payload reconcilePayload
	if err := json.Unmarshal(firstJob.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	var payloadFields map[string]json.RawMessage
	if err := json.Unmarshal(firstJob.Payload, &payloadFields); err != nil {
		t.Fatal(err)
	}
	if len(payloadFields) != 5 || payloadFields["ingress_id"] == nil || payloadFields["environment_id"] == nil || payloadFields["provider"] == nil || payloadFields["provider_account_id"] == nil || payloadFields["account_mode"] == nil {
		t.Fatalf("reconcile payload contains unexpected or missing fields: %s", firstJob.Payload)
	}
	var receiptID uuid.UUID
	var receivedAt time.Time
	if err := queryDB.QueryRow(`SELECT id,received_at FROM billing_verified_webhook_ingress WHERE provider_event_ref=$1`, "evt_test_0001").Scan(&receiptID, &receivedAt); err != nil {
		t.Fatal(err)
	}
	if payload.IngressID != receiptID || payload.EnvironmentID != cfg.Scope.EnvironmentID || payload.Provider != cfg.Scope.Provider || payload.ProviderAccountID != cfg.Scope.ProviderAccountID || payload.AccountMode != string(cfg.Scope.AccountMode) {
		t.Fatalf("reconcile payload is not scoped to receipt/endpoint: %+v", payload)
	}
	if firstJob.Key != "billing:reconcile:"+receiptID.String() {
		t.Fatalf("reconcile idempotency key=%q", firstJob.Key)
	}
	if !firstJob.Deadline.Equal(receivedAt.Add(7 * 24 * time.Hour)) {
		t.Fatalf("reconcile deadline=%s, want received_at + 7 days (%s)", firstJob.Deadline, receivedAt.Add(7*24*time.Hour))
	}
	w = serveSigned(t, h, body, currentSecret, time.Now())
	if w.Code != http.StatusAccepted {
		t.Fatalf("duplicate quarantine response = %d, body=%s", w.Code, w.Body.String())
	}
	if got := countIngress(t, queryDB, "evt_test_0001"); got != 1 {
		t.Fatalf("duplicate receipt created %d rows, want 1", got)
	}
	if got := countJobs(t, queryDB); got != 1 {
		t.Fatalf("duplicate event created %d jobs, want one stable intent", got)
	}
	duplicateJob := getReconcileJob(t, queryDB, "evt_test_0001")
	if duplicateJob.ID != firstJob.ID || duplicateJob.Key != firstJob.Key || !duplicateJob.Deadline.Equal(firstJob.Deadline) {
		t.Fatalf("duplicate delivery changed original reconcile intent: first=%+v duplicate=%+v", firstJob, duplicateJob)
	}
	failedInbox, err := NewSQLInbox(failBeforeCommitRunner{db: db}, storeFactory, jobWriter)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Inbox = failedInbox
	failingHandler, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	failingBody := []byte(strings.Replace(string(body), "evt_test_0001", "evt_test_0002", 1))
	w = serveSigned(t, failingHandler, failingBody, currentSecret, time.Now())
	if w.Code < 500 {
		t.Fatalf("pre-commit DB failure was acknowledged: status=%d body=%s", w.Code, w.Body.String())
	}
	if got := countIngress(t, queryDB, "evt_test_0002"); got != 0 {
		t.Fatalf("failed transaction left %d durable rows, want 0", got)
	}
	if got := countJobs(t, queryDB); got != 1 {
		t.Fatalf("failed transaction left %d jobs, want receipt/job rollback", got)
	}
	// A retry after rollback can create the receipt normally.
	w = serveSigned(t, h, failingBody, currentSecret, time.Now())
	if w.Code != http.StatusAccepted || countIngress(t, queryDB, "evt_test_0002") != 1 {
		t.Fatalf("retry after rollback failed: status=%d body=%s", w.Code, w.Body.String())
	}
	if got := countJobs(t, queryDB); got != 2 {
		t.Fatalf("successful retry jobs=%d, want two total receipts", got)
	}
	conflicting := []byte(strings.Replace(string(body), "amos_workspace_id", "amos_other_workspace", 1))
	w = serveSigned(t, h, conflicting, currentSecret, time.Now())
	if w.Code != http.StatusConflict {
		t.Fatalf("conflicting duplicate response = %d, body=%s", w.Code, w.Body.String())
	}

	// Force the real jobs insert to fail inside the caller transaction. The
	// receipt must roll back and the HTTP handler must leave the event retryable.
	if _, err := queryDB.Exec(`CREATE FUNCTION reject_webhook_reconcile_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='billing.webhook.reconcile' THEN RAISE EXCEPTION 'injected reconcile queue failure'; END IF; RETURN NEW; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := queryDB.Exec(`CREATE TRIGGER reject_webhook_reconcile_job BEFORE INSERT ON amos_jobs FOR EACH ROW EXECUTE FUNCTION reject_webhook_reconcile_job()`); err != nil {
		t.Fatal(err)
	}
	queueFailureBody := []byte(strings.Replace(string(body), "evt_test_0001", "evt_test_0003", 1))
	w = serveSigned(t, h, queueFailureBody, currentSecret, time.Now())
	if w.Code < 500 || countIngress(t, queryDB, "evt_test_0003") != 0 || countJobs(t, queryDB) != 2 {
		t.Fatalf("queue failure wasn't retryable/atomic: status=%d ingress=%d jobs=%d", w.Code, countIngress(t, queryDB, "evt_test_0003"), countJobs(t, queryDB))
	}
}

func newIngressDatabase(t *testing.T) (*storage.DB, *sql.DB) {
	t.Helper()
	queryDB, schema := testkit.NewPostgres(t)
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
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	identitySQL := readMigration(t, "identity.sql")
	workspaceSQL := readMigration(t, "workspace.sql")
	billingSQL := readMigration(t, "billing.sql")
	ingressSQL := readMigration(t, "billing-webhook-ingress.sql")
	jobsSQL := readMigration(t, "jobs.sql")
	registry, err := migrations.NewRegistry(
		migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: identitySQL}}},
		migrations.Fragment{Namespace: "workspace", Migrations: []migrations.Migration{{Sequence: 2, Name: "workspace_base", SQL: workspaceSQL}}},
		migrations.Fragment{Namespace: "billing", Migrations: []migrations.Migration{{Sequence: 3, Name: "billing_base", SQL: billingSQL}}},
		migrations.Fragment{Namespace: "billing", Migrations: []migrations.Migration{{Sequence: 4, Name: "verified_webhook_ingress", SQL: ingressSQL}}},
		migrations.Fragment{Namespace: "jobs", Migrations: []migrations.Migration{{Sequence: 5, Name: "jobs_base", SQL: jobsSQL}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatalf("apply isolated webhook schema: %v", err)
	}
	return db, queryDB
}

func countJobs(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM amos_jobs WHERE kind=$1`, reconcileJobKind).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func getReconcileJob(t *testing.T, db *sql.DB, eventID string) jobs.Job {
	t.Helper()
	var job jobs.Job
	if err := db.QueryRow(`SELECT id,installation_id,application_id,idempotency_key,kind,payload::text,external_effect,max_attempts,deadline_at FROM amos_jobs WHERE idempotency_key=(SELECT 'billing:reconcile:'||id::text FROM billing_verified_webhook_ingress WHERE provider_event_ref=$1)`, eventID).Scan(&job.ID, &job.InstallationID, &job.ApplicationID, &job.Key, &job.Kind, &job.Payload, &job.ExternalEffect, &job.MaxAttempts, &job.Deadline); err != nil {
		t.Fatal(err)
	}
	return job
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "migrations", "fragments", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(data)
}

func countIngress(t *testing.T, db *sql.DB, eventID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM billing_verified_webhook_ingress WHERE provider_event_ref=$1`, eventID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
