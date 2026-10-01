package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testClient struct {
	caps     map[Capability]bool
	calls    int
	quantity int64
	checkout Outcome[CheckoutSession]
}

func (c *testClient) Capabilities() map[Capability]bool { return c.caps }
func (c *testClient) EnsureCustomer(context.Context, CustomerRequest) (Customer, error) {
	c.calls++
	return Customer{}, nil
}
func (c *testClient) CreateCheckout(context.Context, CheckoutRequest) (Outcome[CheckoutSession], error) {
	c.calls++
	return c.checkout, nil
}
func (c *testClient) GetSubscription(context.Context, SubscriptionRequest) (Outcome[SubscriptionSnapshot], error) {
	c.calls++
	return Outcome[SubscriptionSnapshot]{State: SubscriptionConfirmed}, nil
}
func (c *testClient) CreatePortal(context.Context, PortalRequest) (PortalSession, error) {
	c.calls++
	return PortalSession{}, nil
}
func (c *testClient) SetQuantity(_ context.Context, r QuantityRequest) (Outcome[SubscriptionSnapshot], error) {
	c.calls++
	c.quantity = r.Quantity
	return Outcome[SubscriptionSnapshot]{State: SubscriptionConfirmed, Value: SubscriptionSnapshot{Quantity: r.Quantity}}, nil
}
func (c *testClient) RecordMeterEvent(context.Context, MeterEvent) error { c.calls++; return nil }

func validBinding() Binding {
	return Binding{InstallationID: "install", EnvironmentID: "prod", Provider: "stripe", AccountID: "acct", AccountMode: AccountLive, WorkspaceID: "workspace"}
}

func TestT5_2_UnsupportedQuantityDoesNotCallAdapter(t *testing.T) {
	c := &testClient{caps: map[Capability]bool{CapabilityQuantities: false}}
	g := Gate{Client: c}
	b := validBinding()
	_, err := g.SetQuantity(context.Background(), QuantityRequest{Binding: b, Subscription: ObjectRef{Binding: b, ID: "sub"}, PriceKey: "seat", Quantity: 4, IdempotencyKey: "request-1"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected unsupported error, got %v", err)
	}
	if c.calls != 0 || c.quantity != 0 {
		t.Fatalf("unsupported operation reached adapter: calls=%d quantity=%d", c.calls, c.quantity)
	}
}

func TestT5_2_UnknownCheckoutOutcomeIsNotConfirmation(t *testing.T) {
	c := &testClient{caps: map[Capability]bool{CapabilityCheckout: true}, checkout: Outcome[CheckoutSession]{State: SubscriptionUnknown}}
	b := validBinding()
	got, err := (Gate{Client: c}).CreateCheckout(context.Background(), CheckoutRequest{Binding: b, Customer: ObjectRef{Binding: b, ID: "cus"}, PriceKey: "monthly-v1", SuccessURL: "https://app.invalid/success", CancelURL: "https://app.invalid/cancel", IdempotencyKey: "checkout-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("unknown is a valid nonterminal state: %v", err)
	}
	if got.State != SubscriptionUnknown {
		t.Fatalf("timeout outcome changed state: %q", got.State)
	}
}

func TestT5_2_RejectsCrossWorkspaceProviderObject(t *testing.T) {
	c := &testClient{caps: map[Capability]bool{CapabilityPortal: true}}
	b := validBinding()
	other := b
	other.WorkspaceID = "other-workspace"
	_, err := (Gate{Client: c}).CreatePortal(context.Background(), PortalRequest{Binding: b, Customer: ObjectRef{Binding: other, ID: "cus"}, ReturnURL: "https://app.invalid/account"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected scoped binding rejection, got %v", err)
	}
	if c.calls != 0 {
		t.Fatalf("cross-workspace operation reached adapter: calls=%d", c.calls)
	}
}

func TestT5_2_ValidatesRetryWindow(t *testing.T) {
	if err := (RetryPolicy{MaxAttempts: 2, IdempotencyDuration: 24 * time.Hour}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (RetryPolicy{MaxAttempts: 2}).Validate(); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid retry policy, got %v", err)
	}
}
