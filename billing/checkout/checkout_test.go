package checkout

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/catalog"
	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	"github.com/ajent-social/amos/identity"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

var (
	checkoutInstall = uuid.MustParse("018f0000-0000-7000-8000-000000000001")
	checkoutApp     = uuid.MustParse("018f0000-0000-7000-8000-000000000002")
	checkoutEnv     = uuid.MustParse("018f0000-0000-7000-8000-000000000003")
	checkoutWork    = uuid.MustParse("018f0000-0000-7000-8000-000000000004")
	checkoutOther   = uuid.MustParse("018f0000-0000-7000-8000-000000000005")
	checkoutPerson  = uuid.MustParse("018f0000-0000-7000-8000-000000000006")
	checkoutIntent  = uuid.MustParse("018f0000-0000-7000-8000-000000000007")
)

type memoryRepository struct {
	prepared   Prepared
	prepareErr error
	prepareN   int
	last       CheckoutRequest
	find       billingstore.Intent
	findErr    error
}

func (m *memoryRepository) Prepare(_ context.Context, _, _, _ uuid.UUID, req CheckoutRequest) (Prepared, error) {
	m.prepareN++
	m.last = req
	if m.prepareErr != nil {
		return Prepared{}, m.prepareErr
	}
	out := m.prepared
	out.Request = req
	return out, nil
}
func (m *memoryRepository) Resolve(_ context.Context, _ CheckoutRequest, intent billingstore.Intent, _ provider.Outcome[provider.CheckoutSession]) (billingstore.Intent, error) {
	return intent, nil
}

func (m *memoryRepository) Find(_ context.Context, binding provider.Binding, _ uuid.UUID) (billingstore.Intent, error) {
	if m.find.Binding != binding {
		return billingstore.Intent{}, billingstore.ErrIntentUnavailable
	}
	return m.find, m.findErr
}

type memoryProvider struct{ calls int }

func (m *memoryProvider) CreateCheckout(_ context.Context, _ provider.CheckoutRequest) (provider.Outcome[provider.CheckoutSession], error) {
	m.calls++
	return provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionPending, ObservedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}, nil
}
func (m *memoryProvider) RecoverCheckout(context.Context, provider.CheckoutRequest, billingstore.Intent) (provider.Outcome[provider.CheckoutSession], error) {
	return provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionUnknown, ObservedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}, nil
}

func checkoutCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.New(catalog.Config{Revision: "catalog-7", Plans: []catalog.Plan{{
		Key: "pro-v1", Revision: "2026-01", ProductFamily: "application", Model: catalog.ModelFlat,
		Scope:    catalog.Scope{InstallationID: checkoutInstall.String(), ApplicationID: checkoutApp.String(), EnvironmentID: checkoutEnv.String(), Provider: "stripe", ProviderAccountID: "acct_test", AccountMode: provider.AccountTest},
		Currency: "USD", MinorUnit: 2, EffectiveAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Features: []string{"projects"}, Prices: []catalog.Price{
			{Key: "pro-month", ProviderPriceKey: "price_month", AmountMinor: 1200, Interval: catalog.IntervalMonth},
			{Key: "pro-year", ProviderPriceKey: "price_year", AmountMinor: 12000, Interval: catalog.IntervalYear},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func checkoutAuthority(workspaceID uuid.UUID, aal identity.AssuranceLevel) Authority {
	return Authority{ActorKind: "person", PersonID: checkoutPerson, InstallationID: checkoutInstall, ApplicationID: checkoutApp, EnvironmentID: checkoutEnv,
		AssuranceLevel: aal, AssuranceExpiresAt: time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC),
		Workspace: workspacestore.Workspace{ID: workspaceID, Kind: workspacestore.KindPersonal, State: workspacestore.WorkspaceActive, PersonalOwnerID: checkoutPerson,
			Scope: workspacestore.Scope{InstallationID: checkoutInstall, ApplicationID: checkoutApp}},
	}
}

func checkoutHandler(t *testing.T, repo *memoryRepository, providerClient *memoryProvider, authority Authority) http.Handler {
	t.Helper()
	h, err := New(Config{Repository: repo, Provider: providerClient, Catalog: checkoutCatalog(t), ProviderAccountID: "acct_test", AccountMode: provider.AccountTest,
		SuccessURL: "https://app.example.test/billing/return", CancelURL: "https://app.example.test/billing/cancel", Now: func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) },
		ResolveAuthority: func(context.Context) (Authority, bool) { return authority, true }})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func requestStart(handler http.Handler, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/billing/checkout", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "attempt-0001")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestT5_6_OwnerCheckoutUsesServerPriceAndDurablePrepareSeam(t *testing.T) {
	repo := &memoryRepository{prepared: Prepared{Intent: billingstore.Intent{ID: checkoutIntent, State: "pending", CreatedAt: time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)}}}
	client := &memoryProvider{}
	h := checkoutHandler(t, repo, client, checkoutAuthority(checkoutWork, identity.AAL2))
	w := requestStart(h, `{"price_key":"pro-month"}`)
	if w.Code != http.StatusAccepted || client.calls != 0 || repo.prepareN != 1 || repo.last.PriceKey != "pro-month" || repo.last.Binding.WorkspaceID != checkoutWork.String() {
		t.Fatalf("checkout did not stop at durable pending intent: status=%d provider_calls=%d prepare_calls=%d body=%s", w.Code, client.calls, repo.prepareN, w.Body.String())
	}
	var got response
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.State != "pending" {
		t.Fatalf("unexpected response: %+v, err=%v", got, err)
	}
}

func TestT5_6_ProviderWriteRequiresClaimedDurableIntent(t *testing.T) {
	repo := &memoryRepository{prepared: Prepared{Intent: billingstore.Intent{ID: checkoutIntent, State: "claimed", ClaimToken: uuid.MustParse("018f0000-0000-7000-8000-000000000008"), Version: 2, CreatedAt: time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)}, Claimed: true}}
	client := &memoryProvider{}
	w := requestStart(checkoutHandler(t, repo, client, checkoutAuthority(checkoutWork, identity.AAL2)), `{"price_key":"pro-month"}`)
	if w.Code != http.StatusAccepted || client.calls != 1 || repo.prepareN != 1 {
		t.Fatalf("durable claim did not precede provider operation: status=%d provider_calls=%d prepare_calls=%d body=%s", w.Code, client.calls, repo.prepareN, w.Body.String())
	}
}

func TestT5_6_RejectsForgedTermsAndMissingStepUpBeforeStoreOrProvider(t *testing.T) {
	for _, tc := range []struct {
		name string
		aal  identity.AssuranceLevel
		body string
	}{
		{name: "missing assurance", aal: identity.AAL1, body: `{"price_key":"pro-month"}`},
		{name: "expired assurance", aal: identity.AAL2, body: `{"price_key":"pro-month"}`},
		{name: "client amount", aal: identity.AAL2, body: `{"price_key":"pro-month","amount_minor":1}`},
		{name: "client quantity", aal: identity.AAL2, body: `{"price_key":"pro-month","quantity":99}`},
		{name: "forged provider price", aal: identity.AAL2, body: `{"price_key":"price_month"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, client := &memoryRepository{}, &memoryProvider{}
			a := checkoutAuthority(checkoutWork, tc.aal)
			if tc.name == "expired assurance" {
				a.AssuranceExpiresAt = time.Date(2026, 10, 1, 11, 59, 59, 0, time.UTC)
			}
			h := checkoutHandler(t, repo, client, a)
			w := requestStart(h, tc.body)
			if w.Code < 400 || repo.prepareN != 0 || client.calls != 0 {
				t.Fatalf("request crossed trust boundary: status=%d prepare=%d provider=%d body=%s", w.Code, repo.prepareN, client.calls, w.Body.String())
			}
			if tc.name == "missing assurance" || tc.name == "expired assurance" {
				if !strings.Contains(w.Body.String(), "billing.step_up_required") {
					t.Fatalf("step-up denial lacks actionable stable code: %s", w.Body.String())
				}
			}
		})
	}
}

func TestT5_6_OrganizationMemberCannotStartCheckout(t *testing.T) {
	a := checkoutAuthority(checkoutWork, identity.AAL2)
	a.Workspace.Kind = workspacestore.KindOrganization
	a.Workspace.PersonalOwnerID = uuid.Nil
	a.Membership = &workspacestore.Membership{WorkspaceID: checkoutWork, PersonID: checkoutPerson, State: workspacestore.MembershipActive, Role: workspacestore.RoleMember}
	a.Permissions = []string{"billing.manage"}
	repo, client := &memoryRepository{}, &memoryProvider{}
	w := requestStart(checkoutHandler(t, repo, client, a), `{"price_key":"pro-month"}`)
	if w.Code != http.StatusForbidden || repo.prepareN != 0 || client.calls != 0 {
		t.Fatalf("organization member crossed owner boundary: status=%d prepare=%d provider=%d", w.Code, repo.prepareN, client.calls)
	}
}

func TestT5_6_ConfirmedCheckoutResultMustBeBoundAndHTTPS(t *testing.T) {
	req := CheckoutRequest{Binding: testBinding(checkoutWork)}
	ref := provider.ObjectRef{Binding: req.Binding, ID: "cs_test_123"}
	for _, tc := range []struct {
		name string
		url  string
		ref  provider.ObjectRef
	}{
		{name: "http url", url: "http://checkout.example.test/session", ref: ref},
		{name: "foreign binding", url: "https://checkout.example.test/session", ref: provider.ObjectRef{Binding: testBinding(checkoutOther), ID: "cs_test_123"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionConfirmed,
				Value: provider.CheckoutSession{Ref: tc.ref, URL: tc.url}, ProviderObject: tc.ref,
				ObservedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
			if validateOutcome(req, out) == nil {
				t.Fatal("malformed or cross-workspace provider result accepted")
			}
		})
	}
}

func TestT5_6_RejectsForeignPersonalWorkspaceAndOversizedBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    Authority
		body string
	}{
		{name: "foreign workspace", a: func() Authority {
			a := checkoutAuthority(checkoutOther, identity.AAL2)
			a.Workspace.PersonalOwnerID = checkoutOther
			return a
		}(), body: `{"price_key":"pro-month"}`},
		{name: "oversized body", a: checkoutAuthority(checkoutWork, identity.AAL2), body: `{"price_key":"pro-month","extra":"` + strings.Repeat("x", maxRequestBytes) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, client := &memoryRepository{}, &memoryProvider{}
			w := requestStart(checkoutHandler(t, repo, client, tc.a), tc.body)
			if w.Code < 400 || repo.prepareN != 0 || client.calls != 0 {
				t.Fatalf("request crossed trust boundary: status=%d prepare=%d provider=%d", w.Code, repo.prepareN, client.calls)
			}
		})
	}
}

func TestT5_6_StatusDoesNotTrustRedirectOrPaidQuery(t *testing.T) {
	repo := &memoryRepository{find: billingstore.Intent{ID: checkoutIntent, State: "unknown", Binding: testBinding(checkoutWork), CreatedAt: time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)}}
	h := checkoutHandler(t, repo, &memoryProvider{}, checkoutAuthority(checkoutWork, identity.AAL1))
	r := httptest.NewRequest(http.MethodGet, "/billing/checkout/"+checkoutIntent.String(), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status read failed: %d %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got["state"] != "unknown" || got["redirect_url"] != nil || got["entitled"] != nil {
		t.Fatalf("status leaked redirect or entitlement: %v err=%v", got, err)
	}
	r = httptest.NewRequest(http.MethodGet, "/billing/checkout/"+checkoutIntent.String()+"?paid=true", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code < 400 {
		t.Fatalf("query override accepted: status=%d body=%s", w.Code, w.Body.String())
	}
	repo.find = billingstore.Intent{ID: checkoutIntent, State: "confirmed", Binding: testBinding(checkoutOther), CreatedAt: time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)}
	r = httptest.NewRequest(http.MethodGet, "/billing/checkout/"+checkoutIntent.String(), nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("foreign workspace intent was readable: status=%d body=%s", w.Code, w.Body.String())
	}
}

func testBinding(workspaceID uuid.UUID) provider.Binding {
	return provider.Binding{InstallationID: checkoutInstall.String(), EnvironmentID: checkoutEnv.String(), WorkspaceID: workspaceID.String(), Provider: "stripe", AccountID: "acct_test", AccountMode: provider.AccountTest}
}
