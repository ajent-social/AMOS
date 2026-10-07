package login

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	identityemail "github.com/ajent-social/amos/identity/email"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// The standard eleven-field runtime profile admits probe metadata, never admin
// credentials. Only the dedicated same-database login profile is accepted.
type loginRuntimeConfig struct {
	Host           string `json:"host"`
	Port           uint16 `json:"port"`
	Database       string `json:"database"`
	User           string `json:"user"`
	Password       string `json:"password"`
	CAPath         string `json:"ca_path"`
	WrongHost      string `json:"wrong_host"`
	DMLTable       string `json:"dml_table"`
	LedgerTable    string `json:"ledger_table"`
	PrivilegedRole string `json:"privileged_role"`
	OwnerRole      string `json:"owner_role"`
}

func decodeLoginRuntimeConfig(r io.Reader) (loginRuntimeConfig, error) {
	const invalid = "invalid runtime config"
	raw, err := io.ReadAll(io.LimitReader(r, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return loginRuntimeConfig{}, errors.New(invalid)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return loginRuntimeConfig{}, errors.New(invalid)
	}
	fields := make(map[string]json.RawMessage)
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return loginRuntimeConfig{}, errors.New(invalid)
		}
		key, ok := token.(string)
		if !ok {
			return loginRuntimeConfig{}, errors.New(invalid)
		}
		if _, exists := fields[key]; exists {
			return loginRuntimeConfig{}, errors.New(invalid)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return loginRuntimeConfig{}, errors.New(invalid)
		}
		fields[key] = value
	}
	if _, err := dec.Token(); err != nil {
		return loginRuntimeConfig{}, errors.New(invalid)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return loginRuntimeConfig{}, errors.New(invalid)
	}
	for _, key := range []string{"host", "port", "database", "user", "password", "ca_path", "wrong_host", "dml_table", "ledger_table", "privileged_role", "owner_role"} {
		if _, ok := fields[key]; !ok {
			return loginRuntimeConfig{}, errors.New(invalid)
		}
	}
	if len(fields) != 11 {
		return loginRuntimeConfig{}, errors.New(invalid)
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return loginRuntimeConfig{}, errors.New(invalid)
	}
	strict := json.NewDecoder(strings.NewReader(string(data)))
	strict.DisallowUnknownFields()
	var cfg loginRuntimeConfig
	if err := strict.Decode(&cfg); err != nil {
		return loginRuntimeConfig{}, errors.New(invalid)
	}
	if cfg.Host == "" || cfg.Port == 0 || cfg.Database == "" || cfg.User == "" || cfg.Password == "" || cfg.CAPath == "" || cfg.WrongHost == "" || (cfg.DMLTable != "identity_persons" && cfg.DMLTable != "public.identity_persons") || cfg.LedgerTable == "" || cfg.PrivilegedRole == "" || cfg.OwnerRole == "" {
		return loginRuntimeConfig{}, errors.New(invalid)
	}
	return cfg, nil
}

func TestLoginRuntimeConfigFormat(t *testing.T) {
	const standard = `{"host":"fixture.invalid","port":5432,"database":"fixture","user":"runtime","password":"synthetic","ca_path":"fixture-ca.pem","wrong_host":"wrong.invalid","dml_table":"identity_persons","ledger_table":"fixture_migration_ledger","privileged_role":"fixture_admin","owner_role":"fixture_owner"}`
	cfg, e := decodeLoginRuntimeConfig(strings.NewReader(standard))
	if e != nil || cfg.Host != "fixture.invalid" || cfg.Port != 5432 || cfg.OwnerRole != "fixture_owner" {
		t.Fatal("standard eleven-field runtime config rejected")
	}
	if _, e := decodeLoginRuntimeConfig(strings.NewReader(strings.Replace(standard, `"identity_persons"`, `"public.identity_persons"`, 1))); e != nil {
		t.Fatal("qualified standard DML metadata rejected")
	}
	for _, input := range []string{standard + `{}`, standard + strings.Repeat(" ", 2<<20), standard[:len(standard)-1] + `,"unexpected":true}`, `{"port":"invalid"}`, `{}`, `null`, strings.Replace(standard, `"port":5432`, `"port":5432,"port":5432`, 1), strings.Replace(standard, `"user":"runtime"`, `"user":null`, 1), strings.Replace(standard, `"port":5432`, `"port":0`, 1), strings.Replace(standard, `"host"`, `"HOST"`, 1)} {
		if got, e := decodeLoginRuntimeConfig(strings.NewReader(input)); e == nil || got != (loginRuntimeConfig{}) {
			t.Fatal("invalid config accepted or partially disclosed")
		}
	}
}

func loginRuntimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_LOGIN_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required same-database identity/workspace/jobs/material/session TLS PostgreSQL profile absent: set AMOS_LOGIN_RUNTIME_TEST_CONFIG")
	}
	f, e := os.Open(path)
	if e != nil {
		t.Fatal("required login runtime config unavailable")
	}
	defer func() {
		if e := f.Close(); e != nil {
			t.Error("runtime config close failed")
		}
	}()
	cfg, e := decodeLoginRuntimeConfig(f)
	if e != nil {
		t.Fatal("required login runtime JSON invalid")
	}
	roots, e := os.ReadFile(cfg.CAPath)
	if e != nil {
		t.Fatal("required login runtime CA unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, e := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: cfg.Host, Port: cfg.Port, Database: cfg.Database, User: cfg.User, Password: cfg.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if e != nil {
		t.Fatal("required login runtime TLS connection failed")
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error("runtime close failed")
		}
	})
	if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		var roleOK bool
		if e := tx.QueryRowContext(ctx, `SELECT NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&roleOK); e != nil {
			return e
		}
		if !roleOK {
			return errors.New("runtime role required")
		}
		for _, table := range []string{"identity_persons", "identity_emails", "identity_credentials", "identity_challenges", "identity_sessions", "workspaces", "amos_jobs", "email_delivery_material"} {
			var tableOK bool
			if e := tx.QueryRowContext(ctx, `SELECT COALESCE(to_regclass($1)=to_regclass('public.' || $1),false) AND has_table_privilege(current_user,'public.' || $1,'SELECT,INSERT,UPDATE,DELETE') AND NOT pg_has_role(current_user,(SELECT relowner FROM pg_class WHERE oid=to_regclass('public.' || $1)),'USAGE')`, table).Scan(&tableOK); e != nil {
				return e
			}
			if !tableOK {
				return errors.New("precreated runtime table required")
			}
		}
		for _, table := range []string{"workspace_role_versions", "workspace_memberships"} {
			var ok bool
			if e := tx.QueryRowContext(ctx, `SELECT COALESCE(to_regclass($1)=to_regclass('public.' || $1),false) AND has_table_privilege(current_user,'public.' || $1,'SELECT') AND NOT pg_has_role(current_user,(SELECT relowner FROM pg_class WHERE oid=to_regclass('public.' || $1)),'USAGE')`, table).Scan(&ok); e != nil {
				return e
			}
			if !ok {
				return errors.New("workspace trigger read prerequisite absent")
			}
		}
		return nil
	}); e != nil {
		t.Fatal("required precreated same-database identity/workspace/jobs/material/session schema or runtime-only DML role absent")
	}
	return db
}

// The operator precreates identity.sql, jobs.sql, workspace.sql and
// email-material.sql in ONE database. Runtime consumers perform DML only.
// Synthetic local password policy below does not qualify production policy.
func TestLoginRuntimeRequiredService(t *testing.T) {
	db := loginRuntimeDB(t)
	cfg := loginTxConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if e := db.WithTx(c, nil, func(tx *sql.Tx) error {
			for _, query := range []string{
				`DELETE FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM amos_jobs WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM identity_sessions WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM identity_challenges WHERE person_id IN (SELECT id FROM identity_persons WHERE installation_id=$1 AND application_id=$2)`,
				`DELETE FROM identity_credentials WHERE person_id IN (SELECT id FROM identity_persons WHERE installation_id=$1 AND application_id=$2)`,
				`DELETE FROM workspaces WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM identity_emails WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM identity_persons WHERE installation_id=$1 AND application_id=$2`,
			} {
				if _, e := tx.ExecContext(c, query, cfg.InstallationID, cfg.ApplicationID); e != nil {
					return e
				}
			}
			return nil
		}); e != nil {
			t.Error("exact-owned login DML cleanup failed")
		}
	})
	hasher, e := password.New(list{}, budget{}, 1)
	if e != nil {
		t.Fatal(e)
	}
	cfg.Passwords = hasher
	const origin = "https://app.example.test"
	materials, e := materialstore.NewWithTxRunner(db, materialstore.TxConfig{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, ActiveKeyID: "test", Keys: map[string][]byte{"test": make([]byte, 32)}, ApplicationOrigin: origin})
	if e != nil {
		t.Fatal(e)
	}
	renderer, e := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: origin, MaxBodyBytes: 4096})
	if e != nil {
		t.Fatal(e)
	}
	outbox, e := sqlstore.NewWithTx(db, sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
	if e != nil {
		t.Fatal(e)
	}
	mailer, e := identityemail.NewWithTxRunner(db, outbox, renderer, materials, identityemail.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, ApplicationOrigin: origin, ChallengeLifetime: identityemail.DefaultChallengeLifetime})
	if e != nil {
		t.Fatal(e)
	}
	cfg.Email = mailer
	makeService := func(loginDB, sessionDB TxRunner) *Service {
		t.Helper()
		sessions, e := session.NewWithTxRunner(sessionDB, session.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, AllowedOrigins: []string{origin}, CookieSecure: true})
		if e != nil {
			t.Fatal(e)
		}
		c := cfg
		c.Sessions = sessions
		svc, e := NewWithTxRunner(loginDB, c)
		if e != nil {
			t.Fatal(e)
		}
		return svc
	}
	svc := makeService(db, db)
	const secret = "a sufficiently long password"
	post := func(c context.Context, s *Service, path, address, pw string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		body, e := json.Marshal(map[string]string{"email": address, "password": pw})
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body))).WithContext(c)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	// Eight scoped counts prove that the caller's transaction includes every
	// registration participant, and that session results follow durable commit.
	countsTx := func(c context.Context, tx *sql.Tx) ([8]int, error) {
		var n [8]int
		e := tx.QueryRowContext(c, `SELECT
 (SELECT count(*) FROM identity_persons WHERE installation_id=$1 AND application_id=$2),
 (SELECT count(*) FROM identity_emails WHERE installation_id=$1 AND application_id=$2),
 (SELECT count(*) FROM identity_credentials c JOIN identity_persons p ON p.id=c.person_id WHERE p.installation_id=$1 AND p.application_id=$2),
 (SELECT count(*) FROM identity_challenges c JOIN identity_persons p ON p.id=c.person_id WHERE p.installation_id=$1 AND p.application_id=$2),
 (SELECT count(*) FROM workspaces WHERE installation_id=$1 AND application_id=$2),
 (SELECT count(*) FROM amos_jobs WHERE installation_id=$1 AND application_id=$2),
 (SELECT count(*) FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2),
 (SELECT count(*) FROM identity_sessions WHERE installation_id=$1 AND application_id=$2)`, cfg.InstallationID, cfg.ApplicationID).Scan(&n[0], &n[1], &n[2], &n[3], &n[4], &n[5], &n[6], &n[7])
		return n, e
	}
	counts := func() [8]int {
		t.Helper()
		var n [8]int
		if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error { var e error; n, e = countsTx(ctx, tx); return e }); e != nil {
			t.Fatal("runtime login row counts failed")
		}
		return n
	}
	requireUnavailable := func(w *httptest.ResponseRecorder) {
		t.Helper()
		var body response
		if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
			t.Fatal("invalid failure response")
		}
		if w.Code != http.StatusServiceUnavailable || body.Code != "dependency.unavailable" || w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "csrf_token") || strings.Contains(w.Body.String(), "authenticated") || strings.Contains(w.Body.String(), "registration_accepted") {
			t.Fatal("transaction failure exposed acknowledgement or credentials")
		}
	}
	address := "person-" + newID(t).String() + "@example.test"
	w := post(ctx, svc, "/signup", address, secret, nil)
	if w.Code != http.StatusAccepted || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("actual registration failed or issued a session")
	}
	if counts() != ([8]int{1, 1, 1, 1, 1, 1, 1, 0}) {
		t.Fatal("registration did not commit all participants atomically")
	}
	var state, kind, workspaceState string
	if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT p.state,w.kind,w.state FROM identity_persons p JOIN workspaces w ON w.personal_owner_id=p.id AND w.installation_id=p.installation_id AND w.application_id=p.application_id WHERE p.installation_id=$1 AND p.application_id=$2`, cfg.InstallationID, cfg.ApplicationID).Scan(&state, &kind, &workspaceState)
	}); e != nil || state != "pending_verification" || kind != "personal" || workspaceState != "active" {
		t.Fatal("registration identity/personal workspace binding changed")
	}
	before := counts()
	if w := post(ctx, svc, "/signup", address, secret, nil); w.Code != http.StatusAccepted || w.Header().Get("Set-Cookie") != "" || counts() != before {
		t.Fatal("duplicate registration changed durable state")
	}
	var generic response
	for _, tc := range []struct{ address, password string }{{address, secret}, {address, "incorrect password long"}, {"missing@example.test", secret}} {
		w := post(ctx, svc, "/auth", tc.address, tc.password, nil)
		var body response
		if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
			t.Fatal(e)
		}
		if w.Code != http.StatusUnauthorized || w.Header().Get("Set-Cookie") != "" || body.Code != "auth.unauthenticated" {
			t.Fatal("unverified/wrong/unknown sign-in did not fail closed")
		}
		if generic.Code != "" && (body.Code != generic.Code || body.Message != generic.Message) {
			t.Fatal("sign-in disclosed account state")
		}
		generic = body
	}
	if counts() != before {
		t.Fatal("denied sign-in created rows")
	}
	// Resolve actual protected material from the opaque committed job, then use
	// the real confirmation service; no test-only activation bypass is used.
	var payload string
	if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT payload::text FROM amos_jobs WHERE installation_id=$1 AND application_id=$2`, cfg.InstallationID, cfg.ApplicationID).Scan(&payload)
	}); e != nil {
		t.Fatal("read verification job failed")
	}
	var intent deliveryemail.Request
	if e := json.Unmarshal([]byte(payload), &intent); e != nil || intent.Template != deliveryemail.TemplateVerifyEmail {
		t.Fatal("invalid verification intent")
	}
	material, e := materials.ResolveForTemplate(ctx, intent.MaterialRef, deliveryemail.TemplateVerifyEmail)
	if e != nil {
		t.Fatal("resolve actual protected verification material failed")
	}
	action, e := url.Parse(material.ActionURL)
	if e != nil {
		t.Fatal("invalid verification action")
	}
	challenge, e := uuid.Parse(action.Query().Get("challenge"))
	if e != nil {
		t.Fatal("invalid verification challenge")
	}
	token := action.Query().Get("token")
	for _, value := range []string{material.Recipient, material.ActionURL, token} {
		if value == "" || strings.Contains(payload, value) {
			t.Fatal("job disclosed protected material")
		}
	}
	if e := mailer.Confirm(ctx, challenge, token); e != nil {
		t.Fatal("actual email confirmation failed")
	}
	good := post(ctx, svc, "/auth", address, secret, nil)
	var authenticated struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrf_token"`
	}
	if e := json.Unmarshal(good.Body.Bytes(), &authenticated); e != nil || good.Code != http.StatusOK || !authenticated.Authenticated || authenticated.CSRFToken == "" {
		t.Fatal("verified password sign-in failed")
	}
	cookies := good.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == "" || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("committed sign-in cookie absent or weakened")
	}
	if got := counts(); got != ([8]int{1, 1, 1, 1, 1, 1, 1, 1}) {
		t.Fatal("session did not commit")
	}
	rotated := post(ctx, svc, "/auth", address, secret, cookies[0])
	if rotated.Code != http.StatusOK || len(rotated.Result().Cookies()) != 1 {
		t.Fatal("session rotation failed")
	}
	var active int
	if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_sessions WHERE installation_id=$1 AND application_id=$2 AND revoked_at IS NULL`, cfg.InstallationID, cfg.ApplicationID).Scan(&active)
	}); e != nil || active != 1 {
		t.Fatal("session rotation did not preserve one active session")
	}

	// Each injected boundary uses RuntimeDB and the real callback. The commit
	// case rolls back the actual transaction then returns nil, forcing Commit to
	// fail with ErrTransaction under a live context, rather than callback error.
	for _, path := range []string{"/signup", "/auth"} {
		for _, mode := range []string{"rollback", "cancellation", "commit"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				before := counts()
				want := before
				if path == "/signup" {
					for i := 0; i < 7; i++ {
						want[i]++
					}
				} else {
					want[7]++
				}
				requestCtx, stop := context.WithCancel(ctx)
				defer stop()
				reached := false
				var transactionErr error
				rollback := errors.New("requested callback rollback")
				runner := loginRunnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
					if c != requestCtx || o != nil {
						return errors.New("transaction context or options changed")
					}
					transactionErr = db.WithTx(c, o, func(tx *sql.Tx) error {
						if e := fn(tx); e != nil {
							return e
						}
						written, e := countsTx(c, tx)
						if e != nil {
							return e
						}
						if written != want {
							return errors.New("failure injection requires all callback writes")
						}
						switch mode {
						case "rollback":
							reached = true
							return rollback
						case "cancellation":
							reached = true
							stop()
							return c.Err()
						case "commit":
							if e := tx.Rollback(); e != nil {
								return e
							}
							reached = true
							return nil
						default:
							return errors.New("unknown failure mode")
						}
					})
					return transactionErr
				})
				var failing *Service
				target := address
				if path == "/signup" {
					failing = makeService(runner, db)
					target = "rollback-" + newID(t).String() + "@example.test"
				} else {
					failing = makeService(db, runner)
				}
				w := post(requestCtx, failing, path, target, secret, nil)
				if !reached {
					t.Fatal("failure injection did not reach successful SQL callback writes")
				}
				if mode == "commit" && (requestCtx.Err() != nil || transactionErr != storage.ErrTransaction) {
					t.Fatal("real commit-error branch not reached with live context")
				}
				if mode == "rollback" && !errors.Is(transactionErr, rollback) {
					t.Fatal("callback rollback error lost")
				}
				if mode == "cancellation" && (requestCtx.Err() != context.Canceled || !errors.Is(transactionErr, context.Canceled)) {
					t.Fatal("cancellation path not reached")
				}
				requireUnavailable(w)
				if counts() != before {
					t.Fatal("failed transaction left partial registration/session rows")
				}
			})
		}
	}
	if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error { var one int; return tx.QueryRowContext(ctx, `SELECT 1`).Scan(&one) }); e != nil {
		t.Fatal("caller-owned runtime unusable")
	}
}
