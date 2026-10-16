package app_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ajent-social/amos/app"
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

func TestIdentityCompositionAuthPagesSupportHead(t *testing.T) {
	for _, path := range []string{"/signup", "/signin"} {
		t.Run(path, func(t *testing.T) {
			pageCalls, mutationCalls := 0, 0
			page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pageCalls++
				if r.Method != http.MethodGet && r.Method != http.MethodHead {
					t.Fatalf("page received method %s", r.Method)
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(http.StatusOK)
			})
			mutation := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { mutationCalls++; w.WriteHeader(http.StatusAccepted) })
			a, err := app.New(app.Options{Identity: app.IdentityHandlers{SignupPage: page, SigninPage: page, Signup: mutation, Signin: mutation}})
			if err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				w := httptest.NewRecorder()
				a.Handler().ServeHTTP(w, httptest.NewRequest(method, path, nil))
				if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("%s %s did not reach configured page: status=%d headers=%v", method, path, w.Code, w.Header())
				}
			}
			mutationPath := path
			if path == "/signin" {
				mutationPath = "/auth"
			}
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, mutationPath, nil))
			if w.Code != http.StatusAccepted || pageCalls != 2 || mutationCalls != 1 {
				t.Fatalf("page/mutation routing changed: status=%d page=%d mutation=%d", w.Code, pageCalls, mutationCalls)
			}
		})
	}
	callbackCalls := 0
	callback := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { callbackCalls++; w.WriteHeader(http.StatusNoContent) })
	a, err := app.New(app.Options{Identity: app.IdentityHandlers{FederationCallback: callback}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/signup", "/signin", "/oauth/callback"} {
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodHead, path, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("unconfigured HEAD %s=%d", path, w.Code)
		}
	}
	if callbackCalls != 0 {
		t.Fatal("HEAD dispatched the GET-only federation callback")
	}
}
