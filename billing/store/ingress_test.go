package store_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"

	billingprovider "github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	"github.com/google/uuid"
)

func TestVerifiedIngressDurableRoutingQuarantineAndConflict(t *testing.T) {
	db, b, _ := newBillingDB(t)
	account := ensureAccount(t, db, b)
	bindCustomer(t, db, account.ID, b, "cus_ingress")
	var app uuid.UUID
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), "SELECT application_id FROM billing_workspace_accounts WHERE id=$1", account.ID).Scan(&app)
	}); err != nil {
		t.Fatal(err)
	}
	scope := billingstore.EndpointScope{InstallationID: uuid.MustParse(b.InstallationID), ApplicationID: app, EnvironmentID: uuid.MustParse(b.EnvironmentID), Provider: b.Provider, ProviderAccountID: b.AccountID, AccountMode: b.AccountMode}
	lookup := func(e billingstore.EndpointScope) (billingstore.CustomerBinding, error) {
		var out billingstore.CustomerBinding
		err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
			s, err := billingstore.New(tx)
			if err != nil {
				return err
			}
			out, err = s.FindCustomerBindingByRef(context.Background(), billingstore.CustomerLookupInput{Endpoint: e, CustomerRef: "cus_ingress"})
			return err
		})
		return out, err
	}
	if out, err := lookup(scope); err != nil || out.Binding != b {
		t.Fatalf("route unavailable: %v", err)
	}
	wrong := scope
	wrong.ApplicationID = newID(t)
	if _, err := lookup(wrong); !errors.Is(err, billingstore.ErrAccountUnavailable) {
		t.Fatalf("foreign app routed: %v", err)
	}
	write := func(in billingstore.VerifiedWebhookInput) (billingstore.VerifiedWebhookReceipt, error) {
		var out billingstore.VerifiedWebhookReceipt
		err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
			s, err := billingstore.New(tx)
			if err != nil {
				return err
			}
			out, err = s.PersistVerifiedWebhook(context.Background(), in)
			return err
		})
		return out, err
	}
	in := billingstore.VerifiedWebhookInput{ID: newID(t), Endpoint: scope, EventID: "evt_ingress", EventType: "customer.subscription.updated", CustomerRef: "cus_ingress", SubscriptionRef: "sub_ingress", PayloadSHA256: sha256.Sum256([]byte("verified payload")), Binding: b}
	if out, err := write(in); err != nil || out.Duplicate || out.State != "received" {
		t.Fatalf("initial durable ingress: %#v %v", out, err)
	}
	in.ID = newID(t)
	if out, err := write(in); err != nil || !out.Duplicate || out.State != "received" {
		t.Fatalf("duplicate not acknowledged: %#v %v", out, err)
	}
	in.PayloadSHA256 = sha256.Sum256([]byte("changed verified payload"))
	if _, err := write(in); !errors.Is(err, billingstore.ErrInboxConflict) {
		t.Fatalf("changed digest acknowledged: %v", err)
	}
	in.EventID = "evt_quarantine"
	in.Binding = billingprovider.Binding{}
	in.QuarantineReason = "unknown_customer"
	in.CustomerRef = "cus_unknown"
	if out, err := write(in); err != nil || out.State != "quarantined" || out.QuarantineReason != "unknown_customer" {
		t.Fatalf("unknown event not quarantined: %#v %v", out, err)
	}
	var bound bool
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), "SELECT billing_account_id IS NOT NULL OR workspace_id IS NOT NULL FROM billing_verified_webhook_ingress WHERE provider_event_ref='evt_quarantine'").Scan(&bound)
	}); err != nil || bound {
		t.Fatal("quarantine invented workspace")
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), "UPDATE workspaces SET state='suspended' WHERE id=$1", uuid.MustParse(b.WorkspaceID))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := lookup(scope); !errors.Is(err, billingstore.ErrAccountUnavailable) {
		t.Fatalf("inactive workspace routed: %v", err)
	}
}
