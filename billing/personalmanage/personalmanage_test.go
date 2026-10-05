package personalmanage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	"github.com/ajent-social/amos/internal/codegen"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

type testRepository struct {
	binding  provider.Binding
	customer string
	err      error
	calls    int
}

func (r *testRepository) FindCustomer(context.Context, provider.Binding) (billingstore.CustomerBinding, error) {
	r.calls++
	if r.err != nil {
		return billingstore.CustomerBinding{}, r.err
	}
	return billingstore.CustomerBinding{Binding: r.binding, CustomerRef: r.customer, State: "active"}, nil
}

type testProvider struct {
	snapshot              provider.Outcome[provider.SubscriptionSnapshot]
	portal                provider.PortalSession
	getErr, portalErr     error
	getCalls, portalCalls int
	gotCustomer           string
}

func (p *testProvider) GetSubscription(_ context.Context, request provider.SubscriptionRequest) (provider.Outcome[provider.SubscriptionSnapshot], error) {
	p.getCalls++
	p.gotCustomer = request.Customer.ID
	return p.snapshot, p.getErr
}
func (p *testProvider) CreatePortal(_ context.Context, request provider.PortalRequest) (provider.PortalSession, error) {
	p.portalCalls++
	p.gotCustomer = request.Customer.ID
	return p.portal, p.portalErr
}

type testRequestCheck struct{ valid bool }

func (c testRequestCheck) Valid(*http.Request) bool { return c.valid }

type testRequestCheckFunc func(*http.Request) bool

func (c testRequestCheckFunc) Valid(r *http.Request) bool { return c(r) }

func TestPersonalInspectionAndPortalUseInternalCurrentBinding(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	a, _ := scopeIDs(t)
	scope := provider.Binding{InstallationID: a[0].String(), EnvironmentID: a[1].String(), WorkspaceID: a[2].String(), Provider: "stripe", AccountID: "acct_test", AccountMode: provider.AccountTest}
	repo := &testRepository{binding: scope, customer: "cus_private_01"}
	ref := provider.ObjectRef{Binding: scope, ID: "sub_private_01"}
	p := &testProvider{snapshot: provider.Outcome[provider.SubscriptionSnapshot]{State: provider.SubscriptionConfirmed, Value: provider.SubscriptionSnapshot{Ref: ref, State: provider.SubscriptionConfirmed, PriceKey: "flat_month", Quantity: 0, PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(30 * 24 * time.Hour), ObservedAt: now}, ProviderObject: ref, ObservedAt: now}, portal: provider.PortalSession{Ref: provider.ObjectRef{Binding: scope, ID: "bps_private_01"}, URL: "https://billing.example/portal/session", ExpiresAt: now.Add(time.Hour)}}
	owner := Authority{ActorKind: "person", AuthenticationMethod: "email_password", PersonID: a[3], InstallationID: a[0], ApplicationID: a[4], EnvironmentID: a[1], AuthenticatedAt: now.Add(-time.Minute), Workspace: workspacestore.Workspace{ID: a[2], Scope: workspacestore.Scope{InstallationID: a[0], ApplicationID: a[4]}, Kind: workspacestore.KindPersonal, State: workspacestore.WorkspaceActive, PersonalOwnerID: a[3]}}
	owner.Workspace.Scope.ApplicationID = owner.ApplicationID
	h, err := New(Config{Repository: repo, Provider: p, RequestCheck: testRequestCheck{valid: true}, ProviderAccountID: "acct_test", AccountMode: provider.AccountTest, ReturnURL: "https://app.example/billing", ReturnHosts: []string{"app.example"}, PortalHosts: []string{"billing.example"}, Now: func() time.Time { return now }, ResolveAuthority: func(context.Context) (Authority, bool) { return owner, true }})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/billing/personal", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"snapshot_state":"confirmed"`) || !strings.Contains(w.Body.String(), `"quantity":0`) || strings.Contains(w.Body.String(), "cus_private") || strings.Contains(w.Body.String(), "sub_private") {
		t.Fatalf("inspection status=%d body=%s", w.Code, w.Body.String())
	}
	assertPrivateResponseHeaders(t, w)
	for name, mutate := range map[string]func(*provider.Outcome[provider.SubscriptionSnapshot]){
		"outer confirmed inner failed": func(out *provider.Outcome[provider.SubscriptionSnapshot]) {
			out.Value.State = provider.SubscriptionFailed
		},
		"missing period start":  func(out *provider.Outcome[provider.SubscriptionSnapshot]) { out.Value.PeriodStart = time.Time{} },
		"missing period end":    func(out *provider.Outcome[provider.SubscriptionSnapshot]) { out.Value.PeriodEnd = time.Time{} },
		"missing observed time": func(out *provider.Outcome[provider.SubscriptionSnapshot]) { out.Value.ObservedAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			valid := p.snapshot
			mutate(&p.snapshot)
			defer func() { p.snapshot = valid }()
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/billing/personal", nil))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s, want unavailable", response.Code, response.Body.String())
			}
		})
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/billing/personal/cancel", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"state":"portal_ready"`) || strings.Contains(w.Body.String(), "canceled") {
		t.Fatalf("portal status=%d body=%s", w.Code, w.Body.String())
	}
	assertPrivateResponseHeaders(t, w)
	if p.getCalls != 5 || p.portalCalls != 1 || p.gotCustomer != "cus_private_01" {
		t.Fatalf("provider calls=%d/%d customer=%q", p.getCalls, p.portalCalls, p.gotCustomer)
	}
}

func TestPortalCSRFAndOriginCheckFailClosedBeforeBillingCalls(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	ids, _ := scopeIDs(t)
	scope := provider.Binding{InstallationID: ids[0].String(), EnvironmentID: ids[1].String(), WorkspaceID: ids[2].String(), Provider: "stripe", AccountID: "acct_test", AccountMode: provider.AccountTest}
	for _, tc := range []struct{ name, origin, token string }{{name: "missing token", origin: "https://app.example"}, {name: "invalid token", origin: "https://app.example", token: "wrong"}, {name: "wrong origin", origin: "https://foreign.example", token: "valid"}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &testRepository{binding: scope, customer: "cus_private_csrf"}
			p := &testProvider{}
			owner := Authority{ActorKind: "person", AuthenticationMethod: "email_password", PersonID: ids[3], InstallationID: ids[0], ApplicationID: ids[4], EnvironmentID: ids[1], AuthenticatedAt: now, Workspace: workspacestore.Workspace{ID: ids[2], Scope: workspacestore.Scope{InstallationID: ids[0], ApplicationID: ids[4]}, Kind: workspacestore.KindPersonal, State: workspacestore.WorkspaceActive, PersonalOwnerID: ids[3]}}
			checker := testRequestCheckFunc(func(r *http.Request) bool {
				return r.Header.Get("Origin") == "https://app.example" && r.Header.Get("X-CSRF-Token") == "valid"
			})
			h, err := New(Config{Repository: repo, Provider: p, RequestCheck: checker, ProviderAccountID: "acct_test", AccountMode: provider.AccountTest, ReturnURL: "https://app.example/billing", ReturnHosts: []string{"app.example"}, PortalHosts: []string{"billing.example"}, Now: func() time.Time { return now }, ResolveAuthority: func(context.Context) (Authority, bool) { return owner, true }})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, "/billing/personal/portal", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Request-ID", "client-chosen")
			if tc.token != "" {
				r.Header.Set("X-CSRF-Token", tc.token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden || repo.calls != 0 || p.portalCalls != 0 {
				t.Fatalf("status=%d repoCalls=%d portalCalls=%d", w.Code, repo.calls, p.portalCalls)
			}
			assertErrorContract(t, w)
			if w.Header().Get("X-Request-ID") == "client-chosen" {
				t.Fatal("trusted request ID copied from client")
			}
		})
	}
}

func TestPersonalBillingDeniesStaleOrForeignAuthority(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	ids, _ := scopeIDs(t)
	scope := provider.Binding{InstallationID: ids[0].String(), EnvironmentID: ids[1].String(), WorkspaceID: ids[2].String(), Provider: "stripe", AccountID: "acct_test", AccountMode: provider.AccountTest}
	foreign := scope
	foreign.WorkspaceID = ids[5].String()
	repo := &testRepository{binding: scope, customer: "cus_private_02"}
	p := &testProvider{}
	owner := Authority{ActorKind: "person", AuthenticationMethod: "email_password", PersonID: ids[3], InstallationID: ids[0], ApplicationID: ids[4], EnvironmentID: ids[1], AuthenticatedAt: now.Add(-ProofFreshness - time.Second), Workspace: workspacestore.Workspace{ID: ids[2], Scope: workspacestore.Scope{InstallationID: ids[0], ApplicationID: ids[4]}, Kind: workspacestore.KindPersonal, State: workspacestore.WorkspaceActive, PersonalOwnerID: ids[3]}}
	h := newTestHandler(t, now, repo, p, owner)
	for _, path := range []string{"/billing/personal", "/billing/personal/portal"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "portal") {
			method = http.MethodPost
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("stale proof %s status=%d", path, w.Code)
		}
	}
	owner.AuthenticatedAt = now
	repo.binding = foreign
	h = newTestHandler(t, now, repo, p, owner)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/billing/personal/portal", nil))
	if w.Code != http.StatusForbidden || p.portalCalls != 0 {
		t.Fatalf("foreign customer binding status=%d calls=%d", w.Code, p.portalCalls)
	}
}

func TestPersonalBillingProviderUnavailableDoesNotFabricateSuccess(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	ids, _ := scopeIDs(t)
	scope := provider.Binding{InstallationID: ids[0].String(), EnvironmentID: ids[1].String(), WorkspaceID: ids[2].String(), Provider: "stripe", AccountID: "acct_test", AccountMode: provider.AccountTest}
	repo := &testRepository{binding: scope, customer: "cus_private_03"}
	p := &testProvider{getErr: errors.New("offline"), portalErr: errors.New("offline")}
	owner := Authority{ActorKind: "person", AuthenticationMethod: "email_password", PersonID: ids[3], InstallationID: ids[0], ApplicationID: ids[4], EnvironmentID: ids[1], AuthenticatedAt: now, Workspace: workspacestore.Workspace{ID: ids[2], Scope: workspacestore.Scope{InstallationID: ids[0], ApplicationID: ids[4]}, Kind: workspacestore.KindPersonal, State: workspacestore.WorkspaceActive, PersonalOwnerID: ids[3]}}
	h := newTestHandler(t, now, repo, p, owner)
	for _, path := range []string{"/billing/personal", "/billing/personal/portal"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "portal") {
			method = http.MethodPost
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "portal_ready") {
			t.Fatalf("unavailable %s status=%d body=%s", path, w.Code, w.Body.String())
		}
		assertErrorContract(t, w)
	}
}

func TestPersonalCancelPortalNeverConfirmsCancellation(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	ids, _ := scopeIDs(t)
	scope := provider.Binding{InstallationID: ids[0].String(), EnvironmentID: ids[1].String(), WorkspaceID: ids[2].String(), Provider: "stripe", AccountID: "acct_test", AccountMode: provider.AccountTest}
	repo := &testRepository{binding: scope, customer: "cus_private_04"}
	p := &testProvider{portal: provider.PortalSession{Ref: provider.ObjectRef{Binding: scope, ID: "bps_private_02"}, URL: "https://billing.example/session", ExpiresAt: now.Add(time.Minute)}}
	owner := Authority{ActorKind: "person", AuthenticationMethod: "email_password", PersonID: ids[3], InstallationID: ids[0], ApplicationID: ids[4], EnvironmentID: ids[1], AuthenticatedAt: now, Workspace: workspacestore.Workspace{ID: ids[2], Scope: workspacestore.Scope{InstallationID: ids[0], ApplicationID: ids[4]}, Kind: workspacestore.KindPersonal, State: workspacestore.WorkspaceActive, PersonalOwnerID: ids[3]}}
	h := newTestHandler(t, now, repo, p, owner)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/billing/personal/cancel", nil))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "canceled") || strings.Contains(w.Body.String(), "cancelled") {
		t.Fatalf("cancellation assertion status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPersonalBillingFragmentPassesGeneratorContract(t *testing.T) {
	source, err := os.ReadFile("../../api/fragments/personal-billing.openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := codegen.Validate(source)
	if err != nil {
		t.Fatalf("generator contract rejected personal billing fragment: %v", err)
	}
	if len(manifest.Operations) != 3 {
		t.Fatalf("validated operations=%d, want 3", len(manifest.Operations))
	}
}

func newTestHandler(t *testing.T, now time.Time, repo CustomerRepository, p Provider, owner Authority) *Handler {
	t.Helper()
	h, e := New(Config{Repository: repo, Provider: p, RequestCheck: testRequestCheck{valid: true}, ProviderAccountID: "acct_test", AccountMode: provider.AccountTest, ReturnURL: "https://app.example/billing", ReturnHosts: []string{"app.example"}, PortalHosts: []string{"billing.example"}, Now: func() time.Time { return now }, ResolveAuthority: func(context.Context) (Authority, bool) { return owner, true }})
	if e != nil {
		t.Fatal(e)
	}
	return h
}

func assertPrivateResponseHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Header().Get("Cache-Control") != "private, no-store, max-age=0" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("privacy headers missing: cache=%q referrer=%q contentType=%q", w.Header().Get("Cache-Control"), w.Header().Get("Referrer-Policy"), w.Header().Get("X-Content-Type-Options"))
	}
}

func assertErrorContract(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var body struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Code == "" || body.Message != "request could not be completed" || body.RequestID == "" || body.RequestID != w.Header().Get("X-Request-ID") {
		t.Fatalf("error body/header mismatch: body=%+v header=%q", body, w.Header().Get("X-Request-ID"))
	}
}
func scopeIDs(t *testing.T) ([]uuid.UUID, []uuid.UUID) {
	t.Helper()
	out := make([]uuid.UUID, 6)
	for i := range out {
		v, e := uuid.NewV7()
		if e != nil {
			t.Fatal(e)
		}
		out[i] = v
	}
	return out, nil
}
