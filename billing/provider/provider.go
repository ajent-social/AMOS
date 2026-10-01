// Package provider defines the narrow boundary between AMOS billing policy
// and external payment providers. It contains contracts only; it does not
// claim that any provider is configured or qualified.
package provider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrUnsupported    = errors.New("billing provider capability unsupported")
	ErrInvalidBinding = errors.New("invalid billing provider binding")
	ErrInvalidRequest = errors.New("invalid billing provider request")
)

// Capability names are stable identifiers for provider features.
type Capability string

const (
	CapabilityCustomer             Capability = "customer"
	CapabilityCheckout             Capability = "checkout"
	CapabilitySubscriptionSnapshot Capability = "subscription_snapshot"
	CapabilityPortal               Capability = "portal"
	CapabilityQuantities           Capability = "quantities"
	CapabilityMetering             Capability = "metering"
)

// AccountMode separates provider test and live accounts. Unknown modes are
// rejected rather than silently mapped to either mode.
type AccountMode string

const (
	AccountTest AccountMode = "test"
	AccountLive AccountMode = "live"
)

// Binding namespaces every remote object and operation to one workspace and
// one provider account within an installation environment.
type Binding struct {
	InstallationID string
	EnvironmentID  string
	Provider       string
	AccountID      string
	AccountMode    AccountMode
	WorkspaceID    string
}

func (b Binding) Validate() error {
	if b.InstallationID == "" || b.EnvironmentID == "" || b.Provider == "" ||
		b.AccountID == "" || b.WorkspaceID == "" ||
		(b.AccountMode != AccountTest && b.AccountMode != AccountLive) {
		return ErrInvalidBinding
	}
	return nil
}

// ObjectRef carries the complete namespace with the provider's opaque ID.
type ObjectRef struct {
	Binding Binding
	ID      string
}

func (r ObjectRef) Validate() error {
	if err := r.Binding.Validate(); err != nil {
		return err
	}
	if r.ID == "" {
		return ErrInvalidRequest
	}
	return nil
}

type CustomerRequest struct {
	Binding        Binding
	IdempotencyKey string
}
type Customer struct{ Ref ObjectRef }

type CheckoutRequest struct {
	Binding        Binding
	Customer       ObjectRef
	PriceKey       string // server catalog key; never a client-supplied amount
	SuccessURL     string
	CancelURL      string
	IdempotencyKey string
}
type CheckoutSession struct {
	Ref       ObjectRef
	URL       string
	ExpiresAt time.Time
}

type SubscriptionRequest struct {
	Binding  Binding
	Customer ObjectRef
}
type SubscriptionSnapshot struct {
	Ref         ObjectRef
	State       SubscriptionState
	PriceKey    string
	Quantity    int64
	PeriodStart time.Time
	PeriodEnd   time.Time
	ObservedAt  time.Time
}

type PortalRequest struct {
	Binding   Binding
	Customer  ObjectRef
	ReturnURL string
}
type PortalSession struct {
	URL       string
	ExpiresAt time.Time
}

type QuantityRequest struct {
	Binding        Binding
	Subscription   ObjectRef
	PriceKey       string
	Quantity       int64
	IdempotencyKey string
}

type MeterEvent struct {
	Binding    Binding
	EventID    string
	MeterKey   string
	OccurredAt time.Time
	Value      int64
}

type SubscriptionState string

const (
	SubscriptionPending   SubscriptionState = "pending"
	SubscriptionUnknown   SubscriptionState = "unknown"
	SubscriptionConfirmed SubscriptionState = "confirmed"
	SubscriptionFailed    SubscriptionState = "failed"
)

// Outcome represents an external operation whose response may be lost.
// Unknown must remain unknown until a provider read or reconciliation proves
// a terminal state.
type Outcome[T any] struct {
	State          SubscriptionState
	Value          T
	ProviderObject ObjectRef
	ObservedAt     time.Time
	Failure        *ProviderError
}

func (o Outcome[T]) Validate() error {
	switch o.State {
	case SubscriptionPending, SubscriptionUnknown:
		if o.Failure != nil {
			return fmt.Errorf("%w: nonterminal outcome has failure", ErrInvalidRequest)
		}
	case SubscriptionConfirmed:
		if o.Failure != nil {
			return fmt.Errorf("%w: confirmed outcome has failure", ErrInvalidRequest)
		}
	case SubscriptionFailed:
		if o.Failure == nil {
			return fmt.Errorf("%w: failed outcome lacks classification", ErrInvalidRequest)
		}
	default:
		return fmt.Errorf("%w: invalid outcome state", ErrInvalidRequest)
	}
	return nil
}

type ErrorClass string

const (
	ErrorInvalid        ErrorClass = "invalid"
	ErrorConflict       ErrorClass = "conflict"
	ErrorUnavailable    ErrorClass = "unavailable"
	ErrorUnsupported    ErrorClass = "unsupported"
	ErrorUnknownOutcome ErrorClass = "unknown_outcome"
	ErrorPermanent      ErrorClass = "permanent"
)

type ProviderError struct {
	Class     ErrorClass
	Retryable bool
}

func (e *ProviderError) Error() string { return "billing provider operation failed" }

func (e *ProviderError) Is(target error) bool {
	return target == ErrUnsupported && e.Class == ErrorUnsupported
}

type RetryPolicy struct {
	MaxAttempts         uint32
	IdempotencyDuration time.Duration
}

func (p RetryPolicy) Validate() error {
	if p.MaxAttempts == 0 || p.IdempotencyDuration <= 0 {
		return ErrInvalidRequest
	}
	return nil
}

// Client is implemented by provider adapters. Every method must reject an
// unsupported capability before producing a side effect. A transport timeout
// after submission returns an unknown outcome, never a confirmed result.
type Client interface {
	Capabilities() map[Capability]bool
	EnsureCustomer(context.Context, CustomerRequest) (Customer, error)
	CreateCheckout(context.Context, CheckoutRequest) (Outcome[CheckoutSession], error)
	GetSubscription(context.Context, SubscriptionRequest) (Outcome[SubscriptionSnapshot], error)
	CreatePortal(context.Context, PortalRequest) (PortalSession, error)
	SetQuantity(context.Context, QuantityRequest) (Outcome[SubscriptionSnapshot], error)
	RecordMeterEvent(context.Context, MeterEvent) error
}

// RequireCapability provides a side-effect-free check callers can perform
// before dispatching an optional operation.
func RequireCapability(c Client, capability Capability) error {
	if c == nil || !c.Capabilities()[capability] {
		return &ProviderError{Class: ErrorUnsupported}
	}
	return nil
}

// Gate enforces advertised capabilities before invoking an adapter. It also
// validates workspace/account scoping for every operation that accepts an
// existing provider object.
type Gate struct{ Client }

func (g Gate) check(capability Capability, binding Binding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	return RequireCapability(g.Client, capability)
}

func sameBinding(a, b Binding) bool { return a == b }

func (g Gate) EnsureCustomer(ctx context.Context, r CustomerRequest) (Customer, error) {
	if r.IdempotencyKey == "" {
		return Customer{}, ErrInvalidRequest
	}
	if err := g.check(CapabilityCustomer, r.Binding); err != nil {
		return Customer{}, err
	}
	return g.Client.EnsureCustomer(ctx, r)
}
func (g Gate) CreateCheckout(ctx context.Context, r CheckoutRequest) (Outcome[CheckoutSession], error) {
	if r.PriceKey == "" || r.IdempotencyKey == "" || r.SuccessURL == "" || r.CancelURL == "" || !sameBinding(r.Binding, r.Customer.Binding) {
		return Outcome[CheckoutSession]{}, ErrInvalidRequest
	}
	if err := r.Customer.Validate(); err != nil {
		return Outcome[CheckoutSession]{}, err
	}
	if err := g.check(CapabilityCheckout, r.Binding); err != nil {
		return Outcome[CheckoutSession]{}, err
	}
	return g.Client.CreateCheckout(ctx, r)
}
func (g Gate) GetSubscription(ctx context.Context, r SubscriptionRequest) (Outcome[SubscriptionSnapshot], error) {
	if !sameBinding(r.Binding, r.Customer.Binding) {
		return Outcome[SubscriptionSnapshot]{}, ErrInvalidRequest
	}
	if err := r.Customer.Validate(); err != nil {
		return Outcome[SubscriptionSnapshot]{}, err
	}
	if err := g.check(CapabilitySubscriptionSnapshot, r.Binding); err != nil {
		return Outcome[SubscriptionSnapshot]{}, err
	}
	return g.Client.GetSubscription(ctx, r)
}
func (g Gate) CreatePortal(ctx context.Context, r PortalRequest) (PortalSession, error) {
	if r.ReturnURL == "" || !sameBinding(r.Binding, r.Customer.Binding) {
		return PortalSession{}, ErrInvalidRequest
	}
	if err := r.Customer.Validate(); err != nil {
		return PortalSession{}, err
	}
	if err := g.check(CapabilityPortal, r.Binding); err != nil {
		return PortalSession{}, err
	}
	return g.Client.CreatePortal(ctx, r)
}
func (g Gate) SetQuantity(ctx context.Context, r QuantityRequest) (Outcome[SubscriptionSnapshot], error) {
	if r.PriceKey == "" || r.IdempotencyKey == "" || r.Quantity < 0 || !sameBinding(r.Binding, r.Subscription.Binding) {
		return Outcome[SubscriptionSnapshot]{}, ErrInvalidRequest
	}
	if err := r.Subscription.Validate(); err != nil {
		return Outcome[SubscriptionSnapshot]{}, err
	}
	if err := g.check(CapabilityQuantities, r.Binding); err != nil {
		return Outcome[SubscriptionSnapshot]{}, err
	}
	return g.Client.SetQuantity(ctx, r)
}
func (g Gate) RecordMeterEvent(ctx context.Context, e MeterEvent) error {
	if e.EventID == "" || e.MeterKey == "" || e.OccurredAt.IsZero() || e.Value < 0 {
		return ErrInvalidRequest
	}
	if err := g.check(CapabilityMetering, e.Binding); err != nil {
		return err
	}
	return g.Client.RecordMeterEvent(ctx, e)
}
