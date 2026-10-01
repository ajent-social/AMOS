package session_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/session"
)

func TestSessionOrdinaryFormRequiresBoundBodyCSRF(t *testing.T) {
	db, account := readyIdentity(t)
	proof, err := authproof.NewVerifiedCredential(account.PersonID, account.InstallationID, account.ApplicationID, account.EnvironmentID, 0, "email_password", time.Now().UTC(), "aal1", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	service, err := session.New(db, session.Config{InstallationID: account.InstallationID, ApplicationID: account.ApplicationID, EnvironmentID: account.EnvironmentID, AllowedOrigins: []string{"https://amos.example"}, CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := service.Issue(context.Background(), proof, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body, query, origin string
		want                      int
	}{
		{"body proof", url.Values{"_csrf": {issued.CSRFToken}, "title": {"ordinary form"}}.Encode(), "", "https://amos.example", 204},
		{"query cannot supply proof", "title=value", "?_csrf=" + issued.CSRFToken, "https://amos.example", 403},
		{"duplicate proof", url.Values{"_csrf": {issued.CSRFToken, issued.CSRFToken}}.Encode(), "", "https://amos.example", 403},
		{"foreign origin", url.Values{"_csrf": {issued.CSRFToken}}.Encode(), "", "https://foreign.example", 403},
		{"bounded form", "_csrf=" + issued.CSRFToken + "&title=" + strings.Repeat("x", 17<<10), "", "https://amos.example", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			handler := service.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
			request := httptest.NewRequest(http.MethodPost, "https://amos.example/mutate"+tc.query, strings.NewReader(tc.body))
			request.AddCookie(issued.Cookie)
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Origin", tc.origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want || called != (tc.want == 204) {
				t.Fatalf("status=%d called=%v want=%d", response.Code, called, tc.want)
			}
		})
	}
}

func TestDuplicateOriginHeadersFailBeforeMutation(t *testing.T) {
	for _, header := range []string{"Origin", "Referer"} {
		db, account := readyIdentity(t)
		proof, err := authproof.NewVerifiedCredential(account.PersonID, account.InstallationID, account.ApplicationID, account.EnvironmentID, 0, "email_password", time.Now().UTC(), "aal1", time.Now().UTC().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		service, err := session.New(db, session.Config{InstallationID: account.InstallationID, ApplicationID: account.ApplicationID, EnvironmentID: account.EnvironmentID, AllowedOrigins: []string{"https://app.example.test"}, CookieSecure: true})
		if err != nil {
			t.Fatal(err)
		}
		issued, err := service.Issue(context.Background(), proof, "")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "https://app.example.test/mutate", strings.NewReader(url.Values{"_csrf": {issued.CSRFToken}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Add(header, "https://app.example.test")
		req.Header.Add(header, "https://evil.example.test")
		req.AddCookie(issued.Cookie)
		rec := httptest.NewRecorder()
		called := false
		service.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || called {
			t.Fatalf("%s duplicate middleware: %d", header, rec.Code)
		}
		rec = httptest.NewRecorder()
		service.SignOut(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s duplicate signout: %d", header, rec.Code)
		}
	}
}
