package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/identity"
	identityemail "github.com/ajent-social/amos/identity/email"
	"github.com/ajent-social/amos/identity/login"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func TestOwnerBusinessRouteRejectsInvalidConfiguredPersonID(t *testing.T) {
	application, err := New(Options{})
	if err != nil {
		t.Fatalf("construct app: %v", err)
	}
	invalidPersonID, err := uuid.NewRandom()
	if err != nil {
		t.Fatal(err)
	}
	if invalidPersonID.Version() == 7 && invalidPersonID.Variant() == uuid.RFC4122 {
		t.Fatal("generated invalid owner fixture unexpectedly has UUIDv7 shape")
	}
	if err := application.RegisterOwnerBusinessRoute(http.MethodGet, "/owner/invalid", &session.Service{}, invalidPersonID, func(http.ResponseWriter, *http.Request, identity.Principal) {}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("register route with non-v7 expected owner person ID error=%v, want ErrInvalidOptions", err)
	}
}

func TestOwnerBusinessRouteUsesScopedSessionAndBindsConfiguredPerson(t *testing.T) {
	const origin = "https://owner-consumer.example.test"
	db, raw := ownerRouteTestDB(t)
	ids := ownerRouteIDs(t)
	materialCapture := &ownerRouteMaterialCapture{}
	loginService, verification, sessions := ownerRouteServices(t, db, raw, ids, origin, materialCapture)
	ownerCookie, ownerID := registerAndSignInOwnerRoutePerson(t, raw, loginService, verification, materialCapture, ids, "owner@example.test")
	otherCookie, otherID := registerAndSignInOwnerRoutePerson(t, raw, loginService, verification, materialCapture, ids, "other@example.test")

	application, err := New(Options{})
	if err != nil {
		t.Fatalf("construct app: %v", err)
	}
	handlerCalls := 0
	ownerHandler := func(w http.ResponseWriter, r *http.Request, principal identity.Principal) {
		handlerCalls++
		fromContext, ok := identity.PrincipalFromContext(r.Context())
		if !ok || fromContext.PersonID() != principal.PersonID() || principal.PersonID() != ownerID ||
			principal.InstallationID() != ids.installation || principal.ApplicationID() != ids.application || principal.EnvironmentID() != ids.environment {
			http.Error(w, "unexpected owner principal", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
	if err := application.RegisterOwnerBusinessRoute(http.MethodGet, "/owner/private", sessions, ownerID, ownerHandler); err != nil {
		t.Fatalf("register owner route: %v", err)
	}
	if err := application.RegisterOwnerBusinessRoute(http.MethodPost, "/owner/private", sessions, ownerID, ownerHandler); err != nil {
		t.Fatalf("register owner mutation route: %v", err)
	}

	serve := func(method, path string, cookie *http.Cookie, originValue string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, origin+path, nil)
		request.RequestURI = request.URL.RequestURI()
		if cookie != nil {
			request.AddCookie(cookie)
		}
		if originValue != "" {
			request.Header.Set("Origin", originValue)
		}
		response := httptest.NewRecorder()
		application.Handler().ServeHTTP(response, request)
		return response
	}

	if response := serve(http.MethodGet, "/owner/private", nil, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("request without native session status=%d, want 401", response.Code)
	}
	if response := serve(http.MethodGet, "/owner/private", otherCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("different authenticated person status=%d, want 403", response.Code)
	} else if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("personalized authorization denial Cache-Control=%q, want no-store", response.Header().Get("Cache-Control"))
	}
	if handlerCalls != 0 {
		t.Fatalf("protected handler ran for unauthenticated/different-person requests: %d", handlerCalls)
	}
	forged := httptest.NewRequest(http.MethodGet, origin+"/owner/private", nil)
	forged.RequestURI = forged.URL.RequestURI()
	forged.AddCookie(ownerCookie)
	forged.Header.Set("X-AMOS-Person-ID", otherID.String())
	forged.Header.Set("X-Authenticated-User", otherID.String())
	forgedResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(forgedResponse, forged)
	if forgedResponse.Code != http.StatusNoContent || handlerCalls != 1 {
		t.Fatalf("forged identity headers changed trusted-session admission: status=%d calls=%d", forgedResponse.Code, handlerCalls)
	}

	missingCSRF := httptest.NewRequest(http.MethodPost, origin+"/owner/private", strings.NewReader("value=1"))
	missingCSRF.RequestURI = missingCSRF.URL.RequestURI()
	missingCSRF.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	missingCSRF.Header.Set("Origin", origin)
	missingCSRF.AddCookie(ownerCookie)
	missingCSRFResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden || handlerCalls != 1 {
		t.Fatalf("missing CSRF status=%d calls=%d, want 403 without handler", missingCSRFResponse.Code, handlerCalls)
	}

	csrfRequest := httptest.NewRequest(http.MethodGet, origin+"/owner/private", nil)
	csrfRequest.AddCookie(ownerCookie)
	csrf, ok := sessions.CSRFToken(csrfRequest)
	if !ok {
		t.Fatal("native session cookie did not yield its bound CSRF token")
	}
	foreignOrigin := httptest.NewRequest(http.MethodPost, origin+"/owner/private", strings.NewReader(url.Values{"_csrf": {csrf}}.Encode()))
	foreignOrigin.RequestURI = foreignOrigin.URL.RequestURI()
	foreignOrigin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	foreignOrigin.Header.Set("Origin", "https://foreign.example.test")
	foreignOrigin.AddCookie(ownerCookie)
	foreignResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(foreignResponse, foreignOrigin)
	if foreignResponse.Code != http.StatusForbidden || handlerCalls != 1 {
		t.Fatalf("foreign Origin status=%d calls=%d, want 403 without handler", foreignResponse.Code, handlerCalls)
	}

	validMutation := httptest.NewRequest(http.MethodPost, origin+"/owner/private", strings.NewReader(url.Values{"_csrf": {csrf}}.Encode()))
	validMutation.RequestURI = validMutation.URL.RequestURI()
	validMutation.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	validMutation.Header.Set("Origin", origin)
	validMutation.AddCookie(ownerCookie)
	validResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(validResponse, validMutation)
	if validResponse.Code != http.StatusNoContent || handlerCalls != 2 {
		t.Fatalf("valid native CSRF status=%d calls=%d, want 204 and handler", validResponse.Code, handlerCalls)
	}

	wrongInstallation, err := session.New(db, session.Config{
		InstallationID: ownerRouteID(t), ApplicationID: ids.application, EnvironmentID: ids.environment,
		AllowedOrigins: []string{origin}, CookieSecure: true,
	})
	if err != nil {
		t.Fatalf("construct differently scoped native sessions: %v", err)
	}
	if err := application.RegisterOwnerBusinessRoute(http.MethodGet, "/owner/wrong-installation", wrongInstallation, ownerID, ownerHandler); err != nil {
		t.Fatalf("register wrong-installation route: %v", err)
	}
	if response := serve(http.MethodGet, "/owner/wrong-installation", ownerCookie, ""); response.Code != http.StatusUnauthorized || handlerCalls != 2 {
		t.Fatalf("cross-installation cookie status=%d calls=%d, want 401 without handler", response.Code, handlerCalls)
	}

	wrongApplication, err := session.New(db, session.Config{
		InstallationID: ids.installation, ApplicationID: ownerRouteID(t), EnvironmentID: ids.environment,
		AllowedOrigins: []string{origin}, CookieSecure: true,
	})
	if err != nil {
		t.Fatalf("construct differently scoped native sessions: %v", err)
	}
	if err := application.RegisterOwnerBusinessRoute(http.MethodGet, "/owner/wrong-application", wrongApplication, ownerID, ownerHandler); err != nil {
		t.Fatalf("register wrong-application route: %v", err)
	}
	if response := serve(http.MethodGet, "/owner/wrong-application", ownerCookie, ""); response.Code != http.StatusUnauthorized || handlerCalls != 2 {
		t.Fatalf("cross-application cookie status=%d calls=%d, want 401 without handler", response.Code, handlerCalls)
	}

	wrongEnvironment, err := session.New(db, session.Config{
		InstallationID: ids.installation, ApplicationID: ids.application, EnvironmentID: ownerRouteID(t),
		AllowedOrigins: []string{origin}, CookieSecure: true,
	})
	if err != nil {
		t.Fatalf("construct differently scoped native sessions: %v", err)
	}
	if err := application.RegisterOwnerBusinessRoute(http.MethodGet, "/owner/wrong-environment", wrongEnvironment, ownerID, ownerHandler); err != nil {
		t.Fatalf("register wrong-environment route: %v", err)
	}
	if response := serve(http.MethodGet, "/owner/wrong-environment", ownerCookie, ""); response.Code != http.StatusUnauthorized || handlerCalls != 2 {
		t.Fatalf("cross-environment cookie status=%d calls=%d, want 401 without handler", response.Code, handlerCalls)
	}

	signout := httptest.NewRequest(http.MethodPost, origin+"/signout", strings.NewReader(url.Values{"_csrf": {csrf}}.Encode()))
	signout.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	signout.Header.Set("Origin", origin)
	signout.AddCookie(ownerCookie)
	signoutResponse := httptest.NewRecorder()
	sessions.SignOut(signoutResponse, signout)
	if signoutResponse.Code != http.StatusNoContent {
		t.Fatalf("native session signout status=%d body=%s", signoutResponse.Code, signoutResponse.Body.String())
	}
	if response := serve(http.MethodGet, "/owner/private", ownerCookie, ""); response.Code != http.StatusUnauthorized || handlerCalls != 2 {
		t.Fatalf("revoked native session status=%d calls=%d, want 401 without handler", response.Code, handlerCalls)
	}
}

type ownerRouteIDsFixture struct{ installation, application, environment uuid.UUID }

func ownerRouteIDs(t *testing.T) ownerRouteIDsFixture {
	t.Helper()
	return ownerRouteIDsFixture{ownerRouteID(t), ownerRouteID(t), ownerRouteID(t)}
}

func ownerRouteID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ownerRouteTestDB(t *testing.T) (*storage.DB, *sql.DB) {
	t.Helper()
	raw, schema := testkit.NewPostgres(t)
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close isolated storage: %v", err)
		}
	})
	registry, err := migrations.Core()
	if err != nil {
		t.Fatalf("load core migrations: %v", err)
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatalf("migrate isolated storage: %v", err)
	}
	return db, raw
}

type ownerRouteMaterialCapture struct {
	mu       sync.Mutex
	material deliveryemail.PrivateMaterial
}

func (c *ownerRouteMaterialCapture) PutVerificationMaterial(_ context.Context, _ *sql.Tx, _ deliveryemail.SecretReference, material deliveryemail.PrivateMaterial, _ time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.material = material
	return nil
}

func (c *ownerRouteMaterialCapture) latest() deliveryemail.PrivateMaterial {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.material
}

func ownerRouteServices(t *testing.T, db *storage.DB, raw *sql.DB, ids ownerRouteIDsFixture, origin string, capture *ownerRouteMaterialCapture) (*login.Service, *identityemail.Service, *session.Service) {
	t.Helper()
	hasher, err := password.New(ownerRouteBlocklist{}, ownerRouteBudget{}, 1)
	if err != nil {
		t.Fatalf("construct bounded synthetic password hasher: %v", err)
	}
	renderer, err := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: origin, MaxBodyBytes: 4096})
	if err != nil {
		t.Fatalf("construct synthetic email renderer: %v", err)
	}
	outbox, err := sqlstore.New(raw, sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
	if err != nil {
		t.Fatalf("construct synthetic outbox: %v", err)
	}
	verification, err := identityemail.New(db, outbox, renderer, capture, identityemail.Config{
		InstallationID: ids.installation, ApplicationID: ids.application, ApplicationOrigin: origin,
		ChallengeLifetime: identityemail.DefaultChallengeLifetime,
	})
	if err != nil {
		t.Fatalf("construct synthetic verification service: %v", err)
	}
	sessions, err := session.New(db, session.Config{
		InstallationID: ids.installation, ApplicationID: ids.application, EnvironmentID: ids.environment,
		AllowedOrigins: []string{origin}, CookieSecure: true,
	})
	if err != nil {
		t.Fatalf("construct native session service: %v", err)
	}
	service, err := login.New(login.Config{
		DB: db, Passwords: hasher, Email: verification, Sessions: sessions,
		InstallationID: ids.installation, ApplicationID: ids.application, EnvironmentID: ids.environment,
	})
	if err != nil {
		t.Fatalf("construct native login service: %v", err)
	}
	return service, verification, sessions
}

type ownerRouteBlocklist struct{}

func (ownerRouteBlocklist) Ready() bool                    { return true }
func (ownerRouteBlocklist) ContainsNormalized(string) bool { return false }

type ownerRouteBudget struct{}

func (ownerRouteBudget) Allow(context.Context, string) error { return nil }

func registerAndSignInOwnerRoutePerson(t *testing.T, raw *sql.DB, service *login.Service, verification *identityemail.Service, capture *ownerRouteMaterialCapture, ids ownerRouteIDsFixture, address string) (*http.Cookie, uuid.UUID) {
	t.Helper()
	const passwordValue = "Synthetic owner route password 2026!"
	signupBody, err := json.Marshal(map[string]string{"email": address, "password": passwordValue})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(string(signupBody)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://owner-consumer.example.test")
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("synthetic registration status=%d body=%s", response.Code, response.Body.String())
	}
	material := capture.latest()
	link, err := url.Parse(material.ActionURL)
	if err != nil {
		t.Fatalf("parse synthetic verification material: %v", err)
	}
	challenge, err := uuid.Parse(link.Query().Get("challenge"))
	if err != nil {
		t.Fatalf("parse synthetic verification challenge: %v", err)
	}
	if err := verification.Confirm(context.Background(), challenge, link.Query().Get("token")); err != nil {
		t.Fatalf("confirm synthetic email identity: %v", err)
	}
	personID := uuid.Nil
	if err := raw.QueryRow(`SELECT p.id FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id WHERE p.installation_id=$1 AND p.application_id=$2 AND e.comparison_key=lower($3)`, ids.installation, ids.application, address).Scan(&personID); err != nil {
		t.Fatalf("read scoped synthetic person identity: %v", err)
	}
	signinBody, err := json.Marshal(map[string]string{"email": address, "password": passwordValue})
	if err != nil {
		t.Fatal(err)
	}
	signin := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(string(signinBody)))
	signin.Header.Set("Content-Type", "application/json")
	signin.Header.Set("Origin", "https://owner-consumer.example.test")
	signedIn := httptest.NewRecorder()
	service.Handler().ServeHTTP(signedIn, signin)
	if signedIn.Code != http.StatusOK {
		t.Fatalf("synthetic signin status=%d body=%s", signedIn.Code, signedIn.Body.String())
	}
	var cookie *http.Cookie
	for _, candidate := range signedIn.Result().Cookies() {
		if candidate.Name == "__Host-amos_session" {
			cookie = candidate
		}
	}
	if cookie == nil {
		t.Fatal("successful synthetic signin did not issue native session cookie")
	}
	return cookie, personID
}
