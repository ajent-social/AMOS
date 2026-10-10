package main

import (
	"context"
	"net/http"
	"os"
	"sync"

	"github.com/ajent-social/amos/ui/billingcheckout"
)

const intentID = "018f0000-0000-7000-8000-000000000001"

type fixtureService struct {
	mu   sync.RWMutex
	paid bool
}

func (s *fixtureService) Page(context.Context, *http.Request) (billingcheckout.PageView, error) {
	s.mu.RLock()
	paid := s.paid
	s.mu.RUnlock()
	current := billingcheckout.PaymentView{State: billingcheckout.StatePending}
	if paid {
		current = billingcheckout.PaymentView{State: billingcheckout.StatePaid, Payer: "Example workspace", Plan: "Basic", Currency: "USD", AmountMinor: 1200, ActionPath: "/workspace/paid"}
	}
	return billingcheckout.PageView{Payer: "Example workspace", Current: current, Choices: []billingcheckout.Choice{
		{PriceKey: "basic-month", Plan: "Basic", Interval: "month", Currency: "USD", AmountMinor: 1200},
		{PriceKey: "basic-year", Plan: "Basic", Interval: "year", Currency: "USD", AmountMinor: 12000},
	}}, nil
}

func (s *fixtureService) StartCheckout(context.Context, *http.Request, string, string) (billingcheckout.CheckoutResult, error) {
	return billingcheckout.CheckoutResult{State: "redirect", IntentID: intentID, RedirectURL: "https://checkout.example.test/session"}, nil
}

func (s *fixtureService) PaymentStatus(context.Context, *http.Request, string) (billingcheckout.PaymentView, error) {
	s.mu.RLock()
	paid := s.paid
	s.mu.RUnlock()
	if !paid {
		return billingcheckout.PaymentView{State: billingcheckout.StatePending}, nil
	}
	return billingcheckout.PaymentView{State: billingcheckout.StatePaid, Payer: "Example workspace", Plan: "Basic", Currency: "USD", AmountMinor: 1200, ActionPath: "/workspace/paid"}, nil
}

type fixtureCheck struct{}

func (fixtureCheck) Token(*http.Request) (string, error) { return "fixture-csrf", nil }
func (fixtureCheck) Valid(r *http.Request) bool          { return r.PostForm.Get("_csrf") == "fixture-csrf" }

func main() {
	service := &fixtureService{}
	checkout, err := billingcheckout.New(billingcheckout.Config{Service: service, RequestCheck: fixtureCheck{}, CheckoutHosts: []string{"checkout.example.test"}})
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/billing", checkout)
	mux.Handle("/billing/", checkout)
	mux.HandleFunc("/fixture/webhook", func(w http.ResponseWriter, r *http.Request) {
		service.mu.Lock()
		service.paid = true
		service.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/workspace/paid", func(w http.ResponseWriter, r *http.Request) {
		service.mu.RLock()
		paid := service.paid
		service.mu.RUnlock()
		if !paid {
			http.Error(w, "not confirmed", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("Paid action allowed"))
	})
	addr := os.Getenv("AMOS_BILLING_TEST_ADDR")
	if addr == "" {
		addr = "127.0.0.1:4178"
	}
	if err := http.ListenAndServe(addr, mux); err != nil {
		panic(err)
	}
}
