package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testClient struct {
	calls       int
	quantity    int64
	checkoutErr error
	checkout    Outcome[CheckoutSession]
	result      Outcome[SubscriptionSnapshot]
}

func (c *testClient) EnsureCustomer(context.Context, CustomerRequest) (Customer, error) {
	c.calls++
	return Customer{}, nil
}
func (c *testClient) CreateCheckout(context.Context, CheckoutRequest) (Outcome[CheckoutSession], error) {
	c.calls++
	return c.checkout, c.checkoutErr
}
func (c *testClient) GetSubscription(context.Context, SubscriptionRequest) (Outcome[SubscriptionSnapshot], error) {
	c.calls++
	return c.result, nil
}
func (c *testClient) CreatePortal(context.Context, PortalRequest) (PortalSession, error) {
	c.calls++
	return PortalSession{}, nil
}
func (c *testClient) SetQuantity(_ context.Context, r QuantityRequest) (Outcome[SubscriptionSnapshot], error) {
	c.calls++
	c.quantity = r.Quantity
	return confirmedSnapshot(r.Binding, r.Quantity), nil
}
func (c *testClient) RecordMeterEvent(context.Context, MeterEvent) error { c.calls++; return nil }

func validBinding() Binding {
	return Binding{InstallationID: "018f47a2-9000-7000-8000-000000000001", EnvironmentID: "018f47a2-9000-7000-8000-000000000002", Provider: "stripe", AccountID: "acct_test_1", AccountMode: AccountLive, WorkspaceID: "018f47a2-9000-7000-8000-000000000003"}
}

func confirmedSnapshot(b Binding, quantity int64) Outcome[SubscriptionSnapshot] {
	ref := ObjectRef{Binding: b, ID: "sub_123"}
	now := time.Now().UTC()
	return Outcome[SubscriptionSnapshot]{State: SubscriptionConfirmed, Value: SubscriptionSnapshot{Ref: ref, State: SubscriptionConfirmed, PriceKey: "seat", Quantity: quantity, PeriodStart: now, PeriodEnd: now.Add(30 * 24 * time.Hour), ObservedAt: now}, ProviderObject: ref, ObservedAt: now}
}

func checkoutGate(c *testClient) Gate { return Gate{Checkout: c} }

func TestT5_2_UnsupportedQuantityDoesNotCallAdapter(t *testing.T) {
	g := Gate{}
	b := validBinding()
	_, err := g.SetQuantity(context.Background(), QuantityRequest{Binding: b, Subscription: ObjectRef{Binding: b, ID: "sub"}, PriceKey: "seat", Quantity: 4, IdempotencyKey: "request-1"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}

func TestT5_2_UnknownCheckoutOutcomeIsNotConfirmation(t *testing.T) {
	c := &testClient{checkout: Outcome[CheckoutSession]{State: SubscriptionUnknown, ObservedAt: time.Now().UTC()}}
	b := validBinding()
	got, err := checkoutGate(c).CreateCheckout(context.Background(), CheckoutRequest{Binding: b, Customer: ObjectRef{Binding: b, ID: "cus_1"}, PriceKey: "monthly-v1", SuccessURL: "https://app.invalid/success", CancelURL: "https://app.invalid/cancel", IdempotencyKey: "checkout-1"})
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
	c := &testClient{}
	b := validBinding()
	other := b
	other.WorkspaceID = "other-workspace"
	_, err := (Gate{Portal: c}).CreatePortal(context.Background(), PortalRequest{Binding: b, Customer: ObjectRef{Binding: other, ID: "cus_1"}, ReturnURL: "https://app.invalid/account"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected scoped binding rejection, got %v", err)
	}
	if c.calls != 0 {
		t.Fatalf("cross-workspace operation reached adapter: calls=%d", c.calls)
	}
}

func TestT5_2_RejectsConfirmedWrongProviderAccount(t *testing.T) {
	b := validBinding()
	bad := b
	bad.AccountID = "different_account"
	ref := ObjectRef{Binding: bad, ID: "sub_123"}
	now := time.Now().UTC()
	c := &testClient{result: Outcome[SubscriptionSnapshot]{State: SubscriptionConfirmed, Value: SubscriptionSnapshot{Ref: ref, State: SubscriptionConfirmed, ObservedAt: now}, ProviderObject: ref, ObservedAt: now}}
	_, err := (Gate{Subscription: c}).GetSubscription(context.Background(), SubscriptionRequest{Binding: b, Customer: ObjectRef{Binding: b, ID: "cus_1"}})
	if !errors.Is(err, ErrMalformedResult) {
		t.Fatalf("expected malformed result rejection, got %v", err)
	}
}

func TestT5_2_RejectsWrongProviderObjectNamespace(t *testing.T) {
	b := validBinding()
	wrong := b
	wrong.Provider = "other-provider"
	ref := ObjectRef{Binding: b, ID: "sub_123"}
	now := time.Now().UTC()
	c := &testClient{result: Outcome[SubscriptionSnapshot]{State: SubscriptionConfirmed, Value: SubscriptionSnapshot{Ref: ref, State: SubscriptionConfirmed, ObservedAt: now}, ProviderObject: ObjectRef{Binding: wrong, ID: ref.ID}, ObservedAt: now}}
	_, err := (Gate{Subscription: c}).GetSubscription(context.Background(), SubscriptionRequest{Binding: b, Customer: ObjectRef{Binding: b, ID: "cus_1"}})
	if !errors.Is(err, ErrMalformedResult) {
		t.Fatalf("expected wrong provider namespace rejection, got %v", err)
	}
}

func TestT5_2_RejectsInvalidFailureClassification(t *testing.T) {
	got := Outcome[CheckoutSession]{State: SubscriptionFailed, Failure: &ProviderError{Class: "raw-provider-error"}}
	if err := got.Validate(); !errors.Is(err, ErrMalformedResult) {
		t.Fatalf("expected invalid failure class rejection, got %v", err)
	}
}

func TestT5_2_RejectsConfirmedCheckoutWithoutValidObject(t *testing.T) {
	b := validBinding()
	now := time.Now().UTC()
	c := &testClient{checkout: Outcome[CheckoutSession]{State: SubscriptionConfirmed, Value: CheckoutSession{Ref: ObjectRef{Binding: b, ID: "bad id"}, URL: "https://pay.invalid/session", ExpiresAt: now.Add(time.Hour)}, ProviderObject: ObjectRef{Binding: b, ID: "session_1"}, ObservedAt: now}}
	_, err := checkoutGate(c).CreateCheckout(context.Background(), CheckoutRequest{Binding: b, Customer: ObjectRef{Binding: b, ID: "cus_1"}, PriceKey: "monthly-v1", SuccessURL: "https://app.invalid/success", CancelURL: "https://app.invalid/cancel", IdempotencyKey: "checkout-1"})
	if !errors.Is(err, ErrMalformedResult) {
		t.Fatalf("expected malformed adapter result, got %v", err)
	}
}

func TestT5_2_RejectsNonCanonicalTenantIDsAndOversizedProviderIDs(t *testing.T) {
	b := validBinding()
	b.InstallationID = "018F47A2-9000-7000-8000-000000000001"
	if !errors.Is(b.Validate(), ErrInvalidBinding) {
		t.Fatal("uppercase UUIDv7 accepted")
	}
	b = validBinding()
	if !errors.Is((ObjectRef{Binding: b, ID: string(make([]byte, 129))}).Validate(), ErrInvalidRequest) {
		t.Fatal("oversized provider object ID accepted")
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

func TestT5_2_TimeoutAfterDispatchRetainsUnknown(t *testing.T) {
	b := validBinding()
	for _, state := range []SubscriptionState{SubscriptionUnknown, SubscriptionConfirmed} {
		t.Run(string(state), func(t *testing.T) {
			c := &testClient{checkoutErr: context.DeadlineExceeded, checkout: Outcome[CheckoutSession]{State: state, Value: CheckoutSession{URL: "https://pay.invalid/claimed-success"}}}
			result, err := checkoutGate(c).CreateCheckout(context.Background(), CheckoutRequest{Binding: b, Customer: ObjectRef{Binding: b, ID: "cus_1"}, PriceKey: "monthly-v1", SuccessURL: "https://app.invalid/success", CancelURL: "https://app.invalid/cancel", IdempotencyKey: "timeout-1"})
			if !errors.Is(err, context.DeadlineExceeded) || result.State != SubscriptionUnknown || !isZero(result.Value) {
				t.Fatalf("timeout promoted or lost uncertainty: state=%s value=%+v err=%v", result.State, result.Value, err)
			}
		})
	}
}

func TestT5_2_QuantityResultBoundToRequestedSubscriptionAndPrice(t *testing.T) {
	b := validBinding()
	c := &testClient{}
	request := QuantityRequest{Binding: b, Subscription: ObjectRef{Binding: b, ID: "sub_123"}, PriceKey: "seat", Quantity: 3, IdempotencyKey: "seat-1"}
	if _, err := (Gate{Quantity: c}).SetQuantity(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*QuantityRequest){func(r *QuantityRequest) { r.Subscription.ID = "sub_other" }, func(r *QuantityRequest) { r.PriceKey = "other-plan" }} {
		changed := request
		mutate(&changed)
		if _, err := (Gate{Quantity: c}).SetQuantity(context.Background(), changed); !errors.Is(err, ErrMalformedResult) {
			t.Fatalf("wrong subscription/price accepted: %v", err)
		}
	}
}

func TestT5_2_RejectsMalformedSubscriptionSnapshot(t *testing.T) {
	b := validBinding()
	for _, mutate := range []func(*SubscriptionSnapshot){func(s *SubscriptionSnapshot) { s.State = "invented" }, func(s *SubscriptionSnapshot) { s.State = SubscriptionPending }, func(s *SubscriptionSnapshot) { s.State = SubscriptionUnknown }, func(s *SubscriptionSnapshot) { s.State = SubscriptionFailed }, func(s *SubscriptionSnapshot) { s.Quantity = -1 }, func(s *SubscriptionSnapshot) { s.PriceKey = "" }, func(s *SubscriptionSnapshot) { s.PeriodEnd = s.PeriodStart }} {
		result := confirmedSnapshot(b, 1)
		mutate(&result.Value)
		c := &testClient{result: result}
		if _, err := (Gate{Subscription: c}).GetSubscription(context.Background(), SubscriptionRequest{Binding: b, Customer: ObjectRef{Binding: b, ID: "cus_1"}}); !errors.Is(err, ErrMalformedResult) {
			t.Fatalf("malformed snapshot accepted: %v", err)
		}
	}
}
