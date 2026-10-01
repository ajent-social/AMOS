package session_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func TestT3_3_RotationRejectsOldCookieAndProtectedRequestUsesCurrentIdentity(t *testing.T) {
	db, account := readyIdentity(t)
	proof, err := authproof.NewVerifiedCredential(account.PersonID, account.InstallationID, account.ApplicationID, account.EnvironmentID, 0, "email_password", time.Now().UTC(), "aal1", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := session.New(db, session.Config{InstallationID: account.InstallationID, ApplicationID: account.ApplicationID, EnvironmentID: account.EnvironmentID, AllowedOrigins: []string{"https://amos.example"}, CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.Issue(context.Background(), proof, "")
	if err != nil {
		t.Fatalf("issue first session: %v", err)
	}
	second, err := svc.Issue(context.Background(), proof, first.Cookie.Value)
	if err != nil {
		t.Fatalf("rotate session: %v", err)
	}
	if first.Cookie.Value == second.Cookie.Value || first.Cookie.Name != "__Host-amos_session" || !first.Cookie.Secure || !first.Cookie.HttpOnly || first.Cookie.Path != "/" || first.Cookie.Domain != "" {
		t.Fatal("session cookie did not meet production cookie policy")
	}
	handler := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.PrincipalFromContext(r.Context())
		if !ok || principal.PersonID() != account.PersonID || principal.Actor().Kind() != "person" || len(principal.Grants()) != 0 {
			http.Error(w, "missing trusted principal", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		name, token string
		want        int
	}{{"old", first.Cookie.Value, http.StatusUnauthorized}, {"current", second.Cookie.Value, http.StatusNoContent}} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "https://amos.example/protected", nil)
			req.AddCookie(&http.Cookie{Name: second.Cookie.Name, Value: tc.token})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestT3_3_CSRFAndSignoutRevokeCurrentSession(t *testing.T) {
	db, account := readyIdentity(t)
	proof, _ := authproof.NewVerifiedCredential(account.PersonID, account.InstallationID, account.ApplicationID, account.EnvironmentID, 0, "email_password", time.Now().UTC(), "aal1", time.Now().UTC().Add(time.Hour))
	svc, err := session.New(db, session.Config{InstallationID: account.InstallationID, ApplicationID: account.ApplicationID, EnvironmentID: account.EnvironmentID, AllowedOrigins: []string{"https://amos.example"}, CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.Issue(context.Background(), proof, "")
	if err != nil {
		t.Fatal(err)
	}
	handler := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	bad := httptest.NewRequest(http.MethodPost, "https://amos.example/mutate", nil)
	bad.AddCookie(issued.Cookie)
	bad.Header.Set("Origin", "https://amos.example")
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d", badRec.Code)
	}
	out := httptest.NewRequest(http.MethodPost, "https://amos.example/signout", nil)
	out.AddCookie(issued.Cookie)
	out.Header.Set("Origin", "https://amos.example")
	out.Header.Set("X-CSRF-Token", issued.CSRFToken)
	outRec := httptest.NewRecorder()
	svc.SignOut(outRec, out)
	if outRec.Code != http.StatusNoContent {
		t.Fatalf("signout status=%d body=%s", outRec.Code, outRec.Body.String())
	}
	if len(outRec.Result().Cookies()) != 1 || outRec.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("signout did not expire cookie")
	}
	get := httptest.NewRequest(http.MethodGet, "https://amos.example/protected", nil)
	get.AddCookie(issued.Cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, get)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked cookie status=%d", rec.Code)
	}
}

func TestT3_3_EpochDisabledAndExpiredSessionsFailClosed(t *testing.T) {
	db, account := readyIdentity(t)
	issue := func(epoch int64) *http.Cookie {
		proof, err := authproof.NewVerifiedCredential(account.PersonID, account.InstallationID, account.ApplicationID, account.EnvironmentID, epoch, "email_password", time.Now().UTC(), "aal1", time.Now().UTC().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		svc, err := session.New(db, session.Config{InstallationID: account.InstallationID, ApplicationID: account.ApplicationID, EnvironmentID: account.EnvironmentID, AllowedOrigins: []string{"https://amos.example"}, CookieSecure: true})
		if err != nil {
			t.Fatal(err)
		}
		issued, err := svc.Issue(context.Background(), proof, "")
		if err != nil {
			t.Fatalf("issue session: %v", err)
		}
		return issued.Cookie
	}
	request := func(cookie *http.Cookie) int {
		svc, err := session.New(db, session.Config{InstallationID: account.InstallationID, ApplicationID: account.ApplicationID, EnvironmentID: account.EnvironmentID, AllowedOrigins: []string{"https://amos.example"}, CookieSecure: true})
		if err != nil {
			t.Fatal(err)
		}
		h := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		r := httptest.NewRequest(http.MethodGet, "https://amos.example/protected", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	setPerson := func(state string, epochDelta int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE identity_persons SET state = $2, security_epoch = security_epoch + $3 WHERE id = $1`, account.PersonID, state, epochDelta)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	epochZero := issue(0)
	setPerson("active", 1)
	if got := request(epochZero); got != http.StatusUnauthorized {
		t.Fatalf("old security epoch status=%d", got)
	}
	epochOne := issue(1)
	setPerson("self_disabled", 0)
	if got := request(epochOne); got != http.StatusUnauthorized {
		t.Fatalf("disabled account status=%d", got)
	}
	setPerson("active", 0)
	expired := issue(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	digest := sha256.Sum256([]byte(expired.Value))
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE identity_sessions SET authenticated_at = transaction_timestamp() - interval '2 hours', issued_at = transaction_timestamp() - interval '2 hours', expires_at = transaction_timestamp() - interval '1 second', idle_expires_at = transaction_timestamp() - interval '1 second' WHERE token_digest = $1`, digest[:])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := request(expired); got != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d", got)
	}
}

func TestT3_3_DatabaseOutageReturnsUnavailable(t *testing.T) {
	db, account := readyIdentity(t)
	proof, err := authproof.NewVerifiedCredential(account.PersonID, account.InstallationID, account.ApplicationID, account.EnvironmentID, 0, "email_password", time.Now().UTC(), "aal1", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := session.New(db, session.Config{InstallationID: account.InstallationID, ApplicationID: account.ApplicationID, EnvironmentID: account.EnvironmentID, AllowedOrigins: []string{"https://amos.example"}, CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.Issue(context.Background(), proof, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	h := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodGet, "https://amos.example/protected", nil)
	r.AddCookie(issued.Cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("database outage status=%d body=%s", w.Code, w.Body.String())
	}
}

type accountFixture struct{ PersonID, InstallationID, ApplicationID, EnvironmentID uuid.UUID }

func readyIdentity(t *testing.T) (*storage.DB, accountFixture) {
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
		t.Fatalf("open isolated postgres: %v", err)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	sqlText, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "identity.sql"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := migrations.NewRegistry(migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: string(sqlText)}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Migrate(ctx, db, registry); err != nil {
		t.Fatal(err)
	}
	person, email, cred, challenge, installation, application, environment := id(t), id(t), id(t), id(t), id(t), id(t), id(t)
	digest := sha256.Sum256([]byte("session-test-challenge"))
	fixtureVerifier := fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(make([]byte, 16)), base64.RawStdEncoding.EncodeToString(make([]byte, 32)))
	input := store.PendingAccount{PersonID: person, EmailID: email, CredentialID: cred, ChallengeID: challenge, InstallationID: installation, ApplicationID: application, EmailAddress: "test@example.test", PasswordHash: fixtureVerifier, ChallengeDigest: digest[:], ChallengeExpiry: time.Now().Add(time.Hour)}
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		st, e := store.New(tx)
		if e != nil {
			return e
		}
		if e = st.CreatePendingAccount(ctx, input); e != nil {
			return e
		}
		consumed, e := st.ConsumeChallenge(ctx, challenge, store.ChallengeEmailVerification, digest[:])
		if e != nil {
			return e
		}
		if e := st.MarkEmailVerified(ctx, consumed.PersonID, consumed.EmailID); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE identity_persons SET state = 'active' WHERE id = $1`, consumed.PersonID)
		return e
	}); err != nil {
		t.Fatalf("create active identity fixture: %v", err)
	}
	return db, accountFixture{person, installation, application, environment}
}
func id(t *testing.T) uuid.UUID {
	t.Helper()
	v, e := store.NewID()
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestCallerTransactionSessionRotationRollbackAndCommit(t *testing.T) {
	db, account := readyIdentity(t)
	now := time.Now().UTC()
	proof, err := authproof.NewVerifiedCredential(account.PersonID, account.InstallationID, account.ApplicationID, account.EnvironmentID, 0, "email_magic_link", now, "aal1", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := session.New(db, session.Config{InstallationID: account.InstallationID, ApplicationID: account.ApplicationID, EnvironmentID: account.EnvironmentID, AllowedOrigins: []string{"https://amos.example"}, CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.Issue(context.Background(), proof, "")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "https://amos.example/magic-link", nil)
	request.AddCookie(first.Cookie)
	var rolled session.Issued
	rollback := fmt.Errorf("intentional rollback")
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		var err error
		rolled, err = svc.IssueForRequestTx(context.Background(), tx, proof, request)
		if err != nil {
			return err
		}
		return rollback
	}); err == nil {
		t.Fatal("transaction did not rollback")
	}
	handler := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	check := func(cookie *http.Cookie, want int) {
		t.Helper()
		r := httptest.NewRequest("GET", "https://amos.example/protected", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("session status=%d want=%d", w.Code, want)
		}
	}
	check(first.Cookie, 204)
	check(rolled.Cookie, 401)
	var committed session.Issued
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		var err error
		committed, err = svc.IssueForRequestTx(context.Background(), tx, proof, request)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	check(first.Cookie, 401)
	check(committed.Cookie, 204)
}

func TestDurableAssurancePersistsAndExpiresWithoutRevokingSession(t *testing.T) {
	db, account := readyIdentity(t)
	fragment, err := migrations.SessionAssurance(2)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(), fragment.Migrations[0].SQL)
		return e
	}); err != nil {
		t.Fatal("apply assurance migration", err)
	}
	svc, err := session.New(db, session.Config{InstallationID: account.InstallationID, ApplicationID: account.ApplicationID, EnvironmentID: account.EnvironmentID, AllowedOrigins: []string{"https://amos.example"}, CookieSecure: true, PersistAssurance: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	proof, err := authproof.NewVerifiedCredential(account.PersonID, account.InstallationID, account.ApplicationID, account.EnvironmentID, 0, "password+totp", now.Add(-2*time.Minute), "aal2", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.Issue(context.Background(), proof, "")
	if err != nil {
		t.Fatal("issue elevated session", err)
	}
	var got identity.AssuranceLevel
	handler := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.PrincipalFromContext(r.Context())
		if !ok {
			t.Error("missing principal")
		}
		got = principal.Assurance().Level()
		w.WriteHeader(204)
	}))
	check := func(want identity.AssuranceLevel) {
		t.Helper()
		req := httptest.NewRequest("GET", "https://amos.example/protected", nil)
		req.AddCookie(issued.Cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 204 || got != want {
			t.Fatalf("assurance lookup status=%d level=%s want=%s", w.Code, got, want)
		}
	}
	check(identity.AAL2)
	digest := sha256.Sum256([]byte(issued.Cookie.Value))
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(), `UPDATE identity_sessions SET assurance_expires_at=transaction_timestamp()-interval '1 minute' WHERE token_digest=$1`, digest[:])
		return e
	}); err != nil {
		t.Fatal(err)
	}
	check(identity.AAL1)
}
