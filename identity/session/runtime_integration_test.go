package session_test

import (
	"context"
	"crypto/sha256"
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
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/storage"
)

// This suite requires an operator-precreated identity/assurance schema and a
// runtime-only TLS connection. It never creates schema or reads admin credentials.
func sessionRuntimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_SERVICES_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required TLS PostgreSQL fixture absent: set AMOS_SERVICES_RUNTIME_TEST_CONFIG to operator-provided runtime JSON")
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

func TestSessionRuntimeRequiredService(t *testing.T) {
	db := sessionRuntimeDB(t)
	cfg := sessionConfig(t)
	cfg.PersistAssurance = true
	person := id(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	exec := func(query string, args ...any) {
		t.Helper()
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, query, args...); return err }); err != nil {
			t.Fatal("runtime fixture DML failed")
		}
	}
	exec(`INSERT INTO identity_persons (id,installation_id,application_id,state) VALUES ($1,$2,$3,'active')`, person, cfg.InstallationID, cfg.ApplicationID)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanCancel()
		if err := db.WithTx(cleanCtx, nil, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(cleanCtx, `DELETE FROM identity_sessions WHERE person_id=$1`, person); err != nil {
				return err
			}
			_, err := tx.ExecContext(cleanCtx, `DELETE FROM identity_persons WHERE id=$1`, person)
			return err
		}); err != nil {
			t.Error("exact-owned session fixture cleanup failed")
		}
	})
	svc, err := session.NewWithTxRunner(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	proof := func(epoch int64, elevated bool) authproof.VerifiedCredential {
		t.Helper()
		now := time.Now().UTC().Add(-time.Second)
		method, level := "email_password", "aal1"
		expiry := now.Add(time.Hour)
		if elevated {
			method, level, expiry = "password+totp", "aal2", now.Add(5*time.Minute)
		}
		p, err := authproof.NewVerifiedCredential(person, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, epoch, method, now, level, expiry)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	issue := func(epoch int64, prior string) session.Issued {
		t.Helper()
		got, err := svc.Issue(ctx, proof(epoch, false), prior)
		if err != nil {
			t.Fatal("runtime session issuance failed")
		}
		return got
	}
	request := func(issued session.Issued, method, origin, csrf string) *http.Request {
		r := httptest.NewRequest(method, "https://session.example.test/protected", nil).WithContext(ctx)
		r.AddCookie(issued.Cookie)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		return r
	}
	var level identity.AssuranceLevel
	handler := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.PrincipalFromContext(r.Context())
		if !ok || principal.PersonID() != person {
			t.Error("runtime identity missing or incorrect")
		}
		level = principal.Assurance().Level()
		w.WriteHeader(http.StatusNoContent)
	}))
	check := func(issued session.Issued, want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request(issued, http.MethodGet, "", ""))
		if rec.Code != want {
			t.Fatalf("runtime session status=%d want=%d", rec.Code, want)
		}
	}
	first := issue(0, "")
	second := issue(0, first.Cookie.Value)
	if first.Cookie.Value == second.Cookie.Value || second.CSRFToken == "" || !second.Cookie.Secure || !second.Cookie.HttpOnly || second.Cookie.Name != "__Host-amos_session" || second.Cookie.Path != "/" || second.Cookie.Domain != "" {
		t.Fatal("rotation or production cookie policy violated")
	}
	check(first, 401)
	check(second, 204)
	for _, tc := range []struct {
		name, origin, csrf string
		want               int
	}{
		{"missing csrf", "https://session.example.test", "", 403},
		{"wrong csrf", "https://session.example.test", "wrong", 403},
		{"wrong origin", "https://other.example.test", second.CSRFToken, 403},
		{"missing origin", "", second.CSRFToken, 403},
		{"valid", "https://session.example.test", second.CSRFToken, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, request(second, http.MethodPost, tc.origin, tc.csrf))
			if rec.Code != tc.want {
				t.Fatalf("mutation status=%d want=%d", rec.Code, tc.want)
			}
		})
	}

	// The caller owns commit: rollback preserves the old session and discards the new one.
	rollback := errors.New("intentional caller rollback")
	var rolled, committed session.Issued
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var err error
		rolled, err = svc.IssueForRequestTx(ctx, tx, proof(0, false), request(second, http.MethodPost, "", ""))
		if err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal("caller rollback sentinel not preserved")
	}
	check(second, 204)
	check(rolled, 401)
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var err error
		committed, err = svc.IssueForRequestTx(ctx, tx, proof(0, false), request(second, http.MethodPost, "", ""))
		return err
	}); err != nil {
		t.Fatal("caller commit failed")
	}
	check(second, 401)
	check(committed, 204)

	// Force actual sql.Tx commit failure after successful issuance SQL. The
	// underlying runtime runner attempts Commit on the already rolled-back tx.
	var provisional session.Issued
	callbacks := 0
	failing := sessionRunnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
		return db.WithTx(c, o, func(tx *sql.Tx) error {
			callbacks++
			if err := fn(tx); err != nil {
				return err
			}
			// Capture a separate staged credential to demonstrate rollback visibility.
			var err error
			provisional, err = svc.IssueForRequestTx(c, tx, proof(0, false), request(committed, http.MethodPost, "", ""))
			if err != nil {
				return err
			}
			return tx.Rollback()
		})
	})
	failingSvc, err := session.NewWithTxRunner(failing, cfg)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := failingSvc.Issue(ctx, proof(0, false), committed.Cookie.Value)
	if !errors.Is(err, session.ErrUnavailable) || failed.Cookie != nil || failed.CSRFToken != "" || !failed.AssuranceExpires.IsZero() || callbacks != 1 || provisional.Cookie == nil {
		t.Fatal("commit failure did not suppress issued credentials")
	}
	check(committed, 204)
	check(provisional, 401)

	elevated, err := svc.Issue(ctx, proof(0, true), committed.Cookie.Value)
	if err != nil {
		t.Fatal("runtime assurance issuance failed")
	}
	check(elevated, 204)
	if level != identity.AAL2 {
		t.Fatal("durable assurance missing")
	}
	digest := sha256.Sum256([]byte(elevated.Cookie.Value))
	exec(`UPDATE identity_sessions SET authenticated_at=transaction_timestamp()-interval '3 minutes', assurance_expires_at=transaction_timestamp()-interval '1 minute' WHERE token_digest=$1`, digest[:])
	check(elevated, 204)
	if level != identity.AAL1 {
		t.Fatal("expired assurance retained")
	}
	exec(`UPDATE identity_persons SET security_epoch=security_epoch+1 WHERE id=$1`, person)
	check(elevated, 401)
	stale, err := svc.Issue(ctx, proof(0, false), "")
	if !errors.Is(err, session.ErrUnavailable) || stale.Cookie != nil {
		t.Fatal("stale epoch issued session")
	}
	current := issue(1, "")
	check(current, 204)
	exec(`UPDATE identity_persons SET state='self_disabled' WHERE id=$1`, person)
	check(current, 401)
	exec(`UPDATE identity_persons SET state='active' WHERE id=$1`, person)
	expired := issue(1, "")
	digest = sha256.Sum256([]byte(expired.Cookie.Value))
	exec(`UPDATE identity_sessions SET authenticated_at=transaction_timestamp()-interval '2 hours', issued_at=transaction_timestamp()-interval '2 hours', expires_at=transaction_timestamp()-interval '1 second', idle_expires_at=transaction_timestamp()-interval '1 second' WHERE token_digest=$1`, digest[:])
	check(expired, 401)
	out := httptest.NewRecorder()
	svc.SignOut(out, request(current, http.MethodPost, "https://session.example.test", current.CSRFToken))
	if out.Code != 204 || len(out.Result().Cookies()) != 1 || out.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("runtime signout failed")
	}
	check(current, 401)
	canceled, stop := context.WithCancel(ctx)
	stop()
	denied, err := svc.Issue(canceled, proof(1, false), "")
	if !errors.Is(err, session.ErrUnavailable) || denied.Cookie != nil || denied.CSRFToken != "" {
		t.Fatal("canceled transaction exposed credentials")
	}
}
