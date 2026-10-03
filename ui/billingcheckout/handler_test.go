package billingcheckout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ajent-social/amos/identity"
	"github.com/google/uuid"
)

type testService struct {
	state       PageState
	actionCalls int
	startResult StartResult
	priceKey    string
	idempotency string
}

func (s *testService) Page(context.Context, identity.Principal) (PageState, error) {
	return s.state, nil
}
func (s *testService) Start(_ context.Context, _ identity.Principal, priceKey, idempotency string) (StartResult, error) {
	s.priceKey, s.idempotency = priceKey, idempotency
	return s.startResult, nil
}
func (s *testService) Status(context.Context, identity.Principal, uuid.UUID) (PageState, error) {
	return s.state, nil
}
func (s *testService) BusinessAction(context.Context, identity.Principal) error {
	if !s.state.Paid || !s.state.CanManage || !s.state.CanAct {
		return ErrDenied
	}
	s.actionCalls++
	return nil
}

type testCheck struct{}

func (testCheck) Valid(*http.Request) bool            { return true }
func (testCheck) Token(*http.Request) (string, error) { return "synthetic-token", nil }

func TestT6_5_RequiresAuthoritativeDependencies(t *testing.T) {
	if _, err := New(nil, testCheck{}); err == nil {
		t.Fatal("nil service accepted")
	}
	if _, err := New(&testService{}, nil); err == nil {
		t.Fatal("missing origin and CSRF check accepted")
	}
}

func TestT6_5_UnauthenticatedRequestCannotReachBillingService(t *testing.T) {
	svc := &testService{}
	h, err := New(svc, testCheck{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/billing?success=1", "/billing/return/018f47a0-7b3c-7cc1-9a20-123456789abc?success=1"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s status=%d, want 401", path, w.Code)
		}
		if got := w.Header().Get("Cache-Control"); got != "private, no-store, max-age=0" {
			t.Errorf("cache-control=%q", got)
		}
	}
	if svc.actionCalls != 0 {
		t.Fatalf("business action called without authentication: %d", svc.actionCalls)
	}
}

func TestT6_5_ForgeSuccessQueryRemainsPendingUntilServiceProjectionChanges(t *testing.T) {
	id := uuid.MustParse("018f47a0-7b3c-7cc1-9a20-123456789abc")
	offers := []Offer{{PriceKey: "standard-month", Plan: "Standard", Amount: 1200, Currency: "USD", Interval: "month"}}
	svc := &testService{state: PageState{Workspace: "Personal workspace", Payer: "Workspace owner", Offers: offers, CanManage: true, State: "pending", RequestID: "synthetic-idempotency"}}
	h, err := New(svc, testCheck{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/billing/return/"+id.String()+"?success=1", nil)
	h.renderPage(w, r, identity.Principal{}, id)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "Checking payment status") || strings.Contains(body, "Payment confirmed") || !strings.Contains(body, `id="paid-action" disabled`) {
		t.Fatalf("forged success response status=%d body=%q", w.Code, body)
	}
	if svc.actionCalls != 0 {
		t.Fatalf("forged success unlocked action: calls=%d", svc.actionCalls)
	}
	post := httptest.NewRequest(http.MethodPost, "/billing/action", strings.NewReader("_csrf=synthetic-token"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.action(w, post, identity.Principal{})
	if w.Code != http.StatusForbidden || svc.actionCalls != 0 {
		t.Fatalf("unconfirmed business action status=%d serviceCalls=%d", w.Code, svc.actionCalls)
	}

	// The service test double now models a fresh confirmed projection. This
	// verifies presentation and action flow only; it is not provider evidence.
	svc.state = PageState{Workspace: "Personal workspace", Payer: "Workspace owner", Offers: offers, Plan: "Standard", Currency: "USD", Paid: true, CanManage: true, CanAct: true, State: "confirmed", RequestID: "synthetic-idempotency"}
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/billing/return/"+id.String()+"?success=0", nil)
	h.renderPage(w, r, identity.Principal{}, id)
	if !strings.Contains(w.Body.String(), "Payment confirmed") || !strings.Contains(w.Body.String(), `id="paid-action"`) || strings.Contains(w.Body.String(), `id="paid-action" disabled`) {
		t.Fatalf("confirmed service state did not unlock action: %q", w.Body.String())
	}
	post = httptest.NewRequest(http.MethodPost, "/billing/action", strings.NewReader("_csrf=synthetic-token"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.action(w, post, identity.Principal{})
	if w.Code != http.StatusSeeOther || svc.actionCalls != 1 {
		t.Fatalf("confirmed authorized business action status=%d serviceCalls=%d", w.Code, svc.actionCalls)
	}
}

func TestT6_5_ReturnSelectorAndHostedRedirectAreBounded(t *testing.T) {
	valid := "018f47a0-7b3c-7cc1-9a20-123456789abc"
	if parseIntent(valid) == uuid.Nil {
		t.Fatal("canonical UUIDv7 return selector rejected")
	}
	for _, bad := range []string{"not-an-id", "00000000-0000-4000-8000-000000000001", valid + "/extra"} {
		if parseIntent(bad) != uuid.Nil {
			t.Errorf("invalid selector %q accepted", bad)
		}
	}
	for _, test := range []struct {
		url  string
		want bool
	}{
		{"https://checkout.stripe.com/c/session", true},
		{"https://checkout.stripe.com.evil.example/c/session", false},
		{"https://attacker.example/c/session", false},
		{"javascript:alert(1)", false},
		{"https://user@checkout.stripe.com/c/session", false},
	} {
		_, err := safeRedirectHost(test.url)
		if (err == nil) != test.want {
			t.Errorf("safeRedirectHost(%q) err=%v, want accepted=%v", test.url, err, test.want)
		}
	}
}

func TestT6_5_PlanSelectionUsesDomainServiceAndRestrictsRedirect(t *testing.T) {
	id := uuid.MustParse("018f47a0-7b3c-7cc1-9a20-123456789abc")
	for _, tc := range []struct {
		name         string
		redirect     string
		wantStatus   int
		wantLocation string
	}{
		{name: "configured checkout", redirect: "https://checkout.stripe.com/c/session", wantStatus: http.StatusSeeOther, wantLocation: "https://checkout.stripe.com/c/session"},
		{name: "foreign host rejected", redirect: "https://checkout.stripe.com.attacker.example/c/session", wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &testService{startResult: StartResult{IntentID: id, RedirectURL: tc.redirect}}
			h, err := New(svc, testCheck{})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, "/billing/checkout", strings.NewReader("price_key=standard-month&_csrf=synthetic-token&idempotency_key=checkout-1"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
			w := httptest.NewRecorder()
			h.start(w, r, identity.Principal{})
			if w.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantLocation != "" && w.Header().Get("Location") != tc.wantLocation {
				t.Fatalf("Location=%q", w.Header().Get("Location"))
			}
			if svc.priceKey != "standard-month" || svc.idempotency != "checkout-1" {
				t.Fatalf("domain Start got price=%q idempotency=%q", svc.priceKey, svc.idempotency)
			}
		})
	}
}
