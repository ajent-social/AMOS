package apphost

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	business "github.com/ajent-social/amos/examples/reference/app/business"
	reference "github.com/ajent-social/amos/examples/reference/migrations"
	ui "github.com/ajent-social/amos/examples/reference/ui"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/mfa"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	workspaces "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

func localDatabase(t *testing.T) string {
	t.Helper()
	_, schema := testkit.NewPostgres(t)
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	ingress, err := migrations.BillingWebhookIngress(9)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := migrations.RuntimeBinding(10)
	if err != nil {
		t.Fatal(err)
	}
	magic, err := migrations.MagicBrowserBinding(11)
	if err != nil {
		t.Fatal(err)
	}
	assurance, err := migrations.SessionAssurance(12)
	if err != nil {
		t.Fatal(err)
	}
	factors, err := mfa.Fragment(13)
	if err != nil {
		t.Fatal(err)
	}
	limits, err := migrations.MFAProtection(14)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := migrations.Core(reference.Fragment(), ingress, binding, magic, assurance, factors, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatal(err)
	}
	return parsed.String()
}
func localConfig(t *testing.T, config, origin string) LocalConfig {
	t.Helper()
	id := func() uuid.UUID {
		v, e := uuid.NewV7()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	newKey := func() []byte {
		b := make([]byte, 32)
		if _, e := rand.Read(b); e != nil {
			t.Fatal(e)
		}
		return b
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := LocalConfig{InstallationID: id(), ApplicationID: id(), EnvironmentID: id(), Origin: origin, DatabaseURL: config, MailDirectory: dir, MaterialKey: newKey(), RateKey: newKey()}
	cfg.Business = func(db *storage.DB, sessions *session.Service) ([]Route, error) {
		todos, err := business.New(db)
		if err != nil {
			return nil, err
		}
		selector := func(r *http.Request) (string, error) {
			p, ok := identity.PrincipalFromContext(r.Context())
			if !ok {
				return "", ErrLocalConfiguration
			}
			var selected workspaces.Workspace
			err := db.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
				store, err := workspaces.New(tx)
				if err != nil {
					return err
				}
				selected, err = store.FindPersonalWorkspace(r.Context(), workspaces.Scope{InstallationID: p.InstallationID(), ApplicationID: p.ApplicationID()}, p.PersonID())
				return err
			})
			return selected.ID.String(), err
		}
		pages := ui.NewHandler(todos, ui.Options{CSRFToken: sessions.CSRFToken, PersonalWorkspace: selector})
		protected := sessions.Middleware(pages)
		return []Route{{Method: "GET", Pattern: "/todos", Handler: protected}, {Method: "POST", Pattern: "/todos", Handler: protected}, {Method: "GET", Pattern: "/todos/*", Handler: protected}, {Method: "POST", Pattern: "/todos/*", Handler: protected}}, nil
	}
	return cfg
}
func response(t *testing.T, client *http.Client, method, endpoint, origin string, form url.Values) (int, string) {
	t.Helper()
	var reader io.Reader
	if form != nil {
		reader = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		t.Fatal("construct local request")
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal("local request unavailable")
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			t.Error("close local response")
		}
	}()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal("read local response")
	}
	return res.StatusCode, string(body)
}

func TestLocalSignupVerificationSigninPersonalTodoAndSignout(t *testing.T) {
	dsn := localDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	cfg := localConfig(t, dsn, origin)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host, err := NewLocal(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- host.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			_ = listener.Close()
			t.Error("local host failed to stop")
		}
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	email := "owner@example.test"
	phrase := "fresh local owner phrase 2026!"
	status, _ := response(t, client, "POST", origin+"/signup", origin, url.Values{"email": {email}, "password": {phrase}})
	if status != 202 {
		t.Fatalf("signup status=%d", status)
	}
	var action *url.URL
	deadline := time.Now().Add(5 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for action == nil && time.Now().Before(deadline) {
		files, err := os.ReadDir(cfg.MailDirectory)
		if err != nil {
			t.Fatal("read private test mailbox")
		}
		if len(files) > 0 {
			data, err := os.ReadFile(filepath.Join(cfg.MailDirectory, files[0].Name()))
			if err != nil {
				t.Fatal("read private capture")
			}
			var capture struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(data, &capture) != nil {
				t.Fatal("invalid private capture")
			}
			first, _, _ := strings.Cut(capture.Text, "\n")
			action, err = url.Parse(first)
			if err != nil {
				t.Fatal("invalid private action")
			}
			break
		}
		<-ticker.C
	}
	if action == nil {
		t.Fatal("local mail capture not obtained")
	}
	status, _ = response(t, client, "GET", action.String(), "", nil)
	if status != 200 {
		t.Fatalf("preview status=%d", status)
	}
	status, _ = response(t, client, "POST", origin+"/verify-email", origin, url.Values{"challenge": {action.Query().Get("challenge")}, "token": {action.Query().Get("token")}})
	if status != 200 {
		t.Fatalf("verification status=%d", status)
	}
	status, _ = response(t, client, "POST", origin+"/auth", origin, url.Values{"email": {email}, "password": {phrase}})
	if status != 303 {
		t.Fatalf("sign-in status=%d", status)
	}
	status, body := response(t, client, "GET", origin+"/todos", "", nil)
	if status != 200 || strings.Contains(body, "Workspace ID") {
		t.Fatalf("personal workspace status=%d", status)
	}
	match := regexp.MustCompile(`name="_csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("session-bound mutation token unavailable")
	}
	status, _ = response(t, client, "POST", origin+"/todos", origin, url.Values{"_csrf": {match[1]}, "title": {"Owned personal todo"}})
	if status != 303 {
		t.Fatalf("todo create status=%d", status)
	}
	status, body = response(t, client, "GET", origin+"/todos", "", nil)
	if status != 200 || !strings.Contains(body, "Owned personal todo") {
		t.Fatal("created todo unavailable")
	}
	// Exercise the actual composed MFA middleware and encrypted SQL lifecycle.
	mfaPost := func(path, csrf, allowedOrigin string, payload any) (int, []byte, string) {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal("encode factor request")
		}
		request, err := http.NewRequest("POST", origin+path, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal("factor request unavailable")
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", allowedOrigin)
		request.Header.Set("X-CSRF-Token", csrf)
		reply, err := client.Do(request)
		if err != nil {
			t.Fatal("factor HTTP unavailable")
		}
		body, err := io.ReadAll(io.LimitReader(reply.Body, 4097))
		if err != nil {
			t.Fatal("factor response unavailable")
		}
		if err := reply.Body.Close(); err != nil {
			t.Fatal("close factor response")
		}
		return reply.StatusCode, body, reply.Header.Get("X-CSRF-Token")
	}
	for _, rejected := range []struct{ csrf, origin string }{{"", origin}, {match[1], "http://other.invalid"}} {
		code, _, _ := mfaPost("/account/mfa/totp/enroll", rejected.csrf, rejected.origin, map[string]string{"current_password": phrase})
		if code != http.StatusForbidden {
			t.Fatalf("factor boundary rejection status=%d", code)
		}
	}
	code, encoded, _ := mfaPost("/account/mfa/totp/enroll", match[1], origin, map[string]string{"current_password": phrase})
	if code != http.StatusCreated {
		t.Fatalf("factor enrollment status=%d", code)
	}
	var enrollment mfa.Enrollment
	if json.Unmarshal(encoded, &enrollment) != nil || enrollment.Seed == "" {
		t.Fatal("factor enrollment unavailable")
	}
	otp, err := totp.GenerateCode(enrollment.Seed, time.Now())
	if err != nil {
		t.Fatal("factor code unavailable")
	}
	code, encoded, rotatedCSRF := mfaPost("/account/mfa/totp/confirm", match[1], origin, map[string]string{"current_password": phrase, "factor_id": enrollment.FactorID.String(), "code": otp})
	var assurance struct {
		Assurance string    `json:"assurance"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if code != http.StatusOK || rotatedCSRF == "" || json.Unmarshal(encoded, &assurance) != nil || assurance.Assurance != "aal2" {
		t.Fatalf("factor confirmation status=%d", code)
	}
	if err := host.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var persisted time.Time
		if err := tx.QueryRowContext(ctx, `SELECT assurance_expires_at FROM identity_sessions WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND revoked_at IS NULL AND assurance_level='aal2'`, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID).Scan(&persisted); err != nil {
			return err
		}
		if !persisted.Equal(assurance.ExpiresAt) {
			return errors.New("reported MFA expiry differs from persisted authority")
		}
		return nil
	}); err != nil {
		t.Fatal("MFA assurance expiry authority unavailable")
	}
	code, _, _ = mfaPost("/account/mfa/totp/challenge", rotatedCSRF, origin, map[string]string{"current_password": phrase, "code": otp})
	if code == http.StatusOK {
		t.Fatal("composed factor route accepted same-step replay")
	}
	match[1] = rotatedCSRF
	status, _ = response(t, client, "POST", origin+"/signout", origin, url.Values{"_csrf": {match[1]}})
	if status != 204 {
		t.Fatalf("sign-out status=%d", status)
	}
	status, _ = response(t, client, "GET", origin+"/todos", "", nil)
	if status != 401 {
		t.Fatal("revoked session retained business access")
	}
}

func TestLocalDatabaseBindingRejectsDifferentEnvironment(t *testing.T) {
	dsn := localDatabase(t)
	cfg := localConfig(t, dsn, "http://127.0.0.1:8080")
	host, err := NewLocal(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.EnvironmentID, err = uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	if other, err := NewLocal(context.Background(), cfg); err == nil {
		_ = other.Close()
		t.Fatal("different deployment used bound database")
	}
}

func TestLocalDatabaseBindingRejectsForeignQuarantine(t *testing.T) {
	dsn := localDatabase(t)
	cfg := localConfig(t, dsn, "http://127.0.0.1:8080")
	db, err := storage.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal("open scoped database")
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(), `INSERT INTO billing_verified_webhook_ingress(id,installation_id,application_id,environment_id,provider,provider_account_id,account_mode,provider_event_ref,event_type,customer_ref,subscription_ref,payload_sha256,state,quarantine_reason) VALUES($1,$2,$3,$4,'stripe','acct_test','test','evt_test_foreign','customer.subscription.updated','','',decode(repeat('00',32),'hex'),'quarantined','missing_customer')`, id, cfg.InstallationID, foreign, cfg.EnvironmentID)
		return e
	})
	if err != nil {
		t.Fatal("seed foreign quarantine")
	}
	host, err := NewLocal(context.Background(), cfg)
	if err == nil {
		_ = host.Close()
		t.Fatal("foreign quarantine permitted deployment binding")
	}
}

func TestLocalHostRejectsMissingAssurancePrerequisite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	config := localDatabase(t)
	db, err := storage.Open(ctx, config)
	if err != nil {
		t.Fatal("open isolated prerequisite test database")
	}
	defer func() { _ = db.Close() }()
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `ALTER TABLE identity_sessions DROP COLUMN assurance_level CASCADE`)
		return err
	}); err != nil {
		t.Fatal("remove isolated assurance prerequisite")
	}
	cfg := localConfig(t, config, "http://127.0.0.1:4189")
	host, err := NewLocal(ctx, cfg)
	if host != nil {
		_ = host.Close()
		t.Fatal("missing assurance prerequisite started a host")
	}
	if !errors.Is(err, ErrLocalDependency) {
		t.Fatal("missing assurance prerequisite was not visible")
	}
}
