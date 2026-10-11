package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type testReplayStore struct {
	mu   sync.Mutex
	seen map[string]struct{}
	err  error
}

func (s *testReplayStore) Consume(_ context.Context, issuer, tokenID string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false, s.err
	}
	key := issuer + "\x00" + tokenID
	if s.seen == nil {
		s.seen = make(map[string]struct{})
	}
	if _, exists := s.seen[key]; exists {
		return false, nil
	}
	s.seen[key] = struct{}{}
	return true, nil
}

func TestSignerVerifierBindClaimsAndRejectReplay(t *testing.T) {
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	privateKey, publicKey := testKeyPair(t)
	replay := &testReplayStore{}
	signer := mustSigner(t, "key-1", privateKey, func() time.Time { return now })
	verifier := mustVerifier(t, map[string]VerificationKey{"key-1": {PublicKey: publicKey}}, replay, func() time.Time { return now })
	binding := RequestBinding{Method: http.MethodGet, Path: "/v1/orders/7"}
	assertion, err := signer.Sign(testIdentity(), binding)
	if err != nil {
		t.Fatal(err)
	}

	got, err := verifier.Verify(context.Background(), assertion, binding)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "person-ref-7" || got.Workspace != "workspace-ref-3" || got.AuthorizationDecision != "decision-ref-9" || len(got.Scopes) != 2 || got.Scopes[0] != "orders:read" || got.Scopes[1] != "workspace:read" {
		t.Fatalf("verified identity lost signed fields: %#v", got)
	}
	if !validTokenID(got.TokenID) {
		t.Fatalf("token ID is not a 128-bit base64url value: %q", got.TokenID)
	}
	if _, err := verifier.Verify(context.Background(), assertion, binding); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay error = %v, want ErrReplay", err)
	}
}

func TestVerifierRejectsWrongAudienceRequestAndSignature(t *testing.T) {
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	privateKey, publicKey := testKeyPair(t)
	binding := RequestBinding{Method: http.MethodPost, Path: "/v1/orders"}
	replay := &testReplayStore{}
	verifier := mustVerifier(t, map[string]VerificationKey{"key-1": {PublicKey: publicKey}}, replay, func() time.Time { return now })

	good := signClaims(t, "key-1", privateKey, assertionClaims{
		Claims:       jwt.Claims{Issuer: Issuer, Subject: "person-ref-7", Audience: jwt.Audience{"another-service"}, Expiry: jwt.NewNumericDate(now.Add(time.Minute)), NotBefore: jwt.NewNumericDate(now), IssuedAt: jwt.NewNumericDate(now), ID: tokenID(t)},
		Installation: "installation-ref-1", Application: "application-ref-1", Environment: "prod-ref-1", Workspace: "workspace-ref-3", AuthorizationDecision: "decision-ref-9", Scopes: []string{"orders:write"}, Method: binding.Method, Path: binding.Path,
	})
	if _, err := verifier.Verify(context.Background(), good, binding); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("wrong audience error = %v, want ErrInvalidAssertion", err)
	}

	signer := mustSigner(t, "key-1", privateKey, func() time.Time { return now })
	valid, err := signer.Sign(testIdentity(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(context.Background(), valid, RequestBinding{Method: http.MethodGet, Path: binding.Path}); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("wrong method error = %v, want ErrInvalidAssertion", err)
	}
	if _, err := verifier.Verify(context.Background(), valid, RequestBinding{Method: binding.Method, Path: "/v1/other"}); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("wrong path error = %v, want ErrInvalidAssertion", err)
	}
	if _, err := verifier.Verify(context.Background(), tamperSignature(t, valid), binding); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("tampered signature error = %v, want ErrInvalidAssertion", err)
	}
}

func TestVerifierRejectsExpiredFutureAndUnknownKeyAssertions(t *testing.T) {
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	privateKey, publicKey := testKeyPair(t)
	binding := RequestBinding{Method: http.MethodGet, Path: "/v1/orders/7"}
	replay := &testReplayStore{}
	verifier := mustVerifier(t, map[string]VerificationKey{"key-1": {PublicKey: publicKey}}, replay, func() time.Time { return now })
	signer := mustSigner(t, "key-1", privateKey, func() time.Time { return now })
	valid, err := signer.Sign(testIdentity(), binding)
	if err != nil {
		t.Fatal(err)
	}

	verifier.now = func() time.Time { return now.Add(MaxLifetime) }
	if _, err := verifier.Verify(context.Background(), valid, binding); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("expired assertion error = %v, want ErrInvalidAssertion", err)
	}
	verifier.now = func() time.Time { return now }

	futureSigner := mustSigner(t, "key-1", privateKey, func() time.Time { return now.Add(ClockSkew + time.Second) })
	future, err := futureSigner.Sign(testIdentity(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(context.Background(), future, binding); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("future-issued assertion error = %v, want ErrInvalidAssertion", err)
	}

	_, otherPublicKey := testKeyPair(t)
	unknownKeySigner := mustSigner(t, "untrusted-key", privateKey, func() time.Time { return now })
	unknownKeyVerifier := mustVerifier(t, map[string]VerificationKey{"key-1": {PublicKey: otherPublicKey}}, &testReplayStore{}, func() time.Time { return now })
	unknown, err := unknownKeySigner.Sign(testIdentity(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unknownKeyVerifier.Verify(context.Background(), unknown, binding); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("unknown key error = %v, want ErrInvalidAssertion", err)
	}
}

func TestVerifierAcceptsRotationOverlapAndRejectsPostRetirementAssertions(t *testing.T) {
	retiredAt := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	oldPrivate, oldPublic := testKeyPair(t)
	newPrivate, newPublic := testKeyPair(t)
	binding := RequestBinding{Method: http.MethodPost, Path: "/v1/orders"}
	oldSigner := mustSigner(t, "old", oldPrivate, func() time.Time { return retiredAt.Add(-time.Second) })
	oldAssertion, err := oldSigner.Sign(testIdentity(), binding)
	if err != nil {
		t.Fatal(err)
	}
	newSigner := mustSigner(t, "new", newPrivate, func() time.Time { return retiredAt.Add(time.Second) })
	newAssertion, err := newSigner.Sign(testIdentity(), binding)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]VerificationKey{
		"old": {PublicKey: oldPublic, RetiredAt: retiredAt, AcceptUntil: retiredAt.Add(30 * time.Second)},
		"new": {PublicKey: newPublic},
	}
	verifier := mustVerifier(t, keys, &testReplayStore{}, func() time.Time { return retiredAt.Add(time.Second) })
	if _, err := verifier.Verify(context.Background(), oldAssertion, binding); err != nil {
		t.Fatalf("old assertion inside configured overlap rejected: %v", err)
	}
	if _, err := verifier.Verify(context.Background(), newAssertion, binding); err != nil {
		t.Fatalf("new key assertion rejected during overlap: %v", err)
	}
	postRetirementSigner := mustSigner(t, "old", oldPrivate, func() time.Time { return retiredAt.Add(time.Second) })
	postRetirementAssertion, err := postRetirementSigner.Sign(testIdentity(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(context.Background(), postRetirementAssertion, binding); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("post-retirement assertion error = %v, want ErrInvalidAssertion", err)
	}
	verifier = mustVerifier(t, keys, &testReplayStore{}, func() time.Time { return retiredAt.Add(30 * time.Second) })
	if _, err := verifier.Verify(context.Background(), oldAssertion, binding); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("assertion after overlap cutoff error = %v, want ErrInvalidAssertion", err)
	}
}

func TestVerifierRejectsNilContextAndLifetimeOverSixtySeconds(t *testing.T) {
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	privateKey, publicKey := testKeyPair(t)
	binding := RequestBinding{Method: http.MethodGet, Path: "/v1/orders/7"}
	verifier := mustVerifier(t, map[string]VerificationKey{"key-1": {PublicKey: publicKey}}, &testReplayStore{}, func() time.Time { return now })
	claims := assertionClaims{
		Claims:       jwt.Claims{Issuer: Issuer, Subject: "person-ref-7", Audience: jwt.Audience{Audience}, Expiry: jwt.NewNumericDate(now.Add(MaxLifetime + time.Second)), NotBefore: jwt.NewNumericDate(now), IssuedAt: jwt.NewNumericDate(now), ID: tokenID(t)},
		Installation: "installation-ref-1", Application: "application-ref-1", Environment: "prod-ref-1", Workspace: "workspace-ref-3", AuthorizationDecision: "decision-ref-9", Scopes: []string{"orders:read"}, Method: binding.Method, Path: binding.Path,
	}
	assertion := signClaims(t, "key-1", privateKey, claims)
	if _, err := verifier.Verify(context.Background(), assertion, binding); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("overlong lifetime error = %v, want ErrInvalidAssertion", err)
	}
	if _, err := verifier.Verify(nil, assertion, binding); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("nil context error = %v, want ErrInvalidAssertion", err)
	}
}

func TestHTTPTransportDeniesForgedAndWrongPathAssertions(t *testing.T) {
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	privateKey, publicKey := testKeyPair(t)
	correctBinding := RequestBinding{Method: http.MethodGet, Path: "/v1/orders/7"}
	signer := mustSigner(t, "key-1", privateKey, func() time.Time { return now })
	valid, err := signer.Sign(testIdentity(), correctBinding)
	if err != nil {
		t.Fatal(err)
	}
	verifier := mustVerifier(t, map[string]VerificationKey{"key-1": {PublicKey: publicKey}}, &testReplayStore{}, func() time.Time { return now })
	var handled atomic.Int32
	server := httptest.NewServer(verifier.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})))
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/orders/8", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(AssertionHeader, valid)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || handled.Load() != 0 {
		t.Fatalf("wrong-path transport response=%d handled=%d, want 401 and no handler call", response.StatusCode, handled.Load())
	}
	request, err = http.NewRequest(http.MethodGet, server.URL+"/v1/orders/7", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(AssertionHeader, "forged")
	response, err = server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || handled.Load() != 0 {
		t.Fatalf("forged transport response=%d handled=%d, want 401 and no handler call", response.StatusCode, handled.Load())
	}
}

func TestVerifierEnforcesRetiredKeyOverlapAndRequiresReplayStore(t *testing.T) {
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	_, publicKey := testKeyPair(t)
	retiredAt := now
	tooLong := VerificationKey{PublicKey: publicKey, RetiredAt: retiredAt, AcceptUntil: retiredAt.Add(24*time.Hour + time.Second)}
	if _, err := NewVerifier(VerifierConfig{Keys: map[string]VerificationKey{"old": tooLong}, Replay: &testReplayStore{}}); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("overlong overlap error = %v, want ErrInvalidAssertion", err)
	}
	if _, err := NewVerifier(VerifierConfig{Keys: map[string]VerificationKey{"key-1": {PublicKey: publicKey}}}); !errors.Is(err, ErrInvalidAssertion) {
		t.Fatalf("missing replay store error = %v, want ErrInvalidAssertion", err)
	}
}

func TestMiddlewareDeniesBeforeHandlerAndRemovesCallerIdentityHeaders(t *testing.T) {
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	privateKey, publicKey := testKeyPair(t)
	binding := RequestBinding{Method: http.MethodGet, Path: "/v1/orders/7"}
	signer := mustSigner(t, "key-1", privateKey, func() time.Time { return now })
	verifier := mustVerifier(t, map[string]VerificationKey{"key-1": {PublicKey: publicKey}}, &testReplayStore{}, func() time.Time { return now })
	assertion, err := signer.Sign(testIdentity(), binding)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := verifier.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Header.Get(AssertionHeader) != "" || r.Header.Get("X-AMOS-Subject") != "" || r.Header.Get("X-Forwarded-User") != "" || r.Header.Get("X-Authenticated-User-ID") != "" || r.Header.Get("X-Remote-User-ID") != "" {
			t.Error("identity headers reached the private handler")
		}
		identity, ok := IdentityFromContext(r.Context())
		if !ok || identity.Subject != "person-ref-7" {
			t.Errorf("verified identity missing from context: %#v, %v", identity, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "http://example.invalid/v1/orders/7", nil)
	request.Header.Set(AssertionHeader, assertion)
	request.Header.Set("X-AMOS-Subject", "forged-person")
	request.Header.Set("X-Forwarded-User", "forged@example.invalid")
	request.Header.Set("X-Authenticated-User-ID", "forged-admin")
	request.Header.Set("X-Remote-User-ID", "forged-admin")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !called {
		t.Fatalf("response=%d called=%v, want 204 and handler call", response.Code, called)
	}

	called = false
	request = httptest.NewRequest(http.MethodGet, "http://example.invalid/v1/orders/7", nil)
	request.Header.Set(AssertionHeader, "forged")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || called {
		t.Fatalf("forged response=%d called=%v, want 401 and no handler call", response.Code, called)
	}
}

func TestSetAssertionHeaderReplacesCallerSuppliedIdentity(t *testing.T) {
	headers := http.Header{
		"x-amos-identity-assertion": []string{"caller-token"},
		"X-AMOS-Subject":            []string{"caller-person"},
		"X-Forwarded-User":          []string{"caller-user"},
		"X-Auth-Request-User":       []string{"caller-auth-user"},
		"X-Forwarded-Groups":        []string{"caller-groups"},
		"X-Authenticated-Email":     []string{"caller-email"},
		"X-Remote-Groups":           []string{"caller-remote-groups"},
		"X-Authenticated-User-ID":   []string{"caller-admin"},
		"X-Remote-User-ID":          []string{"caller-admin"},
		"X-Other":                   []string{"preserved"},
	}
	if err := SetAssertionHeader(headers, "server-token"); err != nil {
		t.Fatal(err)
	}
	if got := headers.Get(AssertionHeader); got != "server-token" {
		t.Fatalf("assertion header = %q", got)
	}
	for _, name := range []string{"X-AMOS-Subject", "X-Forwarded-User", "X-Auth-Request-User", "X-Forwarded-Groups", "X-Authenticated-Email", "X-Remote-Groups", "X-Authenticated-User-ID", "X-Remote-User-ID"} {
		if got := headers.Get(name); got != "" {
			t.Errorf("caller identity header %s remains: %q", name, got)
		}
	}
	if got := headers.Get("X-Other"); got != "preserved" {
		t.Errorf("unrelated header lost: %q", got)
	}
}

func TestRequestBindingRejectsAmbiguousPaths(t *testing.T) {
	for _, value := range []string{"relative", "/a/../b", "/a/%2f/b", "/a%252fb", "/a%252e%252e/b", "/a%255cb", "/a//b", "/a?query=1", "/a\\b", "/a/%zz"} {
		if validBinding(RequestBinding{Method: http.MethodGet, Path: value}) {
			t.Errorf("ambiguous path accepted: %q", value)
		}
	}
	if validBinding(RequestBinding{Method: "get", Path: "/v1/items"}) {
		t.Error("lowercase method accepted")
	}
}

func TestVerifierFailsClosedWhenReplayStoreErrors(t *testing.T) {
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	privateKey, publicKey := testKeyPair(t)
	binding := RequestBinding{Method: http.MethodGet, Path: "/v1/orders/7"}
	signer := mustSigner(t, "key-1", privateKey, func() time.Time { return now })
	assertion, err := signer.Sign(testIdentity(), binding)
	if err != nil {
		t.Fatal(err)
	}
	store := &testReplayStore{err: errors.New("private storage diagnostic")}
	verifier := mustVerifier(t, map[string]VerificationKey{"key-1": {PublicKey: publicKey}}, store, func() time.Time { return now })
	if _, err := verifier.Verify(context.Background(), assertion, binding); !errors.Is(err, ErrReplayStore) || strings.Contains(err.Error(), "private storage diagnostic") {
		t.Fatalf("replay store failure leaked detail or did not fail closed: %v", err)
	}
}

func signClaims(t *testing.T, keyID string, privateKey ed25519.PrivateKey, claims assertionClaims) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: privateKey}, (&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), keyID))
	if err != nil {
		t.Fatal(err)
	}
	value, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testIdentity() Identity {
	return Identity{
		Subject: "person-ref-7", Installation: "installation-ref-1", Application: "application-ref-1",
		Environment: "prod-ref-1", Workspace: "workspace-ref-3", AuthorizationDecision: "decision-ref-9",
		Scopes: []string{"workspace:read", "orders:read"},
	}
}

func testKeyPair(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, publicKey
}

func tokenID(t *testing.T) string {
	t.Helper()
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}

func tamperSignature(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("expected compact JWS")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) == 0 {
		t.Fatal("invalid test signature")
	}
	signature[0] ^= 0xff
	parts[2] = base64.RawURLEncoding.EncodeToString(signature)
	return strings.Join(parts, ".")
}

func mustSigner(t *testing.T, keyID string, key ed25519.PrivateKey, now func() time.Time) *Signer {
	t.Helper()
	signer, err := NewSigner(SignerConfig{Key: SigningKey{ID: keyID, PrivateKey: key}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func mustVerifier(t *testing.T, keys map[string]VerificationKey, replay ReplayStore, now func() time.Time) *Verifier {
	t.Helper()
	verifier, err := NewVerifier(VerifierConfig{Keys: keys, Replay: replay, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return verifier
}
