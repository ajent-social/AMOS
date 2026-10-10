package apphost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type localBillingTestContextKey struct{}

func TestUnavailableBillingHandlerFailsClosed(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/billing", nil)
	unavailableBillingHandler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want=%d", recorder.Code, http.StatusServiceUnavailable)
	}
	if got := recorder.Body.String(); got != "Billing is unavailable.\n" {
		t.Fatalf("body=%q", got)
	}
}

func TestLocalBillingStartCheckoutPreservesTrustedContextAndServerSelection(t *testing.T) {
	ctx := context.WithValue(context.Background(), localBillingTestContextKey{}, "authorized")
	called := false
	service := &localBillingService{
		selectionKeys: map[string]struct{}{"team-monthly": {}},
		action: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			if r.Context().Value(localBillingTestContextKey{}) != "authorized" {
				t.Error("trusted request context was not preserved")
			}
			if r.Method != http.MethodPost || r.URL.Path != "/billing/checkout" {
				t.Errorf("method/path=%s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("Idempotency-Key") != "checkout-key-1" || r.Header.Get("X-Forwarded-For") != "" {
				t.Errorf("internal action headers were not constrained: %v", r.Header)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			var input struct {
				PriceKey string `json:"price_key"`
			}
			if err := json.Unmarshal(body, &input); err != nil {
				t.Fatal(err)
			}
			if input.PriceKey != "team-monthly" {
				t.Errorf("price_key=%q", input.PriceKey)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"state":"redirect","intent_id":"01900000-0000-7000-8000-000000000001","redirect_url":"https://checkout.example.test/session"}`)
		}),
	}
	request := httptest.NewRequest(http.MethodPost, "/billing/start-checkout", nil).WithContext(ctx)
	request.Header.Set("X-Forwarded-For", "untrusted")
	result, err := service.StartCheckout(ctx, request, "team-monthly", "checkout-key-1")
	if err != nil {
		t.Fatal(err)
	}
	if !called || result.State != "redirect" || result.IntentID == "" || result.RedirectURL != "https://checkout.example.test/session" {
		t.Fatalf("called=%v result=%+v", called, result)
	}
}

func TestLocalBillingStartCheckoutRejectsUnconfiguredSelection(t *testing.T) {
	called := false
	service := &localBillingService{
		selectionKeys: map[string]struct{}{"team-monthly": {}},
		action:        http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }),
	}
	_, err := service.StartCheckout(context.Background(), httptest.NewRequest(http.MethodPost, "/billing/start-checkout", nil), "caller-price", "key-1")
	if err == nil {
		t.Fatal("unconfigured selection was accepted")
	}
	if called {
		t.Fatal("billing action was called for an unconfigured selection")
	}
}
