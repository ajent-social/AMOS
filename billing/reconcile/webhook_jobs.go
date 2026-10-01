package reconcile

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	"github.com/ajent-social/amos/jobs"
	"github.com/google/uuid"
)

const (
	webhookReconcileJobKind      = "billing.webhook.reconcile"
	maxWebhookReconcilePayload   = 2048
	maxWebhookReconcileEndpoints = 64
)

// WebhookJobConfig limits the consumer to explicitly configured provider endpoints.
type WebhookJobConfig struct {
	Database  TxRunner
	Endpoints []ScanScope
}

// WebhookJobConsumer turns verified receipts into durable local reconciliation work.
// It never contacts the provider; a later reconciler owns that external read.
type WebhookJobConsumer struct {
	db        TxRunner
	endpoints map[ScanScope]struct{}
}

// NewWebhookJobConsumer validates and copies the configured endpoint allowlist.
func NewWebhookJobConsumer(cfg WebhookJobConfig) (*WebhookJobConsumer, error) {
	if cfg.Database == nil || len(cfg.Endpoints) == 0 || len(cfg.Endpoints) > maxWebhookReconcileEndpoints {
		return nil, ErrInvalidInput
	}
	endpoints := make(map[ScanScope]struct{}, len(cfg.Endpoints))
	for _, endpoint := range cfg.Endpoints {
		if !endpoint.valid() {
			return nil, ErrInvalidInput
		}
		if _, exists := endpoints[endpoint]; exists {
			return nil, ErrInvalidInput
		}
		endpoints[endpoint] = struct{}{}
	}
	return &WebhookJobConsumer{db: cfg.Database, endpoints: endpoints}, nil
}

// Execute processes a claimed billing webhook job using only local database state.
func (c *WebhookJobConsumer) Execute(ctx context.Context, job jobs.Job, idempotencyKey uuid.UUID) jobs.Resolution {
	return c.consume(ctx, job, idempotencyKey)
}

// Reconcile uses the same local idempotent boundary as Execute.
func (c *WebhookJobConsumer) Reconcile(ctx context.Context, job jobs.Job, idempotencyKey uuid.UUID) jobs.Resolution {
	return c.consume(ctx, job, idempotencyKey)
}

type webhookReconcilePayload struct {
	IngressID         uuid.UUID
	EnvironmentID     uuid.UUID
	Provider          string
	ProviderAccountID string
	AccountMode       provider.AccountMode
}

func (c *WebhookJobConsumer) consume(ctx context.Context, job jobs.Job, idempotencyKey uuid.UUID) jobs.Resolution {
	if c == nil || c.db == nil || ctx == nil {
		return terminalResolution()
	}
	payload, ok := parseWebhookReconcilePayload(job.Payload)
	if !ok || !validWebhookReconcileJob(job, idempotencyKey, payload) {
		return terminalResolution()
	}
	scope := ScanScope{
		InstallationID: job.InstallationID, ApplicationID: job.ApplicationID,
		EnvironmentID: payload.EnvironmentID, Provider: payload.Provider,
		ProviderAccountID: payload.ProviderAccountID, AccountMode: payload.AccountMode,
	}
	if _, allowed := c.endpoints[scope]; !allowed {
		return terminalResolution()
	}

	var outcome jobs.Resolution
	err := c.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := lockWebhookJob(ctx, tx, job); err != nil {
			if errors.Is(err, errWebhookJobRejected) {
				outcome = terminalResolution()
				return err
			}
			return err
		}
		result, err := c.processWebhookReceipt(ctx, tx, job, payload, scope)
		if errors.Is(err, errWebhookJobRejected) {
			outcome = terminalResolution()
			return err
		}
		if errors.Is(err, errWebhookAlreadyLinked) {
			outcome = jobs.Resolution{Kind: jobs.ResolutionSucceeded}
			return err
		}
		if err != nil {
			return err
		}
		outcome = result
		return nil
	})
	if errors.Is(err, errWebhookJobRejected) {
		return terminalResolution()
	}
	if errors.Is(err, errWebhookAlreadyLinked) {
		return jobs.Resolution{Kind: jobs.ResolutionSucceeded}
	}
	if err != nil {
		return retryResolution(job.Action)
	}
	return outcome
}

var (
	errWebhookJobRejected   = errors.New("webhook reconciliation job rejected")
	errWebhookAlreadyLinked = errors.New("webhook receipt already linked")
)

func validWebhookReconcileJob(job jobs.Job, idempotencyKey uuid.UUID, payload webhookReconcilePayload) bool {
	if !validID(job.ID) || idempotencyKey != job.ID || !validID(job.InstallationID) || !validID(job.ApplicationID) ||
		job.Kind != webhookReconcileJobKind || job.ExternalEffect || len(job.Payload) == 0 || len(job.Payload) > maxWebhookReconcilePayload ||
		job.Key != "billing:reconcile:"+payload.IngressID.String() || job.Deadline.IsZero() || job.LeaseOwner == "" || job.FenceToken <= 0 || job.LeaseUntil.IsZero() {
		return false
	}
	switch job.Action {
	case jobs.ActionExecute:
		return job.State == jobs.StateLeased
	case jobs.ActionReconcile:
		return job.State == jobs.StateUnknown
	default:
		return false
	}
}

func parseWebhookReconcilePayload(raw []byte) (webhookReconcilePayload, bool) {
	var out webhookReconcilePayload
	if len(raw) == 0 || len(raw) > maxWebhookReconcilePayload {
		return out, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	open, err := dec.Token()
	if err != nil || open != json.Delim('{') {
		return out, false
	}
	seen := make(map[string]bool, 5)
	values := make(map[string]string, 5)
	for dec.More() {
		token, err := dec.Token()
		key, isString := token.(string)
		if err != nil || !isString || seen[key] {
			return out, false
		}
		seen[key] = true
		switch key {
		case "ingress_id", "environment_id", "provider", "provider_account_id", "account_mode":
			var value string
			if err := dec.Decode(&value); err != nil {
				return out, false
			}
			values[key] = value
		default:
			return out, false
		}
	}
	closeToken, err := dec.Token()
	if err != nil || closeToken != json.Delim('}') || len(seen) != 5 {
		return out, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return out, false
	}
	var valid bool
	if out.IngressID, valid = parseCanonicalUUIDv7(values["ingress_id"]); !valid {
		return out, false
	}
	if out.EnvironmentID, valid = parseCanonicalUUIDv7(values["environment_id"]); !valid {
		return out, false
	}
	out.Provider = values["provider"]
	out.ProviderAccountID = values["provider_account_id"]
	out.AccountMode = provider.AccountMode(values["account_mode"])
	return out, providerIDPattern.MatchString(out.Provider) && providerIDPattern.MatchString(out.ProviderAccountID) && (out.AccountMode == provider.AccountTest || out.AccountMode == provider.AccountLive)
}

func parseCanonicalUUIDv7(raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	return id, err == nil && id.String() == raw && validID(id)
}

func lockWebhookJob(ctx context.Context, tx *sql.Tx, job jobs.Job) error {
	var installationID, applicationID uuid.UUID
	var key, kind, payload, status, action, owner string
	var requestHash []byte
	var externalEffect bool
	var fenceToken int64
	var leaseUntil, deadline sql.NullTime
	var leaseActive bool
	err := tx.QueryRowContext(ctx, `SELECT installation_id,application_id,idempotency_key,request_hash,kind,payload::text,external_effect,status,COALESCE(lease_action,''),COALESCE(lease_owner,''),fence_token,lease_until,deadline_at,COALESCE(lease_until>clock_timestamp(),false) FROM amos_jobs WHERE id=$1 FOR UPDATE`, job.ID).Scan(
		&installationID, &applicationID, &key, &requestHash, &kind, &payload, &externalEffect, &status, &action, &owner, &fenceToken, &leaseUntil, &deadline, &leaseActive)
	if errors.Is(err, sql.ErrNoRows) {
		return errWebhookJobRejected
	}
	if err != nil {
		return ErrPersistence
	}
	if !leaseActive || !leaseUntil.Valid || installationID != job.InstallationID || applicationID != job.ApplicationID || key != job.Key || kind != job.Kind || !bytes.Equal([]byte(payload), job.Payload) || externalEffect != job.ExternalEffect || status != string(job.State) || action != string(job.Action) || owner != job.LeaseOwner || fenceToken != job.FenceToken || !leaseUntil.Time.Equal(job.LeaseUntil) || !deadline.Valid || !deadline.Time.Equal(job.Deadline) || !bytes.Equal(requestHash, job.RequestHash[:]) {
		return errWebhookJobRejected
	}
	return nil
}

type webhookReceiptRow struct {
	installationID, applicationID, environmentID uuid.UUID
	provider, providerAccountID, accountMode     string
	customerRef                                  string
	billingAccountID, workspaceID                sql.NullString
	state, quarantineReason                      sql.NullString
}

func (c *WebhookJobConsumer) processWebhookReceipt(ctx context.Context, tx *sql.Tx, job jobs.Job, payload webhookReconcilePayload, scope ScanScope) (jobs.Resolution, error) {
	var receipt webhookReceiptRow
	err := tx.QueryRowContext(ctx, `SELECT installation_id,application_id,environment_id,provider,provider_account_id,account_mode,COALESCE(customer_ref,''),billing_account_id::text,workspace_id::text,state,quarantine_reason FROM billing_verified_webhook_ingress WHERE id=$1 FOR UPDATE`, payload.IngressID).Scan(
		&receipt.installationID, &receipt.applicationID, &receipt.environmentID, &receipt.provider, &receipt.providerAccountID, &receipt.accountMode, &receipt.customerRef, &receipt.billingAccountID, &receipt.workspaceID, &receipt.state, &receipt.quarantineReason)
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.Resolution{}, errWebhookJobRejected
	}
	if err != nil {
		return jobs.Resolution{}, ErrPersistence
	}
	if receipt.installationID != scope.InstallationID || receipt.applicationID != scope.ApplicationID || receipt.environmentID != scope.EnvironmentID || receipt.provider != scope.Provider || receipt.providerAccountID != scope.ProviderAccountID || receipt.accountMode != string(scope.AccountMode) || job.InstallationID != scope.InstallationID || job.ApplicationID != scope.ApplicationID {
		return jobs.Resolution{}, errWebhookJobRejected
	}
	if receipt.state.String == "quarantined" {
		if receipt.quarantineReason.String == "unknown_customer" && receipt.customerRef != "" && !receipt.billingAccountID.Valid && !receipt.workspaceID.Valid {
			return jobs.Resolution{Kind: jobs.ResolutionSucceeded}, nil
		}
		return jobs.Resolution{}, errWebhookJobRejected
	}
	if receipt.state.String != "received" || receipt.quarantineReason.String != "" || receipt.customerRef == "" || !receipt.billingAccountID.Valid || !receipt.workspaceID.Valid {
		return jobs.Resolution{}, errWebhookJobRejected
	}

	endpoint := billingstore.EndpointScope{InstallationID: scope.InstallationID, ApplicationID: scope.ApplicationID, EnvironmentID: scope.EnvironmentID, Provider: scope.Provider, ProviderAccountID: scope.ProviderAccountID, AccountMode: scope.AccountMode}
	bs, err := billingstore.New(tx)
	if err != nil {
		return jobs.Resolution{}, errWebhookJobRejected
	}
	current, err := bs.FindCustomerBindingByRef(ctx, billingstore.CustomerLookupInput{Endpoint: endpoint, CustomerRef: receipt.customerRef})
	if err != nil {
		if errors.Is(err, billingstore.ErrAccountUnavailable) || errors.Is(err, billingstore.ErrInvalidInput) {
			return jobs.Resolution{}, errWebhookJobRejected
		}
		return jobs.Resolution{}, ErrPersistence
	}
	if current.State != "active" || current.AccountID.String() != receipt.billingAccountID.String || current.Binding.WorkspaceID != receipt.workspaceID.String || current.Binding.InstallationID != scope.InstallationID.String() || current.Binding.EnvironmentID != scope.EnvironmentID.String() || current.Binding.Provider != scope.Provider || current.Binding.AccountID != scope.ProviderAccountID || current.Binding.AccountMode != scope.AccountMode || current.CustomerRef != receipt.customerRef {
		return jobs.Resolution{}, errWebhookJobRejected
	}
	workspaceID, err := uuid.Parse(current.Binding.WorkspaceID)
	if err != nil || workspaceID.String() != current.Binding.WorkspaceID {
		return jobs.Resolution{}, errWebhookJobRejected
	}

	linked, err := linkedWebhookReceipt(ctx, tx, payload.IngressID)
	if err != nil {
		return jobs.Resolution{}, err
	}
	if linked {
		valid, err := linkedWebhookWorkMatches(ctx, tx, payload.IngressID, current, scope)
		if err != nil {
			return jobs.Resolution{}, err
		}
		if !valid {
			return jobs.Resolution{}, errWebhookJobRejected
		}
		return jobs.Resolution{Kind: jobs.ResolutionSucceeded}, nil
	}
	if err := assertWebhookJobLeaseCurrent(ctx, tx, job); err != nil {
		return jobs.Resolution{}, err
	}

	workID, err := uuid.NewV7()
	if err != nil {
		return jobs.Resolution{}, ErrPersistence
	}
	reconcileStore, err := NewStore(tx)
	if err != nil {
		return jobs.Resolution{}, ErrPersistence
	}
	if err := reconcileStore.MarkDirty(ctx, workID, current.Binding, current.CustomerRef); err != nil {
		if errors.Is(err, ErrBindingUnavailable) || errors.Is(err, ErrInvalidInput) {
			return jobs.Resolution{}, errWebhookJobRejected
		}
		return jobs.Resolution{}, ErrPersistence
	}
	var workRef uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT id FROM billing_reconcile_work WHERE billing_account_id=$1 AND customer_binding_id=$2 AND installation_id=$3 AND application_id=$4 AND environment_id=$5 AND workspace_id=$6 AND provider=$7 AND provider_account_id=$8 AND account_mode=$9 AND customer_ref=$10 FOR UPDATE`,
		current.AccountID, current.ID, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, workspaceID, scope.Provider, scope.ProviderAccountID, string(scope.AccountMode), current.CustomerRef).Scan(&workRef)
	if err != nil {
		return jobs.Resolution{}, ErrPersistence
	}
	if err := assertWebhookJobLeaseCurrent(ctx, tx, job); err != nil {
		return jobs.Resolution{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO billing_reconcile_ingress(ingress_id,work_id) VALUES($1,$2) ON CONFLICT(ingress_id) DO NOTHING`, payload.IngressID, workRef)
	if err != nil {
		return jobs.Resolution{}, ErrPersistence
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return jobs.Resolution{}, ErrPersistence
	}
	if rows != 1 {
		valid, err := linkedWebhookWorkMatches(ctx, tx, payload.IngressID, current, scope)
		if err != nil {
			return jobs.Resolution{}, err
		}
		if valid {
			return jobs.Resolution{}, errWebhookAlreadyLinked
		}
		return jobs.Resolution{}, errWebhookJobRejected
	}
	return jobs.Resolution{Kind: jobs.ResolutionSucceeded}, nil
}

func assertWebhookJobLeaseCurrent(ctx context.Context, tx *sql.Tx, job jobs.Job) error {
	var active bool
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(lease_until>clock_timestamp(),false) FROM amos_jobs
		WHERE id=$1 AND installation_id=$2 AND application_id=$3 AND idempotency_key=$4 AND kind=$5 AND external_effect=false
		AND status=$6 AND lease_action=$7 AND lease_owner=$8 AND fence_token=$9`,
		job.ID, job.InstallationID, job.ApplicationID, job.Key, job.Kind, string(job.State), string(job.Action), job.LeaseOwner, job.FenceToken).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return errWebhookJobRejected
	}
	if err != nil {
		return ErrPersistence
	}
	if !active {
		return errWebhookJobRejected
	}
	return nil
}

func linkedWebhookReceipt(ctx context.Context, tx *sql.Tx, ingressID uuid.UUID) (bool, error) {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM billing_reconcile_ingress WHERE ingress_id=$1)`, ingressID).Scan(&exists); err != nil {
		return false, ErrPersistence
	}
	return exists, nil
}

func linkedWebhookWorkMatches(ctx context.Context, tx *sql.Tx, ingressID uuid.UUID, current billingstore.CustomerBinding, scope ScanScope) (bool, error) {
	var matches bool
	workspaceID, err := uuid.Parse(current.Binding.WorkspaceID)
	if err != nil || workspaceID.String() != current.Binding.WorkspaceID {
		return false, errWebhookJobRejected
	}
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM billing_reconcile_ingress i JOIN billing_reconcile_work w ON w.id=i.work_id
		WHERE i.ingress_id=$1 AND w.billing_account_id=$2 AND w.customer_binding_id=$3 AND w.installation_id=$4 AND w.application_id=$5 AND w.environment_id=$6 AND w.workspace_id=$7 AND w.provider=$8 AND w.provider_account_id=$9 AND w.account_mode=$10 AND w.customer_ref=$11)`,
		ingressID, current.AccountID, current.ID, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, workspaceID, scope.Provider, scope.ProviderAccountID, string(scope.AccountMode), current.CustomerRef).Scan(&matches)
	if err != nil {
		return false, ErrPersistence
	}
	return matches, nil
}

func terminalResolution() jobs.Resolution {
	return jobs.Resolution{Kind: jobs.ResolutionTerminal}
}

func retryResolution(action jobs.Action) jobs.Resolution {
	if action == jobs.ActionReconcile {
		return jobs.Resolution{Kind: jobs.ResolutionNoEffect, RetryAfter: time.Minute}
	}
	return jobs.Resolution{Kind: jobs.ResolutionRetrySafe, RetryAfter: time.Minute}
}
