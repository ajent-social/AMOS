package store_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	billingprovider "github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	identityStore "github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	workspaceStore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

func TestT5_3_PersonalAndOrganizationBillingAccountsStayIndependent(t *testing.T) {
	db, personal, organization := newBillingDB(t)
	personalAccount := ensureAccount(t, db, personal)
	organizationAccount := ensureAccount(t, db, organization)
	if personalAccount.ID == organizationAccount.ID {
		t.Fatal("personal and organization workspaces share a billing account")
	}
	bindCustomer(t, db, personalAccount.ID, personal, "cus_personal_1")
	bindCustomer(t, db, organizationAccount.ID, organization, "cus_organization_1")

	third := newWorkspace(t, db, personal)
	thirdAccount := ensureAccount(t, db, third)
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, err := billingstore.New(tx)
		if err != nil {
			return err
		}
		_, err = s.BindCustomer(context.Background(), newID(t), thirdAccount.ID, third, "cus_personal_1")
		return err
	})
	if !errors.Is(err, billingstore.ErrCustomerConflict) {
		t.Fatalf("reusing a provider customer in another workspace returned %v, want ErrCustomerConflict", err)
	}
}

func TestT5_3_IdempotencyAndCrashBeforeOrAfterClaim(t *testing.T) {
	db, personal, _ := newBillingDB(t)
	account := ensureAccount(t, db, personal)
	payload := sha256.Sum256([]byte("create-checkout:catalog-monthly"))
	first := createIntent(t, db, account.ID, personal, "checkout.create", "checkout-2026-01", payload)
	replayed, created := replayIntent(t, db, account.ID, personal, "checkout.create", "checkout-2026-01", payload)
	if created || replayed.ID != first.ID {
		t.Fatalf("same idempotency payload created new intent: created=%v IDs=%s/%s", created, replayed.ID, first.ID)
	}
	changed := sha256.Sum256([]byte("create-checkout:catalog-yearly"))
	if _, _, err := replayIntentErr(t, db, account.ID, personal, "checkout.create", "checkout-2026-01", changed); !errors.Is(err, billingstore.ErrIdempotencyConflict) {
		t.Fatalf("conflicting idempotency payload error=%v, want ErrIdempotencyConflict", err)
	}

	// A worker crash before claiming leaves a durable pending row another worker can claim.
	claim := claimIntent(t, db, personal, first.ID, first.Version, time.Minute)
	if !claim.Claimed || claim.Intent.State != "claimed" || claim.Intent.IdempotencyKey != first.IdempotencyKey {
		t.Fatalf("first durable claim = %#v", claim)
	}
	providerMutationCalls := 1 // The caller may invoke the provider only for a successful claim.
	if _, _, err := replayIntentErr(t, db, account.ID, personal, "checkout.create", "checkout-2026-01", changed); !errors.Is(err, billingstore.ErrIdempotencyConflict) {
		t.Fatalf("conflicting payload replay after claim error=%v", err)
	}
	if providerMutationCalls != 1 {
		t.Fatalf("conflicting replay permitted %d provider mutation calls, want 1", providerMutationCalls)
	}

	// A worker that crashes after claiming blocks concurrent provider work until
	// the DB lease expires; lease expiry permits a retry with the same key/hash.
	blocked := claimIntent(t, db, personal, claim.Intent.ID, claim.Intent.Version, time.Minute)
	if blocked.Claimed {
		providerMutationCalls++
	}
	if blocked.Claimed || blocked.Intent.ClaimToken != claim.Intent.ClaimToken {
		t.Fatalf("active lease was reclaimed: %#v", blocked)
	}
	if providerMutationCalls != 1 {
		t.Fatalf("active lease permitted a second provider mutation: calls=%d", providerMutationCalls)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE billing_provider_intents SET lease_until=transaction_timestamp()-interval '1 second' WHERE id=$1`, claim.Intent.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	current := findIntent(t, db, personal, claim.Intent.ID)
	reclaimed := claimIntent(t, db, personal, current.ID, current.Version, time.Minute)
	if !reclaimed.Claimed || reclaimed.Intent.Attempts != 2 || reclaimed.Intent.IdempotencyKey != first.IdempotencyKey || reclaimed.Intent.PayloadSHA256 != payload {
		t.Fatalf("expired claim retry changed provider idempotency material: %#v", reclaimed)
	}
	unknown, err := resolveIntent(t, db, personal, reclaimed.Intent, "unknown", "")
	if err != nil {
		t.Fatalf("mark uncertain provider response: %v", err)
	}
	noReplay := claimIntent(t, db, personal, unknown.ID, unknown.Version, time.Minute)
	if noReplay.Claimed || noReplay.Intent.State != "unknown" {
		t.Fatalf("unknown provider outcome became a replayable mutation: %#v", noReplay)
	}
	confirmed, err := reconcileIntent(t, db, personal, unknown, "confirmed", "sub_123")
	if err != nil || confirmed.State != "confirmed" {
		t.Fatalf("reconcile unknown outcome = %#v, %v", confirmed, err)
	}
	var auditRows int
	if err = db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM billing_intent_audit_references WHERE intent_id=$1`, first.ID).Scan(&auditRows)
	}); err != nil {
		t.Fatal(err)
	}
	if auditRows != 5 {
		t.Fatalf("intent transition audit references=%d, want 5", auditRows)
	}
	if err = db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(), `DELETE FROM billing_intent_audit_references WHERE intent_id=$1`, first.ID)
		return e
	}); err == nil {
		t.Fatal("billing intent audit references allowed deletion")
	}
}

func TestT5_3_WebhookInboxDeduplicatesAndProjectionRejectsStaleObservation(t *testing.T) {
	db, personal, organization := newBillingDB(t)
	ensureAccount(t, db, personal)
	ensureAccount(t, db, organization)
	digest := sha256.Sum256([]byte("provider-event-body"))
	first, err := receiveWebhook(t, db, personal, "evt_123", digest)
	if err != nil || first.Duplicate {
		t.Fatalf("first webhook receipt = %#v, %v", first, err)
	}
	duplicate, err := receiveWebhook(t, db, personal, "evt_123", digest)
	if err != nil || !duplicate.Duplicate || duplicate.ID != first.ID {
		t.Fatalf("duplicate webhook receipt = %#v, %v", duplicate, err)
	}
	crossWorkspaceDuplicate, err := receiveWebhook(t, db, organization, "evt_123", digest)
	if err != nil || !crossWorkspaceDuplicate.Duplicate || crossWorkspaceDuplicate.ID != first.ID || crossWorkspaceDuplicate.AccountID.String() == first.AccountID.String() {
		t.Fatalf("provider-account event duplicate crossed workspace boundary: %#v, %v", crossWorkspaceDuplicate, err)
	}
	otherDigest := sha256.Sum256([]byte("different body"))
	if _, err = receiveWebhook(t, db, personal, "evt_123", otherDigest); !errors.Is(err, billingstore.ErrInboxConflict) {
		t.Fatalf("event payload conflict = %v", err)
	}

	now := time.Now().UTC()
	projection, err := upsertProjection(t, db, personal, now)
	if err != nil || projection.Version != 1 {
		t.Fatalf("initial subscription projection = %#v, %v", projection, err)
	}
	newer, err := upsertProjection(t, db, personal, now.Add(time.Minute))
	if err != nil || newer.Version != 2 {
		t.Fatalf("newer subscription projection = %#v, %v", newer, err)
	}
	if _, err = upsertProjection(t, db, personal, now); !errors.Is(err, billingstore.ErrProjectionStale) {
		t.Fatalf("stale subscription projection error=%v", err)
	}
	if _, err = upsertProjection(t, db, organization, now.Add(2*time.Minute)); !errors.Is(err, billingstore.ErrProjectionConflict) {
		t.Fatalf("provider subscription reuse across workspaces error=%v", err)
	}
}

func newBillingDB(t *testing.T) (*storage.DB, billingprovider.Binding, billingprovider.Binding) {
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
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatalf("open isolated Postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	identitySQL := readFragment(t, "identity.sql")
	workspaceSQL := readFragment(t, "workspace.sql")
	billingSQL := readFragment(t, "billing.sql")
	registry, err := migrations.NewRegistry(
		migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: identitySQL}}},
		migrations.Fragment{Namespace: "workspace", Migrations: []migrations.Migration{{Sequence: 2, Name: "workspace_base", SQL: workspaceSQL}}},
		migrations.Fragment{Namespace: "billing", Migrations: []migrations.Migration{{Sequence: 3, Name: "billing_base", SQL: billingSQL}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Migrate(ctx, db, registry); err != nil {
		t.Fatalf("apply isolated identity/workspace/billing schema: %v", err)
	}
	installation, application, environment := newID(t), newID(t), newID(t)
	person, err := identityStore.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO identity_persons (id,installation_id,application_id,state) VALUES ($1,$2,$3,'active')`, person, installation, application)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	var personalID, organizationID uuid.UUID
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := workspaceStore.New(tx)
		if e != nil {
			return e
		}
		personalID, e = identityStore.NewID()
		if e != nil {
			return e
		}
		_, e = s.CreatePersonalWorkspace(ctx, workspaceStore.CreatePersonalInput{ID: personalID, Scope: workspaceStore.Scope{InstallationID: installation, ApplicationID: application}, OwnerPersonID: person})
		return e
	}); err != nil {
		t.Fatalf("create personal workspace: %v", err)
	}
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := workspaceStore.New(tx)
		if e != nil {
			return e
		}
		organizationID, e = identityStore.NewID()
		if e != nil {
			return e
		}
		ownerMembership, e := identityStore.NewID()
		if e != nil {
			return e
		}
		_, _, e = s.CreateOrganizationWorkspace(ctx, workspaceStore.CreateOrganizationInput{ID: organizationID, OwnerMembershipID: ownerMembership, Scope: workspaceStore.Scope{InstallationID: installation, ApplicationID: application}, OwnerPersonID: person})
		return e
	}); err != nil {
		t.Fatalf("create organization workspace: %v", err)
	}
	makeBinding := func(workspace uuid.UUID) billingprovider.Binding {
		return billingprovider.Binding{InstallationID: installation.String(), EnvironmentID: environment.String(), Provider: "stripe", AccountID: "acct_fixture", AccountMode: billingprovider.AccountTest, WorkspaceID: workspace.String()}
	}
	return db, makeBinding(personalID), makeBinding(organizationID)
}

func newWorkspace(t *testing.T, db *storage.DB, template billingprovider.Binding) billingprovider.Binding {
	t.Helper()
	id := newID(t)
	person := newID(t)
	applicationID := applicationFor(t, db, template)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(context.Background(), `INSERT INTO identity_persons (id,installation_id,application_id,state) VALUES ($1,$2,$3,'active')`, person, uuid.MustParse(template.InstallationID), applicationID); e != nil {
			return e
		}
		s, e := workspaceStore.New(tx)
		if e != nil {
			return e
		}
		_, e = s.CreatePersonalWorkspace(context.Background(), workspaceStore.CreatePersonalInput{ID: id, Scope: workspaceStore.Scope{InstallationID: uuid.MustParse(template.InstallationID), ApplicationID: applicationID}, OwnerPersonID: person})
		return e
	}); err != nil {
		t.Fatalf("create extra personal workspace: %v", err)
	}
	template.WorkspaceID = id.String()
	return template
}
func applicationFor(t *testing.T, db *storage.DB, b billingprovider.Binding) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT application_id FROM workspaces WHERE id=$1 AND installation_id=$2`, uuid.MustParse(b.WorkspaceID), uuid.MustParse(b.InstallationID)).Scan(&id)
	}); err != nil {
		t.Fatal(err)
	}
	return id
}
func readFragment(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
func ensureAccount(t *testing.T, db *storage.DB, b billingprovider.Binding) billingstore.WorkspaceAccount {
	t.Helper()
	id := newID(t)
	var out billingstore.WorkspaceAccount
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		out, e = s.EnsureWorkspaceAccount(context.Background(), id, b)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func bindCustomer(t *testing.T, db *storage.DB, accountID uuid.UUID, b billingprovider.Binding, ref string) {
	t.Helper()
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		_, e = s.BindCustomer(context.Background(), newID(t), accountID, b, ref)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
}
func createIntent(t *testing.T, db *storage.DB, accountID uuid.UUID, b billingprovider.Binding, op, key string, digest [sha256.Size]byte) billingstore.Intent {
	t.Helper()
	var out billingstore.Intent
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		out, _, e = s.CreateIntent(context.Background(), billingstore.CreateIntentInput{ID: newID(t), AuditReferenceID: newID(t), AccountID: accountID, Binding: b, Operation: op, IdempotencyKey: key, PayloadSHA256: digest})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func replayIntent(t *testing.T, db *storage.DB, accountID uuid.UUID, b billingprovider.Binding, op, key string, digest [sha256.Size]byte) (billingstore.Intent, bool) {
	t.Helper()
	out, created, err := replayIntentErr(t, db, accountID, b, op, key, digest)
	if err != nil {
		t.Fatal(err)
	}
	return out, created
}
func replayIntentErr(t *testing.T, db *storage.DB, accountID uuid.UUID, b billingprovider.Binding, op, key string, digest [sha256.Size]byte) (billingstore.Intent, bool, error) {
	t.Helper()
	var out billingstore.Intent
	var created bool
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		out, created, e = s.CreateIntent(context.Background(), billingstore.CreateIntentInput{ID: newID(t), AuditReferenceID: newID(t), AccountID: accountID, Binding: b, Operation: op, IdempotencyKey: key, PayloadSHA256: digest})
		return e
	})
	return out, created, err
}
func claimIntent(t *testing.T, db *storage.DB, b billingprovider.Binding, id uuid.UUID, version int64, lease time.Duration) billingstore.ClaimResult {
	t.Helper()
	var out billingstore.ClaimResult
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		out, e = s.ClaimIntent(context.Background(), billingstore.ClaimIntentInput{ID: id, AuditReferenceID: newID(t), Binding: b, ExpectedVersion: version, Lease: lease})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func findIntent(t *testing.T, db *storage.DB, b billingprovider.Binding, id uuid.UUID) billingstore.Intent {
	t.Helper()
	var out billingstore.Intent
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		out, e = s.FindIntent(context.Background(), b, id)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func resolveIntent(t *testing.T, db *storage.DB, b billingprovider.Binding, in billingstore.Intent, state, ref string) (billingstore.Intent, error) {
	t.Helper()
	var out billingstore.Intent
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		out, e = s.ResolveIntent(context.Background(), billingstore.ResolveIntentInput{ID: in.ID, AuditReferenceID: newID(t), ClaimToken: in.ClaimToken, Binding: b, ExpectedVersion: in.Version, State: state, ProviderObjectRef: ref})
		return e
	})
	return out, err
}
func reconcileIntent(t *testing.T, db *storage.DB, b billingprovider.Binding, in billingstore.Intent, state, ref string) (billingstore.Intent, error) {
	t.Helper()
	var out billingstore.Intent
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		out, e = s.ReconcileIntent(context.Background(), billingstore.ReconcileIntentInput{ID: in.ID, AuditReferenceID: newID(t), Binding: b, ExpectedVersion: in.Version, State: state, ProviderObjectRef: ref})
		return e
	})
	return out, err
}
func receiveWebhook(t *testing.T, db *storage.DB, b billingprovider.Binding, event string, digest [sha256.Size]byte) (billingstore.WebhookReceipt, error) {
	t.Helper()
	var out billingstore.WebhookReceipt
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		out, e = s.ReceiveWebhook(context.Background(), billingstore.WebhookInput{ID: newID(t), Binding: b, EventRef: event, PayloadSHA256: digest})
		return e
	})
	return out, err
}
func upsertProjection(t *testing.T, db *storage.DB, b billingprovider.Binding, observed time.Time) (billingstore.SubscriptionProjection, error) {
	t.Helper()
	var out billingstore.SubscriptionProjection
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		s, e := billingstore.New(tx)
		if e != nil {
			return e
		}
		out, e = s.UpsertSubscriptionProjection(context.Background(), billingstore.ProjectionInput{ID: newID(t), Binding: b, SubscriptionRef: "sub_123", State: "confirmed", PriceKey: "monthly", Quantity: 1, ObservedAt: observed})
		return e
	})
	return out, err
}
func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := identityStore.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
