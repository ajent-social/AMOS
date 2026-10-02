package stripecheckout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	"github.com/ajent-social/amos/billing/reconcile"
	stripe "github.com/stripe/stripe-go/v87"
)

func TestT5_8_StripeSnapshotPaginatesAllStatusesAndBindsAccount(t *testing.T) {
	const total = 101
	items := make([]map[string]any, total)
	for i := range items {
		items[i] = stripeSubscriptionFixture(fmt.Sprintf("sub_%03d", i), "canceled", "price_month", 1)
	}
	var calls atomic.Int32
	server := stripeSnapshotServer(t, stripeCustomerFixture(), func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("customer") != "cus_fixture" || r.URL.Query().Get("status") != "all" || r.URL.Query().Get("limit") != "100" {
			t.Errorf("subscription list was not complete/customer scoped: query=%s", r.URL.RawQuery)
		}
		if r.Header.Get("Stripe-Account") != "acct_test" {
			t.Errorf("connected account header missing: %q", r.Header.Get("Stripe-Account"))
		}
		start := r.URL.Query().Get("starting_after")
		page := items[:100]
		hasMore := true
		if start != "" {
			if start != "sub_099" {
				t.Errorf("unexpected pagination cursor %q", start)
			}
			page = items[100:]
			hasMore = false
		}
		writeStripeList(w, page, hasMore)
	})
	defer server.Close()
	adapter := fixtureSnapshotAdapter(t, server, true)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.FixedZone("fixture", -7*60*60))
	adapter.now = func() time.Time { return now }
	request := reconcile.SnapshotRequest{Binding: fixtureBinding(), CustomerRef: "cus_fixture"}
	got, err := adapter.GetCustomerSnapshot(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got.Binding != request.Binding || got.CustomerRef != request.CustomerRef || len(got.Subscriptions) != total {
		t.Fatalf("snapshot scope/items mismatch: binding=%+v customer=%s count=%d", got.Binding, got.CustomerRef, len(got.Subscriptions))
	}
	if !got.Complete || got.ObservedAt.Location() != time.UTC || !got.ObservedAt.Equal(now) || !strings.HasPrefix(got.Revision, "rev_") || len(got.Revision) != 68 {
		t.Fatalf("snapshot observation/revision malformed: at=%s revision=%q", got.ObservedAt, got.Revision)
	}
	if got.Subscriptions[0].PriceKey != "price_month" || got.Subscriptions[0].Status != reconcile.StatusCanceled || calls.Load() != 4 {
		t.Fatalf("provider key/lifecycle/page count mismatch: first=%+v requests=%d", got.Subscriptions[0], calls.Load())
	}
}

func TestT5_8_StripeSnapshotMapsFiniteLifecycleStatuses(t *testing.T) {
	cases := []struct {
		stripeStatus stripe.SubscriptionStatus
		want         reconcile.LifecycleStatus
	}{
		{stripe.SubscriptionStatusActive, reconcile.StatusActive},
		{stripe.SubscriptionStatusTrialing, reconcile.StatusTrialing},
		{stripe.SubscriptionStatusPastDue, reconcile.StatusPastDue},
		{stripe.SubscriptionStatusPaused, reconcile.StatusSuspended},
		{stripe.SubscriptionStatusCanceled, reconcile.StatusCanceled},
		{stripe.SubscriptionStatusIncomplete, reconcile.StatusIncomplete},
		{stripe.SubscriptionStatusUnpaid, reconcile.StatusUnpaid},
		{stripe.SubscriptionStatusIncompleteExpired, reconcile.StatusExpired},
	}
	for _, test := range cases {
		t.Run(string(test.stripeStatus), func(t *testing.T) {
			server := stripeSnapshotServer(t, stripeCustomerFixture(), func(w http.ResponseWriter, _ *http.Request) {
				writeStripeList(w, []map[string]any{stripeSubscriptionFixture("sub_status", string(test.stripeStatus), "price_month", 1)}, false)
			})
			defer server.Close()
			got, err := fixtureSnapshotAdapter(t, server, false).GetCustomerSnapshot(context.Background(), reconcile.SnapshotRequest{Binding: fixtureBinding(), CustomerRef: "cus_fixture"})
			if err != nil || len(got.Subscriptions) != 1 || got.Subscriptions[0].Status != test.want {
				t.Fatalf("status mapping got=%+v err=%v want=%q", got.Subscriptions, err, test.want)
			}
		})
	}
}

func TestT5_8_StripeSnapshotRequiresVerifiedCustomerAndSupportedPriceShape(t *testing.T) {
	cases := []struct {
		name         string
		customer     map[string]any
		subscription map[string]any
	}{
		{name: "foreign customer metadata", customer: stripeCustomerFixtureWithMetadata(map[string]string{"amos_installation_id": "foreign"}), subscription: stripeSubscriptionFixture("sub_ok", "active", "price_month", 1)},
		{name: "wrong customer mode", customer: stripeCustomerFixtureMode(true), subscription: stripeSubscriptionFixture("sub_ok", "active", "price_month", 1)},
		{name: "deleted customer", customer: map[string]any{"id": "cus_fixture", "object": "customer", "deleted": true}, subscription: stripeSubscriptionFixture("sub_ok", "active", "price_month", 1)},
		{name: "foreign subscription customer", customer: stripeCustomerFixture(), subscription: stripeSubscriptionFixtureCustomer("sub_ok", "active", "price_month", 1, "cus_other")},
		{name: "wrong subscription mode", customer: stripeCustomerFixture(), subscription: stripeSubscriptionFixtureMode("sub_ok", true)},
		{name: "foreign subscription metadata", customer: stripeCustomerFixture(), subscription: stripeSubscriptionFixtureMetadata("sub_ok", "active", "price_month", 1, map[string]string{"amos_workspace_id": "foreign"})},
		{name: "unknown catalog price", customer: stripeCustomerFixture(), subscription: stripeSubscriptionFixture("sub_ok", "active", "price_unknown", 1)},
		{name: "catalog amount mismatch", customer: stripeCustomerFixture(), subscription: stripeSubscriptionFixtureAmount("sub_ok", 1199)},
		{name: "unsupported status", customer: stripeCustomerFixture(), subscription: stripeSubscriptionFixture("sub_ok", "future_status", "price_month", 1)},
		{name: "quantity is not one", customer: stripeCustomerFixture(), subscription: stripeSubscriptionFixture("sub_ok", "active", "price_month", 2)},
		{name: "multiple items", customer: stripeCustomerFixture(), subscription: stripeSubscriptionFixtureMultipleItems("sub_ok")},
		{name: "partial subscription items", customer: stripeCustomerFixture(), subscription: stripeSubscriptionFixturePartialItems("sub_ok")},
		{name: "interval count is unsupported", customer: stripeCustomerFixture(), subscription: stripeSubscriptionFixtureInterval("sub_ok", 2)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var listCalls atomic.Int32
			server := stripeSnapshotServer(t, test.customer, func(w http.ResponseWriter, _ *http.Request) {
				listCalls.Add(1)
				writeStripeList(w, []map[string]any{test.subscription}, false)
			})
			defer server.Close()
			_, err := fixtureSnapshotAdapter(t, server, false).GetCustomerSnapshot(context.Background(), reconcile.SnapshotRequest{Binding: fixtureBinding(), CustomerRef: "cus_fixture"})
			if err == nil {
				t.Fatal("unsupported or foreign Stripe state was accepted")
			}
			if test.name == "foreign customer metadata" || test.name == "wrong customer mode" || test.name == "deleted customer" {
				if listCalls.Load() != 0 {
					t.Fatalf("customer scope failure reached subscriptions endpoint %d times", listCalls.Load())
				}
			}
		})
	}
}

func TestT5_8_StripeSnapshotRejectsUnstableAndOverLimitCollections(t *testing.T) {
	t.Run("unstable second read", func(t *testing.T) {
		var calls atomic.Int32
		server := stripeSnapshotServer(t, stripeCustomerFixture(), func(w http.ResponseWriter, _ *http.Request) {
			status := "active"
			if calls.Add(1) > 1 {
				status = "canceled"
			}
			writeStripeList(w, []map[string]any{stripeSubscriptionFixture("sub_changed", status, "price_month", 1)}, false)
		})
		defer server.Close()
		_, err := fixtureSnapshotAdapter(t, server, false).GetCustomerSnapshot(context.Background(), reconcile.SnapshotRequest{Binding: fixtureBinding(), CustomerRef: "cus_fixture"})
		var providerErr *provider.ProviderError
		if !errors.As(err, &providerErr) || !providerErr.Retryable || strings.Contains(err.Error(), "cus_fixture") {
			t.Fatalf("unstable provider collection was not generically retryable: %v", err)
		}
	})

	t.Run("collection cap refuses partial result", func(t *testing.T) {
		const total = snapshotMaxItems + 1
		items := make([]map[string]any, total)
		for i := range items {
			items[i] = stripeSubscriptionFixture(fmt.Sprintf("sub_%03d", i), "canceled", "price_month", 1)
		}
		var listCalls atomic.Int32
		server := stripeSnapshotServer(t, stripeCustomerFixture(), func(w http.ResponseWriter, r *http.Request) {
			listCalls.Add(1)
			start := r.URL.Query().Get("starting_after")
			page := items[:100]
			if start != "" {
				after := 0
				if _, err := fmt.Sscanf(start, "sub_%03d", &after); err != nil {
					t.Errorf("invalid cursor %q", start)
				}
				page = items[after+1 : after+101]
			}
			writeStripeList(w, page, len(items)-(len(page)+indexOfSubscription(items, page[0])) > 0)
		})
		defer server.Close()
		_, err := fixtureSnapshotAdapter(t, server, false).GetCustomerSnapshot(context.Background(), reconcile.SnapshotRequest{Binding: fixtureBinding(), CustomerRef: "cus_fixture"})
		if err == nil || listCalls.Load() != snapshotMaxItems/snapshotPageSize {
			t.Fatalf("over-limit list returned partial data or fetched unbounded pages: err=%v pages=%d", err, listCalls.Load())
		}
	})
}

func TestT5_8_StripeSnapshotHidesProviderDiagnostics(t *testing.T) {
	const fakeCredential = "sk_test_secret_should_not_escape"
	var stdout, stderr bytes.Buffer
	priorStdout, priorStderr := os.Stdout, os.Stderr
	stdoutFile, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatal(err)
	}
	stderrFile, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = stdoutFile, stderrFile
	t.Cleanup(func() {
		os.Stdout, os.Stderr = priorStdout, priorStderr
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
	})
	var lists atomic.Int32
	server := stripeSnapshotServer(t, stripeCustomerFixture(), func(w http.ResponseWriter, r *http.Request) {
		lists.Add(1)
		if r.URL.Query().Get("starting_after") != "" {
			http.Error(w, `{"error":{"type":"api_error","message":"`+fakeCredential+`"}}`, http.StatusInternalServerError)
			return
		}
		items := make([]map[string]any, snapshotPageSize)
		for i := range items {
			items[i] = stripeSubscriptionFixture(fmt.Sprintf("sub_%03d", i), "canceled", "price_month", 1)
		}
		writeStripeList(w, items, true)
	})
	defer server.Close()
	_, err = fixtureSnapshotAdapter(t, server, false).GetCustomerSnapshot(context.Background(), reconcile.SnapshotRequest{Binding: fixtureBinding(), CustomerRef: "cus_fixture"})
	if _, seekErr := stdoutFile.Seek(0, 0); seekErr != nil {
		t.Fatal(seekErr)
	}
	if _, readErr := stdout.ReadFrom(stdoutFile); readErr != nil {
		t.Fatal(readErr)
	}
	if _, seekErr := stderrFile.Seek(0, 0); seekErr != nil {
		t.Fatal(seekErr)
	}
	if _, readErr := stderr.ReadFrom(stderrFile); readErr != nil {
		t.Fatal(readErr)
	}
	if err == nil || strings.Contains(err.Error(), fakeCredential) || lists.Load() != 2 || strings.Contains(stdout.String()+stderr.String(), fakeCredential) || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("Stripe diagnostics escaped or failed page was retried: err=%v listCalls=%d", err, lists.Load())
	}
}

func fixtureSnapshotAdapter(t *testing.T, server *httptest.Server, connected bool) *Adapter {
	t.Helper()
	config := strings.Join([]string{"sk", "test", "fixture", "key"}, "_")
	options := Config{APIKey: config, ProviderAccountID: "acct_test", AccountMode: provider.AccountTest, Catalog: fixtureCatalog(t), ReturnHosts: []string{"app.example"}, APIBaseURL: server.URL, Now: func() time.Time { return time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) }}
	if connected {
		options.ConnectAccountID = "acct_test"
	}
	adapter, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func stripeSnapshotServer(t *testing.T, customer map[string]any, list http.HandlerFunc) *httptest.Server {
	t.Helper()
	return fixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Stripe-Version") != StripeAPIVersion {
			t.Errorf("wrong Stripe API version: %q", r.Header.Get("Stripe-Version"))
		}
		if r.Header.Get("Authorization") != "Bearer sk_test_fixture_key" {
			t.Errorf("SDK authorization header missing")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/customers/cus_fixture":
			writeJSON(w, customer)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions":
			list(w, r)
		default:
			t.Errorf("unexpected Stripe request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
}

func stripeCustomerFixture() map[string]any {
	return stripeCustomerFixtureWithMetadata(bindingMetadata(fixtureBinding()))
}

func stripeCustomerFixtureMode(live bool) map[string]any {
	customer := stripeCustomerFixture()
	customer["livemode"] = live
	return customer
}

func stripeCustomerFixtureWithMetadata(metadata map[string]string) map[string]any {
	return map[string]any{"id": "cus_fixture", "object": "customer", "livemode": false, "metadata": metadata}
}

func stripeSubscriptionFixture(id, status, priceID string, quantity int64) map[string]any {
	return stripeSubscriptionFixtureCustomer(id, status, priceID, quantity, "cus_fixture")
}

func stripeSubscriptionFixtureCustomer(id, status, priceID string, quantity int64, customer string) map[string]any {
	return stripeSubscriptionFixtureFull(id, status, priceID, quantity, customer, bindingMetadata(fixtureBinding()), stripe.PriceRecurringIntervalMonth, 1, false, nil)
}

func stripeSubscriptionFixtureMode(id string, live bool) map[string]any {
	subscription := stripeSubscriptionFixture(id, "active", "price_month", 1)
	subscription["livemode"] = live
	return subscription
}

func stripeSubscriptionFixtureAmount(id string, amount int64) map[string]any {
	subscription := stripeSubscriptionFixture(id, "active", "price_month", 1)
	items := subscription["items"].(map[string]any)["data"].([]map[string]any)
	items[0]["price"].(map[string]any)["unit_amount"] = amount
	return subscription
}

func stripeSubscriptionFixtureMetadata(id, status, priceID string, quantity int64, metadata map[string]string) map[string]any {
	return stripeSubscriptionFixtureFull(id, status, priceID, quantity, "cus_fixture", metadata, stripe.PriceRecurringIntervalMonth, 1, false, nil)
}

func stripeSubscriptionFixtureInterval(id string, count int64) map[string]any {
	return stripeSubscriptionFixtureFull(id, "active", "price_month", 1, "cus_fixture", bindingMetadata(fixtureBinding()), stripe.PriceRecurringIntervalMonth, count, false, nil)
}

func stripeSubscriptionFixtureMultipleItems(id string) map[string]any {
	first := stripeSubscriptionItemFixture("price_month", 1, stripe.PriceRecurringIntervalMonth, 1)
	second := stripeSubscriptionItemFixture("price_year", 1, stripe.PriceRecurringIntervalYear, 1)
	return stripeSubscriptionFixtureFull(id, "active", "price_month", 1, "cus_fixture", bindingMetadata(fixtureBinding()), stripe.PriceRecurringIntervalMonth, 1, false, []map[string]any{first, second})
}

func stripeSubscriptionFixturePartialItems(id string) map[string]any {
	return stripeSubscriptionFixtureFull(id, "active", "price_month", 1, "cus_fixture", bindingMetadata(fixtureBinding()), stripe.PriceRecurringIntervalMonth, 1, true, nil)
}

func stripeSubscriptionFixtureFull(id, status, priceID string, quantity int64, customer string, metadata map[string]string, interval stripe.PriceRecurringInterval, intervalCount int64, itemsHasMore bool, extraItems []map[string]any) map[string]any {
	item := stripeSubscriptionItemFixture(priceID, quantity, interval, intervalCount)
	items := []map[string]any{item}
	items = append(items, extraItems...)
	return map[string]any{
		"id": id, "object": "subscription", "customer": customer, "livemode": false, "status": status,
		"metadata": metadata,
		"items":    map[string]any{"object": "list", "data": items, "has_more": itemsHasMore},
	}
}

func stripeSubscriptionItemFixture(priceID string, quantity int64, interval stripe.PriceRecurringInterval, intervalCount int64) map[string]any {
	return map[string]any{
		"id": "si_fixture", "object": "subscription_item", "quantity": quantity,
		"current_period_start": int64(1_780_000_000), "current_period_end": int64(1_782_592_000),
		"price": map[string]any{
			"id": priceID, "object": "price", "type": "recurring", "billing_scheme": "per_unit", "livemode": false,
			"currency": "usd", "unit_amount": fixtureUnitAmount(priceID),
			"recurring": map[string]any{"interval": string(interval), "interval_count": intervalCount, "usage_type": "licensed"},
		},
	}
}

func fixtureUnitAmount(priceID string) int64 {
	if priceID == "price_year" {
		return 12000
	}
	return 1200
}

func writeStripeList(w http.ResponseWriter, items []map[string]any, hasMore bool) {
	writeJSON(w, map[string]any{"object": "list", "data": items, "has_more": hasMore, "url": "/v1/subscriptions"})
}

func indexOfSubscription(items []map[string]any, target map[string]any) int {
	for i, item := range items {
		if item["id"] == target["id"] {
			return i
		}
	}
	return -1
}
