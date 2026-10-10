package billingcheckout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fixtureIntent = "018f0000-0000-7000-8000-000000000001"

type fixtureService struct {
	starts  int
	start   CheckoutResult
	payment PaymentView
	pageErr error
}

func (s *fixtureService) Page(context.Context, *http.Request) (PageView, error) {
	if s.pageErr != nil {
		return PageView{}, s.pageErr
	}
	return PageView{
		Payer:   "Example workspace",
		Current: PaymentView{State: StatePending},
		Choices: []Choice{
			{PriceKey: "basic-month", Plan: "Basic", Interval: "month", Currency: "USD", AmountMinor: 1200},
			{PriceKey: "basic-year", Plan: "Basic", Interval: "year", Currency: "USD", AmountMinor: 12000},
		},
	}, nil
}

func (s *fixtureService) StartCheckout(_ context.Context, _ *http.Request, priceKey, key string) (CheckoutResult, error) {
	s.starts++
	if priceKey == "" || key == "" {
		return CheckoutResult{}, http.ErrMissingFile
	}
	return s.start, nil
}

func (s *fixtureService) PaymentStatus(context.Context, *http.Request, string) (PaymentView, error) {
	return s.payment, nil
}

type fixtureCheck struct{}

func (fixtureCheck) Token(*http.Request) (string, error) { return "csrf-fixture", nil }
func (fixtureCheck) Valid(r *http.Request) bool          { return r.PostForm.Get("_csrf") == "csrf-fixture" }

func newFixtureHandler(t *testing.T, service *fixtureService) *Handler {
	t.Helper()
	handler, err := New(Config{Service: service, RequestCheck: fixtureCheck{}, CheckoutHosts: []string{"checkout.example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestBillingPageShowsCatalogChoicesAndWorkspacePayer(t *testing.T) {
	service := &fixtureService{payment: PaymentView{State: StatePending}}
	recorder := httptest.NewRecorder()
	newFixtureHandler(t, service).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/billing", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{"Active workspace payer: <strong>Example workspace</strong>", "basic-month", "basic-year", "USD 12.00 / month", "USD 120.00 / year"} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("billing page missing %q", want)
		}
	}
	if !strings.Contains(recorder.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("billing page is cacheable")
	}
}

func TestReturnQueryCannotForgePaidProjection(t *testing.T) {
	service := &fixtureService{payment: PaymentView{State: StatePending}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/billing/return?success=1&state=paid", nil)
	request.AddCookie(&http.Cookie{Name: intentCookie, Value: fixtureIntent})
	newFixtureHandler(t, service).ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "Payment is still pending confirmation") {
		t.Fatalf("return did not remain pending: status %d body %s", recorder.Code, body)
	}
	if strings.Contains(body, `href="/private/paid-action"`) {
		t.Fatal("return query unlocked the paid action")
	}
	if strings.Contains(recorder.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatal("unconfirmed intent cookie was cleared")
	}
}

func TestStartRejectsPriceOutsideCurrentCatalog(t *testing.T) {
	service := &fixtureService{payment: PaymentView{State: StatePending}}
	handler := newFixtureHandler(t, service)
	request := httptest.NewRequest(http.MethodPost, "/billing/checkout", strings.NewReader("_csrf=csrf-fixture&price_key=forged& idempotency_key=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.starts != 0 {
		t.Fatalf("status %d, starts %d; expected invalid selection before action", recorder.Code, service.starts)
	}
}

func TestStartRedirectStoresScopedIntentAndRequiresAllowedHTTPSHost(t *testing.T) {
	service := &fixtureService{
		start:   CheckoutResult{State: "redirect", IntentID: fixtureIntent, RedirectURL: "https://checkout.example.test/session"},
		payment: PaymentView{State: StatePending},
	}
	handler := newFixtureHandler(t, service)
	body := "_csrf=csrf-fixture&price_key=basic-month&idempotency_key=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	request := httptest.NewRequest(http.MethodPost, "/billing/checkout", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != service.start.RedirectURL {
		t.Fatalf("status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	cookie := recorder.Result().Cookies()[0]
	if cookie.Name != intentCookie || cookie.Value != fixtureIntent || cookie.Path != "/billing/return" || !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("unsafe intent cookie: %#v", cookie)
	}

	service.start.RedirectURL = "https://evil.example.test/session"
	request = httptest.NewRequest(http.MethodPost, "/billing/checkout", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Header().Get("Location"), "evil.example.test") {
		t.Fatalf("unapproved host result: status %d location %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestUnknownCheckoutOutcomeCanBeManuallyReconciled(t *testing.T) {
	service := &fixtureService{start: CheckoutResult{State: "unknown", IntentID: fixtureIntent}, payment: PaymentView{State: StateUnknown}}
	handler := newFixtureHandler(t, service)
	body := "_csrf=csrf-fixture&price_key=basic-month&idempotency_key=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	request := httptest.NewRequest(http.MethodPost, "/billing/checkout", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || !strings.Contains(recorder.Body.String(), "outcome is unknown") ||
		!strings.Contains(recorder.Body.String(), "Check payment status again") {
		t.Fatalf("unknown checkout state was not surfaced with retry: status %d body %s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != fixtureIntent {
		t.Fatalf("unknown checkout intent was not retained: %#v", cookies)
	}
	statusRequest := httptest.NewRequest(http.MethodGet, "/billing/return/status?state=paid", nil)
	statusRequest.AddCookie(cookies[0])
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK || !strings.Contains(statusRecorder.Body.String(), `"state":"unknown"`) ||
		strings.Contains(statusRecorder.Body.String(), "action_path") {
		t.Fatalf("unknown outcome was converted to paid: status %d body %s", statusRecorder.Code, statusRecorder.Body.String())
	}
}

func TestManualRetryRendersPaidProjectionAndAction(t *testing.T) {
	service := &fixtureService{payment: PaymentView{State: StatePaid, Payer: "Example workspace", Plan: "Basic", Currency: "USD", AmountMinor: 1200, ActionPath: "/workspace/paid"}}
	handler := newFixtureHandler(t, service)
	request := httptest.NewRequest(http.MethodPost, "/billing/return/retry", strings.NewReader("_csrf=csrf-fixture"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: intentCookie, Value: fixtureIntent})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "Payment is confirmed for the active workspace") ||
		!strings.Contains(body, `href="/workspace/paid"`) || !strings.Contains(body, "Basic &middot; USD 12.00") {
		t.Fatalf("manual retry did not render confirmed projection: status %d body %s", recorder.Code, body)
	}
}

func TestBillingPageShowsDependencyUnavailable(t *testing.T) {
	service := &fixtureService{payment: PaymentView{State: StatePending}}
	handler := newFixtureHandler(t, service)
	service.pageErr = http.ErrHandlerTimeout
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/billing", nil))
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "Billing is temporarily unavailable") {
		t.Fatalf("dependency error was not visible: status %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestUnpaidProjectionCannotReturnActionPath(t *testing.T) {
	service := &fixtureService{payment: PaymentView{State: StatePending, ActionPath: "/private/paid-action"}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/billing/return", nil)
	request.AddCookie(&http.Cookie{Name: intentCookie, Value: fixtureIntent})
	newFixtureHandler(t, service).ServeHTTP(recorder, request)
	if strings.Contains(recorder.Body.String(), `href="/private/paid-action"`) {
		t.Fatal("unpaid projection exposed an action")
	}
}
