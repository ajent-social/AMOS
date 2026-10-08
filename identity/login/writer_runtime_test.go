package login

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	"github.com/ajent-social/amos/identity"
	identityemail "github.com/ajent-social/amos/identity/email"
	"github.com/ajent-social/amos/identity/magiclink"
	"github.com/ajent-social/amos/identity/mfa"
	mfavault "github.com/ajent-social/amos/identity/mfa/vault"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/primaryproof"
	"github.com/ajent-social/amos/identity/protection"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

// This source suite requires a separately provisioned runtime-only TLS profile
// with W1, workspace, D, assurance, MFA, protection and magic-binding fragments.
// It creates no schema and is isolated from the immutable Legacy process profile.
// Synthetic policy/key material qualifies neither production policy nor providers.
func TestNativeWriterRuntimeRequiredService(t *testing.T) {
	if os.Getenv("AMOS_LOGIN_RUNTIME_TEST_CONFIG") == "" {
		t.Fatal("required native writer TLS runtime fixture absent: AMOS_LOGIN_RUNTIME_TEST_CONFIG")
	}
	if os.Getenv("AMOS_NATIVE_WRITER_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeWriterRuntimeRequiredService$", "-test.v")
		cmd.Env = append(os.Environ(), "AMOS_NATIVE_WRITER_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("native writer child failed: %v\n%s", err, output)
		}
		return
	}
	db := loginRuntimeDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg := loginTxConfig(t)
	const origin = "https://native.example.test"
	const phrase = "a native guarded password for writer qualification"
	root, err := aw.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = aw.ActivateW1(root); err != nil {
		t.Fatal(err)
	}
	// Fail visibly for an old fixture; its ordinary identity schema cannot qualify
	// browser-bound magic links or MFA. Do not silently skip these scenarios.
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		for _, q := range []string{`SELECT browser_binding_digest FROM public.identity_challenges WHERE false`, `SELECT assurance_level FROM public.identity_sessions WHERE false`, `SELECT seed_ciphertext FROM public.identity_totp_factors WHERE false`, `SELECT id FROM public.identity_writer_gate WHERE id=1`} {
			rows, e := tx.QueryContext(ctx, q)
			if e != nil {
				return e
			}
			if e = rows.Close(); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatal("required precreated W1/magic-binding/assurance/MFA fixture unavailable")
	}
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		err := db.WithTx(c, nil, func(tx *sql.Tx) error {
			for _, q := range []string{`DELETE FROM public.email_delivery_material WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM public.amos_jobs WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM public.identity_totp_factors WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM public.identity_sessions WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM public.identity_challenges WHERE person_id IN(SELECT id FROM public.identity_persons WHERE installation_id=$1 AND application_id=$2)`, `DELETE FROM public.identity_credentials WHERE person_id IN(SELECT id FROM public.identity_persons WHERE installation_id=$1 AND application_id=$2)`, `DELETE FROM public.workspaces WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM public.identity_emails WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM public.identity_persons WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM public.identity_auth_limits WHERE installation_id=$1 AND application_id=$2`} {
				if _, e := tx.ExecContext(c, q, cfg.InstallationID, cfg.ApplicationID); e != nil {
					return e
				}
			}
			return nil
		})
		if err != nil {
			t.Error("exact-owned native writer row cleanup failed")
		}
	})
	sessions, err := session.NewWithWriter(root, session.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, AllowedOrigins: []string{origin}, CookieSecure: true, PersistAssurance: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Sessions = sessions
	limiter, err := protection.NewWithTxRunner(db, protection.TxConfig{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, Key: bytes.Repeat([]byte{0x42}, 32), Window: time.Minute, IPLimit: 100, AccountLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Passwords, err = password.New(list{}, limiter.PasswordBudget(), 2)
	if err != nil {
		t.Fatal(err)
	}
	materialCfg := materialstore.TxConfig{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, ActiveKeyID: "synthetic", Keys: map[string][]byte{"synthetic": bytes.Repeat([]byte{0x63}, 32)}, ApplicationOrigin: origin}
	materials, err := materialstore.NewWriter(materialCfg)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: origin, MaxBodyBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := sqlstore.NewTxWriter(sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Email, err = identityemail.NewWithWriter(root, outbox, renderer, materials, identityemail.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, ApplicationOrigin: origin, ChallengeLifetime: DefaultChallengeLifetime})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewWithWriter(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	guard := protection.Guard{Limiter: limiter, Sessions: sessions}
	signup, err := guard.PublicJSON(protection.Signup, service.Handler())
	if err != nil {
		t.Fatal(err)
	}
	signin, err := guard.PublicJSON(protection.Signin, service.Handler())
	if err != nil {
		t.Fatal(err)
	}
	post := func(t *testing.T, handler http.Handler, path string, payload any, cookies []*http.Cookie, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		body, e := json.Marshal(payload)
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest(http.MethodPost, origin+path, bytes.NewReader(body)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.RemoteAddr = "192.0.2.25:1234"
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	expect := func(t *testing.T, w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("native entry status=%d want=%d", w.Code, status)
		}
	}
	count := func(t *testing.T, table string) int {
		t.Helper()
		var n int
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT count(*) FROM public.`+table+` WHERE installation_id=$1 AND application_id=$2`, cfg.InstallationID, cfg.ApplicationID).Scan(&n)
		}); e != nil {
			t.Fatal("native scoped count failed")
		}
		return n
	}
	address := "native-" + cfg.InstallationID.String() + "@example.test"
	t.Run("registration failed delivery rolls back every participant", func(t *testing.T) {
		wrong := materialCfg
		wrong.EnvironmentID = newID(t)
		badMaterial, e := materialstore.NewWriter(wrong)
		if e != nil {
			t.Fatal(e)
		}
		badEmail, e := identityemail.NewWithWriter(root, outbox, renderer, badMaterial, identityemail.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, ApplicationOrigin: origin, ChallengeLifetime: DefaultChallengeLifetime})
		if e != nil {
			t.Fatal(e)
		}
		badCfg := cfg
		badCfg.Email = badEmail
		badService, e := NewWithWriter(root, badCfg)
		if e != nil {
			t.Fatal(e)
		}
		badHandler, e := guard.PublicJSON(protection.Signup, badService.Handler())
		if e != nil {
			t.Fatal(e)
		}
		tables := []string{"identity_persons", "identity_emails", "workspaces", "identity_sessions", "email_delivery_material", "amos_jobs"}
		before := make([]int, len(tables))
		for i, table := range tables {
			before[i] = count(t, table)
		}
		w := post(t, badHandler, "/signup", registrationRequest{"rollback-" + address, phrase}, nil, "")
		expect(t, w, http.StatusServiceUnavailable)
		for i, table := range tables {
			if count(t, table) != before[i] {
				t.Fatal("failed D participant leaked registration rows")
			}
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("failed registration published cookie")
		}
	})

	t.Run("registration composes account workspace delivery without session", func(t *testing.T) {
		w := post(t, signup, "/signup", registrationRequest{address, phrase}, nil, "")
		expect(t, w, http.StatusAccepted)
		if len(w.Result().Cookies()) != 0 || count(t, "identity_persons") != 1 || count(t, "workspaces") != 1 || count(t, "amos_jobs") != 1 || count(t, "email_delivery_material") != 1 || count(t, "identity_sessions") != 0 {
			t.Fatal("registration was not atomic pending composition")
		}
	})
	t.Run("duplicate registration stays generic without new rows", func(t *testing.T) {
		people, workspaces, jobsBefore, materialsBefore := count(t, "identity_persons"), count(t, "workspaces"), count(t, "amos_jobs"), count(t, "email_delivery_material")
		w := post(t, signup, "/signup", registrationRequest{address, phrase}, nil, "")
		expect(t, w, http.StatusAccepted)
		if count(t, "identity_persons") != people || count(t, "workspaces") != workspaces || count(t, "amos_jobs") != jobsBefore || count(t, "email_delivery_material") != materialsBefore || len(w.Result().Cookies()) != 0 {
			t.Fatal("duplicate registration changed pending graph")
		}
	})

	t.Run("pending and missing budget cannot issue", func(t *testing.T) {
		w := post(t, signin, "/auth", signInRequest{address, phrase}, nil, "")
		expect(t, w, http.StatusUnauthorized)
		w = post(t, service.Handler(), "/auth", signInRequest{address, phrase}, nil, "")
		expect(t, w, http.StatusServiceUnavailable)
		if count(t, "identity_sessions") != 0 {
			t.Fatal("pending or unguarded request issued")
		}
	})
	// Fixture-only activation isolates native sign-in from the separately owned
	// email-confirm producer; it is not email confirmation qualification.
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `UPDATE public.identity_emails SET verified_at=pg_catalog.clock_timestamp() WHERE installation_id=$1 AND application_id=$2`, cfg.InstallationID, cfg.ApplicationID); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `UPDATE public.identity_persons SET state='active' WHERE installation_id=$1 AND application_id=$2`, cfg.InstallationID, cfg.ApplicationID)
		return e
	}); err != nil {
		t.Fatal("synthetic activation failed")
	}
	var cookie *http.Cookie
	var csrf string
	capture := func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		var body struct {
			CSRF string `json:"csrf_token"`
		}
		if json.Unmarshal(w.Body.Bytes(), &body) != nil {
			t.Fatal("committed native session response invalid")
		}
		if body.CSRF == "" {
			body.CSRF = w.Header().Get("X-CSRF-Token")
		}
		if body.CSRF == "" {
			t.Fatal("committed native session missing CSRF")
		}
		for _, c := range w.Result().Cookies() {
			if strings.Contains(c.Name, "amos_session") {
				cookie = c
			}
		}
		if cookie == nil {
			t.Fatal("committed native session missing cookie")
		}
		csrf = body.CSRF
	}
	t.Run("signin wrong password and original bounds", func(t *testing.T) {
		w := post(t, signin, "/auth", signInRequest{address, "incorrect password"}, nil, "")
		expect(t, w, http.StatusUnauthorized)
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("wrong password disclosed cookie")
		}
		w = post(t, signin, "/auth", signInRequest{address, phrase}, nil, "")
		expect(t, w, http.StatusOK)
		capture(t, w)
		var correct bool
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT bool_and(idle_expires_at=authenticated_at+interval '30 minutes' AND expires_at=authenticated_at+interval '12 hours') FROM public.identity_sessions WHERE installation_id=$1 AND application_id=$2`, cfg.InstallationID, cfg.ApplicationID).Scan(&correct)
		}); e != nil || !correct {
			t.Fatal("native session refreshed original V bounds")
		}
	})
	if cookie == nil {
		t.Fatal("native prerequisite signin failed")
	}
	policy := nativeWriterPolicy{installation: cfg.InstallationID, application: cfg.ApplicationID}
	primary, err := primaryproof.New(cfg.Passwords)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := mfavault.NewKeyring("synthetic", map[string][]byte{"synthetic": bytes.Repeat([]byte{0x55}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	factors, err := mfa.NewWithWriter(root, mfa.TxConfig{Vault: vault, Primary: primary, Sessions: sessions, Policy: policy, Issuer: "Native test"})
	if err != nil {
		t.Fatal(err)
	}
	mfaGuard, err := guard.CookieMutation(protection.MFA, factors.Handler())
	if err != nil {
		t.Fatal(err)
	}
	mfaHTTP := sessions.Middleware(mfaGuard)
	var enrollment mfa.Enrollment
	t.Run("MFA begin and read-only status", func(t *testing.T) {
		before := count(t, "identity_sessions")
		w := post(t, mfaHTTP, "/account/mfa/totp/enroll", map[string]string{"current_password": phrase}, []*http.Cookie{cookie}, csrf)
		expect(t, w, http.StatusCreated)
		if json.Unmarshal(w.Body.Bytes(), &enrollment) != nil || enrollment.Seed == "" || count(t, "identity_sessions") != before {
			t.Fatal("enrollment missing or issued a session")
		}
		r := httptest.NewRequest(http.MethodGet, origin+"/account/mfa/totp", nil).WithContext(ctx)
		r.AddCookie(cookie)
		rec := httptest.NewRecorder()
		sessions.Middleware(factors.Handler()).ServeHTTP(rec, r)
		expect(t, rec, http.StatusOK)
	})
	t.Run("factor malformed participant is terminal before SQL", func(t *testing.T) {
		if enrollment.FactorID == uuid.Nil {
			t.Fatal("enrollment prerequisite failed")
		}
		var person uuid.UUID
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT person_id FROM public.identity_totp_factors WHERE id=$1`, enrollment.FactorID).Scan(&person)
		}); e != nil {
			t.Fatal("factor owner unavailable")
		}
		scope := mfa.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, PersonID: person}
		for _, invalidContext := range []bool{true, false} {
			retained := false
			_, e := root.Run(ctx, func(c context.Context, a *aw.Attempt) aw.Outcome {
				plan, e := aw.NewPlan(aw.Realm{Installation: cfg.InstallationID, Application: cfg.ApplicationID, Environment: cfg.EnvironmentID}, []aw.Row{{Table: aw.Persons, ID: person, Access: aw.ExistingUpdate}, {Table: aw.Factors, ID: enrollment.FactorID, Access: aw.ExistingUpdate}}, nil)
				if e != nil {
					return loginFinish(a, aw.UnavailableRollback)
				}
				if e = a.SealPlan(plan); e != nil {
					return loginFinish(a, aw.UnavailableRollback)
				}
				if e = acquire(c, a, aw.P, aw.C, aw.H, aw.S, aw.W); e != nil {
					return loginFinish(a, aw.UnavailableRollback)
				}
				factorStore, e := mfa.NewWriter(a)
				if e != nil {
					return loginFinish(a, aw.UnavailableRollback)
				}
				badContext, badScope := c, scope
				if invalidContext {
					badContext = nil
				} else {
					badScope.PersonID = uuid.Nil
				}
				if e = factorStore.RecordFailure(badContext, badScope, enrollment.FactorID, time.Now()); e == nil {
					retained = true
					return loginFinish(a, aw.UnavailableRollback)
				}
				if _, e = a.Binding(); e == nil {
					retained = true
					return loginFinish(a, aw.DeniedRollback)
				}
				return aw.UnavailableRollback // failed participant already finished
			})
			if retained || !errors.Is(e, aw.ErrUnavailable) {
				t.Fatal("ignored malformed participant left a usable attempt")
			}
		}
	})

	t.Run("MFA final policy denial rolls counter back", func(t *testing.T) {
		if enrollment.Seed == "" {
			t.Fatal("enrollment prerequisite failed")
		}
		denial := &nativeWriterFinalDenial{base: policy}
		deniedService, e := mfa.NewWithWriter(root, mfa.TxConfig{Vault: vault, Primary: primary, Sessions: sessions, Policy: denial, Issuer: "Native test"})
		if e != nil {
			t.Fatal(e)
		}
		deniedGuard, e := guard.CookieMutation(protection.MFA, deniedService.Handler())
		if e != nil {
			t.Fatal(e)
		}
		bad := nativeUnusedCode(t, ctx, db, enrollment.Seed)
		var before, after string
		snapshot := func(dst *string) {
			t.Helper()
			if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				return tx.QueryRowContext(ctx, `SELECT row_to_json(f)::text FROM public.identity_totp_factors f WHERE id=$1`, enrollment.FactorID).Scan(dst)
			}); e != nil {
				t.Fatal("factor snapshot unavailable")
			}
		}
		snapshot(&before)
		sessionsBefore := count(t, "identity_sessions")
		w := post(t, sessions.Middleware(deniedGuard), "/account/mfa/totp/confirm", map[string]any{"factor_id": enrollment.FactorID, "current_password": phrase, "code": bad}, []*http.Cookie{cookie}, csrf)
		expect(t, w, http.StatusForbidden)
		snapshot(&after)
		if denial.calls != 2 || before != after || count(t, "identity_sessions") != sessionsBefore || len(w.Result().Cookies()) != 0 {
			t.Fatal("final denied factor request committed counter or issuance")
		}
	})

	t.Run("MFA bad-code counter and confirm", func(t *testing.T) {
		if enrollment.Seed == "" {
			t.Fatal("enrollment prerequisite failed")
		}
		var dbTime time.Time
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&dbTime)
		}); e != nil {
			t.Fatal(e)
		}
		code, e := totp.GenerateCode(enrollment.Seed, dbTime)
		if e != nil {
			t.Fatal(e)
		}
		bad := nativeUnusedCode(t, ctx, db, enrollment.Seed)
		before := count(t, "identity_sessions")
		w := post(t, mfaHTTP, "/account/mfa/totp/confirm", map[string]any{"factor_id": enrollment.FactorID, "current_password": phrase, "code": bad}, []*http.Cookie{cookie}, csrf)
		expect(t, w, http.StatusUnauthorized)
		if count(t, "identity_sessions") != before || len(w.Result().Cookies()) != 0 {
			t.Fatal("counter denial issued session")
		}
		var attempts int
		if e = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT failed_attempts FROM public.identity_totp_factors WHERE id=$1`, enrollment.FactorID).Scan(&attempts)
		}); e != nil || attempts != 1 {
			t.Fatal("bad code counter did not commit once")
		}
		w = post(t, mfaHTTP, "/account/mfa/totp/confirm", map[string]any{"factor_id": enrollment.FactorID, "current_password": phrase, "code": code}, []*http.Cookie{cookie}, csrf)
		expect(t, w, http.StatusOK)
		capture(t, w)
		var method string
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT authentication_method FROM public.identity_sessions WHERE installation_id=$1 AND application_id=$2 AND revoked_at IS NULL AND assurance_level='aal2'`, cfg.InstallationID, cfg.ApplicationID).Scan(&method)
		}); e != nil || method != "password+totp" {
			t.Fatal("native TOTP issuance lost its closed authentication method")
		}

	})
	t.Run("MFA replay retains only denial counter", func(t *testing.T) {
		var step int64
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT last_used_step FROM public.identity_totp_factors WHERE id=$1`, enrollment.FactorID).Scan(&step)
		}); e != nil {
			t.Fatal(e)
		}
		code, e := totp.GenerateCode(enrollment.Seed, time.Unix(step*30, 0))
		if e != nil {
			t.Fatal(e)
		}
		before := count(t, "identity_sessions")
		w := post(t, mfaHTTP, "/account/mfa/totp/challenge", map[string]string{"current_password": phrase, "code": code}, []*http.Cookie{cookie}, csrf)
		expect(t, w, http.StatusUnauthorized)
		if count(t, "identity_sessions") != before || len(w.Result().Cookies()) != 0 {
			t.Fatal("replayed factor issued session")
		}
	})
	t.Run("MFA policy receives assurance downgraded after admission", func(t *testing.T) {
		if cookie == nil {
			t.Fatal("native elevated session prerequisite absent")
		}
		var expiry time.Time
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `UPDATE public.identity_sessions SET assurance_expires_at=pg_catalog.clock_timestamp()+interval '500 milliseconds' WHERE installation_id=$1 AND application_id=$2 AND revoked_at IS NULL AND assurance_level='aal2' RETURNING assurance_expires_at`, cfg.InstallationID, cfg.ApplicationID).Scan(&expiry)
		}); e != nil {
			t.Fatal("synthetic bounded elevation unavailable")
		}
		assurancePolicy := &nativeWriterAssurancePolicy{base: policy}
		currentService, e := mfa.NewWithWriter(root, mfa.TxConfig{Vault: vault, Primary: primary, Sessions: sessions, Policy: assurancePolicy, Issuer: "Native test"})
		if e != nil {
			t.Fatal(e)
		}
		currentGuard, e := guard.CookieMutation(protection.MFA, currentService.Handler())
		if e != nil {
			t.Fatal(e)
		}
		admittedLevel := ""
		crossed := false
		afterAdmission := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := identity.PrincipalFromContext(r.Context())
			if !ok {
				t.Error("middleware private admission absent")
				return
			}
			admittedLevel = string(p.Assurance().Level())
			// Observe the actual database bound between committed middleware admission
			// and writer entry. No sleep or production clock override synchronizes it.
			for polls := 0; polls < 2000; polls++ {
				if e := db.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
					return tx.QueryRowContext(r.Context(), `SELECT pg_catalog.clock_timestamp()>=$1`, expiry).Scan(&crossed)
				}); e != nil {
					t.Error("database expiry observation unavailable")
					return
				}
				if crossed {
					break
				}
			}
			if !crossed {
				t.Error("database expiry boundary was not observed")
				return
			}
			currentGuard.ServeHTTP(w, r)
		})
		w := post(t, sessions.Middleware(afterAdmission), "/account/mfa/totp/challenge", map[string]string{"current_password": phrase, "code": "000000"}, []*http.Cookie{cookie}, csrf)
		expect(t, w, http.StatusForbidden)
		if admittedLevel != "aal2" || !crossed || assurancePolicy.observed != "aal1" || len(w.Result().Cookies()) != 0 {
			t.Fatal("policy consumed stale admitted assurance")
		}
	})

	magic, err := magiclink.NewWithWriter(root, outbox, materials, magiclink.Config{Renderer: renderer, Sessions: sessions, Policy: policy, InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, ApplicationOrigin: origin})
	if err != nil {
		t.Fatal(err)
	}
	magicRequest, err := guard.PublicJSON(protection.MagicLink, magic.RequestHandler())
	if err != nil {
		t.Fatal(err)
	}
	magicConfirm, err := guard.PublicJSON(protection.MagicLink, magic.ConfirmHandler())
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := materialstore.NewWithTxRunner(db, materialCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("magic request browser binding confirmation and replay", func(t *testing.T) {
		w := post(t, magicRequest, "/api/v1/identity/magic-links", map[string]string{"email": address}, nil, "")
		expect(t, w, http.StatusAccepted)
		var flow *http.Cookie
		for _, c := range w.Result().Cookies() {
			if strings.Contains(c.Name, "amos_magic_") {
				flow = c
			}
		}
		if flow == nil {
			t.Fatal("magic request missing committed browser binding")
		}
		var id uuid.UUID
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT h.id FROM public.identity_challenges h JOIN public.identity_persons p ON p.id=h.person_id WHERE p.installation_id=$1 AND p.application_id=$2 AND h.purpose='email_magic_link' ORDER BY h.created_at DESC LIMIT 1`, cfg.InstallationID, cfg.ApplicationID).Scan(&id)
		}); e != nil {
			t.Fatal("magic challenge missing")
		}
		material, e := resolver.ResolveForTemplate(ctx, deliveryemail.SecretReference("material:"+id.String()), deliveryemail.TemplateSignIn)
		if e != nil {
			t.Fatal("magic durable material unavailable")
		}
		link, e := url.Parse(material.ActionURL)
		if e != nil {
			t.Fatal(e)
		}
		payload := map[string]any{"challenge_id": id.String(), "token": link.Query().Get("token")}
		w = post(t, magicConfirm, "/magic-link/confirm", payload, nil, "")
		expect(t, w, http.StatusForbidden)
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("missing browser consent issued")
		}
		w = post(t, magicConfirm, "/magic-link/confirm", payload, []*http.Cookie{flow}, "")
		expect(t, w, http.StatusOK)
		w = post(t, magicConfirm, "/magic-link/confirm", payload, []*http.Cookie{flow}, "")
		expect(t, w, http.StatusUnauthorized)
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("consumed magic challenge issued")
		}
	})
}

type nativeWriterPolicy struct{ installation, application uuid.UUID }

func (p nativeWriterPolicy) Ready(ctx context.Context, tx *sql.Tx) error {
	var n int
	return tx.QueryRowContext(ctx, `SELECT count(*) FROM public.workspaces WHERE false`).Scan(&n)
}
func (p nativeWriterPolicy) allowed(ctx context.Context, tx *sql.Tx, person uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.identity_persons p JOIN public.workspaces w ON w.personal_owner_id=p.id WHERE p.id=$1 AND p.installation_id=$2 AND p.application_id=$3 AND p.state='active' AND w.state='active' AND w.kind='personal')`, person, p.installation, p.application).Scan(&ok)
	return ok, err
}
func (p nativeWriterPolicy) AuthorizeMFA(ctx context.Context, tx *sql.Tx, principal identity.Principal, _ string) error {
	ok, err := p.allowed(ctx, tx, principal.PersonID())
	if err != nil {
		return err
	}
	if !ok {
		return mfa.ErrDenied
	}
	return nil
}
func (p nativeWriterPolicy) AuthorizeMagicLink(ctx context.Context, tx *sql.Tx, person uuid.UUID) error {
	ok, err := p.allowed(ctx, tx, person)
	if err != nil {
		return err
	}
	if !ok {
		return magiclink.ErrPolicyDenied
	}
	return nil
}

// Native policy fault is test-only; production constructors accept no test hook.
type nativeWriterFinalDenial struct {
	base  nativeWriterPolicy
	calls int
}

func (p *nativeWriterFinalDenial) Ready(ctx context.Context, tx *sql.Tx) error {
	return p.base.Ready(ctx, tx)
}
func (p *nativeWriterFinalDenial) AuthorizeMFA(ctx context.Context, tx *sql.Tx, principal identity.Principal, action string) error {
	p.calls++
	if p.calls == 2 {
		return mfa.ErrDenied
	}
	return p.base.AuthorizeMFA(ctx, tx, principal, action)
}
func nativeUnusedCode(t *testing.T, ctx context.Context, db *storage.RuntimeDB, seed string) string {
	t.Helper()
	var at time.Time
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&at)
	}); err != nil {
		t.Fatal("TOTP clock unavailable")
	}
	// Cover adjacent windows beyond the five-second root ceiling; no accidental
	// six-digit collision can turn the intended bad-code case into success.
	used := make(map[string]bool)
	for offset := -2; offset <= 2; offset++ {
		code, err := totp.GenerateCode(seed, at.Add(time.Duration(offset)*30*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		used[code] = true
	}
	for n := 0; n < 1000000; n++ {
		candidate := fmt.Sprintf("%06d", n)
		if !used[candidate] {
			return candidate
		}
	}
	t.Fatal("no unused synthetic TOTP code")
	return ""
}

type nativeWriterAssurancePolicy struct {
	base     nativeWriterPolicy
	observed string
}

func (p *nativeWriterAssurancePolicy) Ready(ctx context.Context, tx *sql.Tx) error {
	return p.base.Ready(ctx, tx)
}
func (p *nativeWriterAssurancePolicy) AuthorizeMFA(ctx context.Context, tx *sql.Tx, principal identity.Principal, action string) error {
	p.observed = string(principal.Assurance().Level())
	if p.observed != "aal2" {
		return mfa.ErrDenied
	}
	return p.base.AuthorizeMFA(ctx, tx, principal, action)
}
