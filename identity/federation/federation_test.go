package federation_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/federation"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

type tokenReply struct{ Subject, Email, Issuer string }
type exchangeHarness struct {
	mu         sync.Mutex
	replies    map[string]tokenReply
	challenges map[string]string
	calls      int
	server     *httptest.Server
}

func newExchangeHarness() *exchangeHarness {
	h := &exchangeHarness{replies: map[string]tokenReply{}, challenges: map[string]string{}}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "form", http.StatusBadRequest)
			return
		}
		h.mu.Lock()
		h.calls++
		reply, ok := h.replies[r.Form.Get("code")]
		challenge := h.challenges[r.Form.Get("code")]
		h.mu.Unlock()
		digest := sha256.Sum256([]byte(r.Form.Get("verifier")))
		gotChallenge := base64.RawURLEncoding.EncodeToString(digest[:])
		if !ok || challenge == "" || challenge != gotChallenge || r.Form.Get("nonce") == "" {
			http.Error(w, "exchange rejected", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": reply.Issuer, "subject": reply.Subject, "email": reply.Email, "nonce": r.Form.Get("nonce")})
	}))
	return h
}
func (h *exchangeHarness) Close() { h.server.Close() }
func (h *exchangeHarness) set(code, challenge string, reply tokenReply) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.challenges[code] = challenge
	h.replies[code] = reply
}
func (h *exchangeHarness) count() int { h.mu.Lock(); defer h.mu.Unlock(); return h.calls }

type httpProvider struct {
	endpoint string
	client   *http.Client
}

func (p httpProvider) AuthorizationURL(_ context.Context, flow federation.Authorization) (string, error) {
	query := url.Values{"state": {flow.State}, "nonce": {flow.Nonce},
		"redirect_uri": {flow.CallbackURL}, "code_challenge": {flow.CodeChallenge},
		"code_challenge_method": {flow.CodeChallengeMethod}}
	return "https://provider.example/authorize?" + query.Encode(), nil
}

func (p httpProvider) Exchange(ctx context.Context, code, verifier, nonce string) (federation.Identity, error) {
	form := url.Values{"code": {code}, "verifier": {verifier}, "nonce": {nonce}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return federation.Identity{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return federation.Identity{}, err
	}
	if resp.StatusCode != http.StatusOK {
		if err := resp.Body.Close(); err != nil {
			return federation.Identity{}, err
		}
		return federation.Identity{}, fmt.Errorf("local exchange rejected")
	}
	var got struct {
		Issuer  string `json:"issuer"`
		Subject string `json:"subject"`
		Email   string `json:"email"`
		Nonce   string `json:"nonce"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return federation.Identity{}, closeErr
		}
		return federation.Identity{}, err
	}
	if err := resp.Body.Close(); err != nil {
		return federation.Identity{}, err
	}
	return federation.Identity{Issuer: got.Issuer, Subject: got.Subject, Email: got.Email, Nonce: got.Nonce}, nil
}

func TestT3_10_CallbackBindsBrowserProviderAndConsumesOnce(t *testing.T) {
	db, scope, victim, actor, googleID, enterpriseID := federationDB(t)
	h := newExchangeHarness()
	defer h.Close()
	var svc *federation.Service
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { svc.Callback(w, r) }))
	defer callback.Close()
	sessions, err := session.New(db, session.Config{InstallationID: scope.install, ApplicationID: scope.app, EnvironmentID: scope.env, AllowedOrigins: []string{callback.URL}, DevelopmentLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	svc, err = federation.New(federation.Config{DB: db, Sessions: sessions, InstallationID: scope.install, ApplicationID: scope.app, EnvironmentID: scope.env,
		CallbackURL: callback.URL + "/oauth/callback", Providers: map[string]federation.Provider{"google": httpProvider{h.server.URL, h.server.Client()}, "enterprise_oidc": httpProvider{h.server.URL, h.server.Client()}},
		Connections: map[string]federation.Connection{"google": {Provider: "google", ProviderConnectionID: googleID, Issuer: "https://issuer.example"}, "enterprise": {Provider: "enterprise_oidc", ProviderConnectionID: enterpriseID, Issuer: "https://issuer.example"}}})
	if err != nil {
		t.Fatal(err)
	}

	// A subject owned by another provider and carrying the victim's email is
	// not an account link and cannot issue a session.
	flow, cookie := beginLogin(t, svc, "enterprise", "/")
	h.set("attacker", flow.CodeChallenge, tokenReply{Subject: "provider-subject-1", Email: "victim@example.test", Issuer: "https://issuer.example"})
	wrongProvider := callbackRequest(t, callback.URL, flow, "attacker", "google", cookie)
	if got := serve(t, callback.Client(), wrongProvider); got.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cross-provider callback status=%d", got.StatusCode)
	}
	if h.count() != 0 {
		t.Fatal("provider exchange ran for a mismatched provider")
	}
	wrongBrowser := callbackRequest(t, callback.URL, flow, "attacker", "enterprise_oidc", &http.Cookie{Name: cookie.Name, Value: randomBrowserToken()})
	if got := serve(t, callback.Client(), wrongBrowser); got.StatusCode != http.StatusUnauthorized {
		t.Fatalf("browser mismatch status=%d", got.StatusCode)
	}
	good := callbackRequest(t, callback.URL, flow, "attacker", "enterprise_oidc", cookie)
	if got := serve(t, callback.Client(), good); got.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unbound subject same-email login status=%d", got.StatusCode)
	}
	if got := countSessions(t, db); got != 0 {
		t.Fatalf("same-email collision created %d sessions", got)
	}

	// The same issuer and subject under another connection do not resolve to
	// the Google binding. Provider connection remains part of the identity key.
	cross, crossCookie := beginLogin(t, svc, "enterprise", "/")
	h.set("cross-provider", cross.CodeChallenge, tokenReply{Subject: "google-subject", Email: "victim@example.test", Issuer: "https://issuer.example"})
	if got := serve(t, callback.Client(), callbackRequest(t, callback.URL, cross, "cross-provider", "enterprise_oidc", crossCookie)); got.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cross-provider subject status=%d", got.StatusCode)
	}

	// Browser mismatch does not consume valid state. The original browser can
	// finish once; callback replay cannot mint a second session.
	login, loginCookie := beginLogin(t, svc, "google", "/account?tab=security")
	h.set("valid", login.CodeChallenge, tokenReply{Subject: "google-subject", Email: "other@example.test", Issuer: "https://issuer.example"})
	if got := serve(t, callback.Client(), callbackRequest(t, callback.URL, login, "valid", "google", loginCookie)); got.StatusCode != http.StatusSeeOther {
		t.Fatalf("valid callback status=%d body=%s", got.StatusCode, got.Status)
	}
	if got := countSessions(t, db); got != 1 {
		t.Fatalf("valid callback created %d sessions", got)
	}
	if got := serve(t, callback.Client(), callbackRequest(t, callback.URL, login, "valid", "google", loginCookie)); got.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replayed callback status=%d", got.StatusCode)
	}
	if got := countSessions(t, db); got != 1 {
		t.Fatalf("replay changed session count to %d", got)
	}
	if victim == actor {
		t.Fatal("fixture people must be distinct")
	}
}

func TestT3_10_LinkRequiresCSRFAndCurrentProofEpoch(t *testing.T) {
	db, scope, victim, actor, googleID, _ := federationDB(t)
	h := newExchangeHarness()
	defer h.Close()
	var svc *federation.Service
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { svc.Callback(w, r) }))
	defer callback.Close()
	sessions, err := session.New(db, session.Config{InstallationID: scope.install, ApplicationID: scope.app, EnvironmentID: scope.env, AllowedOrigins: []string{callback.URL}, DevelopmentLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	svc, err = federation.New(federation.Config{DB: db, Sessions: sessions, InstallationID: scope.install, ApplicationID: scope.app, EnvironmentID: scope.env, CallbackURL: callback.URL + "/oauth/callback", Providers: map[string]federation.Provider{"google": httpProvider{h.server.URL, h.server.Client()}}, Connections: map[string]federation.Connection{"google": {Provider: "google", ProviderConnectionID: googleID, Issuer: "https://issuer.example"}}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	proof, err := authproof.NewVerifiedCredential(actor, scope.install, scope.app, scope.env, 0, "email_password", now, "aal1", now.Add(12*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.Issue(context.Background(), proof, "")
	if err != nil {
		t.Fatal(err)
	}
	begin := func(withCSRF bool) (f federation.Authorization, c *http.Cookie) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, callback.URL+"/oauth/link", nil).WithContext(identity.ContextWithVerifiedCredential(context.Background(), proof))
		req.Header.Set("Origin", callback.URL)
		req.AddCookie(issued.Cookie)
		if withCSRF {
			token, ok := sessions.CSRFToken(req)
			if !ok {
				t.Fatal("session CSRF token unavailable")
			}
			req.Header.Set("X-CSRF-Token", token)
		}
		w := httptest.NewRecorder()
		f, e := svc.BeginLink(w, req, federation.BeginRequest{Connection: "google"})
		if e != nil {
			return f, nil
		}
		return f, w.Result().Cookies()[0]
	}
	if _, c := begin(false); c != nil {
		t.Fatal("link initiation succeeded without CSRF")
	}
	flow, browser := begin(true)
	if browser == nil {
		t.Fatal("link flow failed with valid CSRF")
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, e := tx.Exec(`UPDATE identity_persons SET security_epoch=security_epoch+1 WHERE id=$1`, actor)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	h.set("stale", flow.CodeChallenge, tokenReply{Subject: "attacker-link-subject", Email: "victim@example.test", Issuer: "https://issuer.example"})
	staleRequest := callbackRequest(t, callback.URL, flow, "stale", "google", browser)
	staleRequest.AddCookie(issued.Cookie)
	res := serve(t, callback.Client(), staleRequest)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stale-proof callback status=%d", res.StatusCode)
	}
	if owner, ok := bindingOwner(t, db, googleID, "attacker-link-subject"); ok {
		t.Fatalf("stale proof linked identity to %s", owner)
	}
	if owner, ok := bindingOwner(t, db, googleID, "google-subject"); !ok || owner != victim {
		t.Fatalf("existing provider binding owner=%s found=%t want victim %s", owner, ok, victim)
	}
	proof, err = authproof.NewVerifiedCredential(actor, scope.install, scope.app, scope.env, 1,
		"email_password", time.Now().UTC(), "aal1", time.Now().Add(12*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issued, err = sessions.Issue(context.Background(), proof, "")
	if err != nil {
		t.Fatal(err)
	}
	flow, browser = begin(true)
	if browser == nil {
		t.Fatal("fresh link flow failed")
	}
	revokedFlow, revokedBrowser := flow, browser
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		sessionHash := sha256.Sum256([]byte(issued.Cookie.Value))
		_, e := tx.Exec(`UPDATE identity_sessions SET revoked_at=clock_timestamp() WHERE token_digest=$1`, sessionHash[:])
		return e
	}); err != nil {
		t.Fatal(err)
	}
	h.set("signed-out", revokedFlow.CodeChallenge, tokenReply{Subject: "signed-out-subject", Email: "victim@example.test", Issuer: "https://issuer.example"})
	revokedRequest := callbackRequest(t, callback.URL, revokedFlow, "signed-out", "google", revokedBrowser)
	revokedRequest.AddCookie(issued.Cookie)
	if res = serve(t, callback.Client(), revokedRequest); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed-out callback status=%d", res.StatusCode)
	}
	if owner, ok := bindingOwner(t, db, googleID, "signed-out-subject"); ok {
		t.Fatalf("signed-out session linked identity to %s", owner)
	}
	proof, err = authproof.NewVerifiedCredential(actor, scope.install, scope.app, scope.env, 1,
		"email_password", time.Now().UTC(), "aal1", time.Now().Add(12*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issued, err = sessions.Issue(context.Background(), proof, "")
	if err != nil {
		t.Fatal(err)
	}
	flow, browser = begin(true)
	h.set("fresh", flow.CodeChallenge, tokenReply{Subject: "attacker-link-subject", Email: "victim@example.test", Issuer: "https://issuer.example"})
	freshRequest := callbackRequest(t, callback.URL, flow, "fresh", "google", browser)
	freshRequest.AddCookie(issued.Cookie)
	res = serve(t, callback.Client(), freshRequest)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("fresh explicit link status=%d", res.StatusCode)
	}
	if owner, ok := bindingOwner(t, db, googleID, "attacker-link-subject"); !ok || owner != actor {
		t.Fatalf("explicit link owner=%s found=%t want actor %s", owner, ok, actor)
	}
	if victim == actor {
		t.Fatal("fixture people must be distinct")
	}
}

func TestT3_10_CallbackRechecksCurrentEnabledConnection(t *testing.T) {
	db, scope, _, _, googleID, _ := federationDB(t)
	h := newExchangeHarness()
	defer h.Close()
	var svc *federation.Service
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { svc.Callback(w, r) }))
	defer callback.Close()
	sessions, err := session.New(db, session.Config{InstallationID: scope.install, ApplicationID: scope.app,
		EnvironmentID: scope.env, AllowedOrigins: []string{callback.URL}, DevelopmentLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	svc, err = federation.New(federation.Config{DB: db, Sessions: sessions, InstallationID: scope.install,
		ApplicationID: scope.app, EnvironmentID: scope.env, CallbackURL: callback.URL + "/oauth/callback",
		Providers:   map[string]federation.Provider{"google": httpProvider{h.server.URL, h.server.Client()}},
		Connections: map[string]federation.Connection{"google": {Provider: "google", ProviderConnectionID: googleID, Issuer: "https://issuer.example"}}})
	if err != nil {
		t.Fatal(err)
	}
	flow, browser := beginLogin(t, svc, "google", "/")
	h.set("disabled", flow.CodeChallenge, tokenReply{Subject: "google-subject", Email: "victim@example.test", Issuer: "https://issuer.example"})
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, e := tx.Exec(`UPDATE identity_federation_connections SET issuer='https://replacement.example' WHERE id=$1`, googleID)
		return e
	}); err == nil {
		t.Fatal("provider connection issuer tuple was mutable")
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE identity_federation_connections SET enabled=false WHERE id=$1`, googleID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	response := serve(t, callback.Client(), callbackRequest(t, callback.URL, flow, "disabled", "google", browser))
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled connection status=%d", response.StatusCode)
	}
	if h.count() != 0 {
		t.Fatal("disabled connection reached provider exchange")
	}
	if got := countSessions(t, db); got != 0 {
		t.Fatalf("disabled connection created %d sessions", got)
	}
}

func TestT3_10_ExpiredCallbackAfterRowLockWaitCommitsNoAuthority(t *testing.T) {
	db, scope, _, actor, googleID, _ := federationDB(t)
	h := newExchangeHarness()
	defer h.Close()
	var svc *federation.Service
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { svc.Callback(w, r) }))
	defer callback.Close()
	sessions, err := session.New(db, session.Config{InstallationID: scope.install, ApplicationID: scope.app,
		EnvironmentID: scope.env, AllowedOrigins: []string{callback.URL}, DevelopmentLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	svc, err = federation.New(federation.Config{DB: db, Sessions: sessions, InstallationID: scope.install,
		ApplicationID: scope.app, EnvironmentID: scope.env, CallbackURL: callback.URL + "/oauth/callback",
		Providers:   map[string]federation.Provider{"google": httpProvider{h.server.URL, h.server.Client()}},
		Connections: map[string]federation.Connection{"google": {Provider: "google", ProviderConnectionID: googleID, Issuer: "https://issuer.example"}}})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := authproof.NewVerifiedCredential(actor, scope.install, scope.app, scope.env, 0,
		"email_password", time.Now().UTC(), "aal1", time.Now().Add(12*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.Issue(context.Background(), proof, "")
	if err != nil {
		t.Fatal(err)
	}
	start := httptest.NewRequest(http.MethodPost, callback.URL+"/oauth/link", nil)
	start.Header.Set("Origin", callback.URL)
	start.AddCookie(issued.Cookie)
	start = start.WithContext(identity.ContextWithVerifiedCredential(start.Context(), proof))
	token, ok := sessions.CSRFToken(start)
	if !ok {
		t.Fatal("session CSRF token unavailable")
	}
	start.Header.Set("X-CSRF-Token", token)
	started := httptest.NewRecorder()
	flow, err := svc.BeginLink(started, start, federation.BeginRequest{Connection: "google"})
	if err != nil {
		t.Fatal(err)
	}
	browser := started.Result().Cookies()[0]
	stateHash := sha256.Sum256([]byte(flow.State))
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, e := tx.Exec(`UPDATE identity_federation_flows SET expires_at=clock_timestamp()+interval '3 seconds' WHERE state_digest=$1`, stateHash[:])
		return e
	}); err != nil {
		t.Fatal(err)
	}
	lockCtx, cancelLock := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelLock()
	held, release := make(chan struct{}), make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- db.WithTx(lockCtx, nil, func(tx *sql.Tx) error {
			var id uuid.UUID
			if err := tx.QueryRowContext(lockCtx, `SELECT id FROM identity_federation_flows WHERE state_digest=$1 FOR UPDATE`, stateHash[:]).Scan(&id); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	h.set("expires-while-locked", flow.CodeChallenge, tokenReply{Subject: "link-expires", Email: "victim@example.test", Issuer: "https://issuer.example"})
	request := callbackRequest(t, callback.URL, flow, "expires-while-locked", "google", browser)
	request.AddCookie(issued.Cookie)
	callbackDone := make(chan *http.Response, 1)
	callbackErr := make(chan error, 1)
	go func() {
		response, requestErr := callback.Client().Do(request)
		if requestErr != nil {
			callbackErr <- requestErr
			return
		}
		callbackDone <- response
	}()
	deadline := time.NewTimer(8 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	waitedAndExpired := false
	for !waitedAndExpired {
		queryCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = db.WithTx(queryCtx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(queryCtx, `SELECT
				EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock'
				AND query LIKE '%FROM identity_federation_flows f JOIN identity_federation_connections c%')
				AND EXISTS(SELECT 1 FROM identity_federation_flows WHERE state_digest=$1 AND expires_at<=clock_timestamp())`, stateHash[:]).Scan(&waitedAndExpired)
		})
		cancel()
		if err != nil {
			close(release)
			<-lockDone
			t.Fatalf("observe callback lock wait/expiry: %v", err)
		}
		if waitedAndExpired {
			break
		}
		select {
		case err := <-callbackErr:
			close(release)
			<-lockDone
			t.Fatalf("callback ended before the pending flow expired: %v", err)
		case <-deadline.C:
			close(release)
			<-lockDone
			t.Fatal("callback never waited on the flow row through expiry")
		case <-ticker.C:
		}
	}
	close(release)
	if err := <-lockDone; err != nil {
		t.Fatal(err)
	}
	var response *http.Response
	select {
	case err := <-callbackErr:
		t.Fatal(err)
	case response = <-callbackDone:
	case <-time.After(5 * time.Second):
		t.Fatal("expired callback did not finish")
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired callback status=%d", response.StatusCode)
	}
	if owner, ok := bindingOwner(t, db, googleID, "link-expires"); ok {
		t.Fatalf("expired flow linked identity to %s", owner)
	}
	if got := countSessions(t, db); got != 1 {
		t.Fatalf("expired flow changed session count to %d", got)
	}
}

func federationDB(t *testing.T) (*storage.DB, scopeIDs, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	_, schema := testkit.NewPostgres(t)
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	q.Set("search_path", schema)
	parsed.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	base, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "identity.sql"))
	if err != nil {
		t.Fatal(err)
	}
	federationSchema, err := federation.Fragment(2)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := migrations.NewRegistry(migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Namespace: "identity", Name: "base", SQL: string(base)}}}, federationSchema)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatal(err)
	}
	scope := scopeIDs{newUUID(t), newUUID(t), newUUID(t)}
	victim, actor := newUUID(t), newUUID(t)
	googleID, enterpriseID := newUUID(t), newUUID(t)
	err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		for _, person := range []uuid.UUID{victim, actor} {
			if _, e := tx.ExecContext(ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, person, scope.install, scope.app); e != nil {
				return e
			}
		}
		_, e := tx.ExecContext(ctx, `INSERT INTO identity_federation_connections(id,installation_id,application_id,provider,issuer,enabled) VALUES
			($1,$2,$3,'google','https://issuer.example',true),($4,$2,$3,'enterprise_oidc','https://issuer.example',true)`, googleID, scope.install, scope.app, enterpriseID)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO identity_external_bindings(id,person_id,provider_connection_id,provider,issuer,subject) VALUES($1,$2,$3,'google','https://issuer.example','google-subject')`, newUUID(t), victim, googleID)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, scope, victim, actor, googleID, enterpriseID
}

type scopeIDs struct{ install, app, env uuid.UUID }

func newUUID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func beginLogin(t *testing.T, s *federation.Service, connection, returnTo string) (federation.Authorization, *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/oauth/start", nil)
	w := httptest.NewRecorder()
	flow, err := s.BeginLogin(w, req, federation.BeginRequest{Connection: connection, ReturnTo: returnTo})
	if err != nil {
		t.Fatal(err)
	}
	authorize, err := url.Parse(flow.ProviderURL)
	if err != nil || authorize.Scheme != "https" || authorize.Query().Get("state") != flow.State ||
		authorize.Query().Get("nonce") != flow.Nonce || authorize.Query().Get("redirect_uri") != flow.CallbackURL ||
		authorize.Query().Get("code_challenge") != flow.CodeChallenge || authorize.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("provider authorization URL did not carry the bound callback and PKCE values")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("begin cookies=%d", len(cookies))
	}
	return flow, cookies[0]
}
func callbackRequest(t *testing.T, base string, flow federation.Authorization, code, provider string, cookie *http.Cookie) *http.Request {
	t.Helper()
	u := base + "/oauth/callback?provider=" + url.QueryEscape(provider) + "&state=" + url.QueryEscape(flow.State) + "&code=" + url.QueryEscape(code)
	r, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(cookie)
	return r
}
func serve(t *testing.T, c *http.Client, r *http.Request) *http.Response {
	t.Helper()
	client := *c
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close callback response: %v", closeErr)
		}
	})
	return resp
}
func countSessions(t *testing.T, db *storage.DB) int {
	t.Helper()
	var count int
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM identity_sessions WHERE revoked_at IS NULL`).Scan(&count)
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}
func bindingOwner(t *testing.T, db *storage.DB, connection uuid.UUID, subject string) (uuid.UUID, bool) {
	t.Helper()
	var id uuid.UUID
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT person_id FROM identity_external_bindings WHERE provider_connection_id=$1 AND issuer=$2 AND subject=$3`, connection, "https://issuer.example", subject).Scan(&id)
	})
	if err == sql.ErrNoRows {
		return uuid.Nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return id, true
}
func randomBrowserToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
