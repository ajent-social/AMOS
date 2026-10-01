package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"

	"github.com/ajent-social/amos/billing/provider"
	"github.com/google/uuid"
)

// EndpointScope identifies an owner-configured, signature-verified endpoint.
// Payload metadata cannot choose this scope.
type EndpointScope struct {
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	Provider, ProviderAccountID                  string
	AccountMode                                  provider.AccountMode
}

func (s EndpointScope) valid() bool {
	return validID(s.InstallationID) && validID(s.ApplicationID) && validID(s.EnvironmentID) && (provider.Binding{InstallationID: s.InstallationID.String(), EnvironmentID: s.EnvironmentID.String(), WorkspaceID: s.ApplicationID.String(), Provider: s.Provider, AccountID: s.ProviderAccountID, AccountMode: s.AccountMode}).Validate() == nil
}

type CustomerLookupInput struct {
	Endpoint    EndpointScope
	CustomerRef string
}

// FindCustomerBindingByRef resolves only persisted, currently active bindings.
func (s *Store) FindCustomerBindingByRef(ctx context.Context, in CustomerLookupInput) (CustomerBinding, error) {
	if !s.valid(ctx) || !in.Endpoint.valid() || !externalRefPattern.MatchString(in.CustomerRef) {
		return CustomerBinding{}, ErrInvalidInput
	}
	e := in.Endpoint
	var out CustomerBinding
	var workspace uuid.UUID
	err := s.tx.QueryRowContext(ctx, `SELECT c.id,c.billing_account_id,c.workspace_id,c.customer_ref,c.state,c.created_at FROM billing_customer_bindings c JOIN billing_workspace_accounts a ON (a.id,a.installation_id,a.application_id,a.environment_id,a.workspace_id,a.provider,a.provider_account_id,a.account_mode)=(c.billing_account_id,c.installation_id,c.application_id,c.environment_id,c.workspace_id,c.provider,c.provider_account_id,c.account_mode) JOIN workspaces w ON (w.id,w.installation_id,w.application_id)=(c.workspace_id,c.installation_id,c.application_id) WHERE c.installation_id=$1 AND c.application_id=$2 AND c.environment_id=$3 AND c.provider=$4 AND c.provider_account_id=$5 AND c.account_mode=$6 AND c.customer_ref=$7 AND c.state='active' AND a.state='active' AND w.state='active'`, e.InstallationID, e.ApplicationID, e.EnvironmentID, e.Provider, e.ProviderAccountID, string(e.AccountMode), in.CustomerRef).Scan(&out.ID, &out.AccountID, &workspace, &out.CustomerRef, &out.State, &out.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CustomerBinding{}, ErrAccountUnavailable
	}
	if err != nil {
		return CustomerBinding{}, ErrPersistence
	}
	out.Binding = provider.Binding{InstallationID: e.InstallationID.String(), EnvironmentID: e.EnvironmentID.String(), WorkspaceID: workspace.String(), Provider: e.Provider, AccountID: e.ProviderAccountID, AccountMode: e.AccountMode}
	return out, nil
}

type VerifiedWebhookInput struct {
	ID                                               uuid.UUID
	Endpoint                                         EndpointScope
	EventID, EventType, CustomerRef, SubscriptionRef string
	PayloadSHA256                                    [sha256.Size]byte
	Binding                                          provider.Binding
	// QuarantineReason is closed to unknown_customer, metadata_mismatch,
	// unsupported_event, missing_customer, or account_mismatch.
	QuarantineReason string
}
type VerifiedWebhookReceipt struct {
	Duplicate               bool
	State, QuarantineReason string
}

func quarantineValid(s string) bool {
	switch s {
	case "unknown_customer", "metadata_mismatch", "unsupported_event", "missing_customer", "account_mismatch":
		return true
	}
	return false
}

// PersistVerifiedWebhook commits normalized metadata only. It neither stores
// raw provider bodies nor grants entitlements. The caller commits its transaction
// before acknowledging receipt to the provider.
func (s *Store) PersistVerifiedWebhook(ctx context.Context, in VerifiedWebhookInput) (VerifiedWebhookReceipt, error) {
	if !s.valid(ctx) || !validID(in.ID) || !in.Endpoint.valid() || !externalRefPattern.MatchString(in.EventID) || !externalRefPattern.MatchString(in.EventType) || !optionalRef(in.CustomerRef) || !optionalRef(in.SubscriptionRef) || in.PayloadSHA256 == ([sha256.Size]byte{}) {
		return VerifiedWebhookReceipt{}, ErrInvalidInput
	}
	e := in.Endpoint
	var account, workspace any
	state := "quarantined"
	if in.QuarantineReason == "" {
		if in.Binding.Validate() != nil || in.Binding.InstallationID != e.InstallationID.String() || in.Binding.EnvironmentID != e.EnvironmentID.String() || in.Binding.Provider != e.Provider || in.Binding.AccountID != e.ProviderAccountID || in.Binding.AccountMode != e.AccountMode || in.CustomerRef == "" {
			return VerifiedWebhookReceipt{}, ErrInvalidInput
		}
		current, err := s.FindCustomerBindingByRef(ctx, CustomerLookupInput{Endpoint: e, CustomerRef: in.CustomerRef})
		if err != nil {
			return VerifiedWebhookReceipt{}, err
		}
		if current.Binding != in.Binding {
			return VerifiedWebhookReceipt{}, ErrAccountUnavailable
		}
		account = current.AccountID
		workspace = uuid.MustParse(in.Binding.WorkspaceID)
		state = "received"
	} else if !quarantineValid(in.QuarantineReason) || in.Binding != (provider.Binding{}) {
		return VerifiedWebhookReceipt{}, ErrInvalidInput
	}
	var receipt VerifiedWebhookReceipt
	var digest []byte
	err := s.tx.QueryRowContext(ctx, `INSERT INTO billing_verified_webhook_ingress(id,installation_id,application_id,environment_id,provider,provider_account_id,account_mode,provider_event_ref,event_type,customer_ref,subscription_ref,payload_sha256,billing_account_id,workspace_id,state,quarantine_reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT(installation_id,application_id,environment_id,provider,provider_account_id,account_mode,provider_event_ref) DO NOTHING RETURNING state,quarantine_reason,payload_sha256`, in.ID, e.InstallationID, e.ApplicationID, e.EnvironmentID, e.Provider, e.ProviderAccountID, string(e.AccountMode), in.EventID, in.EventType, in.CustomerRef, in.SubscriptionRef, in.PayloadSHA256[:], account, workspace, state, in.QuarantineReason).Scan(&receipt.State, &receipt.QuarantineReason, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		receipt.Duplicate = true
		err = s.tx.QueryRowContext(ctx, `SELECT state,quarantine_reason,payload_sha256 FROM billing_verified_webhook_ingress WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND provider=$4 AND provider_account_id=$5 AND account_mode=$6 AND provider_event_ref=$7`, e.InstallationID, e.ApplicationID, e.EnvironmentID, e.Provider, e.ProviderAccountID, string(e.AccountMode), in.EventID).Scan(&receipt.State, &receipt.QuarantineReason, &digest)
	}
	if err != nil {
		return VerifiedWebhookReceipt{}, ErrPersistence
	}
	if string(digest) != string(in.PayloadSHA256[:]) {
		return VerifiedWebhookReceipt{}, ErrInboxConflict
	}
	return receipt, nil
}
func optionalRef(s string) bool {
	return s == "" || (strings.TrimSpace(s) == s && externalRefPattern.MatchString(s))
}
