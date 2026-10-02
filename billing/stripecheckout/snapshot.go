package stripecheckout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ajent-social/amos/billing/catalog"
	"github.com/ajent-social/amos/billing/provider"
	"github.com/ajent-social/amos/billing/reconcile"
	stripe "github.com/stripe/stripe-go/v87"
)

const (
	snapshotPageSize = 100
	snapshotMaxItems = 500
	snapshotReads    = 2
)

var errInvalidStripeSnapshot = errors.New("stripe response did not match the supported subscription contract")

// GetCustomerSnapshot returns a stable, complete, customer-scoped subscription
// collection. Two bounded reads reduce the chance that a collection changing
// during pagination is mistaken for one coherent snapshot; Stripe list pages
// are not an atomic read, so agreement is evidence of stability, not a provider
// guarantee of transactional snapshot isolation.
func (a *Adapter) GetCustomerSnapshot(ctx context.Context, request reconcile.SnapshotRequest) (reconcile.CustomerSnapshot, error) {
	if ctx == nil || a == nil || a.client == nil || !a.validBinding(request.Binding) || !validStripeID(request.CustomerRef) || a.catalog == nil {
		return reconcile.CustomerSnapshot{}, provider.ErrInvalidRequest
	}
	if err := a.CheckAccount(ctx); err != nil {
		return reconcile.CustomerSnapshot{}, err
	}
	if err := a.verifySnapshotCustomer(ctx, request); err != nil {
		return reconcile.CustomerSnapshot{}, err
	}

	var previous []reconcile.Subscription
	var previousRevision string
	for read := 0; read < snapshotReads; read++ {
		items, err := a.listCustomerSubscriptions(ctx, request)
		if err != nil {
			return reconcile.CustomerSnapshot{}, err
		}
		revision, err := snapshotRevision(items)
		if err != nil {
			return reconcile.CustomerSnapshot{}, err
		}
		if read > 0 && (revision != previousRevision || !sameNormalizedSubscriptions(items, previous)) {
			return reconcile.CustomerSnapshot{}, &provider.ProviderError{Class: provider.ErrorUnavailable, Retryable: true}
		}
		previous, previousRevision = items, revision
	}

	observedAt := a.now().UTC()
	if observedAt.IsZero() {
		return reconcile.CustomerSnapshot{}, &provider.ProviderError{Class: provider.ErrorUnavailable, Retryable: false}
	}
	return reconcile.CustomerSnapshot{Complete: true, Binding: request.Binding, CustomerRef: request.CustomerRef, Revision: previousRevision, ObservedAt: observedAt, Subscriptions: previous}, nil
}

func (a *Adapter) verifySnapshotCustomer(ctx context.Context, request reconcile.SnapshotRequest) error {
	params := &stripe.CustomerRetrieveParams{}
	if a.connectAccount != "" {
		params.SetStripeAccount(a.connectAccount)
	}
	customer, err := a.client.V1Customers.Retrieve(ctx, request.CustomerRef, params)
	if err != nil {
		return snapshotReadError(ctx, err)
	}
	if customer == nil || customer.Deleted || customer.ID != request.CustomerRef || customer.Livemode != (a.mode == provider.AccountLive) || !metadataMatches(customer.Metadata, bindingMetadata(request.Binding)) {
		return errors.Join(ErrWrongAccount, &provider.ProviderError{Class: provider.ErrorUnavailable, Retryable: false})
	}
	return nil
}

func (a *Adapter) listCustomerSubscriptions(ctx context.Context, request reconcile.SnapshotRequest) ([]reconcile.Subscription, error) {
	params := &stripe.SubscriptionListParams{Customer: stripe.String(request.CustomerRef), Status: stripe.String("all")}
	params.Limit = stripe.Int64(snapshotPageSize)
	if a.connectAccount != "" {
		params.SetStripeAccount(a.connectAccount)
	}
	list := a.client.V1Subscriptions.List(ctx, params)
	items := make([]reconcile.Subscription, 0, snapshotPageSize)
	seen := make(map[string]struct{})
	for remote, err := range list.All(ctx) {
		if err != nil {
			return nil, snapshotReadError(ctx, err)
		}
		if remote == nil || len(items) == snapshotMaxItems {
			return nil, errInvalidStripeSnapshot
		}
		mapped, err := a.mapSubscription(request, remote)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[mapped.Reference]; duplicate {
			return nil, errInvalidStripeSnapshot
		}
		seen[mapped.Reference] = struct{}{}
		items = append(items, mapped)
		if len(items) == snapshotMaxItems && list.Meta().HasMore {
			return nil, errInvalidStripeSnapshot
		}
	}
	if err := list.Err(); err != nil {
		return nil, snapshotReadError(ctx, err)
	}
	return items, nil
}

func snapshotReadError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	var stripeErr *stripe.Error
	if errors.As(err, &stripeErr) {
		switch {
		case stripeErr.HTTPStatusCode == http.StatusTooManyRequests || stripeErr.HTTPStatusCode >= http.StatusInternalServerError:
			return &provider.ProviderError{Class: provider.ErrorUnavailable, Retryable: true}
		case stripeErr.HTTPStatusCode >= http.StatusBadRequest:
			return &provider.ProviderError{Class: provider.ErrorInvalid, Retryable: false}
		}
	}
	return &provider.ProviderError{Class: provider.ErrorUnavailable, Retryable: true}
}

func (a *Adapter) mapSubscription(request reconcile.SnapshotRequest, remote *stripe.Subscription) (reconcile.Subscription, error) {
	if remote == nil || !validStripeID(remote.ID) || remote.Customer == nil || remote.Customer.ID != request.CustomerRef || remote.Livemode != (a.mode == provider.AccountLive) || !metadataMatches(remote.Metadata, bindingMetadata(request.Binding)) || remote.Items == nil || remote.Items.HasMore || len(remote.Items.Data) != 1 {
		return reconcile.Subscription{}, errInvalidStripeSnapshot
	}
	status, ok := mapLifecycleStatus(remote.Status)
	if !ok {
		return reconcile.Subscription{}, errInvalidStripeSnapshot
	}
	item := remote.Items.Data[0]
	if item == nil || item.Price == nil || !validStripeID(item.Price.ID) || item.Price.Livemode != (a.mode == provider.AccountLive) || item.Price.Deleted || item.Price.Type != stripe.PriceTypeRecurring || item.Price.BillingScheme != stripe.PriceBillingSchemePerUnit || item.Price.CustomUnitAmount != nil || item.Price.TransformQuantity != nil || len(item.Price.CurrencyOptions) != 0 || item.Price.Recurring == nil || item.Price.Recurring.UsageType != stripe.PriceRecurringUsageTypeLicensed || item.Price.Recurring.IntervalCount != 1 || item.Quantity != 1 {
		return reconcile.Subscription{}, errInvalidStripeSnapshot
	}
	plan, knownPrice, err := a.catalog.ResolveProviderPrice(item.Price.ID)
	if err != nil || plan.Model != catalog.ModelFlat || plan.Scope.InstallationID != request.Binding.InstallationID || plan.Scope.EnvironmentID != request.Binding.EnvironmentID || plan.Scope.Provider != request.Binding.Provider || plan.Scope.ProviderAccountID != request.Binding.AccountID || plan.Scope.AccountMode != request.Binding.AccountMode || knownPrice.ProviderPriceKey != item.Price.ID || !strings.EqualFold(plan.Currency, string(item.Price.Currency)) || knownPrice.AmountMinor != item.Price.UnitAmount || !intervalMatches(knownPrice.Interval, item.Price.Recurring.Interval) {
		return reconcile.Subscription{}, errInvalidStripeSnapshot
	}
	periodStart, periodEnd, validPeriod := stripePeriod(item.CurrentPeriodStart, item.CurrentPeriodEnd)
	if !validPeriod || (status == reconcile.StatusActive && (periodStart.IsZero() || !periodEnd.After(periodStart))) {
		return reconcile.Subscription{}, errInvalidStripeSnapshot
	}
	return reconcile.Subscription{Reference: remote.ID, Status: status, PriceKey: item.Price.ID, Quantity: item.Quantity, PeriodStart: periodStart, PeriodEnd: periodEnd}, nil
}

func mapLifecycleStatus(status stripe.SubscriptionStatus) (reconcile.LifecycleStatus, bool) {
	switch status {
	case stripe.SubscriptionStatusActive:
		return reconcile.StatusActive, true
	case stripe.SubscriptionStatusTrialing:
		return reconcile.StatusTrialing, true
	case stripe.SubscriptionStatusPastDue:
		return reconcile.StatusPastDue, true
	case stripe.SubscriptionStatusPaused:
		return reconcile.StatusSuspended, true
	case stripe.SubscriptionStatusCanceled:
		return reconcile.StatusCanceled, true
	case stripe.SubscriptionStatusIncomplete:
		return reconcile.StatusIncomplete, true
	case stripe.SubscriptionStatusUnpaid:
		return reconcile.StatusUnpaid, true
	case stripe.SubscriptionStatusIncompleteExpired:
		return reconcile.StatusExpired, true
	default:
		return "", false
	}
}

func intervalMatches(expected catalog.Interval, actual stripe.PriceRecurringInterval) bool {
	switch expected {
	case catalog.IntervalMonth:
		return actual == stripe.PriceRecurringIntervalMonth
	case catalog.IntervalYear:
		return actual == stripe.PriceRecurringIntervalYear
	default:
		return false
	}
}

func stripePeriod(start, end int64) (time.Time, time.Time, bool) {
	if start == 0 && end == 0 {
		return time.Time{}, time.Time{}, true
	}
	if start <= 0 || end <= start {
		return time.Time{}, time.Time{}, false
	}
	return time.Unix(start, 0).UTC(), time.Unix(end, 0).UTC(), true
}

func snapshotRevision(items []reconcile.Subscription) (string, error) {
	normalized := append([]reconcile.Subscription(nil), items...)
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Reference < normalized[j].Reference })
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", errInvalidStripeSnapshot
	}
	digest := sha256.Sum256(data)
	return "rev_" + hex.EncodeToString(digest[:]), nil
}

func sameNormalizedSubscriptions(left, right []reconcile.Subscription) bool {
	leftRevision, leftErr := snapshotRevision(left)
	rightRevision, rightErr := snapshotRevision(right)
	return leftErr == nil && rightErr == nil && leftRevision == rightRevision
}

var _ reconcile.SnapshotSource = (*Adapter)(nil)
