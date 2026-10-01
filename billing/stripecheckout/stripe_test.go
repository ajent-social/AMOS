package stripecheckout

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/catalog"
	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
)

var (
	stripeInstall   = "018f0000-0000-7000-8000-000000000001"
	stripeApp       = "018f0000-0000-7000-8000-000000000002"
	stripeEnv       = "018f0000-0000-7000-8000-000000000003"
	stripeWorkspace = "018f0000-0000-7000-8000-000000000004"
)

func fixtureBinding() provider.Binding {
	return provider.Binding{InstallationID: stripeInstall, EnvironmentID: stripeEnv, WorkspaceID: stripeWorkspace, Provider: "stripe", AccountID: "acct_test", AccountMode: provider.AccountTest}
}
func fixtureCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	b := fixtureBinding()
	c, err := catalog.New(catalog.Config{Revision: "catalog-1", Plans: []catalog.Plan{{Key: "flat-v1", Revision: "2026-01", ProductFamily: "app", Model: catalog.ModelFlat,
		Scope:    catalog.Scope{InstallationID: b.InstallationID, ApplicationID: stripeApp, EnvironmentID: b.EnvironmentID, Provider: b.Provider, ProviderAccountID: b.AccountID, AccountMode: b.AccountMode},
		Currency: "USD", MinorUnit: 2, EffectiveAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Features: []string{"projects"}, Prices: []catalog.Price{{Key: "month", ProviderPriceKey: "price_month", AmountMinor: 1200, Interval: catalog.IntervalMonth}, {Key: "year", ProviderPriceKey: "price_year", AmountMinor: 12000, Interval: catalog.IntervalYear}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func fixtureAdapter(t *testing.T, server *httptest.Server) *Adapter {
	t.Helper()
	config := strings.Join([]string{"sk", "test", "fixture", "key"}, "_")
	a, err := New(Config{APIKey: config, ProviderAccountID: "acct_test", AccountMode: provider.AccountTest, Catalog: fixtureCatalog(t), ReturnHosts: []string{"app.example"}, APIBaseURL: server.URL, Now: func() time.Time { return time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func fixtureRequest() provider.CheckoutRequest {
	b := fixtureBinding()
	return provider.CheckoutRequest{Binding: b, Customer: provider.ObjectRef{Binding: b, ID: "cus_fixture"}, PriceKey: "month", SuccessURL: "https://app.example/billing/success?checkout={CHECKOUT_SESSION_ID}", CancelURL: "https://app.example/billing/cancel", IdempotencyKey: "intent_checkout_01"}
}
func customerJSON(livemode bool) map[string]any {
	b := fixtureBinding()
	return map[string]any{"id": "cus_fixture", "object": "customer", "livemode": livemode, "metadata": bindingMetadata(b)}
}
func sessionJSON(req provider.CheckoutRequest, livemode bool) map[string]any {
	metadata := bindingMetadata(req.Binding)
	metadata["amos_price_key"] = req.PriceKey
	metadata["amos_catalog_revision"] = "catalog-1"
	return map[string]any{"id": "cs_fixture123", "object": "checkout.session", "livemode": livemode, "client_reference_id": req.IdempotencyKey, "mode": "subscription", "url": "https://checkout.stripe.com/c/pay/cs_fixture123", "expires_at": time.Date(2026, 2, 1, 1, 0, 0, 0, time.UTC).Unix(), "customer": map[string]any{"id": req.Customer.ID, "object": "customer", "livemode": livemode}, "metadata": metadata}
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func TestT5_5_UsesOfficialSDKWithBoundMetadataAndFixedQuantity(t *testing.T) {
	var customerCalls, checkoutCalls atomic.Int64
	req := fixtureRequest()
	server := fixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Stripe-Version") != StripeAPIVersion {
			t.Errorf("wrong Stripe API version: %q", r.Header.Get("Stripe-Version"))
		}
		if r.Header.Get("Authorization") != "Bearer sk_test_fixture_key" {
			t.Errorf("SDK auth header missing")
		}
		switch r.URL.Path {
		case "/v1/customers":
			customerCalls.Add(1)
			if r.Method != "POST" || r.Header.Get("Idempotency-Key") != "customer_setup_001" {
				t.Errorf("customer request not idempotent")
			}
			_ = r.ParseForm()
			if r.Form.Get("metadata[amos_workspace_id]") != stripeWorkspace || r.Form.Get("metadata[amos_account_mode]") != "test" {
				t.Errorf("customer binding metadata missing")
			}
			writeJSON(w, customerJSON(false))
		case "/v1/checkout/sessions":
			checkoutCalls.Add(1)
			if r.Method != "POST" || r.Header.Get("Idempotency-Key") != req.IdempotencyKey {
				t.Errorf("checkout request not idempotent")
			}
			_ = r.ParseForm()
			if r.Form.Get("line_items[0][price]") != "price_month" || r.Form.Get("line_items[0][quantity]") != "1" || r.Form.Get("mode") != "subscription" {
				t.Errorf("price, quantity, or mode mismatch: %v", r.Form)
			}
			if r.Form.Get("client_reference_id") != req.IdempotencyKey || r.Form.Get("metadata[amos_workspace_id]") != stripeWorkspace {
				t.Errorf("checkout binding metadata missing")
			}
			writeJSON(w, sessionJSON(req, false))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	adapter := fixtureAdapter(t, server)
	customer, err := adapter.EnsureCustomer(context.Background(), provider.CustomerRequest{Binding: req.Binding, IdempotencyKey: "customer_setup_001"})
	if err != nil || customer.Ref.ID != "cus_fixture" {
		t.Fatalf("EnsureCustomer: %+v %v", customer, err)
	}
	out, err := adapter.CreateCheckout(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if err := out.Validate(); err != nil {
		t.Fatalf("outcome invalid: %v", err)
	}
	if out.State != provider.SubscriptionConfirmed || out.Value.Ref.ID != "cs_fixture123" || out.Value.URL != "https://checkout.stripe.com/c/pay/cs_fixture123" {
		t.Fatalf("checkout not confirmed: %+v", out)
	}
	if customerCalls.Load() != 1 || checkoutCalls.Load() != 1 {
		t.Fatalf("SDK call counts customer=%d checkout=%d", customerCalls.Load(), checkoutCalls.Load())
	}
}

func TestT5_5_LostCheckoutResponseRecoversSameSessionFromDurableIntent(t *testing.T) {
	req := fixtureRequest()
	created := map[string]map[string]any{}
	var createdMu sync.Mutex
	var checkoutCalls atomic.Int64
	server := fixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/checkout/sessions" {
			http.NotFound(w, r)
			return
		}
		checkoutCalls.Add(1)
		_ = r.ParseForm()
		key := r.Header.Get("Idempotency-Key")
		createdMu.Lock()
		session, exists := created[key]
		if !exists {
			session = sessionJSON(req, false)
			created[key] = session
		}
		createdMu.Unlock()
		call := checkoutCalls.Load()
		if call == 1 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("fixture response cannot be dropped")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		writeJSON(w, session)
	}))
	defer server.Close()
	adapter := fixtureAdapter(t, server)
	first, err := adapter.CreateCheckout(context.Background(), req)
	if err != nil || first.State != provider.SubscriptionUnknown {
		t.Fatalf("lost response must stay unknown, got %+v %v", first, err)
	}
	if checkoutCalls.Load() != 1 {
		t.Fatalf("SDK automatically retried unknown write %d times", checkoutCalls.Load())
	}
	intent := billingstore.Intent{CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC).Add(-time.Hour), Binding: req.Binding, Operation: CheckoutIntentOperation, IdempotencyKey: req.IdempotencyKey, PayloadSHA256: CheckoutPayloadHash(req), State: "unknown", Version: 2}
	recovered, err := adapter.RecoverCheckout(context.Background(), req, intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Validate(); err != nil {
		t.Fatalf("recovered outcome invalid: %v", err)
	}
	if recovered.State != provider.SubscriptionConfirmed || recovered.Value.Ref.ID != "cs_fixture123" || recovered.ProviderObject.ID != "cs_fixture123" {
		t.Fatalf("did not recover original checkout: %+v", recovered)
	}
	if checkoutCalls.Load() != 2 {
		t.Fatalf("expected one approved idempotent recovery call, got %d total", checkoutCalls.Load())
	}
}

func TestT5_5_DurableRecoveryRejectsChangedPriceAndWrongBinding(t *testing.T) {
	req := fixtureRequest()
	var calls atomic.Int64
	server := fixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); writeJSON(w, sessionJSON(req, false)) }))
	defer server.Close()
	adapter := fixtureAdapter(t, server)
	intent := billingstore.Intent{CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC).Add(-time.Hour), Binding: req.Binding, Operation: CheckoutIntentOperation, IdempotencyKey: req.IdempotencyKey, PayloadSHA256: CheckoutPayloadHash(req), State: "unknown", Version: 2}
	changed := req
	changed.PriceKey = "year"
	if _, err := adapter.RecoverCheckout(context.Background(), changed, intent); !errors.Is(err, provider.ErrInvalidRequest) {
		t.Fatalf("changed-price replay not blocked: %v", err)
	}
	foreign := req
	foreign.Binding.AccountMode = provider.AccountLive
	if _, err := adapter.RecoverCheckout(context.Background(), foreign, intent); !errors.Is(err, provider.ErrInvalidRequest) {
		t.Fatalf("wrong account mode recovery accepted: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid recoveries reached provider fixture: %d", calls.Load())
	}
}

func TestT5_5_RejectsWrongModeResponseAndUntrustedURLs(t *testing.T) {
	req := fixtureRequest()
	server := fixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, customerJSON(true)) }))
	defer server.Close()
	adapter := fixtureAdapter(t, server)
	_, err := adapter.EnsureCustomer(context.Background(), provider.CustomerRequest{Binding: req.Binding, IdempotencyKey: "customer_setup_002"})
	var pe *provider.ProviderError
	if !errors.As(err, &pe) || pe.Class != provider.ErrorUnknownOutcome {
		t.Fatalf("wrong mode response was not unknown: %v", err)
	}
	badURLs := []string{"http://app.example/success", "https://attacker.example/success", "https://user@app.example/success", "https://app.example:443/success"}
	for _, raw := range badURLs {
		if validReturnURL(raw, map[string]struct{}{"app.example": {}}) {
			t.Errorf("accepted unsafe return URL %q", raw)
		}
	}
	if validHostedURL("https://evil.example/c/pay/cs_fixture123") {
		t.Fatal("accepted non-Stripe redirect host")
	}
}

func TestT5_5_BindsCustomerModeAndConfiguration(t *testing.T) {
	config := strings.Join([]string{"sk", "test", "fixture", "key"}, "_")
	cfg := Config{APIKey: config, ProviderAccountID: "acct_test", AccountMode: provider.AccountTest, Catalog: fixtureCatalog(t), ReturnHosts: []string{"app.example"}}
	if _, err := New(cfg); err != nil {
		t.Fatalf("valid fixture config rejected: %v", err)
	}
	cfg.AccountMode = provider.AccountLive
	if _, err := New(cfg); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("test key accepted for live mode: %v", err)
	}
	if !validIdempotencyKey("intent_checkout_01") || validIdempotencyKey("short") || validIdempotencyKey(strings.Repeat("x", 129)) {
		t.Fatal("idempotency key bound validation failed")
	}
}

func TestT5_5_CustomerTimeoutRequiresMatchingDurableIntentToRecover(t *testing.T) {
	b := fixtureBinding()
	req := provider.CustomerRequest{Binding: b, IdempotencyKey: "customer_recovery_01"}
	var calls atomic.Int64
	server := fixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Idempotency-Key") != req.IdempotencyKey {
			t.Errorf("customer idempotency key changed")
		}
		if calls.Load() == 1 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("fixture response cannot be dropped")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		writeJSON(w, customerJSON(false))
	}))
	defer server.Close()
	adapter := fixtureAdapter(t, server)
	if _, err := adapter.EnsureCustomer(context.Background(), req); err == nil {
		t.Fatal("lost customer response reported as success")
	} else {
		var pe *provider.ProviderError
		if !errors.As(err, &pe) || pe.Class != provider.ErrorUnknownOutcome {
			t.Fatalf("customer timeout was not unknown: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("customer write auto-retried %d times", calls.Load())
	}
	intent := billingstore.Intent{CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC).Add(-time.Hour), Binding: b, Operation: CustomerIntentOperation, IdempotencyKey: req.IdempotencyKey, PayloadSHA256: CustomerPayloadHash(req), State: "unknown", Version: 2}
	customer, err := adapter.RecoverCustomer(context.Background(), req, intent)
	if err != nil || customer.Ref.ID != "cus_fixture" {
		t.Fatalf("durable customer recovery: %+v %v", customer, err)
	}
	changed := req
	changed.Binding.WorkspaceID = "018f0000-0000-7000-8000-000000000005"
	if _, err := adapter.RecoverCustomer(context.Background(), changed, intent); !errors.Is(err, provider.ErrInvalidRequest) {
		t.Fatalf("cross-workspace recovery accepted: %v", err)
	}
}

func fixtureServer(next http.Handler) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			writeJSON(w, map[string]any{"id": "acct_test", "object": "account"})
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func TestT5_5_VerifiesCredentialAccountBeforeAnyMutation(t *testing.T) {
	for _, remote := range []string{"acct_other", ""} {
		t.Run(remote, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					writeJSON(w, customerJSON(false))
					return
				}
				writeJSON(w, map[string]any{"id": remote, "object": "account"})
			}))
			defer server.Close()
			a := fixtureAdapter(t, server)
			if _, e := a.EnsureCustomer(context.Background(), provider.CustomerRequest{Binding: fixtureBinding(), IdempotencyKey: "intent_customer_01"}); e == nil {
				t.Fatal("wrong merchant accepted")
			}
			if _, e := a.CreateCheckout(context.Background(), fixtureRequest()); e == nil {
				t.Fatal("wrong merchant checkout accepted")
			}
			if posts != 0 {
				t.Fatalf("unverified merchant mutated %d times", posts)
			}
		})
	}
}

func TestT5_5_UnknownRecoveryStopsBeforeRetentionExpiry(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, sessionJSON(fixtureRequest(), false))
	}))
	defer server.Close()
	a := fixtureAdapter(t, server)
	req := fixtureRequest()
	for _, created := range []time.Time{{}, a.now().Add(time.Second), a.now().Add(-23 * time.Hour), a.now().Add(-48 * time.Hour)} {
		intent := billingstore.Intent{CreatedAt: created, Binding: req.Binding, Operation: CheckoutIntentOperation, IdempotencyKey: req.IdempotencyKey, PayloadSHA256: CheckoutPayloadHash(req), State: "unknown", Version: 2}
		out, e := a.RecoverCheckout(context.Background(), req, intent)
		if e == nil || out.State != provider.SubscriptionUnknown {
			t.Fatal("expired unknown checkout replayed")
		}
		customer := provider.CustomerRequest{Binding: req.Binding, IdempotencyKey: "intent_customer_01"}
		intent.Operation = CustomerIntentOperation
		intent.IdempotencyKey = customer.IdempotencyKey
		intent.PayloadSHA256 = CustomerPayloadHash(customer)
		if _, e := a.RecoverCustomer(context.Background(), customer, intent); e == nil {
			t.Fatal("expired unknown customer replayed")
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("unsafe aged recovery reached provider %d times", calls.Load())
	}
}

func TestT5_5_CheckoutRequiresMatchingPresentCustomer(t *testing.T) {
	for _, customer := range []any{nil, map[string]any{"id": "cus_other", "object": "customer"}} {
		server := fixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v := sessionJSON(fixtureRequest(), false)
			v["customer"] = customer
			writeJSON(w, v)
		}))
		a := fixtureAdapter(t, server)
		out, e := a.CreateCheckout(context.Background(), fixtureRequest())
		server.Close()
		if e != nil || out.State != provider.SubscriptionUnknown {
			t.Fatalf("unbound customer checkout confirmed: %v", e)
		}
	}
}
