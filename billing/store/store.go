// Package store persists workspace-scoped billing bindings and provider work.
// It does not authorize callers or invoke a payment provider; callers must
// authorize the workspace before entering these transaction-scoped methods.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	"github.com/ajent-social/amos/identity/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrInvalidInput        = errors.New("invalid billing store input")
	ErrPersistence         = errors.New("billing persistence failed")
	ErrAccountUnavailable  = errors.New("workspace billing account unavailable")
	ErrAccountExists       = errors.New("workspace billing account already exists")
	ErrCustomerConflict    = errors.New("provider customer is bound to another workspace")
	ErrCustomerExists      = errors.New("workspace already has an active provider customer")
	ErrIntentUnavailable   = errors.New("billing intent unavailable")
	ErrIdempotencyConflict = errors.New("billing idempotency key conflicts with existing intent")
	ErrStaleVersion        = errors.New("billing intent version is stale")
	ErrStaleClaim          = errors.New("billing intent claim is stale")
	ErrProjectionStale     = errors.New("billing subscription projection is stale")
	ErrProjectionConflict  = errors.New("provider subscription reference is bound to another workspace")
	ErrInboxConflict       = errors.New("provider event conflicts with existing inbox record")
)

type Store struct{ tx *sql.Tx }

func New(tx *sql.Tx) (*Store, error) {
	if tx == nil {
		return nil, ErrInvalidInput
	}
	return &Store{tx: tx}, nil
}

type WorkspaceAccount struct {
	ID                   uuid.UUID
	Binding              provider.Binding
	State                string
	Version              int64
	CreatedAt, UpdatedAt time.Time
}

type CustomerBinding struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	Binding     provider.Binding
	CustomerRef string
	State       string
	CreatedAt   time.Time
}

type Intent struct {
	ID                   uuid.UUID
	AccountID            uuid.UUID
	Binding              provider.Binding
	Operation            string
	IdempotencyKey       string
	PayloadSHA256        [sha256.Size]byte
	State                string
	ClaimToken           uuid.UUID
	LeaseUntil           time.Time
	Attempts             int
	Version              int64
	ProviderObjectRef    string
	LastAuditReferenceID uuid.UUID
	CreatedAt, UpdatedAt time.Time
}

type CreateIntentInput struct {
	ID, AuditReferenceID      uuid.UUID
	AccountID                 uuid.UUID
	Binding                   provider.Binding
	Operation, IdempotencyKey string
	PayloadSHA256             [sha256.Size]byte
}

type ClaimIntentInput struct {
	ID, AuditReferenceID uuid.UUID
	Binding              provider.Binding
	ExpectedVersion      int64
	Lease                time.Duration
}

type ClaimResult struct {
	Intent  Intent
	Claimed bool
}

type ResolveIntentInput struct {
	ID, AuditReferenceID, ClaimToken uuid.UUID
	Binding                          provider.Binding
	ExpectedVersion                  int64
	State                            string // confirmed, unknown, or failed
	ProviderObjectRef                string
}

type ReconcileIntentInput struct {
	ID, AuditReferenceID uuid.UUID
	Binding              provider.Binding
	ExpectedVersion      int64
	State                string // confirmed or failed; reconciliation never replays provider work
	ProviderObjectRef    string
}

type WebhookInput struct {
	ID            uuid.UUID
	Binding       provider.Binding
	EventRef      string
	PayloadSHA256 [sha256.Size]byte
}

type WebhookReceipt struct {
	ID            uuid.UUID
	AccountID     uuid.UUID
	Binding       provider.Binding
	EventRef      string
	PayloadSHA256 [sha256.Size]byte
	State         string
	ReceivedAt    time.Time
	ProcessedAt   sql.NullTime
	Version       int64
	Duplicate     bool
}

type ProjectionInput struct {
	ID                                 uuid.UUID
	Binding                            provider.Binding
	SubscriptionRef, State, PriceKey   string
	Quantity                           int64
	PeriodStart, PeriodEnd, ObservedAt time.Time
}

type SubscriptionProjection struct {
	ID                                 uuid.UUID
	AccountID                          uuid.UUID
	Binding                            provider.Binding
	SubscriptionRef, State, PriceKey   string
	Quantity                           int64
	PeriodStart, PeriodEnd, ObservedAt time.Time
	Version                            int64
	UpdatedAt                          time.Time
}

var operationPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)
var externalRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_:.\-/]{0,127}$`)

// EnsureWorkspaceAccount creates one independent billing account for the
// complete provider binding. Repeating the same binding is idempotent.
func (s *Store) EnsureWorkspaceAccount(ctx context.Context, id uuid.UUID, binding provider.Binding) (WorkspaceAccount, error) {
	if !s.valid(ctx) || !validID(id) || binding.Validate() != nil {
		return WorkspaceAccount{}, ErrInvalidInput
	}
	var out WorkspaceAccount
	err := s.tx.QueryRowContext(ctx, `INSERT INTO billing_workspace_accounts
		(id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode)
		SELECT $1,$2,w.application_id,$3,w.id,$5,$6,$7 FROM workspaces w
		WHERE w.id=$4 AND w.installation_id=$2 AND w.state='active'
		ON CONFLICT (installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode) DO UPDATE
		SET updated_at=billing_workspace_accounts.updated_at
		RETURNING id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,state,version,created_at,updated_at`,
		id, uuid.MustParse(binding.InstallationID), uuid.MustParse(binding.EnvironmentID), uuid.MustParse(binding.WorkspaceID), binding.Provider, binding.AccountID, string(binding.AccountMode)).Scan(
		&out.ID, new(uuid.UUID), new(uuid.UUID), new(uuid.UUID), new(uuid.UUID), new(string), new(string), new(string), &out.State, &out.Version, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WorkspaceAccount{}, ErrAccountUnavailable
		}
		return WorkspaceAccount{}, mapAccountError(err)
	}
	out.Binding = binding
	return out, nil
}

// BindCustomer attaches a provider customer to this workspace billing account.
// A provider customer can have one active workspace binding per provider
// account and mode; uniqueness is also enforced by PostgreSQL.
func (s *Store) BindCustomer(ctx context.Context, id, bindingID uuid.UUID, binding provider.Binding, customerRef string) (CustomerBinding, error) {
	if !s.valid(ctx) || !validID(id) || !validID(bindingID) || binding.Validate() != nil || !externalRefPattern.MatchString(customerRef) {
		return CustomerBinding{}, ErrInvalidInput
	}
	var out CustomerBinding
	err := s.tx.QueryRowContext(ctx, `INSERT INTO billing_customer_bindings
		(id,billing_account_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,customer_ref)
		SELECT $1,a.id,a.installation_id,a.application_id,a.environment_id,a.workspace_id,a.provider,a.provider_account_id,a.account_mode,$9
		FROM billing_workspace_accounts a WHERE a.id=$2 AND a.installation_id=$3 AND a.environment_id=$4 AND a.workspace_id=$5 AND a.provider=$6 AND a.provider_account_id=$7 AND a.account_mode=$8 AND a.state='active'
		ON CONFLICT (billing_account_id) WHERE state='active' DO NOTHING
		RETURNING id,billing_account_id,customer_ref,state,created_at`,
		id, bindingID, uuid.MustParse(binding.InstallationID), uuid.MustParse(binding.EnvironmentID), uuid.MustParse(binding.WorkspaceID), binding.Provider, binding.AccountID, string(binding.AccountMode), customerRef).Scan(&out.ID, &out.AccountID, &out.CustomerRef, &out.State, &out.CreatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CustomerBinding{}, mapCustomerError(err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		var prior CustomerBinding
		priorErr := s.tx.QueryRowContext(ctx, `SELECT id,billing_account_id,customer_ref,state,created_at FROM billing_customer_bindings
			WHERE billing_account_id=$1 AND installation_id=$2 AND environment_id=$3 AND workspace_id=$4 AND provider=$5 AND provider_account_id=$6 AND account_mode=$7 AND state='active' FOR UPDATE`,
			bindingID, uuid.MustParse(binding.InstallationID), uuid.MustParse(binding.EnvironmentID), uuid.MustParse(binding.WorkspaceID), binding.Provider, binding.AccountID, string(binding.AccountMode)).Scan(&prior.ID, &prior.AccountID, &prior.CustomerRef, &prior.State, &prior.CreatedAt)
		if priorErr == nil {
			if prior.CustomerRef != customerRef {
				return CustomerBinding{}, ErrCustomerExists
			}
			prior.Binding = binding
			return prior, nil
		}
		if errors.Is(priorErr, sql.ErrNoRows) {
			return CustomerBinding{}, ErrAccountUnavailable
		}
		return CustomerBinding{}, ErrPersistence
	}
	out.Binding = binding
	return out, nil
}

// CreateIntent stores an immutable idempotency payload before any provider
// mutation. Same-key/same-payload replay returns the original intent.
func (s *Store) CreateIntent(ctx context.Context, in CreateIntentInput) (Intent, bool, error) {
	if !s.valid(ctx) || !validID(in.ID) || !validID(in.AuditReferenceID) || !validID(in.AccountID) || in.Binding.Validate() != nil || !operationPattern.MatchString(in.Operation) || !externalRefPattern.MatchString(in.IdempotencyKey) {
		return Intent{}, false, ErrInvalidInput
	}
	var out Intent
	err := scanIntent(s.tx.QueryRowContext(ctx, `INSERT INTO billing_provider_intents
		(id,billing_account_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,operation,idempotency_key,payload_sha256,last_audit_reference_id)
		SELECT $1,a.id,a.installation_id,a.application_id,a.environment_id,a.workspace_id,a.provider,a.provider_account_id,a.account_mode,$9,$10,$11,$12
		FROM billing_workspace_accounts a WHERE a.id=$2 AND a.installation_id=$3 AND a.environment_id=$4 AND a.workspace_id=$5 AND a.provider=$6 AND a.provider_account_id=$7 AND a.account_mode=$8 AND a.state='active'
		ON CONFLICT (billing_account_id,operation,idempotency_key) DO NOTHING
		RETURNING `+intentColumns,
		in.ID, in.AccountID, uuid.MustParse(in.Binding.InstallationID), uuid.MustParse(in.Binding.EnvironmentID), uuid.MustParse(in.Binding.WorkspaceID), in.Binding.Provider, in.Binding.AccountID, string(in.Binding.AccountMode), in.Operation, in.IdempotencyKey, in.PayloadSHA256[:], in.AuditReferenceID), &out)
	created := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		err = scanIntent(s.tx.QueryRowContext(ctx, `SELECT `+intentColumns+` FROM billing_provider_intents WHERE billing_account_id=$1 AND operation=$2 AND idempotency_key=$3
			AND installation_id=$4 AND environment_id=$5 AND workspace_id=$6 AND provider=$7 AND provider_account_id=$8 AND account_mode=$9 FOR UPDATE`,
			in.AccountID, in.Operation, in.IdempotencyKey, uuid.MustParse(in.Binding.InstallationID), uuid.MustParse(in.Binding.EnvironmentID), uuid.MustParse(in.Binding.WorkspaceID), in.Binding.Provider, in.Binding.AccountID, string(in.Binding.AccountMode)), &out)
		if errors.Is(err, sql.ErrNoRows) {
			return Intent{}, false, ErrAccountUnavailable
		}
		if err == nil && out.PayloadSHA256 != in.PayloadSHA256 {
			return Intent{}, false, ErrIdempotencyConflict
		}
	}
	if err != nil {
		return Intent{}, false, mapIntentError(err)
	}
	out.Binding = in.Binding
	if created {
		if err = s.appendAuditReference(ctx, out.ID, in.AuditReferenceID, "created", out.Version); err != nil {
			return Intent{}, false, err
		}
	}
	return out, created, nil
}

// FindIntent reads an intent through its complete workspace/provider binding.
// Workers use its current Version when attempting a claim or reconciliation.
func (s *Store) FindIntent(ctx context.Context, binding provider.Binding, id uuid.UUID) (Intent, error) {
	if !s.valid(ctx) || !validID(id) || binding.Validate() != nil {
		return Intent{}, ErrInvalidInput
	}
	var out Intent
	err := scanIntent(s.tx.QueryRowContext(ctx, `SELECT `+intentColumns+` FROM billing_provider_intents
		WHERE id=$1 AND installation_id=$2 AND environment_id=$3 AND workspace_id=$4
		AND provider=$5 AND provider_account_id=$6 AND account_mode=$7`,
		id, uuid.MustParse(binding.InstallationID), uuid.MustParse(binding.EnvironmentID), uuid.MustParse(binding.WorkspaceID), binding.Provider, binding.AccountID, string(binding.AccountMode)), &out)
	if errors.Is(err, sql.ErrNoRows) {
		return Intent{}, ErrIntentUnavailable
	}
	if err != nil {
		return Intent{}, ErrPersistence
	}
	out.Binding = binding
	return out, nil
}

// ClaimIntent grants one bounded provider-operation lease. Unknown outcomes
// are never claimable; they must be reconciled with provider state first.
func (s *Store) ClaimIntent(ctx context.Context, in ClaimIntentInput) (ClaimResult, error) {
	if !s.valid(ctx) || !validID(in.ID) || !validID(in.AuditReferenceID) || in.Binding.Validate() != nil || in.ExpectedVersion < 1 || in.Lease <= 0 || in.Lease > 15*time.Minute {
		return ClaimResult{}, ErrInvalidInput
	}
	current, err := s.findIntentForUpdate(ctx, in.Binding, in.ID)
	if err != nil {
		return ClaimResult{}, err
	}
	if current.Version != in.ExpectedVersion {
		return ClaimResult{Intent: current}, ErrStaleVersion
	}
	var leaseActive bool
	if current.State == "claimed" {
		if err = s.tx.QueryRowContext(ctx, `SELECT lease_until > transaction_timestamp() FROM billing_provider_intents WHERE id=$1`, in.ID).Scan(&leaseActive); err != nil {
			return ClaimResult{}, ErrPersistence
		}
	}
	if current.State == "claimed" && leaseActive {
		return ClaimResult{Intent: current}, nil
	}
	if current.State != "pending" && current.State != "claimed" {
		return ClaimResult{Intent: current}, nil
	}
	claimToken, err := store.NewID()
	if err != nil {
		return ClaimResult{}, ErrPersistence
	}
	transition := "claimed"
	if current.State == "claimed" {
		transition = "reclaimed"
	}
	var updated Intent
	err = scanIntent(s.tx.QueryRowContext(ctx, `UPDATE billing_provider_intents SET state='claimed',claim_token=$2,lease_until=transaction_timestamp()+make_interval(secs => $3::double precision),attempts=attempts+1,version=version+1,last_audit_reference_id=$4,updated_at=transaction_timestamp()
		WHERE id=$1 RETURNING `+intentColumns, in.ID, claimToken, in.Lease.Seconds(), in.AuditReferenceID), &updated)
	if err != nil {
		return ClaimResult{}, mapIntentError(err)
	}
	updated.Binding = in.Binding
	if err = s.appendAuditReference(ctx, in.ID, in.AuditReferenceID, transition, updated.Version); err != nil {
		return ClaimResult{}, err
	}
	return ClaimResult{Intent: updated, Claimed: true}, nil
}

func (s *Store) ResolveIntent(ctx context.Context, in ResolveIntentInput) (Intent, error) {
	if !s.valid(ctx) || !validID(in.ID) || !validID(in.AuditReferenceID) || !validID(in.ClaimToken) || in.Binding.Validate() != nil || in.ExpectedVersion < 1 || !terminalIntentState(in.State) || !validOptionalExternalRef(in.ProviderObjectRef) {
		return Intent{}, ErrInvalidInput
	}
	var out Intent
	err := scanIntent(s.tx.QueryRowContext(ctx, `UPDATE billing_provider_intents SET state=$4,claim_token=NULL,lease_until=NULL,provider_object_ref=NULLIF($5,''),last_audit_reference_id=$6,version=version+1,updated_at=transaction_timestamp()
		WHERE id=$1 AND workspace_id=$2 AND installation_id=$7 AND environment_id=$8 AND provider=$9 AND provider_account_id=$10 AND account_mode=$11 AND state='claimed' AND claim_token=$3 AND version=$12
		RETURNING `+intentColumns,
		in.ID, uuid.MustParse(in.Binding.WorkspaceID), in.ClaimToken, in.State, in.ProviderObjectRef, in.AuditReferenceID, uuid.MustParse(in.Binding.InstallationID), uuid.MustParse(in.Binding.EnvironmentID), in.Binding.Provider, in.Binding.AccountID, string(in.Binding.AccountMode), in.ExpectedVersion), &out)
	if errors.Is(err, sql.ErrNoRows) {
		return Intent{}, ErrStaleClaim
	}
	if err != nil {
		return Intent{}, mapIntentError(err)
	}
	out.Binding = in.Binding
	if err = s.appendAuditReference(ctx, out.ID, in.AuditReferenceID, in.State, out.Version); err != nil {
		return Intent{}, err
	}
	return out, nil
}

// ReconcileIntent resolves an unknown provider result without issuing another
// provider mutation. The expected version keeps concurrent observations safe.
func (s *Store) ReconcileIntent(ctx context.Context, in ReconcileIntentInput) (Intent, error) {
	if !s.valid(ctx) || !validID(in.ID) || !validID(in.AuditReferenceID) || in.Binding.Validate() != nil || in.ExpectedVersion < 1 || (in.State != "confirmed" && in.State != "failed") || !validOptionalExternalRef(in.ProviderObjectRef) {
		return Intent{}, ErrInvalidInput
	}
	var out Intent
	err := scanIntent(s.tx.QueryRowContext(ctx, `UPDATE billing_provider_intents SET state=$3,provider_object_ref=NULLIF($4,''),last_audit_reference_id=$5,version=version+1,updated_at=transaction_timestamp() WHERE id=$1 AND workspace_id=$2 AND installation_id=$6 AND environment_id=$7 AND provider=$8 AND provider_account_id=$9 AND account_mode=$10 AND state='unknown' AND version=$11 RETURNING `+intentColumns, in.ID, uuid.MustParse(in.Binding.WorkspaceID), in.State, in.ProviderObjectRef, in.AuditReferenceID, uuid.MustParse(in.Binding.InstallationID), uuid.MustParse(in.Binding.EnvironmentID), in.Binding.Provider, in.Binding.AccountID, string(in.Binding.AccountMode), in.ExpectedVersion), &out)
	if errors.Is(err, sql.ErrNoRows) {
		return Intent{}, ErrStaleVersion
	}
	if err != nil {
		return Intent{}, mapIntentError(err)
	}
	out.Binding = in.Binding
	if err = s.appendAuditReference(ctx, out.ID, in.AuditReferenceID, in.State, out.Version); err != nil {
		return Intent{}, err
	}
	return out, nil
}

func (s *Store) ReceiveWebhook(ctx context.Context, in WebhookInput) (WebhookReceipt, error) {
	if !s.valid(ctx) || !validID(in.ID) || in.Binding.Validate() != nil || !externalRefPattern.MatchString(in.EventRef) {
		return WebhookReceipt{}, ErrInvalidInput
	}
	var localAccountID uuid.UUID
	err := s.tx.QueryRowContext(ctx, `SELECT id FROM billing_workspace_accounts WHERE installation_id=$1 AND environment_id=$2 AND workspace_id=$3 AND provider=$4 AND provider_account_id=$5 AND account_mode=$6 AND state='active'`, uuid.MustParse(in.Binding.InstallationID), uuid.MustParse(in.Binding.EnvironmentID), uuid.MustParse(in.Binding.WorkspaceID), in.Binding.Provider, in.Binding.AccountID, string(in.Binding.AccountMode)).Scan(&localAccountID)
	if errors.Is(err, sql.ErrNoRows) {
		return WebhookReceipt{}, ErrAccountUnavailable
	}
	if err != nil {
		return WebhookReceipt{}, ErrPersistence
	}
	var out WebhookReceipt
	err = scanWebhook(s.tx.QueryRowContext(ctx, `INSERT INTO billing_provider_inbox (id,billing_account_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,provider_event_ref,payload_sha256)
		SELECT $1,a.id,a.installation_id,a.application_id,a.environment_id,a.workspace_id,a.provider,a.provider_account_id,a.account_mode,$8,$9 FROM billing_workspace_accounts a WHERE a.installation_id=$2 AND a.environment_id=$3 AND a.workspace_id=$4 AND a.provider=$5 AND a.provider_account_id=$6 AND a.account_mode=$7 AND a.state='active'
		ON CONFLICT DO NOTHING RETURNING id,billing_account_id,provider_event_ref,payload_sha256,state,received_at,processed_at,version`, in.ID, uuid.MustParse(in.Binding.InstallationID), uuid.MustParse(in.Binding.EnvironmentID), uuid.MustParse(in.Binding.WorkspaceID), in.Binding.Provider, in.Binding.AccountID, string(in.Binding.AccountMode), in.EventRef, in.PayloadSHA256[:]), &out)
	created := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		err = scanWebhook(s.tx.QueryRowContext(ctx, `SELECT id,billing_account_id,provider_event_ref,payload_sha256,state,received_at,processed_at,version FROM billing_provider_inbox WHERE installation_id=$1 AND environment_id=$2 AND provider=$3 AND provider_account_id=$4 AND account_mode=$5 AND provider_event_ref=$6`, uuid.MustParse(in.Binding.InstallationID), uuid.MustParse(in.Binding.EnvironmentID), in.Binding.Provider, in.Binding.AccountID, string(in.Binding.AccountMode), in.EventRef), &out)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WebhookReceipt{}, ErrAccountUnavailable
		}
		return WebhookReceipt{}, ErrPersistence
	}
	if !created && out.PayloadSHA256 != in.PayloadSHA256 {
		return WebhookReceipt{}, ErrInboxConflict
	}
	// Provider event rows deduplicate at provider-account scope; the receipt's
	// workspace account remains the locally verified binding above.
	out.AccountID = localAccountID
	out.Binding = in.Binding
	out.Duplicate = !created
	return out, nil
}

func (s *Store) UpsertSubscriptionProjection(ctx context.Context, in ProjectionInput) (SubscriptionProjection, error) {
	if !s.valid(ctx) || !validID(in.ID) || in.Binding.Validate() != nil || !externalRefPattern.MatchString(in.SubscriptionRef) || !validProjectionState(in.State) || !externalRefPattern.MatchString(in.PriceKey) || in.Quantity < 0 || in.ObservedAt.IsZero() || (!in.PeriodStart.IsZero() && !in.PeriodEnd.IsZero() && !in.PeriodEnd.After(in.PeriodStart)) {
		return SubscriptionProjection{}, ErrInvalidInput
	}
	var accountID uuid.UUID
	err := s.tx.QueryRowContext(ctx, `SELECT id FROM billing_workspace_accounts WHERE installation_id=$1 AND environment_id=$2 AND workspace_id=$3 AND provider=$4 AND provider_account_id=$5 AND account_mode=$6 AND state='active'`, uuid.MustParse(in.Binding.InstallationID), uuid.MustParse(in.Binding.EnvironmentID), uuid.MustParse(in.Binding.WorkspaceID), in.Binding.Provider, in.Binding.AccountID, string(in.Binding.AccountMode)).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return SubscriptionProjection{}, ErrAccountUnavailable
	}
	if err != nil {
		return SubscriptionProjection{}, ErrPersistence
	}
	var out SubscriptionProjection
	err = scanProjection(s.tx.QueryRowContext(ctx, `INSERT INTO billing_subscription_projections (id,billing_account_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,subscription_ref,state,price_key,quantity,period_start,period_end,observed_at)
		SELECT $1,a.id,a.installation_id,a.application_id,a.environment_id,a.workspace_id,a.provider,a.provider_account_id,a.account_mode,$8,$9,$10,$11,NULLIF($12,'0001-01-01 00:00:00+00'::timestamptz),NULLIF($13,'0001-01-01 00:00:00+00'::timestamptz),$14 FROM billing_workspace_accounts a WHERE a.installation_id=$2 AND a.environment_id=$3 AND a.workspace_id=$4 AND a.provider=$5 AND a.provider_account_id=$6 AND a.account_mode=$7 AND a.state='active'
		ON CONFLICT (billing_account_id,subscription_ref) DO UPDATE SET state=EXCLUDED.state,price_key=EXCLUDED.price_key,quantity=EXCLUDED.quantity,period_start=EXCLUDED.period_start,period_end=EXCLUDED.period_end,observed_at=EXCLUDED.observed_at,version=billing_subscription_projections.version+1,updated_at=transaction_timestamp() WHERE billing_subscription_projections.observed_at < EXCLUDED.observed_at
		RETURNING id,billing_account_id,subscription_ref,state,price_key,quantity,period_start,period_end,observed_at,version,updated_at`, in.ID, uuid.MustParse(in.Binding.InstallationID), uuid.MustParse(in.Binding.EnvironmentID), uuid.MustParse(in.Binding.WorkspaceID), in.Binding.Provider, in.Binding.AccountID, string(in.Binding.AccountMode), in.SubscriptionRef, in.State, in.PriceKey, in.Quantity, in.PeriodStart, in.PeriodEnd, in.ObservedAt), &out)
	if errors.Is(err, sql.ErrNoRows) {
		return SubscriptionProjection{}, ErrProjectionStale
	}
	if err != nil {
		return SubscriptionProjection{}, mapProjectionError(err)
	}
	out.Binding = in.Binding
	return out, nil
}

const intentColumns = `id,billing_account_id,operation,idempotency_key,payload_sha256,state,COALESCE(claim_token,'00000000-0000-0000-0000-000000000000'::uuid),COALESCE(lease_until,'0001-01-01 00:00:00+00'::timestamptz),attempts,version,COALESCE(provider_object_ref,''),last_audit_reference_id,created_at,updated_at`

func scanIntent(row *sql.Row, out *Intent) error {
	var digest []byte
	err := row.Scan(&out.ID, &out.AccountID, &out.Operation, &out.IdempotencyKey, &digest, &out.State, &out.ClaimToken, &out.LeaseUntil, &out.Attempts, &out.Version, &out.ProviderObjectRef, &out.LastAuditReferenceID, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return err
	}
	if len(digest) != len(out.PayloadSHA256) {
		return ErrPersistence
	}
	copy(out.PayloadSHA256[:], digest)
	return nil
}
func scanProjection(row *sql.Row, out *SubscriptionProjection) error {
	var start, end sql.NullTime
	err := row.Scan(&out.ID, &out.AccountID, &out.SubscriptionRef, &out.State, &out.PriceKey, &out.Quantity, &start, &end, &out.ObservedAt, &out.Version, &out.UpdatedAt)
	if err != nil {
		return err
	}
	if start.Valid {
		out.PeriodStart = start.Time
	}
	if end.Valid {
		out.PeriodEnd = end.Time
	}
	return nil
}

func scanWebhook(row *sql.Row, out *WebhookReceipt) error {
	var digest []byte
	err := row.Scan(&out.ID, &out.AccountID, &out.EventRef, &digest, &out.State, &out.ReceivedAt, &out.ProcessedAt, &out.Version)
	if err != nil {
		return err
	}
	if len(digest) != len(out.PayloadSHA256) {
		return ErrPersistence
	}
	copy(out.PayloadSHA256[:], digest)
	return nil
}

func (s *Store) findIntentForUpdate(ctx context.Context, binding provider.Binding, id uuid.UUID) (Intent, error) {
	var out Intent
	err := scanIntent(s.tx.QueryRowContext(ctx, `SELECT `+intentColumns+` FROM billing_provider_intents WHERE id=$1 AND installation_id=$2 AND environment_id=$3 AND workspace_id=$4 AND provider=$5 AND provider_account_id=$6 AND account_mode=$7 FOR UPDATE`, id, uuid.MustParse(binding.InstallationID), uuid.MustParse(binding.EnvironmentID), uuid.MustParse(binding.WorkspaceID), binding.Provider, binding.AccountID, string(binding.AccountMode)), &out)
	if errors.Is(err, sql.ErrNoRows) {
		return Intent{}, ErrIntentUnavailable
	}
	if err != nil {
		return Intent{}, ErrPersistence
	}
	out.Binding = binding
	return out, nil
}
func (s *Store) appendAuditReference(ctx context.Context, intentID, auditID uuid.UUID, transition string, version int64) error {
	id, err := store.NewID()
	if err != nil {
		return ErrPersistence
	}
	_, err = s.tx.ExecContext(ctx, `INSERT INTO billing_intent_audit_references (id,intent_id,audit_reference_id,transition,intent_version) VALUES ($1,$2,$3,$4,$5)`, id, intentID, auditID, transition, version)
	if err != nil {
		return ErrPersistence
	}
	return nil
}
func (s *Store) valid(ctx context.Context) bool { return s != nil && s.tx != nil && ctx != nil }
func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
func validOptionalExternalRef(ref string) bool {
	return ref == "" || externalRefPattern.MatchString(ref)
}
func terminalIntentState(state string) bool {
	return state == "confirmed" || state == "unknown" || state == "failed"
}
func validProjectionState(state string) bool {
	switch state {
	case "pending", "unknown", "confirmed", "failed", "canceled", "past_due", "incomplete":
		return true
	default:
		return false
	}
}
func mapAccountError(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.ConstraintName {
		case "billing_workspace_accounts_pkey":
			return ErrAccountExists
		case "billing_workspace_accounts_workspace_provider_key":
			return ErrAccountExists
		case "billing_workspace_accounts_workspace_fk":
			return ErrAccountUnavailable
		}
	}
	return fmt.Errorf("%w: billing account", ErrPersistence)
}
func mapCustomerError(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.ConstraintName {
		case "billing_customer_provider_ref_idx":
			return ErrCustomerConflict
		case "billing_customer_active_per_workspace_idx":
			return ErrCustomerExists
		case "billing_customer_account_fk":
			return ErrAccountUnavailable
		}
	}
	return fmt.Errorf("%w: customer binding", ErrPersistence)
}
func mapIntentError(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.ConstraintName {
		case "billing_intent_idempotency_key":
			return ErrIdempotencyConflict
		case "billing_intent_account_fk":
			return ErrAccountUnavailable
		}
	}
	return fmt.Errorf("%w: billing intent", ErrPersistence)
}

func mapProjectionError(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.ConstraintName {
		case "billing_projection_provider_object_unique":
			return ErrProjectionConflict
		case "billing_projection_account_fk":
			return ErrAccountUnavailable
		}
	}
	return fmt.Errorf("%w: subscription projection", ErrPersistence)
}
