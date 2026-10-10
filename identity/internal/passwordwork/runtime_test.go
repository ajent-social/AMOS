package passwordwork

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
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/password/policy"
	"github.com/ajent-social/amos/identity/protection"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// Operator-owned SERVICES fixture, unchanged schema; no DDL/admin credentials.
func runtimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_SERVICES_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required TLS runtime fixture absent: AMOS_SERVICES_RUNTIME_TEST_CONFIG")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("runtime fixture configuration unavailable")
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error("runtime fixture configuration close failed")
		}
	}()
	var config struct {
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
	if err := dec.Decode(&config); err != nil {
		t.Fatal("runtime fixture configuration invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Fatal("runtime fixture configuration trailing data")
	}
	ca, err := os.ReadFile(config.CAPath)
	if err != nil {
		t.Fatal("runtime fixture trust unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: config.Host, Port: config.Port, Database: config.Database, User: config.User, Password: config.Password, RootCAPEM: ca, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal("required TLS runtime unavailable")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("runtime close failed")
		}
	})
	return db
}

func TestPasswordWorkRuntimeRequiredService(t *testing.T) {
	db := runtimeDB(t)
	newID := func() uuid.UUID {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatal("test ID unavailable")
		}
		return id
	}
	installation, application, environment, person := newID(), newID(), newID(), newID()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		err := db.WithTx(cleanup, nil, func(tx *sql.Tx) error {
			for _, query := range []string{
				`DELETE FROM public.identity_sessions WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM public.identity_persons WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM public.identity_auth_limits WHERE installation_id=$1 AND application_id=$2`,
			} {
				if _, err := tx.ExecContext(cleanup, query, installation, application); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Error("exact-owned runtime rows cleanup failed")
		}
	})
	limiter, err := protection.NewWithTxRunner(db, protection.TxConfig{InstallationID: installation, ApplicationID: application, EnvironmentID: environment, Key: bytes.Repeat([]byte{0x42}, 32), Window: time.Minute, IPLimit: 100, AccountLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.NewWithTxRunner(db, session.Config{InstallationID: installation, ApplicationID: application, EnvironmentID: environment, AllowedOrigins: []string{"https://password-work.example.test"}, CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	blocklist, err := policy.New(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := password.New(blocklist, limiter.PasswordBudget(), 2)
	if err != nil {
		t.Fatal(err)
	}
	guard := protection.Guard{Limiter: limiter, Sessions: sessions}
	phrase := "synthetic native budget password"
	var encoded string
	signup, err := guard.PublicJSON(protection.Signup, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoded, err = Hash(r.Context(), hasher, "register:bounded@example.test", phrase)
		if err != nil || encoded == "" {
			t.Error("real signup admission did not reach bounded hash")
		}
		if got, err := Hash(r.Context(), hasher, "register:bounded@example.test", phrase); got != "" || !errors.Is(err, password.ErrBusy) {
			t.Error("signup admission was reused")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := func(h http.Handler, cookie *http.Cookie, csrf string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "https://password-work.example.test/auth", strings.NewReader(`{"email":"bounded@example.test"}`)).WithContext(ctx)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://password-work.example.test")
		if cookie != nil {
			r.AddCookie(cookie)
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNoContent {
			t.Fatalf("native guard status %d, want 204", w.Code)
		}
	}
	request(signup, nil, "")
	signin, err := guard.PublicJSON(protection.Signin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, err := Verify(r.Context(), hasher, "signin:bounded@example.test", phrase, encoded, true)
		if err != nil || !got.Verified {
			t.Error("real signin admission lost in computation worker")
		}
		if again, err := Verify(r.Context(), hasher, "signin:bounded@example.test", phrase, encoded, false); again != (Verification{}) || !errors.Is(err, password.ErrBusy) {
			t.Error("signin admission was reused")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err != nil {
		t.Fatal(err)
	}
	request(signin, nil, "")
	if got, err := Verify(ctx, hasher, "signin:bounded@example.test", phrase, encoded, false); got != (Verification{}) || !errors.Is(err, password.ErrBusy) {
		t.Fatal("missing native admission was accepted")
	}
	var now time.Time
	err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO public.identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, person, installation, application); e != nil {
			return e
		}
		return tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&now)
	})
	if err != nil {
		t.Fatal("runtime test person setup failed")
	}
	// Synthetic credential setup qualifies the real guard budget, not a native
	// credential producer, W1 writer, or complete authentication journey.
	proof, err := authproof.NewVerifiedCredential(person, installation, application, environment, 0, "email_password", now, "aal1", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.Issue(ctx, proof, "")
	if err != nil {
		t.Fatal("runtime test session unavailable")
	}
	change, err := guard.CookieMutation(protection.PasswordChange, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, err := Hash(r.Context(), hasher, "change-new:"+person.String(), phrase); got != "" || !errors.Is(err, password.ErrBusy) {
			t.Error("replacement hashing preceded current verification")
		}
		got, err := Verify(r.Context(), hasher, "change-current:"+person.String(), phrase, encoded, false)
		if err != nil || !got.Verified {
			t.Error("current password budget failed")
		}
		if got, err := Hash(r.Context(), hasher, "change-new:"+person.String(), phrase); got == "" || err != nil {
			t.Error("ordered replacement hash budget failed")
		}
		if got, err := Hash(r.Context(), hasher, "change-new:"+person.String(), phrase); got != "" || !errors.Is(err, password.ErrBusy) {
			t.Error("replacement hash budget was reused")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err != nil {
		t.Fatal(err)
	}
	request(sessions.Middleware(change), issued.Cookie, issued.CSRFToken)
	awaitSlots(t, 0)
}
