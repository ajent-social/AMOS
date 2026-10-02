// Package reconcile durably schedules and applies current-provider billing
// snapshots. Provider work runs outside database transactions; all local state
// changes are fenced by the claimed work version and token.
package reconcile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	"github.com/google/uuid"
)

var (
	ErrInvalidInput       = errors.New("invalid reconciliation input")
	ErrPersistence        = errors.New("reconciliation persistence failed")
	ErrNotClaimed         = errors.New("reconciliation work is not claimed")
	ErrStaleClaim         = errors.New("reconciliation claim is stale")
	ErrStaleSnapshot      = errors.New("provider snapshot is older than current projection")
	ErrIncompleteSnapshot = errors.New("provider snapshot is incomplete")
	ErrAmbiguousSnapshot  = errors.New("provider returned ambiguous subscription snapshot")
	ErrUnknownPrice       = errors.New("provider snapshot contains an unknown price")
	ErrBindingUnavailable = errors.New("active billing customer binding unavailable")
)

var providerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_:./-]{0,127}$`)

// Work is a leased, immutable claim for one current customer binding. Version
// is the dirty generation captured by the claim. Any later signal increments
// dirty_version and makes this result uncommittable.
type Work struct {
	ID, BillingAccountID, CustomerBindingID uuid.UUID
	ApplicationID                           uuid.UUID
	Binding                                 provider.Binding
	CustomerRef                             string
	Version                                 int64
	Token                                   uuid.UUID
	LeaseUntil                              time.Time
	Attempts                                int
}

// ScanScope identifies one provider endpoint's durable keyset scan.
type ScanScope struct {
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	Provider, ProviderAccountID                  string
	AccountMode                                  provider.AccountMode
}

// ScanResult reports one bounded page of active bindings and newly matched
// unknown-customer webhook receipts. Reaching the end advances the durable
// checkpoint to the next complete scan cycle.
type ScanResult struct {
	Bindings, UnmatchedEvents int
	Cycle                     uint64
	Complete                  bool
}

// Store applies reconciliation mutations inside one caller-owned transaction.
type Store struct{ tx *sql.Tx }

// NewStore creates a transaction-scoped reconciliation store.
func NewStore(tx *sql.Tx) (*Store, error) {
	if tx == nil {
		return nil, ErrInvalidInput
	}
	return &Store{tx: tx}, nil
}

// MarkDirty records a provider-state signal for the active customer. It is
// intentionally harmless to repeat: duplicate signals can cause another read,
// but cannot replay a provider mutation.
func (s *Store) MarkDirty(ctx context.Context, id uuid.UUID, binding provider.Binding, customerRef string) error {
	if s == nil || s.tx == nil || ctx == nil || !validID(id) || binding.Validate() != nil || !providerIDPattern.MatchString(customerRef) {
		return ErrInvalidInput
	}
	var workID uuid.UUID
	err := s.tx.QueryRowContext(ctx, `INSERT INTO billing_reconcile_work
		(id,billing_account_id,customer_binding_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,customer_ref)
		SELECT $1,a.id,c.id,a.installation_id,a.application_id,a.environment_id,a.workspace_id,a.provider,a.provider_account_id,a.account_mode,c.customer_ref
		FROM billing_workspace_accounts a JOIN billing_customer_bindings c
		ON (c.billing_account_id,c.installation_id,c.application_id,c.environment_id,c.workspace_id,c.provider,c.provider_account_id,c.account_mode)=(a.id,a.installation_id,a.application_id,a.environment_id,a.workspace_id,a.provider,a.provider_account_id,a.account_mode)
		JOIN workspaces w ON (w.id,w.installation_id,w.application_id)=(a.workspace_id,a.installation_id,a.application_id)
		WHERE a.installation_id=$2 AND a.environment_id=$3 AND a.workspace_id=$4 AND a.provider=$5 AND a.provider_account_id=$6 AND a.account_mode=$7 AND a.state='active' AND c.state='active' AND w.state='active' AND c.customer_ref=$8
		ON CONFLICT (billing_account_id) DO UPDATE SET
		customer_binding_id=EXCLUDED.customer_binding_id, customer_ref=EXCLUDED.customer_ref,
		dirty_version=billing_reconcile_work.dirty_version+1, next_attempt_at=LEAST(billing_reconcile_work.next_attempt_at,transaction_timestamp()),
		claim_token=NULL,claim_version=NULL,lease_until=NULL,
		updated_at=transaction_timestamp()
		RETURNING id`, id, uuid.MustParse(binding.InstallationID), uuid.MustParse(binding.EnvironmentID), uuid.MustParse(binding.WorkspaceID), binding.Provider, binding.AccountID, string(binding.AccountMode), customerRef).Scan(&workID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("mark customer reconciliation dirty: %w", ErrBindingUnavailable)
	}
	if err != nil {
		return fmt.Errorf("mark customer reconciliation dirty: %w", ErrPersistence)
	}
	return nil
}

// ClaimDue atomically leases the next due work item. Expired leases can be
// reclaimed; SKIP LOCKED permits independent workers without duplicate claims.
func (s *Store) ClaimDue(ctx context.Context, token uuid.UUID, lease time.Duration) (Work, bool, error) {
	return s.claimDue(ctx, token, lease, nil)
}

// ClaimDueInScope atomically leases due work for exactly one provider endpoint.
// Installation and application are both part of this security boundary.
func (s *Store) ClaimDueInScope(ctx context.Context, token uuid.UUID, lease time.Duration, scope ScanScope) (Work, bool, error) {
	if !scope.valid() {
		return Work{}, false, ErrInvalidInput
	}
	return s.claimDue(ctx, token, lease, &scope)
}

func (s *Store) claimDue(ctx context.Context, token uuid.UUID, lease time.Duration, scope *ScanScope) (Work, bool, error) {
	if s == nil || s.tx == nil || ctx == nil || !validID(token) || lease <= 0 || lease > 15*time.Minute {
		return Work{}, false, ErrInvalidInput
	}
	scopeFilter := ""
	args := []any{token, lease.Milliseconds()}
	if scope != nil {
		scopeFilter = `w.installation_id=$3 AND w.application_id=$4 AND w.environment_id=$5 AND w.provider=$6 AND w.provider_account_id=$7 AND w.account_mode=$8 AND `
		args = append(args, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.Provider, scope.ProviderAccountID, string(scope.AccountMode))
	}
	var out Work
	err := s.tx.QueryRowContext(ctx, fmt.Sprintf(`WITH candidate AS (
		SELECT w.id FROM billing_reconcile_work w
		JOIN billing_workspace_accounts a ON a.id=w.billing_account_id AND a.state='active'
		JOIN billing_customer_bindings c ON c.id=w.customer_binding_id AND c.billing_account_id=w.billing_account_id AND c.installation_id=w.installation_id AND c.application_id=w.application_id AND c.environment_id=w.environment_id AND c.workspace_id=w.workspace_id AND c.provider=w.provider AND c.provider_account_id=w.provider_account_id AND c.account_mode=w.account_mode AND c.state='active' AND c.customer_ref=w.customer_ref
		JOIN workspaces x ON (x.id,x.installation_id,x.application_id)=(w.workspace_id,w.installation_id,w.application_id) AND x.state='active'
		WHERE %s((w.claim_token IS NULL AND w.next_attempt_at<=transaction_timestamp()) OR (w.claim_token IS NOT NULL AND w.lease_until<=transaction_timestamp()))
		ORDER BY w.next_attempt_at,w.updated_at,w.id FOR UPDATE OF w SKIP LOCKED LIMIT 1
		) UPDATE billing_reconcile_work w SET claim_token=$1,claim_version=w.dirty_version,lease_until=transaction_timestamp()+($2 * interval '1 millisecond'),
		attempts=w.attempts+1,updated_at=transaction_timestamp()
	FROM candidate c WHERE w.id=c.id
		RETURNING w.id,w.billing_account_id,w.customer_binding_id,w.installation_id,w.application_id,w.environment_id,w.workspace_id,w.provider,w.provider_account_id,w.account_mode,w.customer_ref,w.claim_version,w.claim_token,w.lease_until,w.attempts`, scopeFilter), args...).Scan(
		&out.ID, &out.BillingAccountID, &out.CustomerBindingID, &out.Binding.InstallationID, &out.ApplicationID, &out.Binding.EnvironmentID, &out.Binding.WorkspaceID, &out.Binding.Provider, &out.Binding.AccountID, &out.Binding.AccountMode, &out.CustomerRef, &out.Version, &out.Token, &out.LeaseUntil, &out.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return Work{}, false, nil
	}
	if err != nil {
		return Work{}, false, fmt.Errorf("claim due reconciliation work: %w", ErrPersistence)
	}
	return out, true, nil
}

// Retry releases a still-current claim for a bounded delayed retry. It never
// clears a newer dirty signal or a replacement worker's lease.
func (s *Store) Retry(ctx context.Context, work Work, next time.Time) error {
	if s == nil || s.tx == nil || ctx == nil || !validWork(work) || next.IsZero() {
		return ErrInvalidInput
	}
	result, err := s.tx.ExecContext(ctx, `UPDATE billing_reconcile_work SET claim_token=NULL,claim_version=NULL,lease_until=NULL,next_attempt_at=$4,updated_at=transaction_timestamp()
		WHERE id=$1 AND claim_token=$2 AND claim_version=$3 AND dirty_version=$3 AND lease_until>clock_timestamp()`, work.ID, work.Token, work.Version, next)
	if err != nil {
		return fmt.Errorf("release failed reconciliation claim: %w", ErrPersistence)
	}
	return requireOne(result, ErrStaleClaim)
}

// ApplySnapshot stores provider state and completes the queue row in one SQL
// transaction. A concurrent dirty signal or reclaimed lease rejects the whole
// operation, so no stale worker can overwrite a newer reconciliation.
func (s *Store) applySnapshot(ctx context.Context, work Work, snapshot CustomerSnapshot, refreshAfter, freshness time.Duration) error {
	if s == nil || s.tx == nil || ctx == nil || !validWork(work) || snapshot.Binding != work.Binding || snapshot.CustomerRef != work.CustomerRef || snapshot.ObservedAt.IsZero() || refreshAfter <= 0 || refreshAfter > 24*time.Hour || freshness <= 0 || freshness > MaxEntitlementFreshness {
		return ErrInvalidInput
	}
	if !snapshot.Complete {
		return ErrIncompleteSnapshot
	}
	if snapshot.ObservedAt.After(time.Now().UTC()) || time.Since(snapshot.ObservedAt) > freshness {
		return ErrStaleSnapshot
	}
	var dirtyVersion int64
	var currentToken uuid.UUID
	var lease sql.NullTime
	var previousObserved sql.NullTime
	var previousRevision sql.NullString
	err := s.tx.QueryRowContext(ctx, `SELECT dirty_version,claim_token,lease_until,last_observed_at,last_provider_revision FROM billing_reconcile_work WHERE id=$1 FOR UPDATE`, work.ID).Scan(&dirtyVersion, &currentToken, &lease, &previousObserved, &previousRevision)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrStaleClaim
		}
		return fmt.Errorf("lock reconciliation work: %w", ErrPersistence)
	}
	var databaseNow time.Time
	if err = s.tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
		return fmt.Errorf("read database clock for reconciliation lease: %w", ErrPersistence)
	}
	if currentToken != work.Token || dirtyVersion != work.Version || !lease.Valid || !lease.Time.After(databaseNow) {
		return ErrStaleClaim
	}
	if previousObserved.Valid {
		if snapshot.ObservedAt.Before(previousObserved.Time) {
			return ErrStaleSnapshot
		}
		if snapshot.ObservedAt.Equal(previousObserved.Time) && previousRevision.String != snapshot.Revision {
			return ErrStaleSnapshot
		}
	}
	var maxObserved sql.NullTime
	err = s.tx.QueryRowContext(ctx, `SELECT max(observed_at) FROM billing_subscription_projections WHERE billing_account_id=$1`, work.BillingAccountID).Scan(&maxObserved)
	if err != nil {
		return fmt.Errorf("read current subscription observation: %w", ErrPersistence)
	}
	if maxObserved.Valid && snapshot.ObservedAt.Before(maxObserved.Time) {
		return ErrStaleSnapshot
	}
	selected, err := currentSubscription(snapshot.Subscriptions)
	if err != nil {
		return err
	}
	// Preserve the provider's complete lifecycle facts separately from the
	// frozen entitlement vocabulary. Unknown future authority requires an
	// integrator policy review rather than a wider shared projection enum.
	_, err = s.tx.ExecContext(ctx, `UPDATE billing_reconcile_projection_metadata SET provider_status='absent',provider_revision=$2,observed_at=$3,updated_at=transaction_timestamp()
		WHERE billing_account_id=$1 AND observed_at<=$3`, work.BillingAccountID, snapshot.Revision, snapshot.ObservedAt)
	if err != nil {
		return fmt.Errorf("retire absent provider lifecycle metadata: %w", ErrPersistence)
	}
	for _, sub := range snapshot.Subscriptions {
		_, err = s.tx.ExecContext(ctx, `INSERT INTO billing_reconcile_projection_metadata(billing_account_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,subscription_ref,provider_status,provider_revision,observed_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT(billing_account_id,subscription_ref) DO UPDATE SET provider_status=EXCLUDED.provider_status,provider_revision=EXCLUDED.provider_revision,observed_at=EXCLUDED.observed_at,updated_at=transaction_timestamp()
			WHERE billing_reconcile_projection_metadata.observed_at<=EXCLUDED.observed_at`, work.BillingAccountID, uuid.MustParse(work.Binding.InstallationID), work.ApplicationID, uuid.MustParse(work.Binding.EnvironmentID), uuid.MustParse(work.Binding.WorkspaceID), work.Binding.Provider, work.Binding.AccountID, string(work.Binding.AccountMode), sub.Reference, string(sub.Status), snapshot.Revision, snapshot.ObservedAt)
		if err != nil {
			return fmt.Errorf("persist provider lifecycle metadata: %w", ErrPersistence)
		}
	}
	// Preserve catalog's existing entitlement-state meanings. Only an active
	// provider subscription with a valid paid period becomes confirmed. Trialing,
	// suspended and unpaid remain fail-closed in the canonical projection.
	var currentRef any
	if selected != nil {
		currentRef = selected.Reference
	}
	_, err = s.tx.ExecContext(ctx, `UPDATE billing_subscription_projections SET state='canceled',observed_at=$2,version=version+1,updated_at=transaction_timestamp()
		WHERE billing_account_id=$1 AND ($3::text IS NULL OR subscription_ref<>$3) AND state NOT IN ('canceled','expired') AND observed_at<=$2`, work.BillingAccountID, snapshot.ObservedAt, currentRef)
	if err != nil {
		return fmt.Errorf("retire superseded subscription projections: %w", ErrPersistence)
	}
	if selected != nil {
		start, end := nullableTime(selected.PeriodStart), nullableTime(selected.PeriodEnd)
		var projectionID uuid.UUID
		projectionID, err = uuid.NewV7()
		if err != nil {
			return fmt.Errorf("create subscription projection identity: %w", err)
		}
		err = s.tx.QueryRowContext(ctx, `INSERT INTO billing_subscription_projections(id,billing_account_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,subscription_ref,state,price_key,quantity,period_start,period_end,observed_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT(billing_account_id,subscription_ref) DO UPDATE SET state=EXCLUDED.state,price_key=EXCLUDED.price_key,quantity=EXCLUDED.quantity,period_start=EXCLUDED.period_start,period_end=EXCLUDED.period_end,observed_at=EXCLUDED.observed_at,version=billing_subscription_projections.version+1,updated_at=transaction_timestamp()
		WHERE billing_subscription_projections.observed_at<=EXCLUDED.observed_at
		RETURNING id`, projectionID, work.BillingAccountID, uuid.MustParse(work.Binding.InstallationID), work.ApplicationID, uuid.MustParse(work.Binding.EnvironmentID), uuid.MustParse(work.Binding.WorkspaceID), work.Binding.Provider, work.Binding.AccountID, string(work.Binding.AccountMode), selected.Reference, string(canonicalProjectionState(*selected)), selected.PriceKey, selected.Quantity, start, end, snapshot.ObservedAt).Scan(&projectionID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrStaleSnapshot
		}
		if err != nil {
			return fmt.Errorf("upsert authoritative subscription projection: %w", errors.Join(ErrPersistence, err))
		}
	}
	result, err := s.tx.ExecContext(ctx, `UPDATE billing_reconcile_work SET claim_token=NULL,claim_version=NULL,lease_until=NULL,next_attempt_at=clock_timestamp()+($4 * interval '1 millisecond'),last_observed_at=$5,last_provider_revision=$6,updated_at=transaction_timestamp()
		WHERE id=$1 AND claim_token=$2 AND claim_version=$3 AND dirty_version=$3 AND lease_until>clock_timestamp()`, work.ID, work.Token, work.Version, refreshAfter.Milliseconds(), snapshot.ObservedAt, snapshot.Revision)
	if err != nil {
		return fmt.Errorf("complete provider reconciliation: %w", ErrPersistence)
	}
	return requireOne(result, ErrStaleClaim)
}

func currentSubscription(subscriptions []Subscription) (*Subscription, error) {
	var current *Subscription
	for i := range subscriptions {
		sub := &subscriptions[i]
		if sub.Status == StatusCanceled || sub.Status == StatusExpired {
			if current == nil {
				current = sub
			}
			continue
		}
		if current != nil && current.Status != StatusCanceled && current.Status != StatusExpired {
			return nil, ErrAmbiguousSnapshot
		}
		if current != nil && (current.Status == StatusCanceled || current.Status == StatusExpired) {
			current = sub
			continue
		}
		current = sub
	}
	return current, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func canonicalProjectionState(sub Subscription) string {
	switch sub.Status {
	case StatusActive:
		if !sub.PeriodStart.IsZero() && sub.PeriodEnd.After(sub.PeriodStart) {
			return "confirmed"
		}
		return "incomplete"
	case StatusPastDue:
		return "past_due"
	case StatusCanceled, StatusExpired:
		return "canceled"
	case StatusIncomplete, StatusTrialing:
		return "incomplete"
	case StatusSuspended, StatusUnpaid:
		return "failed"
	default:
		return "unknown"
	}
}

// ScanPage schedules the next keyset page of active customer bindings and
// retries durable unknown-customer receipts that may now match a binding. The
// checkpoint row lock serializes scanners for one provider endpoint; all work
// insertion and cursor advancement commit with the caller's transaction.
func (s *Store) ScanPage(ctx context.Context, checkpointID uuid.UUID, scope ScanScope, limit int) (ScanResult, error) {
	if s == nil || s.tx == nil || ctx == nil || !validID(checkpointID) || !scope.valid() || limit < 1 || limit > 500 {
		return ScanResult{}, ErrInvalidInput
	}
	_, err := s.tx.ExecContext(ctx, `INSERT INTO billing_reconcile_scan_checkpoints (id,installation_id,application_id,environment_id,provider,provider_account_id,account_mode,cycle)
		VALUES($1,$2,$3,$4,$5,$6,$7,1) ON CONFLICT(installation_id,application_id,environment_id,provider,provider_account_id,account_mode) DO NOTHING`, checkpointID, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.Provider, scope.ProviderAccountID, string(scope.AccountMode))
	if err != nil {
		return ScanResult{}, fmt.Errorf("create reconciliation scan checkpoint: %w", ErrPersistence)
	}
	var after sql.NullString
	var cycle int64
	err = s.tx.QueryRowContext(ctx, `SELECT after_account_id::text,cycle FROM billing_reconcile_scan_checkpoints WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND provider=$4 AND provider_account_id=$5 AND account_mode=$6 FOR UPDATE`, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.Provider, scope.ProviderAccountID, string(scope.AccountMode)).Scan(&after, &cycle)
	if err != nil {
		return ScanResult{}, fmt.Errorf("lock reconciliation scan checkpoint: %w", ErrPersistence)
	}
	var cursor any
	if after.Valid {
		cursor = after.String
	}
	rows, err := s.tx.QueryContext(ctx, `SELECT a.id,c.id,a.workspace_id,a.environment_id,a.provider,a.provider_account_id,a.account_mode,c.customer_ref
		FROM billing_workspace_accounts a JOIN billing_customer_bindings c ON (c.billing_account_id,c.installation_id,c.application_id,c.environment_id,c.workspace_id,c.provider,c.provider_account_id,c.account_mode)=(a.id,a.installation_id,a.application_id,a.environment_id,a.workspace_id,a.provider,a.provider_account_id,a.account_mode) AND c.state='active'
		JOIN workspaces w ON (w.id,w.installation_id,w.application_id)=(a.workspace_id,a.installation_id,a.application_id) AND w.state='active'
		WHERE a.installation_id=$1 AND a.application_id=$2 AND a.environment_id=$3 AND a.provider=$4 AND a.provider_account_id=$5 AND a.account_mode=$6 AND a.state='active' AND ($7::uuid IS NULL OR a.id>$7)
		ORDER BY a.id LIMIT $8`, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.Provider, scope.ProviderAccountID, string(scope.AccountMode), cursor, limit)
	if err != nil {
		return ScanResult{}, fmt.Errorf("read reconciliation scan page: %w", ErrPersistence)
	}
	type customer struct {
		account, binding, workspace, environment    uuid.UUID
		providerName, accountRef, mode, customerRef string
	}
	customers := make([]customer, 0, limit)
	for rows.Next() {
		var c customer
		if err := rows.Scan(&c.account, &c.binding, &c.workspace, &c.environment, &c.providerName, &c.accountRef, &c.mode, &c.customerRef); err != nil {
			closeErr := rows.Close()
			return ScanResult{}, fmt.Errorf("scan reconciliation customer: %w", errors.Join(ErrPersistence, err, closeErr))
		}
		customers = append(customers, c)
	}
	if err := rows.Err(); err != nil {
		closeErr := rows.Close()
		return ScanResult{}, fmt.Errorf("iterate reconciliation customers: %w", errors.Join(ErrPersistence, err, closeErr))
	}
	if err := rows.Close(); err != nil {
		return ScanResult{}, fmt.Errorf("close reconciliation customer page: %w", ErrPersistence)
	}
	for _, c := range customers {
		id, err := uuid.NewV7()
		if err != nil {
			return ScanResult{}, fmt.Errorf("create reconciliation work identity: %w", err)
		}
		_, err = s.tx.ExecContext(ctx, `INSERT INTO billing_reconcile_work (id,billing_account_id,customer_binding_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,customer_ref,last_scan_cycle)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT(billing_account_id) DO UPDATE SET customer_binding_id=EXCLUDED.customer_binding_id,customer_ref=EXCLUDED.customer_ref,
			dirty_version=billing_reconcile_work.dirty_version+CASE WHEN billing_reconcile_work.last_scan_cycle<$12 THEN 1 ELSE 0 END,
			last_scan_cycle=GREATEST(billing_reconcile_work.last_scan_cycle,$12),next_attempt_at=LEAST(billing_reconcile_work.next_attempt_at,transaction_timestamp()),
			claim_token=CASE WHEN billing_reconcile_work.last_scan_cycle<$12 THEN NULL ELSE billing_reconcile_work.claim_token END,
			claim_version=CASE WHEN billing_reconcile_work.last_scan_cycle<$12 THEN NULL ELSE billing_reconcile_work.claim_version END,
			lease_until=CASE WHEN billing_reconcile_work.last_scan_cycle<$12 THEN NULL ELSE billing_reconcile_work.lease_until END,updated_at=transaction_timestamp()`, id, c.account, c.binding, scope.InstallationID, scope.ApplicationID, c.environment, c.workspace, c.providerName, c.accountRef, c.mode, c.customerRef, cycle)
		if err != nil {
			return ScanResult{}, fmt.Errorf("schedule reconciliation scan customer: %w", ErrPersistence)
		}
	}
	matched, err := s.matchUnknownCustomers(ctx, scope, limit)
	if err != nil {
		return ScanResult{}, err
	}
	result := ScanResult{Bindings: len(customers), UnmatchedEvents: matched, Cycle: uint64(cycle)}
	if len(customers) < limit {
		result.Complete = true
		_, err = s.tx.ExecContext(ctx, `UPDATE billing_reconcile_scan_checkpoints SET after_account_id=NULL,cycle=cycle+1,version=version+1,updated_at=transaction_timestamp()
			WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND provider=$4 AND provider_account_id=$5 AND account_mode=$6`, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.Provider, scope.ProviderAccountID, string(scope.AccountMode))
	} else {
		last := customers[len(customers)-1].account
		_, err = s.tx.ExecContext(ctx, `UPDATE billing_reconcile_scan_checkpoints SET after_account_id=$7,version=version+1,updated_at=transaction_timestamp()
			WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND provider=$4 AND provider_account_id=$5 AND account_mode=$6`, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.Provider, scope.ProviderAccountID, string(scope.AccountMode), last)
	}
	if err != nil {
		return ScanResult{}, fmt.Errorf("advance reconciliation scan checkpoint: %w", ErrPersistence)
	}
	return result, nil
}

func (s ScanScope) valid() bool {
	return validID(s.InstallationID) && validID(s.ApplicationID) && validID(s.EnvironmentID) && providerIDPattern.MatchString(s.Provider) && providerIDPattern.MatchString(s.ProviderAccountID) && (s.AccountMode == provider.AccountTest || s.AccountMode == provider.AccountLive)
}

func (s *Store) matchUnknownCustomers(ctx context.Context, scope ScanScope, limit int) (int, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT i.id,c.id,c.billing_account_id,c.workspace_id,c.customer_ref
		FROM billing_verified_webhook_ingress i JOIN billing_customer_bindings c ON c.installation_id=i.installation_id AND c.application_id=i.application_id AND c.environment_id=i.environment_id AND c.provider=i.provider AND c.provider_account_id=i.provider_account_id AND c.account_mode=i.account_mode AND c.customer_ref=i.customer_ref AND c.state='active'
		JOIN billing_workspace_accounts a ON a.id=c.billing_account_id AND a.state='active'
		JOIN workspaces w ON (w.id,w.installation_id,w.application_id)=(a.workspace_id,a.installation_id,a.application_id) AND w.state='active'
		WHERE i.installation_id=$1 AND i.application_id=$2 AND i.environment_id=$3 AND i.provider=$4 AND i.provider_account_id=$5 AND i.account_mode=$6 AND i.state='quarantined' AND i.quarantine_reason='unknown_customer' AND NOT EXISTS (SELECT 1 FROM billing_reconcile_ingress x WHERE x.ingress_id=i.id)
		ORDER BY i.received_at,i.id LIMIT $7`, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.Provider, scope.ProviderAccountID, string(scope.AccountMode), limit)
	if err != nil {
		return 0, fmt.Errorf("read unmatched billing receipts: %w", ErrPersistence)
	}
	type match struct {
		ingress, binding, account, workspace uuid.UUID
		customerRef                          string
	}
	matches := make([]match, 0, limit)
	for rows.Next() {
		var m match
		if err := rows.Scan(&m.ingress, &m.binding, &m.account, &m.workspace, &m.customerRef); err != nil {
			closeErr := rows.Close()
			return 0, fmt.Errorf("scan unmatched billing receipt: %w", errors.Join(ErrPersistence, err, closeErr))
		}
		matches = append(matches, m)
	}
	if err := rows.Err(); err != nil {
		closeErr := rows.Close()
		return 0, fmt.Errorf("iterate unmatched billing receipts: %w", errors.Join(ErrPersistence, err, closeErr))
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close unmatched billing receipts: %w", ErrPersistence)
	}
	for _, m := range matches {
		id, err := uuid.NewV7()
		if err != nil {
			return 0, fmt.Errorf("create unmatched-event work identity: %w", err)
		}
		var workID uuid.UUID
		err = s.tx.QueryRowContext(ctx, `INSERT INTO billing_reconcile_work(id,billing_account_id,customer_binding_id,installation_id,application_id,environment_id,workspace_id,provider,provider_account_id,account_mode,customer_ref)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT(billing_account_id) DO UPDATE SET customer_binding_id=EXCLUDED.customer_binding_id,customer_ref=EXCLUDED.customer_ref,dirty_version=billing_reconcile_work.dirty_version+1,next_attempt_at=LEAST(billing_reconcile_work.next_attempt_at,transaction_timestamp()),claim_token=NULL,claim_version=NULL,lease_until=NULL,updated_at=transaction_timestamp()
			RETURNING id`, id, m.account, m.binding, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, m.workspace, scope.Provider, scope.ProviderAccountID, string(scope.AccountMode), m.customerRef).Scan(&workID)
		if err != nil {
			return 0, fmt.Errorf("schedule previously unmatched customer: %w", ErrPersistence)
		}
		if _, err = s.tx.ExecContext(ctx, `INSERT INTO billing_reconcile_ingress(ingress_id,work_id) VALUES($1,$2) ON CONFLICT(ingress_id) DO NOTHING`, m.ingress, workID); err != nil {
			return 0, fmt.Errorf("link unmatched receipt to reconciliation: %w", ErrPersistence)
		}
	}
	return len(matches), nil
}

func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
func validWork(w Work) bool {
	return validID(w.ID) && validID(w.BillingAccountID) && validID(w.CustomerBindingID) && validID(w.ApplicationID) && validID(w.Token) && w.Version > 0 && w.Binding.Validate() == nil && providerIDPattern.MatchString(w.CustomerRef)
}
func requireOne(result sql.Result, zero error) error {
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect reconciliation update: %w", ErrPersistence)
	}
	if n != 1 {
		return zero
	}
	return nil
}
