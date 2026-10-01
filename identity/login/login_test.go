package login

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/delivery/email"
	identityemail "github.com/ajent-social/amos/identity/email"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func TestT3_5_RegistrationConcurrentReplayCreatesOnePendingPersonAndWorkspace(t *testing.T) {
	db, raw := loginDB(t)
	svc, ids := loginService(t, db, raw)
	request := `{"email":"Person@example.test","password":"a sufficiently long password"}`
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			<-start
			req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(request))
			rec := httptest.NewRecorder()
			svc.Handler().ServeHTTP(rec, req)
			results <- rec
		}()
	}
	close(start)
	for range 2 {
		rec := <-results
		if rec.Code != http.StatusAccepted {
			t.Fatalf("registration status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	var persons, emails, workspaces, challenges, sessions int
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT count(*) FROM identity_persons WHERE installation_id=$1 AND application_id=$2`, ids.installation, ids.application).Scan(&persons); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT count(*) FROM identity_emails WHERE installation_id=$1 AND application_id=$2`, ids.installation, ids.application).Scan(&emails); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT count(*) FROM workspaces WHERE installation_id=$1 AND application_id=$2 AND kind='personal'`, ids.installation, ids.application).Scan(&workspaces); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT count(*) FROM identity_challenges c JOIN identity_persons p ON p.id=c.person_id WHERE p.installation_id=$1 AND p.application_id=$2 AND c.purpose='email_verification'`, ids.installation, ids.application).Scan(&challenges); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT count(*) FROM identity_sessions s JOIN identity_persons p ON p.id=s.person_id WHERE p.installation_id=$1 AND p.application_id=$2`, ids.installation, ids.application).Scan(&sessions)
	}); err != nil {
		t.Fatal(err)
	}
	if persons != 1 || emails != 1 || workspaces != 1 || challenges != 1 || sessions != 0 {
		t.Fatalf("durable counts persons=%d emails=%d workspaces=%d challenges=%d sessions=%d", persons, emails, workspaces, challenges, sessions)
	}
	var state string
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT state FROM identity_persons WHERE installation_id=$1 AND application_id=$2`, ids.installation, ids.application).Scan(&state)
	}); err != nil {
		t.Fatal(err)
	}
	if state != store.AccountPendingVerification {
		t.Fatalf("state=%q", state)
	}
}

func TestT3_5_SignInUsesGenericFailureAndOnlyVerifiedActiveAccountGetsSession(t *testing.T) {
	db, raw := loginDB(t)
	svc, ids := loginService(t, db, raw)
	// Seed via the public signup path, then prove its pending account is denied.
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(body))
		w := httptest.NewRecorder()
		svc.Handler().ServeHTTP(w, r)
		return w
	}
	if rec := post(`{"email":"pending@example.test","password":"a sufficiently long password"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("signup=%d %s", rec.Code, rec.Body.String())
	}
	call := func(address, pw string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"email": address, "password": pw})
		r := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(string(body)))
		w := httptest.NewRecorder()
		svc.Handler().ServeHTTP(w, r)
		return w
	}
	unverified := call("pending@example.test", "a sufficiently long password")
	wrong := call("pending@example.test", "incorrect password long")
	unknown := call("missing@example.test", "incorrect password long")
	if unverified.Code != http.StatusUnauthorized || wrong.Code != unverified.Code || unknown.Code != unverified.Code {
		t.Fatalf("failure statuses unverified=%d wrong=%d unknown=%d", unverified.Code, wrong.Code, unknown.Code)
	}
	var unverifiedBody, wrongBody, unknownBody response
	if err := json.Unmarshal(unverified.Body.Bytes(), &unverifiedBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wrong.Body.Bytes(), &wrongBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(unknown.Body.Bytes(), &unknownBody); err != nil {
		t.Fatal(err)
	}
	if wrongBody.Code != unknownBody.Code || wrongBody.Message != unknownBody.Message || wrongBody.Code != unverifiedBody.Code || wrongBody.Message != unverifiedBody.Message || wrongBody.Code != "auth.unauthenticated" {
		t.Fatalf("failure disclosure differs: unverified=%+v wrong=%+v unknown=%+v", unverifiedBody, wrongBody, unknownBody)
	}
	var personID uuid.UUID
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT id FROM identity_persons WHERE installation_id=$1 AND application_id=$2`, ids.installation, ids.application).Scan(&personID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE identity_emails SET verified_at=transaction_timestamp() WHERE person_id=$1`, personID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE identity_persons SET state='active' WHERE id=$1`, personID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	good := call("pending@example.test", "a sufficiently long password")
	if good.Code != http.StatusOK || good.Header().Get("Set-Cookie") == "" {
		t.Fatalf("verified sign-in status=%d body=%s", good.Code, good.Body.String())
	}
	var sessions int
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM identity_sessions WHERE person_id=$1`, personID).Scan(&sessions)
	}); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf("persisted sessions=%d", sessions)
	}
}

type list struct{}

func (list) Ready() bool                    { return true }
func (list) ContainsNormalized(string) bool { return false }

type budget struct{}

func (budget) Allow(context.Context, string) error { return nil }

type materials struct{}

func (materials) PutVerificationMaterial(_ context.Context, _ *sql.Tx, _ email.SecretReference, _ email.PrivateMaterial, _ time.Time) error {
	return nil
}

type testIDs struct{ installation, application, environment uuid.UUID }

func loginService(t *testing.T, db *storage.DB, raw *sql.DB) (*Service, testIDs) {
	t.Helper()
	ids := testIDs{newID(t), newID(t), newID(t)}
	hasher, err := password.New(list{}, budget{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := email.NewRenderer(email.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: "https://app.example.test", MaxBodyBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := sqlstore.New(raw, sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	mailer, err := identityemail.New(db, outbox, renderer, materials{}, identityemail.Config{InstallationID: ids.installation, ApplicationID: ids.application, ApplicationOrigin: "https://app.example.test", ChallengeLifetime: identityemail.DefaultChallengeLifetime})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.New(db, session.Config{InstallationID: ids.installation, ApplicationID: ids.application, EnvironmentID: ids.environment, AllowedOrigins: []string{"https://app.example.test"}, CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{DB: db, Passwords: hasher, Email: mailer, Sessions: sessions, InstallationID: ids.installation, ApplicationID: ids.application, EnvironmentID: ids.environment})
	if err != nil {
		t.Fatal(err)
	}
	return svc, ids
}

func loginDB(t *testing.T) (*storage.DB, *sql.DB) {
	t.Helper()
	raw, schema := testkit.NewPostgres(t)
	raw.SetMaxOpenConns(1)
	if _, err := raw.ExecContext(context.Background(), `SET search_path TO `+`"`+schema+`"`); err != nil {
		t.Fatal(err)
	}
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
	t.Cleanup(func() { _ = db.Close(); _ = raw.Close() })
	identitySQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "identity.sql"))
	if err != nil {
		t.Fatal(err)
	}
	jobsSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "jobs.sql"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "workspace.sql"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := migrations.NewRegistry(
		migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: string(identitySQL)}}},
		migrations.Fragment{Namespace: "jobs", Migrations: []migrations.Migration{{Sequence: 2, Name: "jobs_base", SQL: string(jobsSQL)}}},
		migrations.Fragment{Namespace: "workspace", Migrations: []migrations.Migration{{Sequence: 3, Name: "workspace_base", SQL: string(workspaceSQL)}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatal(err)
	}
	return db, raw
}
func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
