package mfa_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/mfa"
	mfavault "github.com/ajent-social/amos/identity/mfa/vault"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/password/policy"
	"github.com/ajent-social/amos/identity/primaryproof"
	"github.com/ajent-social/amos/identity/protection"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

// This suite requires an operator-precreated identity/assurance/MFA/protection schema and a
// runtime-only TLS connection. It never creates schema or reads admin credentials.
func mfaRuntimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_MFA_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required TLS PostgreSQL fixture absent: set AMOS_MFA_RUNTIME_TEST_CONFIG to operator-provided runtime JSON")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("required runtime fixture config cannot be opened")
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error("runtime fixture config close failed")
		}
	}()
	var cfg struct {
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
	dec := json.NewDecoder(io.LimitReader(f, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		t.Fatal("required runtime fixture JSON invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Fatal("required runtime fixture JSON has trailing data")
	}
	roots, err := os.ReadFile(cfg.CAPath)
	if err != nil {
		t.Fatal("required runtime fixture CA unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: cfg.Host, Port: cfg.Port, Database: cfg.Database, User: cfg.User, Password: cfg.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal("required runtime TLS PostgreSQL connection failed")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("runtime database close failed")
		}
	})
	return db
}

// This is local component evidence only. The policy below is synthetic; this
// suite does not qualify production policy, key custody, or external providers.
func TestMFARuntimeRequiredService(t *testing.T) {
	db := mfaRuntimeDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	id := func() uuid.UUID {
		t.Helper()
		v, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	scope := mfa.Scope{InstallationID: id(), ApplicationID: id(), EnvironmentID: id(), PersonID: id()}
	exec := func(query string, args ...any) {
		t.Helper()
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, query, args...); return err }); err != nil {
			t.Fatal("runtime fixture DML failed")
		}
	}
	// Register exact-owned cleanup before the first insertion, with a fresh context.
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := db.WithTx(c, nil, func(tx *sql.Tx) error {
			for _, q := range []string{
				`DELETE FROM identity_totp_factors WHERE person_id=$1`,
				`DELETE FROM identity_sessions WHERE person_id=$1`,
				`DELETE FROM identity_credentials WHERE person_id=$1`,
				`DELETE FROM identity_emails WHERE person_id=$1`,
				`DELETE FROM identity_persons WHERE id=$1`,
			} {
				if _, err := tx.ExecContext(c, q, scope.PersonID); err != nil {
					return err
				}
			}
			_, err := tx.ExecContext(c, `DELETE FROM identity_auth_limits WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3`, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID)
			return err
		}); err != nil {
			t.Error("exact-owned MFA fixture cleanup failed")
		}
	})
	blocklist, err := policy.New(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Only fixture provisioning uses a setup budget. Request verification below
	// uses the concrete durable guard and its one-use password budget.
	setupHasher, err := password.New(blocklist, mfaSetupBudget{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	const phrase = "MFA runtime fixture current password only"
	encoded, err := setupHasher.Hash(ctx, "fixture", phrase)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, scope.PersonID, scope.InstallationID, scope.ApplicationID)
	exec(`INSERT INTO identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key,verified_at) VALUES($1,$2,$3,$4,'mfa@example.test','mfa@example.test',transaction_timestamp())`, id(), scope.PersonID, scope.InstallationID, scope.ApplicationID)
	exec(`INSERT INTO identity_credentials(id,person_id,method,verifier_hash) VALUES($1,$2,'email_password',$3)`, id(), scope.PersonID, encoded)
	sessions, err := session.NewWithTxRunner(db, session.Config{InstallationID: scope.InstallationID, ApplicationID: scope.ApplicationID, EnvironmentID: scope.EnvironmentID, AllowedOrigins: []string{"https://mfa.example.test"}, CookieSecure: true, PersistAssurance: true})
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := protection.NewWithTxRunner(db, protection.TxConfig{InstallationID: scope.InstallationID, ApplicationID: scope.ApplicationID, EnvironmentID: scope.EnvironmentID, Key: bytes.Repeat([]byte{0x35}, 32), Window: time.Minute, IPLimit: 100, AccountLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := password.New(blocklist, limiter.PasswordBudget(), 1)
	if err != nil {
		t.Fatal(err)
	}
	primary, err := primaryproof.New(hasher)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := mfavault.NewKeyring("fixture-v1", map[string][]byte{"fixture-v1": bytes.Repeat([]byte{0x63}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	cfg := mfa.TxConfig{Vault: vault, Primary: primary, Policy: mfaRuntimePolicy{scope: scope}, Sessions: sessions, Issuer: "AMOS runtime fixture"}
	svc, err := mfa.NewWithTxRunner(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Second)
	proof, err := authproof.NewVerifiedCredential(scope.PersonID, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, 0, "email_password", now, "aal1", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	current, err := sessions.Issue(ctx, proof, "")
	if err != nil {
		t.Fatal("runtime initial session unavailable")
	}
	guard := protection.Guard{Limiter: limiter, Sessions: sessions}
	request := func(service *mfa.Service, path string, payload any, want int) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		guarded, err := guard.CookieMutation(protection.MFA, service.Handler())
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "https://mfa.example.test"+path, bytes.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://mfa.example.test")
		req.Header.Set("X-CSRF-Token", current.CSRFToken)
		req.AddCookie(current.Cookie)
		rec := httptest.NewRecorder()
		sessions.Middleware(guarded).ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("MFA runtime response status=%d want=%d", rec.Code, want)
		}
		if want != http.StatusOK && want != http.StatusCreated && (len(rec.Result().Cookies()) != 0 || rec.Header().Get("X-CSRF-Token") != "") {
			t.Fatal("failed request exposed session")
		}
		return rec
	}
	readFactor := func(factor uuid.UUID) mfa.Factor {
		t.Helper()
		var got mfa.Factor
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			st, err := mfa.NewStore(tx)
			if err != nil {
				return err
			}
			got, err = st.Find(ctx, scope, factor)
			return err
		}); err != nil {
			t.Fatal("runtime factor read failed")
		}
		return got
	}
	sessionCount := func() int {
		t.Helper()
		var count int
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_sessions WHERE person_id=$1`, scope.PersonID).Scan(&count)
		}); err != nil {
			t.Fatal("runtime session count failed")
		}
		return count
	}
	enroll := request(svc, "/account/mfa/totp/enroll", map[string]string{"current_password": phrase}, http.StatusCreated)
	var enrollment mfa.Enrollment
	if err := json.Unmarshal(enroll.Body.Bytes(), &enrollment); err != nil || enrollment.Seed == "" || enrollment.FactorID == uuid.Nil {
		t.Fatal("runtime enrollment missing")
	}
	factor := readFactor(enrollment.FactorID)
	if factor.State != mfa.FactorPending || factor.ExpiresAt == nil || !factor.ExpiresAt.Equal(enrollment.ExpiresAt) || bytes.Contains(factor.SeedCiphertext, []byte(enrollment.Seed)) {
		t.Fatal("pending factor binding or encryption incorrect")
	}
	code, err := totp.GenerateCode(enrollment.Seed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"factor_id": enrollment.FactorID, "current_password": phrase, "code": code}
	before := sessionCount()
	// Force caller rollback after successful factor/session writes. Then force
	// Commit on an already rolled-back SQL transaction. Both must suppress output.
	for _, mode := range []string{"callback rollback", "commit failure", "cancellation"} {
		t.Run(mode, func(t *testing.T) {
			callbacks := 0
			completed := 0
			runner := mfaRuntimeRunner(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
				child, stop := context.WithCancel(c)
				defer stop()
				return db.WithTx(child, o, func(tx *sql.Tx) error {
					callbacks++
					if err := fn(tx); err != nil {
						return err
					}
					var state string
					var count int
					if err := tx.QueryRowContext(c, `SELECT state FROM identity_totp_factors WHERE id=$1`, enrollment.FactorID).Scan(&state); err != nil {
						return err
					}
					if err := tx.QueryRowContext(c, `SELECT count(*) FROM identity_sessions WHERE person_id=$1`, scope.PersonID).Scan(&count); err != nil {
						return err
					}
					if state != "active" || count != before+1 {
						return errors.New("fixture transition was not staged")
					}
					completed++
					switch mode {
					case "commit failure":
						return tx.Rollback()
					case "cancellation":
						stop()
						return child.Err()
					default:
						return errors.New("intentional fixture rollback")
					}
				})
			})
			failing, err := mfa.NewWithTxRunner(runner, cfg)
			if err != nil {
				t.Fatal(err)
			}
			request(failing, "/account/mfa/totp/confirm", payload, http.StatusServiceUnavailable)
			if callbacks != 1 || completed != 1 || readFactor(enrollment.FactorID).State != mfa.FactorPending || sessionCount() != before {
				t.Fatal("failed transaction committed factor or session state")
			}
		})
	}
	// Refresh the code after the rollback checks; no wall-clock sleeps are needed.
	code, err = totp.GenerateCode(enrollment.Seed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	payload["code"] = code
	confirmed := request(svc, "/account/mfa/totp/confirm", payload, http.StatusOK)
	cookies := confirmed.Result().Cookies()
	if len(cookies) != 1 || confirmed.Header().Get("X-CSRF-Token") == "" || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("runtime confirmation session absent")
	}
	current.Cookie = cookies[0]
	current.CSRFToken = confirmed.Header().Get("X-CSRF-Token")
	factor = readFactor(enrollment.FactorID)
	if factor.State != mfa.FactorActive || factor.PendingSecurityEpoch != nil || factor.ExpiresAt != nil || factor.LastUsedStep < 0 || sessionCount() != before+1 {
		t.Fatal("factor and session did not commit together")
	}
	replay, err := totp.GenerateCode(enrollment.Seed, time.Unix(factor.LastUsedStep*30, 0))
	if err != nil {
		t.Fatal(err)
	}
	payload["code"] = replay
	request(svc, "/account/mfa/totp/challenge", payload, http.StatusUnauthorized)
	if sessionCount() != before+1 || readFactor(enrollment.FactorID).LastUsedStep != factor.LastUsedStep {
		t.Fatal("replay issued session or changed accepted step")
	}
	// Current password failure must not consume a factor or issue a session.
	payload["current_password"] = "incorrect runtime fixture current password"
	request(svc, "/account/mfa/totp/challenge", payload, http.StatusUnauthorized)
	if sessionCount() != before+1 {
		t.Fatal("wrong password issued session")
	}
	var level identity.AssuranceLevel
	check := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := identity.PrincipalFromContext(r.Context())
		if !ok {
			t.Error("runtime principal missing")
		}
		level = p.Assurance().Level()
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "https://mfa.example.test/protected", nil).WithContext(ctx)
	req.AddCookie(current.Cookie)
	rec := httptest.NewRecorder()
	check.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || level != identity.AAL2 {
		t.Fatal("committed session lacks durable AAL2")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	principal, ok := identity.PrincipalFromContext(identity.ContextWithVerifiedCredential(ctx, proof))
	if !ok {
		t.Fatal("fixture principal missing")
	}
	if got, err := svc.BeginEnrollment(canceled, principal, phrase); !errors.Is(err, mfa.ErrUnavailable) || got.Seed != "" {
		t.Fatal("canceled enrollment exposed material")
	}
}

type mfaSetupBudget struct{}

func (mfaSetupBudget) Allow(context.Context, string) error { return nil }

type mfaRuntimeRunner func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error

func (f mfaRuntimeRunner) WithTx(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
	return f(c, o, fn)
}

type mfaRuntimePolicy struct{ scope mfa.Scope }

func (mfaRuntimePolicy) Ready(context.Context, *sql.Tx) error { return nil }
func (p mfaRuntimePolicy) AuthorizeMFA(_ context.Context, _ *sql.Tx, principal identity.Principal, _ string) error {
	if principal.PersonID() != p.scope.PersonID || principal.InstallationID() != p.scope.InstallationID || principal.ApplicationID() != p.scope.ApplicationID || principal.EnvironmentID() != p.scope.EnvironmentID {
		return mfa.ErrDenied
	}
	return nil
}
