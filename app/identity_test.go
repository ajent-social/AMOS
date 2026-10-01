package app_test

import (
	"github.com/ajent-social/amos/app"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIdentityCompositionKeepsBusinessReservations(t *testing.T) {
	calls := 0
	configured := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) })
	a, e := app.New(app.Options{Identity: app.IdentityHandlers{Signup: configured, SigninPage: configured, VerifyEmail: configured}})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		method, path string
		want         int
	}{{"POST", "/signup", 204}, {"GET", "/signin", 204}, {"GET", "/verify-email", 204}, {"POST", "/verify-email", 204}, {"GET", "/signup", 503}, {"POST", "/auth", 503}, {"PUT", "/signup", 503}, {"GET", "/signup/child", 503}, {"POST", "/SIGNUP", 503}, {"POST", "/signup//child", 400}, {"GET", "/healthz", 200}, {"GET", "/readyz", 200}} {
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.want {
			t.Fatalf("%s %s=%d want%d", tc.method, tc.path, w.Code, tc.want)
		}
	}
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
	for _, path := range []string{"/signup", "/auth", "/verify-email", "/signup/*", "/healthz", "/SIGNIN"} {
		if e := a.RegisterBusinessRoute("POST", path, configured); e != app.ErrReservedRoute {
			t.Fatalf("business override %s: %v", path, e)
		}
	}
}
