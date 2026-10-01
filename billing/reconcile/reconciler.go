package reconcile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	"github.com/google/uuid"
)

// LifecycleStatus is the finite provider lifecycle vocabulary stored without
// collapsing non-entitled states into active access.
type LifecycleStatus string

const (
	StatusActive     LifecycleStatus = "active"
	StatusTrialing   LifecycleStatus = "trialing"
	StatusPastDue    LifecycleStatus = "past_due"
	StatusSuspended  LifecycleStatus = "suspended"
	StatusCanceled   LifecycleStatus = "canceled"
	StatusIncomplete LifecycleStatus = "incomplete"
	StatusUnpaid     LifecycleStatus = "unpaid"
	StatusExpired    LifecycleStatus = "expired"
)

// SnapshotRequest always binds the remote read to the persisted customer and
// account-mode scope. A browser or webhook payload cannot construct this input.
type SnapshotRequest struct {
	Binding     provider.Binding
	CustomerRef string
}

// Subscription is one provider-reported subscription item. PriceKey is the
// provider's configured price identifier, never an amount or client selection.
type Subscription struct {
	Reference              string
	Status                 LifecycleStatus
	PriceKey               string
	Quantity               int64
	PeriodStart, PeriodEnd time.Time
}

// CustomerSnapshot is the provider's authoritative complete subscription
// collection for one customer. Revision is opaque; observedAt is the adapter's
// UTC time at which this complete collection was fetched from the provider.
// An empty list authoritatively means no current subscriptions. More than one
// non-terminal subscription is rejected as ambiguous.
type CustomerSnapshot struct {
	Binding       provider.Binding
	CustomerRef   string
	Revision      string
	ObservedAt    time.Time
	Subscriptions []Subscription
}

// SnapshotSource is implemented by an official provider adapter. It must
// retrieve the complete current collection, bound pagination, enforce the
// provider's account/mode scope, and return only normalized fields. It must not
// infer success from webhook order or a cached event payload.
type SnapshotSource interface {
	GetCustomerSnapshot(context.Context, SnapshotRequest) (CustomerSnapshot, error)
}

// TxRunner owns SQL transaction boundaries for work and projections.
type TxRunner interface {
	WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}

// Config bounds provider reads, durable retries, request rate, snapshot age,
// and the catalog price identifiers accepted by this worker.
type Config struct {
	Lease              time.Duration
	RequestTimeout     time.Duration
	Freshness          time.Duration
	RefreshAfter       time.Duration
	RetryBase          time.Duration
	RetryMax           time.Duration
	MaxAttempts        uint32 // After this many attempts, durable retries use the maximum delay.
	MinRequestInterval time.Duration
	PriceKeys          map[string]struct{}
}

// Reconciler fetches current provider state and atomically applies it to the
// scoped entitlement projection and reconciliation checkpoint.
type Reconciler struct {
	db          TxRunner
	source      SnapshotSource
	cfg         Config
	mu          sync.Mutex
	nextRequest time.Time
	runGate     chan struct{}
}

var revisionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

// New validates the required provider source and all retry/lease limits.
func New(db TxRunner, source SnapshotSource, cfg Config) (*Reconciler, error) {
	if db == nil || source == nil || cfg.Lease <= 0 || cfg.Lease > 15*time.Minute || cfg.RequestTimeout <= 0 || cfg.RequestTimeout > 30*time.Second || cfg.Lease < cfg.RequestTimeout || cfg.Freshness <= 0 || cfg.Freshness > 15*time.Minute || cfg.RefreshAfter <= 0 || cfg.RefreshAfter > 24*time.Hour || cfg.RetryBase <= 0 || cfg.RetryMax < cfg.RetryBase || cfg.RetryMax > time.Hour || cfg.MaxAttempts == 0 || cfg.MaxAttempts > 10 || cfg.MinRequestInterval <= 0 || cfg.MinRequestInterval > time.Minute || len(cfg.PriceKeys) == 0 {
		return nil, ErrInvalidInput
	}
	prices := make(map[string]struct{}, len(cfg.PriceKeys))
	for key := range cfg.PriceKeys {
		if !providerIDPattern.MatchString(key) {
			return nil, ErrInvalidInput
		}
		prices[key] = struct{}{}
	}
	cfg.PriceKeys = prices
	return &Reconciler{db: db, source: source, cfg: cfg, runGate: make(chan struct{}, 1)}, nil
}

// RunOne claims and processes at most one durable customer. The database
// transaction ends before the provider call; applying the result starts a new
// transaction that fences both dirty generation and lease token.
func (r *Reconciler) RunOne(ctx context.Context) (bool, error) {
	if r == nil || r.db == nil || r.source == nil || ctx == nil {
		return false, ErrInvalidInput
	}
	// Serialize calls on this worker before taking a durable lease. Concurrent
	// callers wait cancellably without holding queue rows while provider capacity
	// is occupied by another call.
	select {
	case r.runGate <- struct{}{}:
		defer func() { <-r.runGate }()
	case <-ctx.Done():
		return false, ctx.Err()
	}
	if err := r.waitRate(ctx); err != nil {
		return false, err
	}
	token, err := uuid.NewV7()
	if err != nil {
		return false, fmt.Errorf("mint reconciliation lease token: %w", err)
	}
	var work Work
	var claimed bool
	err = r.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		work, claimed, e = s.ClaimDue(ctx, token, r.cfg.Lease)
		return e
	})
	if err != nil || !claimed {
		return claimed, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, r.cfg.RequestTimeout)
	snapshot, fetchErr := r.source.GetCustomerSnapshot(requestCtx, SnapshotRequest{Binding: work.Binding, CustomerRef: work.CustomerRef})
	cancel()
	if fetchErr != nil {
		return true, r.retry(ctx, work, fmt.Errorf("fetch authoritative customer snapshot: %w", fetchErr))
	}
	if err = validateSnapshot(work, snapshot, time.Now().UTC(), r.cfg.Freshness, r.cfg.PriceKeys); err != nil {
		return true, r.retry(ctx, work, err)
	}
	err = r.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		return s.applySnapshot(ctx, work, snapshot, r.cfg.RefreshAfter, r.cfg.Freshness)
	})
	if err != nil {
		return true, r.retry(ctx, work, err)
	}
	return true, nil
}

func (r *Reconciler) waitRate(ctx context.Context) error {
	r.mu.Lock()
	now := time.Now()
	wait := time.Duration(0)
	if r.nextRequest.After(now) {
		wait = r.nextRequest.Sub(now)
	}
	r.nextRequest = now.Add(wait).Add(r.cfg.MinRequestInterval)
	r.mu.Unlock()
	if wait == 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *Reconciler) retry(ctx context.Context, work Work, cause error) error {
	delay := r.cfg.RetryBase
	for i := uint32(1); i < uint32(work.Attempts) && delay < r.cfg.RetryMax; i++ {
		delay *= 2
		if delay > r.cfg.RetryMax {
			delay = r.cfg.RetryMax
		}
	}
	if work.Attempts >= int(r.cfg.MaxAttempts) {
		delay = r.cfg.RetryMax
	}
	next := time.Now().UTC().Add(delay)
	err := r.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, e := NewStore(tx)
		if e != nil {
			return e
		}
		return s.Retry(ctx, work, next)
	})
	if err != nil && !errors.Is(err, ErrStaleClaim) {
		return errors.Join(cause, fmt.Errorf("schedule reconciliation retry: %w", err))
	}
	if errors.Is(err, ErrStaleClaim) {
		return errors.Join(cause, ErrStaleClaim)
	}
	return cause
}

func validateSnapshot(work Work, s CustomerSnapshot, now time.Time, freshness time.Duration, prices map[string]struct{}) error {
	if !validWork(work) || s.Binding.Validate() != nil || s.Binding != work.Binding || s.CustomerRef != work.CustomerRef || !revisionPattern.MatchString(s.Revision) || s.ObservedAt.IsZero() || s.ObservedAt.After(now.Add(30*time.Second)) || now.Sub(s.ObservedAt) > freshness || len(s.Subscriptions) > 500 {
		return ErrInvalidInput
	}
	nonterminal := 0
	seen := make(map[string]struct{}, len(s.Subscriptions))
	for _, sub := range s.Subscriptions {
		if !providerIDPattern.MatchString(sub.Reference) || !validStatus(sub.Status) || sub.Quantity < 0 || (!sub.PeriodStart.IsZero() && !sub.PeriodEnd.IsZero() && !sub.PeriodEnd.After(sub.PeriodStart)) {
			return ErrInvalidInput
		}
		if _, exists := seen[sub.Reference]; exists {
			return ErrAmbiguousSnapshot
		}
		seen[sub.Reference] = struct{}{}
		if sub.Status != StatusCanceled && sub.Status != StatusExpired {
			nonterminal++
		}
		if sub.Status == StatusActive || sub.Status == StatusTrialing || sub.Status == StatusPastDue || sub.Status == StatusSuspended {
			if _, ok := prices[sub.PriceKey]; !ok {
				return ErrUnknownPrice
			}
		}
	}
	if nonterminal > 1 {
		return ErrAmbiguousSnapshot
	}
	return nil
}

func validStatus(s LifecycleStatus) bool {
	switch s {
	case StatusActive, StatusTrialing, StatusPastDue, StatusSuspended, StatusCanceled, StatusIncomplete, StatusUnpaid, StatusExpired:
		return true
	default:
		return false
	}
}
