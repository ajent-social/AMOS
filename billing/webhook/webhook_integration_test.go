package webhook

import (
	"context"
	"database/sql"
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
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
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
	inbox, err := NewSQLInbox(db, storeFactory)
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
	w = serveSigned(t, h, body, currentSecret, time.Now())
	if w.Code != http.StatusAccepted {
		t.Fatalf("duplicate quarantine response = %d, body=%s", w.Code, w.Body.String())
	}
	if got := countIngress(t, queryDB, "evt_test_0001"); got != 1 {
		t.Fatalf("duplicate receipt created %d rows, want 1", got)
	}
	conflicting := []byte(strings.Replace(string(body), "amos_workspace_id", "amos_other_workspace", 1))
	w = serveSigned(t, h, conflicting, currentSecret, time.Now())
	if w.Code != http.StatusConflict {
		t.Fatalf("conflicting duplicate response = %d, body=%s", w.Code, w.Body.String())
	}

	failedInbox, err := NewSQLInbox(failBeforeCommitRunner{db: db}, storeFactory)
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
	// A retry after rollback can create the receipt normally.
	w = serveSigned(t, h, failingBody, currentSecret, time.Now())
	if w.Code != http.StatusAccepted || countIngress(t, queryDB, "evt_test_0002") != 1 {
		t.Fatalf("retry after rollback failed: status=%d body=%s", w.Code, w.Body.String())
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
	registry, err := migrations.NewRegistry(
		migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: identitySQL}}},
		migrations.Fragment{Namespace: "workspace", Migrations: []migrations.Migration{{Sequence: 2, Name: "workspace_base", SQL: workspaceSQL}}},
		migrations.Fragment{Namespace: "billing", Migrations: []migrations.Migration{{Sequence: 3, Name: "billing_base", SQL: billingSQL}}},
		migrations.Fragment{Namespace: "billing", Migrations: []migrations.Migration{{Sequence: 4, Name: "verified_webhook_ingress", SQL: ingressSQL}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatalf("apply isolated webhook schema: %v", err)
	}
	return db, queryDB
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
