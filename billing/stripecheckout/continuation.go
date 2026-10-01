package stripecheckout

import (
	"context"
	"errors"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	stripe "github.com/stripe/stripe-go/v87"
)

var ErrCheckoutExpired = errors.New("confirmed checkout is no longer available")

// ReadConfirmedCheckout retrieves the same durable provider object without
// creating or replaying a provider mutation. It grants no subscription rights.
func (a *Adapter) ReadConfirmedCheckout(ctx context.Context, req provider.CheckoutRequest, intent billingstore.Intent) (provider.Outcome[provider.CheckoutSession], error) {
	if ctx == nil || a == nil || a.client == nil || !a.validBinding(req.Binding) || req.Customer.Validate() != nil || req.Customer.Binding != req.Binding || intent.Binding != req.Binding || intent.State != "confirmed" || intent.Operation != CheckoutIntentOperation || intent.Version < 1 || intent.IdempotencyKey != req.IdempotencyKey || intent.PayloadSHA256 != CheckoutPayloadHash(req) || !validStripeID(intent.ProviderObjectRef) || !validIdempotencyKey(req.IdempotencyKey) || !validReturnURL(req.SuccessURL, a.returnHosts) || !validReturnURL(req.CancelURL, a.returnHosts) {
		return provider.Outcome[provider.CheckoutSession]{}, provider.ErrInvalidRequest
	}
	if err := a.CheckAccount(ctx); err != nil {
		return unknownOutcome(a.now()), err
	}
	params := &stripe.CheckoutSessionRetrieveParams{}
	if a.connectAccount != "" {
		params.SetStripeAccount(a.connectAccount)
	}
	session, err := a.client.V1CheckoutSessions.Retrieve(ctx, intent.ProviderObjectRef, params)
	if err != nil {
		return unknownOutcome(a.now()), &provider.ProviderError{Class: provider.ErrorUnavailable, Retryable: true}
	}
	metadata := bindingMetadata(req.Binding)
	metadata["amos_price_key"] = req.PriceKey
	// Validate ownership before revealing even the expiry classification.
	if session == nil || session.ID != intent.ProviderObjectRef || session.Livemode != (a.mode == provider.AccountLive) || session.ClientReferenceID != req.IdempotencyKey || string(session.Mode) != "subscription" || !metadataMatches(session.Metadata, metadata) || session.Customer == nil || session.Customer.ID != req.Customer.ID {
		return unknownOutcome(a.now()), ErrWrongAccount
	}
	if session.ExpiresAt <= a.now().Unix() || string(session.Status) != "open" {
		return provider.Outcome[provider.CheckoutSession]{}, ErrCheckoutExpired
	}
	if !a.validCheckoutResponse(session, req, metadata) {
		return unknownOutcome(a.now()), ErrWrongAccount
	}
	ref := provider.ObjectRef{Binding: req.Binding, ID: session.ID}
	return provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionConfirmed, ProviderObject: ref, Value: provider.CheckoutSession{Ref: ref, URL: session.URL, ExpiresAt: time.Unix(session.ExpiresAt, 0).UTC()}, ObservedAt: a.now()}, nil
}
