package login

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
)

const freshnessSecret = "a sufficiently long password"
const freshnessOrigin = "https://app.example.test"

type freshnessBudget struct{ calls atomic.Int32 }

func (b *freshnessBudget) Allow(context.Context, string) error { b.calls.Add(1); return nil }

type signinFixture struct {
	db                          *storage.RuntimeDB
	ctx                         context.Context
	cfg                         TxConfig
	person, email, credentialID uuid.UUID
	address, hash, replacement  string
	budget                      *freshnessBudget
}

func newSigninFixture(t *testing.T) *signinFixture {
	t.Helper()
	f := &signinFixture{db: loginRuntimeDB(t), cfg: loginTxConfig(t), person: newID(t), email: newID(t), credentialID: newID(t), budget: &freshnessBudget{}}
	var stop context.CancelFunc
	f.ctx, stop = context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(stop)
	f.address = f.email.String() + "@example.test"
	var err error
	f.cfg.Passwords, err = password.New(list{}, f.budget, 1)
	if err != nil {
		t.Fatal("create real hasher failed")
	}
	f.hash, err = f.cfg.Passwords.Hash(f.ctx, "fixture", freshnessSecret)
	if err != nil {
		t.Fatal("hash fixture password failed")
	}
	f.replacement, err = f.cfg.Passwords.Hash(f.ctx, "fixture", "a different sufficiently long password")
	if err != nil {
		t.Fatal("hash replacement password failed")
	}
	f.budget.calls.Store(0)
	f.exec(t, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, f.person, f.cfg.InstallationID, f.cfg.ApplicationID)
	f.exec(t, `INSERT INTO identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key,verified_at) VALUES($1,$2,$3,$4,$5,$5,clock_timestamp())`, f.email, f.person, f.cfg.InstallationID, f.cfg.ApplicationID, f.address)
	f.exec(t, `INSERT INTO identity_credentials(id,person_id,method,verifier_hash) VALUES($1,$2,'email_password',$3)`, f.credentialID, f.person, f.hash)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			for _, q := range []string{`DELETE FROM identity_sessions WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM identity_credentials WHERE person_id IN (SELECT id FROM identity_persons WHERE installation_id=$1 AND application_id=$2)`, `DELETE FROM identity_emails WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM identity_persons WHERE installation_id=$1 AND application_id=$2`} {
				if _, err := tx.ExecContext(ctx, q, f.cfg.InstallationID, f.cfg.ApplicationID); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Error("exact-owned sign-in fixture cleanup failed")
		}
	})
	return f
}
func (f *signinFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(f.ctx, q, args...)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e == nil && n != 1 {
			return errors.New("mutation did not affect exact row")
		}
		return e
	}); err != nil {
		t.Fatal("exact fixture DML failed")
	}
}
func (f *signinFixture) service(t *testing.T, runner TxRunner) (*Service, *session.Service) {
	t.Helper()
	sessions, err := session.NewWithTxRunner(runner, session.Config{InstallationID: f.cfg.InstallationID, ApplicationID: f.cfg.ApplicationID, EnvironmentID: f.cfg.EnvironmentID, AllowedOrigins: []string{freshnessOrigin}, CookieSecure: true})
	if err != nil {
		t.Fatal("session construction failed")
	}
	cfg := f.cfg
	cfg.Sessions = sessions
	s, err := NewWithTxRunner(runner, cfg)
	if err != nil {
		t.Fatal("login construction failed")
	}
	return s, sessions
}
func freshnessRequest(ctx context.Context, address, suppliedPassword string, cookies ...*http.Cookie) *http.Request {
	b, _ := json.Marshal(map[string]string{"email": address, "password": suppliedPassword})
	r := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(string(b))).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", freshnessOrigin)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	return r
}
func (f *signinFixture) post(s *Service, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, freshnessRequest(f.ctx, f.address, freshnessSecret, cookies...))
	return w
}
func (f *signinFixture) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(f.ctx, `SELECT count(*) FROM identity_sessions WHERE installation_id=$1 AND application_id=$2`, f.cfg.InstallationID, f.cfg.ApplicationID).Scan(&n)
	}); err != nil {
		t.Fatal("read session count failed")
	}
	return n
}
func freshnessDenied(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	var body response
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal("invalid generic response")
	}
	code := "auth.unauthenticated"
	if status == 503 {
		code = "dependency.unavailable"
	}
	if w.Code != status || body.Code != code || w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "csrf_token") || strings.Contains(w.Body.String(), `"authenticated"`) {
		t.Fatalf("freshness response status=%d code=%s; want %d %s without provisional output", w.Code, body.Code, status, code)
	}
}
func (f *signinFixture) success(t *testing.T, w *httptest.ResponseRecorder, sessions *session.Service) *http.Cookie {
	t.Helper()
	var body struct {
		Authenticated bool   `json:"authenticated"`
		CSRF          string `json:"csrf_token"`
	}
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || w.Code != 200 || !body.Authenticated || body.CSRF == "" || len(w.Result().Cookies()) != 1 {
		t.Fatalf("real sign-in status=%d; want committed success", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly {
		t.Fatal("cookie weakened")
	}
	reached := false
	handler := sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := identity.PrincipalFromContext(r.Context())
		person := p.Actor().PersonID()
		if !ok || person != f.person || p.InstallationID() != f.cfg.InstallationID || p.ApplicationID() != f.cfg.ApplicationID || p.EnvironmentID() != f.cfg.EnvironmentID {
			t.Error("middleware scope mismatch")
			return
		}
		reached = true
		w.WriteHeader(204)
	}))
	r := httptest.NewRequest(http.MethodGet, "/protected", nil).WithContext(f.ctx)
	r.AddCookie(cookie)
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	if !reached || out.Code != 204 {
		t.Fatal("committed cookie did not authenticate through middleware")
	}
	return cookie
}

// This shared forwarding runner counts both login and session transactions. The
// second call is intercepted on BOTH detached original and caller-owned issuance,
// independently of nil versus explicit options. Fixture DML bypasses the counter.
func TestSigninFreshnessRequiredServiceStale(t *testing.T) {
	for _, kind := range []string{"same_epoch_hash", "credential_revoked", "credential_id", "email_key", "email_unverified", "email_timestamp", "email_reassigned", "person_disabled", "epoch"} {
		t.Run(kind, func(t *testing.T) {
			f := newSigninFixture(t)
			calls := 0
			mutated := false
			runner := loginRunnerFunc(func(ctx context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
				calls++
				if calls == 2 {
					if f.budget.calls.Load() != 1 {
						return errors.New("barrier was not after exactly one Verify")
					}
					switch kind {
					case "same_epoch_hash":
						f.exec(t, `UPDATE identity_credentials SET verifier_hash=$2 WHERE id=$1`, f.credentialID, f.replacement)
					case "credential_revoked":
						f.exec(t, `UPDATE identity_credentials SET revoked_at=clock_timestamp() WHERE id=$1`, f.credentialID)
					case "credential_id":
						f.exec(t, `UPDATE identity_credentials SET id=$2 WHERE id=$1`, f.credentialID, newID(t))
					case "email_key":
						f.exec(t, `UPDATE identity_emails SET comparison_key=$2 WHERE id=$1`, f.email, "changed@example.test")
					case "email_unverified":
						f.exec(t, `UPDATE identity_emails SET verified_at=NULL WHERE id=$1`, f.email)
					case "email_timestamp":
						f.exec(t, `UPDATE identity_emails SET verified_at=verified_at+interval '1 second' WHERE id=$1`, f.email)
					case "email_reassigned":
						other := newID(t)
						f.exec(t, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, other, f.cfg.InstallationID, f.cfg.ApplicationID)
						f.exec(t, `UPDATE identity_emails SET person_id=$2 WHERE id=$1`, f.email, other)
					case "person_disabled":
						f.exec(t, `UPDATE identity_persons SET state='self_disabled' WHERE id=$1`, f.person)
					case "epoch":
						f.exec(t, `UPDATE identity_persons SET security_epoch=security_epoch+1 WHERE id=$1`, f.person)
					}
					if kind == "same_epoch_hash" {
						var exact bool
						if e := f.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
							return tx.QueryRowContext(ctx, `SELECT c.verifier_hash=$2 AND p.security_epoch=0 FROM identity_credentials c JOIN identity_persons p ON p.id=c.person_id WHERE c.id=$1`, f.credentialID, f.replacement).Scan(&exact)
						}); e != nil || !exact {
							return errors.New("same-epoch mutation not actually committed")
						}
					}
					mutated = true
				}
				return f.db.WithTx(ctx, o, fn)
			})
			s, _ := f.service(t, runner)
			w := f.post(s)
			if !mutated || calls != 2 || f.budget.calls.Load() != 1 {
				t.Fatal("stale-proof mutation barrier not reached after one Verify")
			}
			freshnessDenied(t, w, 401)
			if f.count(t) != 0 {
				t.Fatal("stale proof issued a session")
			}
		})
	}
}

func TestSigninFreshnessRequiredServiceRehash(t *testing.T) {
	for _, mode := range []string{"current", "success", "loss", "failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newSigninFixture(t)
			if mode != "current" {
				salt := []byte("0123456789abcdef")
				out := argon2.IDKey([]byte(freshnessSecret), salt, 2, 32768, 1, 32)
				f.hash = fmt.Sprintf("$argon2id$v=19$m=32768,t=2,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(out))
				f.exec(t, `UPDATE identity_credentials SET verifier_hash=$2 WHERE id=$1`, f.credentialID, f.hash)
			}
			calls := 0
			casObserved := false
			runner := loginRunnerFunc(func(ctx context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
				calls++
				if mode != "current" && calls == 2 {
					casObserved = true
					if mode == "loss" {
						f.exec(t, `UPDATE identity_credentials SET verifier_hash=$2 WHERE id=$1`, f.credentialID, f.replacement)
					}
					if mode == "failure" {
						return errors.New("optional rehash dependency failure")
					}
				}
				return f.db.WithTx(ctx, o, fn)
			})
			s, _ := f.service(t, runner)
			w := f.post(s)
			if f.budget.calls.Load() != 1 {
				t.Fatal("password verified more than once")
			}
			wantCalls := 3
			if mode == "current" {
				wantCalls = 2
			}
			if calls != wantCalls || casObserved != (mode != "current") {
				t.Fatal("optional rehash transaction schedule changed")
			}
			var stored string
			if e := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
				return tx.QueryRowContext(f.ctx, `SELECT verifier_hash FROM identity_credentials WHERE id=$1`, f.credentialID).Scan(&stored)
			}); e != nil {
				t.Fatal("read rehash result failed")
			}
			if mode == "loss" {
				freshnessDenied(t, w, 401)
				if stored != f.replacement || f.count(t) != 0 {
					t.Fatal("losing CAS overwrote replacement or issued session")
				}
				return
			}
			_, sessions := f.service(t, f.db)
			f.success(t, w, sessions)
			if mode == "success" {
				if stored == f.hash || !strings.Contains(stored, "m=65536,t=3") {
					t.Fatal("successful CAS receipt not persisted")
				}
			} else if stored != f.hash {
				t.Fatal("failed or unnecessary rehash changed verifier")
			}
		})
	}
}

func signinObservedWait(t *testing.T, ctx context.Context, holder *sql.Tx, pid int) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := holder.ExecContext(ctx, `SELECT pg_stat_clear_snapshot()`); err != nil {
			t.Fatal("clear wait snapshot failed")
		}
		var blocked bool
		if err := holder.QueryRowContext(ctx, `SELECT COALESCE((SELECT wait_event_type='Lock' AND pg_backend_pid()=ANY(pg_blocking_pids(pid)) FROM pg_stat_activity WHERE pid=$1),false)`, pid).Scan(&blocked); err != nil {
			t.Fatal("observe exact backend wait failed")
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("database-observed row wait absent")
		case <-ticker.C:
		}
	}
}
func TestSigninFreshnessRequiredServiceWaits(t *testing.T) {
	for _, row := range []string{"person", "email", "credentialID"} {
		for _, mode := range []string{"unchanged", "stale", "cancel"} {
			t.Run(row+"/"+mode, func(t *testing.T) {
				f := newSigninFixture(t)
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				pid := make(chan int, 1)
				done := make(chan *httptest.ResponseRecorder, 1)
				joined := make(chan struct{})
				calls := 0
				runner := loginRunnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
					calls++
					if calls != 2 {
						return f.db.WithTx(c, o, fn)
					}
					return f.db.WithTx(c, o, func(tx *sql.Tx) error {
						var n int
						if e := tx.QueryRowContext(c, `SELECT pg_backend_pid()`).Scan(&n); e != nil {
							return e
						}
						pid <- n
						return fn(tx)
					})
				})
				s, _ := f.service(t, runner)
				if err := f.db.WithTx(f.ctx, nil, func(holder *sql.Tx) error {
					table, id := "identity_persons", f.person
					if row == "email" {
						table, id = "identity_emails", f.email
					}
					if row == "credentialID" {
						table, id = "identity_credentials", f.credentialID
					}
					var locked string
					if e := holder.QueryRowContext(f.ctx, `SELECT id::text FROM `+table+` WHERE id=$1 FOR UPDATE`, id).Scan(&locked); e != nil {
						return e
					}
					go func() {
						defer close(joined)
						w := httptest.NewRecorder()
						s.Handler().ServeHTTP(w, freshnessRequest(ctx, f.address, freshnessSecret))
						done <- w
					}()
					t.Cleanup(func() {
						cancel()
						select {
						case <-joined:
						case <-time.After(5 * time.Second):
							t.Error("sign-in waiter did not stop")
						}
					})
					var n int
					select {
					case n = <-pid:
					case <-f.ctx.Done():
						return errors.New("issuance backend absent")
					}
					observe, stop := context.WithTimeout(f.ctx, 2*time.Second)
					defer stop()
					signinObservedWait(t, observe, holder, n)
					if mode == "cancel" {
						cancel()
						return nil
					}
					if mode == "stale" {
						q := `UPDATE identity_persons SET security_epoch=security_epoch+1 WHERE id=$1`
						if row == "email" {
							q = `UPDATE identity_emails SET verified_at=NULL WHERE id=$1`
						}
						if row == "credentialID" {
							_, e := holder.ExecContext(f.ctx, `UPDATE identity_credentials SET verifier_hash=$2 WHERE id=$1`, id, f.replacement)
							return e
						}
						_, e := holder.ExecContext(f.ctx, q, id)
						return e
					}
					return nil
				}); err != nil {
					t.Fatal("holder transaction failed")
				}
				var w *httptest.ResponseRecorder
				select {
				case w = <-done:
				case <-f.ctx.Done():
					t.Fatal("sign-in waiter response absent")
				}
				<-joined
				if mode == "unchanged" {
					_, sessions := f.service(t, f.db)
					f.success(t, w, sessions)
				} else {
					status := 401
					if mode == "cancel" {
						status = 503
					}
					freshnessDenied(t, w, status)
					if f.count(t) != 0 {
						t.Fatal("wait failure committed session")
					}
				}
			})
		}
	}
}

func TestSigninFreshnessRequiredServiceCompletion(t *testing.T) {
	for _, mode := range []string{"read_only", "repeatable_read", "closed", "rollback", "cancel", "commit"} {
		t.Run(mode, func(t *testing.T) {
			f := newSigninFixture(t)
			plain, sessions := f.service(t, f.db)
			prior := f.success(t, f.post(plain), sessions)
			f.budget.calls.Store(0)
			calls := 0
			reached := false
			forced := errors.New("forced issuance rollback")
			var observed error
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			runner := loginRunnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
				calls++
				if calls != 2 {
					return f.db.WithTx(c, o, fn)
				}
				if o == nil || o.ReadOnly || o.Isolation != sql.LevelReadCommitted {
					return errors.New("missing issuance options")
				}
				deadline, ok := c.Deadline()
				if !ok || time.Until(deadline) > 3*time.Second {
					return errors.New("missing bounded issuance context")
				}
				options := *o
				if mode == "read_only" {
					options.ReadOnly = true
				}
				if mode == "repeatable_read" {
					options.Isolation = sql.LevelRepeatableRead
				}
				observed = f.db.WithTx(c, &options, func(tx *sql.Tx) error {
					if mode == "closed" {
						if e := tx.Rollback(); e != nil {
							return e
						}
						reached = true
						return fn(tx)
					}
					e := fn(tx)
					if mode == "read_only" || mode == "repeatable_read" {
						reached = e != nil
						return e
					}
					if e != nil {
						return e
					}
					var total, active int
					if e := tx.QueryRowContext(c, `SELECT count(*),count(*) FILTER(WHERE revoked_at IS NULL) FROM identity_sessions WHERE installation_id=$1 AND application_id=$2`, f.cfg.InstallationID, f.cfg.ApplicationID).Scan(&total, &active); e != nil {
						return e
					}
					if total != 2 || active != 1 {
						return errors.New("injection did not observe exact staged issuance and rotation")
					}
					reached = true
					switch mode {
					case "rollback":
						return forced
					case "cancel":
						cancel()
						return c.Err()
					case "commit":
						return tx.Rollback()
					}
					return errors.New("unknown fault")
				})
				return observed
			})
			s, _ := f.service(t, runner)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, freshnessRequest(ctx, f.address, freshnessSecret, prior))
			if !reached {
				t.Fatal("real fault branch not reached")
			}
			freshnessDenied(t, w, 503)
			if mode == "commit" && (observed != storage.ErrTransaction || ctx.Err() != nil) {
				t.Fatal("real completion failure not observed")
			}
			if mode == "rollback" && !errors.Is(observed, forced) {
				t.Fatal("rollback error lost")
			}
			if mode == "cancel" && !errors.Is(observed, context.Canceled) {
				t.Fatal("derived context did not inherit cancellation")
			}
			if f.count(t) != 1 {
				t.Fatal("failure committed provisional session")
			}
			f.assertCookieState(t, prior, false)
		})
	}
}
func (f *signinFixture) assertCookieState(t *testing.T, cookie *http.Cookie, revoked bool) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(raw) != 32 {
		t.Fatal("invalid issued cookie")
	}
	digest := sha256.Sum256([]byte(cookie.Value))
	var got bool
	if e := f.db.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(f.ctx, `SELECT revoked_at IS NOT NULL FROM identity_sessions WHERE token_digest=$1`, digest[:]).Scan(&got)
	}); e != nil || got != revoked {
		t.Fatal("prior session revocation mismatch")
	}
}
func TestSigninFreshnessRequiredServiceCookies(t *testing.T) {
	for _, mode := range []string{"same_person", "cross_person", "foreign_realm", "malformed", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			f := newSigninFixture(t)
			plain, sessions := f.service(t, f.db)
			prior := f.success(t, f.post(plain), sessions)
			cookies := []*http.Cookie{prior}
			switch mode {
			case "cross_person":
				person, email, credentialID := newID(t), newID(t), newID(t)
				address := email.String() + "@example.test"
				f.exec(t, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, person, f.cfg.InstallationID, f.cfg.ApplicationID)
				f.exec(t, `INSERT INTO identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key,verified_at) VALUES($1,$2,$3,$4,$5,$5,clock_timestamp())`, email, person, f.cfg.InstallationID, f.cfg.ApplicationID, address)
				f.exec(t, `INSERT INTO identity_credentials(id,person_id,method,verifier_hash) VALUES($1,$2,'email_password',$3)`, credentialID, person, f.hash)
				f.person, f.email, f.credentialID, f.address = person, email, credentialID, address
			case "foreign_realm":
				other := newSigninFixture(t)
				otherService, otherSessions := other.service(t, other.db)
				prior = other.success(t, other.post(otherService), otherSessions)
				cookies = []*http.Cookie{prior}
			case "malformed":
				cookies = []*http.Cookie{{Name: prior.Name, Value: "malformed"}}
			case "duplicate":
				cookies = append(cookies, prior)
			}
			before := f.count(t)
			w := f.post(plain, cookies...)
			if mode == "duplicate" {
				freshnessDenied(t, w, 503)
				if f.count(t) != before {
					t.Fatal("duplicate cookie committed a session")
				}
				f.assertCookieState(t, prior, false)
				return
			}
			f.success(t, w, sessions)
			if f.count(t) != before+1 {
				t.Fatal("issuance row count mismatch")
			}
			f.assertCookieState(t, prior, mode == "same_person" || mode == "cross_person")
		})
	}
}

// A pause AFTER the real issuance callback keeps its actual row locks held.
// The observer runs on that same connection, so no third pool slot is needed.
func TestSigninFreshnessRequiredServiceHeldRows(t *testing.T) {
	for _, row := range []string{"person", "email", "credentialID"} {
		t.Run(row, func(t *testing.T) {
			f := newSigninFixture(t)
			calls := 0
			writerPID := make(chan int, 1)
			writerDone := make(chan error, 1)
			joined := make(chan struct{})
			observed := false
			runner := loginRunnerFunc(func(ctx context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
				calls++
				if calls != 2 {
					return f.db.WithTx(ctx, o, fn)
				}
				return f.db.WithTx(ctx, o, func(tx *sql.Tx) error {
					if e := fn(tx); e != nil {
						return e
					}
					table, id := "identity_persons", f.person
					if row == "email" {
						table, id = "identity_emails", f.email
					}
					if row == "credentialID" {
						table, id = "identity_credentials", f.credentialID
					}
					go func() {
						defer close(joined)
						writerDone <- f.db.WithTx(f.ctx, nil, func(writer *sql.Tx) error {
							var pid int
							if e := writer.QueryRowContext(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
								return e
							}
							writerPID <- pid
							_, e := writer.ExecContext(f.ctx, `UPDATE `+table+` SET id=id WHERE id=$1`, id)
							return e
						})
					}()
					t.Cleanup(func() {
						select {
						case <-joined:
						case <-time.After(5 * time.Second):
							t.Error("writer failed to terminate")
						}
					})
					var pid int
					select {
					case pid = <-writerPID:
					case <-ctx.Done():
						return ctx.Err()
					}
					observe, stop := context.WithTimeout(ctx, 2*time.Second)
					defer stop()
					signinObservedWait(t, observe, tx, pid)
					observed = true
					return nil
				})
			})
			s, _ := f.service(t, runner)
			w := f.post(s)
			if !observed {
				t.Fatal("writer lock wait not actually observed")
			}
			select {
			case e := <-writerDone:
				if e != nil {
					t.Fatal("writer failed after issuance commit")
				}
			case <-f.ctx.Done():
				t.Fatal("writer did not finish")
			}
			<-joined
			_, sessions := f.service(t, f.db)
			f.success(t, w, sessions)
		})
	}
}

func TestSigninFreshnessRequiredServiceGeneric(t *testing.T) {
	for _, mode := range []string{"unknown", "malformed", "wrong", "unverified", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			f := newSigninFixture(t)
			address, suppliedPassword := f.address, freshnessSecret
			switch mode {
			case "unknown":
				address = "missing@example.test"
			case "malformed":
				address = "not an address"
			case "wrong":
				suppliedPassword = "a wrong sufficiently long password"
			case "unverified":
				f.exec(t, `UPDATE identity_emails SET verified_at=NULL WHERE id=$1`, f.email)
			case "inactive":
				f.exec(t, `UPDATE identity_persons SET state='self_disabled' WHERE id=$1`, f.person)
			}
			s, _ := f.service(t, f.db)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, freshnessRequest(f.ctx, address, suppliedPassword))
			freshnessDenied(t, w, 401)
			if f.budget.calls.Load() != 1 || f.count(t) != 0 {
				t.Fatal("generic denial skipped verification budget or issued a session")
			}
		})
	}
}
