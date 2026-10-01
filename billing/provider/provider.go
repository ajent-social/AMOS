// Package provider defines the narrow boundary between AMOS billing policy
// and external payment providers. It contains contracts only; it does not
// claim that any provider is configured or qualified.
package provider

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

var (
	ErrUnsupported     = errors.New("billing provider capability unsupported")
	ErrInvalidBinding  = errors.New("invalid billing provider binding")
	ErrInvalidRequest  = errors.New("invalid billing provider request")
	ErrMalformedResult = errors.New("malformed billing provider result")
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
	if !validUUIDv7(b.InstallationID) || !validUUIDv7(b.EnvironmentID) ||
		!validIdentifier(b.Provider, 64) || !validIdentifier(b.AccountID, 128) ||
		!validUUIDv7(b.WorkspaceID) ||
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
	if !validIdentifier(r.ID, 128) {
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
	Ref       ObjectRef
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
		if o.Failure != nil || !isZero(o.Value) {
			return fmt.Errorf("%w: invalid nonterminal outcome", ErrMalformedResult)
		}
	case SubscriptionConfirmed:
		if o.Failure != nil || o.ObservedAt.IsZero() {
			return fmt.Errorf("%w: invalid confirmed outcome", ErrMalformedResult)
		}
	case SubscriptionFailed:
		if o.Failure == nil || !validErrorClass(o.Failure.Class) || !isZero(o.Value) {
			return fmt.Errorf("%w: invalid failed outcome", ErrMalformedResult)
		}
	default:
		return ErrMalformedResult
	}
	return nil
}

func isZero[T any](value T) bool { return reflect.ValueOf(&value).Elem().IsZero() }

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

func validErrorClass(c ErrorClass) bool {
	switch c {
	case ErrorInvalid, ErrorConflict, ErrorUnavailable, ErrorUnsupported, ErrorUnknownOutcome, ErrorPermanent:
		return true
	default:
		return false
	}
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
type CustomerClient interface {
	EnsureCustomer(context.Context, CustomerRequest) (Customer, error)
}
type CheckoutClient interface {
	CreateCheckout(context.Context, CheckoutRequest) (Outcome[CheckoutSession], error)
}
type SubscriptionClient interface {
	GetSubscription(context.Context, SubscriptionRequest) (Outcome[SubscriptionSnapshot], error)
}
type PortalClient interface {
	CreatePortal(context.Context, PortalRequest) (PortalSession, error)
}
type QuantityClient interface {
	SetQuantity(context.Context, QuantityRequest) (Outcome[SubscriptionSnapshot], error)
}
type MeteringClient interface {
	RecordMeterEvent(context.Context, MeterEvent) error
}

// Client is a convenience aggregate for adapters that implement all operations.
// Integrations may implement only the narrow interfaces they need.
type Client interface {
	CustomerClient
	CheckoutClient
	SubscriptionClient
	PortalClient
	QuantityClient
	MeteringClient
}

// RequireCapability provides a side-effect-free check callers can perform
// before dispatching an optional operation.
func nilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func:
		return rv.IsNil()
	}
	return false
}

func unsupported() error { return &ProviderError{Class: ErrorUnsupported} }

// Gate enforces advertised capabilities before invoking an adapter. It also
// validates workspace/account scoping for every operation that accepts an
// existing provider object.
type Gate struct {
	Customer     CustomerClient
	Checkout     CheckoutClient
	Subscription SubscriptionClient
	Portal       PortalClient
	Quantity     QuantityClient
	Metering     MeteringClient
}

func (g Gate) check(capability Capability, binding Binding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	var available bool
	switch capability {
	case CapabilityCustomer:
		available = !nilInterface(g.Customer)
	case CapabilityCheckout:
		available = !nilInterface(g.Checkout)
	case CapabilitySubscriptionSnapshot:
		available = !nilInterface(g.Subscription)
	case CapabilityPortal:
		available = !nilInterface(g.Portal)
	case CapabilityQuantities:
		available = !nilInterface(g.Quantity)
	case CapabilityMetering:
		available = !nilInterface(g.Metering)
	}
	if !available {
		return unsupported()
	}
	return nil
}

func sameBinding(a, b Binding) bool { return a == b }

func (g Gate) EnsureCustomer(ctx context.Context, r CustomerRequest) (Customer, error) {
	if r.IdempotencyKey == "" {
		return Customer{}, ErrInvalidRequest
	}
	if err := g.check(CapabilityCustomer, r.Binding); err != nil {
		return Customer{}, err
	}
	got, err := g.Customer.EnsureCustomer(ctx, r)
	if err != nil {
		return Customer{}, err
	}
	if err := got.Ref.Validate(); err != nil || !sameBinding(got.Ref.Binding, r.Binding) {
		return Customer{}, ErrMalformedResult
	}
	return got, nil
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
	got, err := g.Checkout.CreateCheckout(ctx, r)
	if err != nil {
		return Outcome[CheckoutSession]{}, err
	}
	if err := validateOutcome(got, r.Binding); err != nil {
		return Outcome[CheckoutSession]{}, err
	}
	if got.State == SubscriptionConfirmed && (got.Value.Ref.Validate() != nil || !sameBinding(got.Value.Ref.Binding, r.Binding) || got.Value.Ref.ID != got.ProviderObject.ID || got.Value.URL == "" || got.Value.ExpiresAt.IsZero()) {
		return Outcome[CheckoutSession]{}, ErrMalformedResult
	}
	return got, nil
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
	got, err := g.Subscription.GetSubscription(ctx, r)
	if err != nil {
		return Outcome[SubscriptionSnapshot]{}, err
	}
	if err := validateOutcome(got, r.Binding); err != nil {
		return Outcome[SubscriptionSnapshot]{}, err
	}
	if got.State == SubscriptionConfirmed && (got.Value.Ref.Validate() != nil || !sameBinding(got.Value.Ref.Binding, r.Binding) || got.Value.Ref.ID != got.ProviderObject.ID || got.Value.ObservedAt.IsZero()) {
		return Outcome[SubscriptionSnapshot]{}, ErrMalformedResult
	}
	return got, nil
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
	got, err := g.Portal.CreatePortal(ctx, r)
	if err != nil {
		return PortalSession{}, err
	}
	if got.Ref.Validate() != nil || !sameBinding(got.Ref.Binding, r.Binding) || got.URL == "" || got.ExpiresAt.IsZero() {
		return PortalSession{}, ErrMalformedResult
	}
	return got, nil
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
	got, err := g.Quantity.SetQuantity(ctx, r)
	if err != nil {
		return Outcome[SubscriptionSnapshot]{}, err
	}
	if err := validateOutcome(got, r.Binding); err != nil {
		return Outcome[SubscriptionSnapshot]{}, err
	}
	if got.State == SubscriptionConfirmed && (got.Value.Ref.Validate() != nil || !sameBinding(got.Value.Ref.Binding, r.Binding) || got.Value.Ref.ID != got.ProviderObject.ID || got.Value.Quantity != r.Quantity || got.Value.ObservedAt.IsZero()) {
		return Outcome[SubscriptionSnapshot]{}, ErrMalformedResult
	}
	return got, nil
}
func (g Gate) RecordMeterEvent(ctx context.Context, e MeterEvent) error {
	if e.EventID == "" || e.MeterKey == "" || e.OccurredAt.IsZero() || e.Value < 0 {
		return ErrInvalidRequest
	}
	if err := g.check(CapabilityMetering, e.Binding); err != nil {
		return err
	}
	return g.Metering.RecordMeterEvent(ctx, e)
}

func validateOutcome[T any](o Outcome[T], binding Binding) error {
	if err := o.Validate(); err != nil {
		return ErrMalformedResult
	}
	if o.State == SubscriptionConfirmed {
		if err := o.ProviderObject.Validate(); err != nil || !sameBinding(o.ProviderObject.Binding, binding) {
			return ErrMalformedResult
		}
	} else if o.ProviderObject.ID != "" {
		if err := o.ProviderObject.Validate(); err != nil || !sameBinding(o.ProviderObject.Binding, binding) {
			return ErrMalformedResult
		}
	}
	return nil
}

func validIdentifier(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			if r < 'A' || r > 'Z' {
				if r < '0' || r > '9' {
					if !strings.ContainsRune("_:-", r) {
						return false
					}
				}
			}
		}
	}
	return true
}

func validUUIDv7(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' || s[14] != '7' {
		return false
	}
	if s[19] != '8' && s[19] != '9' && s[19] != 'a' && s[19] != 'b' {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
