package recovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/protection"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Actual guarded reset request and completion use the native writer, real
// hasher, runtime transactions and encrypted durable material. Setup policy is
// synthetic and does not qualify host policy or a delivery provider.
func TestRecoveryWriterRuntimeRequiredService(t *testing.T) {
	if os.Getenv("AMOS_RECOVERY_RUNTIME_TEST_CONFIG") == "" {
		t.Fatal("required recovery writer TLS fixture absent")
	}
	if os.Getenv("AMOS_RECOVERY_WRITER_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecoveryWriterRuntimeRequiredService$", "-test.v")
		cmd.Env = append(os.Environ(), "AMOS_RECOVERY_WRITER_CHILD=1")
		if output, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("recovery writer child failed: %v\n%s", e, output)
		}
		return
	}
	db := recoveryRuntimeDB(t)
	root, e := aw.New(db)
	if e != nil {
		t.Fatal(e)
	}
	if e = aw.ActivateW1(root); e != nil {
		t.Fatal(e)
	}
	cfg := recoveryTxConfig(t)
	cfg.Outbox = nil
	cfg.Materials = nil
	cfg.Policy = &recoveryWriterTestPolicy{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	limiter, e := protection.NewWithTxRunner(db, protection.TxConfig{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, Key: bytes.Repeat([]byte{0x43}, 32), Window: time.Minute, IPLimit: 100, AccountLimit: 100})
	if e != nil {
		t.Fatal(e)
	}
	cfg.Passwords, e = password.New(recoveryBlocklist{}, limiter.PasswordBudget(), 2)
	if e != nil {
		t.Fatal(e)
	}
	sessions, e := session.NewWithWriter(root, session.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, AllowedOrigins: []string{cfg.ApplicationOrigin}, CookieSecure: true, PersistAssurance: true})
	if e != nil {
		t.Fatal(e)
	}
	outbox, e := sqlstore.NewTxWriter(sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
	if e != nil {
		t.Fatal(e)
	}
	materialCfg := materialstore.TxConfig{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, ActiveKeyID: "test", Keys: map[string][]byte{"test": bytes32(11)}, ApplicationOrigin: cfg.ApplicationOrigin}
	materials, e := materialstore.NewWriter(materialCfg)
	if e != nil {
		t.Fatal(e)
	}
	resolver, e := materialstore.NewWithTxRunner(db, materialCfg)
	if e != nil {
		t.Fatal(e)
	}
	svc, e := NewWithWriter(root, sessions, outbox, materials, cfg)
	if e != nil {
		t.Fatal(e)
	}
	// Setup hashing is deliberately separate from an admitted operation budget.
	setupHasher, e := password.New(recoveryBlocklist{}, recoveryBudget{}, 2)
	if e != nil {
		t.Fatal(e)
	}
	setupSvc := *svc
	setupSvc.cfg.Passwords = setupHasher
	account := recoveryRuntimeAccount(t, db, &setupSvc, cfg)
	t.Cleanup(func() {
		recoveryRuntimeCleanup(t, db, cfg)
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if e := db.WithTx(c, nil, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(c, `DELETE FROM identity_auth_limits WHERE installation_id=$1 AND application_id=$2`, cfg.InstallationID, cfg.ApplicationID)
			return e
		}); e != nil {
			t.Error("owned recovery protection cleanup failed")
		}
	})
	guard := protection.Guard{Limiter: limiter, Sessions: sessions}
	request, e := guard.PublicJSON(protection.Recovery, svc.RequestHandler())
	if e != nil {
		t.Fatal(e)
	}
	complete, e := guard.PublicJSON(protection.Recovery, svc.CompleteHandler())
	if e != nil {
		t.Fatal(e)
	}
	post := func(t *testing.T, h http.Handler, payload any) *httptest.ResponseRecorder {
		t.Helper()
		data, e := json.Marshal(payload)
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest(http.MethodPost, cfg.ApplicationOrigin+"/reset-password", bytes.NewReader(data)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", cfg.ApplicationOrigin)
		r.RemoteAddr = "192.0.2.37:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	count := func(t *testing.T, table string) int {
		t.Helper()
		var n int
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT count(*) FROM public.`+table+` WHERE installation_id=$1 AND application_id=$2`, cfg.InstallationID, cfg.ApplicationID).Scan(&n)
		}); e != nil {
			t.Fatal("scoped recovery count failed")
		}
		return n
	}
	t.Run("unknown address generic without delivery", func(t *testing.T) {
		w := post(t, request, map[string]string{"email": "unknown@example.test"})
		if w.Code != http.StatusAccepted || count(t, "amos_jobs") != 0 {
			t.Fatal("unknown reset request disclosed or staged delivery")
		}
	})
	t.Run("guarded request durable and capped", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			w := post(t, request, map[string]string{"email": "person@example.test"})
			if w.Code != http.StatusAccepted {
				t.Fatalf("request status=%d", w.Code)
			}
		}
		if count(t, "amos_jobs") != 3 || count(t, "email_delivery_material") != 3 {
			t.Fatal("reset request cap or durable delivery mismatch")
		}
	})
	var challenge uuid.UUID
	if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT id FROM identity_challenges WHERE person_id=$1 AND purpose=$2 AND consumed_at IS NULL`, account.personID, passwordResetPurpose).Scan(&challenge)
	}); e != nil {
		t.Fatal("current reset challenge absent")
	}
	material, e := resolver.ResolveForTemplate(ctx, deliveryemail.SecretReference("material:"+challenge.String()), deliveryemail.TemplatePasswordReset)
	if e != nil {
		t.Fatal(e)
	}
	action, e := url.Parse(material.ActionURL)
	if e != nil {
		t.Fatal(e)
	}
	token := action.Query().Get("token")
	completed := false
	t.Run("guarded completion advances epoch and revokes all environments", func(t *testing.T) {
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `INSERT INTO identity_sessions(id,person_id,installation_id,application_id,environment_id,token_digest,security_epoch,authentication_method,issued_at,authenticated_at,expires_at,idle_expires_at) SELECT $1,$2,$3,$4,$5,$6,0,'email_password',at-interval '2 hours',at-interval '2 hours',at-interval '1 hour',at-interval '90 minutes' FROM (SELECT clock_timestamp() AS at) sample`, newRecoveryID(t), account.personID, cfg.InstallationID, cfg.ApplicationID, newRecoveryID(t), bytes.Repeat([]byte{0x71}, 32))
			return e
		}); e != nil {
			t.Fatal(e)
		}
		w := post(t, complete, map[string]string{"challenge_id": challenge.String(), "token": token, "password": newPassword})
		if w.Code != http.StatusNoContent {
			t.Fatalf("completion status=%d", w.Code)
		}
		var epoch int64
		var remaining int
		var hash string
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if e := tx.QueryRowContext(ctx, `SELECT security_epoch FROM identity_persons WHERE id=$1`, account.personID).Scan(&epoch); e != nil {
				return e
			}
			if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_sessions WHERE person_id=$1 AND revoked_at IS NULL`, account.personID).Scan(&remaining); e != nil {
				return e
			}
			return tx.QueryRowContext(ctx, `SELECT verifier_hash FROM identity_credentials WHERE person_id=$1 AND method='email_password' AND revoked_at IS NULL`, account.personID).Scan(&hash)
		}); e != nil {
			t.Fatal(e)
		}
		checked, e := setupHasher.Verify(ctx, "verify-reset", newPassword, hash, nil)
		if e != nil || !checked.Verified || epoch != 1 || remaining != 0 {
			t.Fatal("password/epoch/all-session transition incomplete")
		}
		completed = true
	})
	if !completed {
		t.Fatal("completion prerequisite failed; dependent replay/change not run")
	}
	t.Run("reset replay denied", func(t *testing.T) {
		if w := post(t, complete, map[string]string{"challenge_id": challenge.String(), "token": token, "password": oldPassword}); w.Code != http.StatusUnauthorized {
			t.Fatalf("replay status=%d", w.Code)
		}
	})
	t.Run("guarded password change preserves actor and completes epoch transition", func(t *testing.T) {
		raw := bytes.Repeat([]byte{0x52}, 32)
		digest := sha256.Sum256([]byte(base64.RawURLEncoding.EncodeToString(raw)))
		cookie := &http.Cookie{Name: "__Host-amos_session", Value: base64.RawURLEncoding.EncodeToString(raw)}
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `INSERT INTO identity_sessions(id,person_id,installation_id,application_id,environment_id,token_digest,security_epoch,authentication_method,issued_at,authenticated_at,last_seen_at,expires_at,idle_expires_at) SELECT $1,$2,$3,$4,$5,$6,1,'email_password',at-interval '1 minute',at-interval '1 minute',at,at+interval '1 hour',at+interval '30 minutes' FROM (SELECT clock_timestamp() AS at) sample`, newRecoveryID(t), account.personID, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, digest[:])
			return e
		}); e != nil {
			t.Fatal(e)
		}
		reached := false
		native := svc.PasswordChangeHandler()
		handler, e := guard.CookieMutation(protection.PasswordChange, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; native.ServeHTTP(w, r) }))
		if e != nil {
			t.Fatal(e)
		}
		handler = sessions.Middleware(handler)
		data, e := json.Marshal(map[string]string{"current_password": newPassword, "new_password": oldPassword})
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest(http.MethodPost, cfg.ApplicationOrigin+"/change-password", bytes.NewReader(data)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", cfg.ApplicationOrigin)
		r.RemoteAddr = "192.0.2.37:1234"
		r.AddCookie(cookie)
		csrf, ok := sessions.CSRFToken(r)
		if !ok {
			t.Fatal("native CSRF token unavailable")
		}
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusNoContent {
			policy := cfg.Policy.(*recoveryWriterTestPolicy)
			t.Fatalf("password change status=%d native_handler_reached=%t initial_policy_reached=%t completion_policy_reached=%t", w.Code, reached, policy.initialEpoch != 0, policy.completedTo != 0)
		}
		var epoch int64
		var remaining int
		var encoded string
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if e := tx.QueryRowContext(ctx, `SELECT security_epoch FROM identity_persons WHERE id=$1`, account.personID).Scan(&epoch); e != nil {
				return e
			}
			if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_sessions WHERE person_id=$1 AND revoked_at IS NULL`, account.personID).Scan(&remaining); e != nil {
				return e
			}
			return tx.QueryRowContext(ctx, `SELECT verifier_hash FROM identity_credentials WHERE person_id=$1 AND method='email_password' AND revoked_at IS NULL`, account.personID).Scan(&encoded)
		}); e != nil {
			t.Fatal(e)
		}
		checked, e := setupHasher.Verify(ctx, "verify-change", oldPassword, encoded, nil)
		if e != nil || !checked.Verified || epoch != 2 || remaining != 0 {
			t.Fatal("password change durable transition incomplete")
		}
		policy := cfg.Policy.(*recoveryWriterTestPolicy)
		if policy.initialEpoch != 1 || policy.completedFrom != 1 || policy.completedTo != 2 {
			t.Fatal("completion did not preserve original policy actor")
		}
	})

}

type recoveryWriterTestPolicy struct {
	allowRecoveryPolicy
	initialEpoch, completedFrom, completedTo int64
}

func (policy *recoveryWriterTestPolicy) AuthorizePasswordChangeCompletion(_ context.Context, _ *sql.Tx, p identity.Principal, epoch int64) error {
	if p.SecurityEpoch() < 0 || p.SecurityEpoch() == int64(^uint64(0)>>1) || epoch != p.SecurityEpoch()+1 {
		return ErrPolicyDenied
	}
	policy.completedFrom = p.SecurityEpoch()
	policy.completedTo = epoch
	return nil
}

func (policy *recoveryWriterTestPolicy) AuthorizePasswordChange(_ context.Context, _ *sql.Tx, p identity.Principal) error {
	policy.initialEpoch = p.SecurityEpoch()
	return nil
}
