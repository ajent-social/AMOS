package stripecheckout

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
)

func TestConfirmedContinuationReadsOnlyBoundDurableObject(t *testing.T) {
	req := fixtureRequest()
	for _, tc := range []struct {
		name  string
		alter func(map[string]any)
		want  error
	}{
		{name: "exact"},
		{name: "foreign customer", alter: func(s map[string]any) { s["customer"] = map[string]any{"id": "cus_foreign"} }, want: ErrWrongAccount},
		{name: "wrong object", alter: func(s map[string]any) { s["id"] = "cs_other" }, want: ErrWrongAccount},
		{name: "evil host", alter: func(s map[string]any) { s["url"] = "https://evil.example/c/pay/cs_fixture123" }, want: ErrWrongAccount},
		{name: "expired", alter: func(s map[string]any) { s["expires_at"] = int64(1) }, want: ErrCheckoutExpired},
		{name: "closed", alter: func(s map[string]any) { s["status"] = "complete" }, want: ErrCheckoutExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" {
					t.Error("continuation mutated provider")
					w.WriteHeader(500)
					return
				}
				switch r.URL.Path {
				case "/v1/account":
					writeJSON(w, map[string]any{"id": "acct_test", "object": "account"})
				case "/v1/checkout/sessions/cs_fixture123":
					s := sessionJSON(req, false)
					s["status"] = "open"
					if tc.alter != nil {
						tc.alter(s)
					}
					writeJSON(w, s)
				default:
					t.Error("unexpected provider read")
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			a := fixtureAdapter(t, server)
			intent := billingstore.Intent{Binding: req.Binding, State: "confirmed", Operation: CheckoutIntentOperation, Version: 2, IdempotencyKey: req.IdempotencyKey, PayloadSHA256: CheckoutPayloadHash(req), ProviderObjectRef: "cs_fixture123", CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
			out, err := a.ReadConfirmedCheckout(context.Background(), req, intent)
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("error=%v want=%v", err, tc.want)
				}
			} else if err != nil || out.State != provider.SubscriptionConfirmed || out.Value.Ref.ID != intent.ProviderObjectRef {
				t.Fatalf("continuation=%#v err=%v", out, err)
			}
			if calls != 2 {
				t.Fatalf("reads=%d", calls)
			}
			intent.PayloadSHA256[0] ^= 1
			if _, err := a.ReadConfirmedCheckout(context.Background(), req, intent); !errors.Is(err, provider.ErrInvalidRequest) {
				t.Fatalf("changed durable payload accepted: %v", err)
			}
			if calls != 2 {
				t.Fatal("invalid proof reached provider")
			}
		})
	}
}
