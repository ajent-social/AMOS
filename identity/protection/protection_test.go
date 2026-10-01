package protection

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func fixture(t *testing.T, limit int) (*Limiter, *session.Service) {
	t.Helper()
	_, schema := testkit.NewPostgres(t)
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := storage.Open(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var fragments []migrations.Fragment
	for i, name := range []string{"identity", "identity-protection"} {
		b, e := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", name+".sql"))
		if e != nil {
			t.Fatal(e)
		}
		fragments = append(fragments, migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: uint64(i + 1), Name: strings.ReplaceAll(name, "-", "_") + "_base", SQL: string(b)}}})
	}
	registry, err := migrations.NewRegistry(fragments...)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Migrate(context.Background(), db, registry); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal(err)
	}
	id := func() uuid.UUID {
		v, e := uuid.NewV7()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	limiter, err := New(Config{DB: db, InstallationID: id(), ApplicationID: id(), EnvironmentID: id(), Key: key, Window: time.Minute, IPLimit: limit, AccountLimit: limit})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.New(db, session.Config{InstallationID: limiter.cfg.InstallationID, ApplicationID: limiter.cfg.ApplicationID, EnvironmentID: limiter.cfg.EnvironmentID, AllowedOrigins: []string{"https://app.example.test"}, CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	return limiter, sessions
}
func TestT3_7_DurableConcurrentExhaustion(t *testing.T) {
	l, _ := fixture(t, 7)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := l.Allow(context.Background(), Signin, "192.0.2.7", "absent@example.test")
			if e != nil {
				t.Error(e)
				return
			}
			if v.Allowed {
				accepted.Add(1)
			} else if v.RetryAfter <= 0 {
				t.Error("missing retry hint")
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 7 {
		t.Fatalf("accepted=%d", accepted.Load())
	}
	other := *l
	other.cfg.Key = append([]byte(nil), l.cfg.Key...)
	v, e := other.Allow(context.Background(), Signin, "192.0.2.7", "new@example.test")
	if e != nil || v.Allowed {
		t.Fatalf("replica bypass %v %v", v, e)
	}
	var accounts int
	err := l.cfg.DB.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM identity_auth_limits WHERE dimension='account'`).Scan(&accounts)
	})
	if err != nil || accounts != 1 {
		t.Fatalf("denied IP expanded accounts=%d err=%v", accounts, err)
	}
	// Account budgets also bind distinct peers, without an account existence query.
	for i := 0; i < 7; i++ {
		v, e = l.Allow(context.Background(), Recovery, fmt.Sprintf("192.0.2.%d", i+10), "another@example.test")
		if e != nil || !v.Allowed {
			t.Fatal(v, e)
		}
	}
	v, e = l.Allow(context.Background(), Recovery, "192.0.2.40", "another@example.test")
	if e != nil || v.Allowed {
		t.Fatal("account budget bypass", v, e)
	}
	if err = l.cfg.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, e = l.Allow(context.Background(), Signup, "192.0.2.5", "x@example.test"); e != ErrUnavailable {
		t.Fatalf("outage=%v", e)
	}
}
func TestT3_7_PublicBoundaryBeforeExpensiveWork(t *testing.T) {
	l, s := fixture(t, 2)
	called := 0
	h, e := (Guard{l, s}).PublicJSON(Signup, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(202) }))
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		origin, body string
		want         int
	}{{"https://evil.example.test", `{"email":"x@example.test"}`, 403}, {"https://app.example.test", strings.Repeat("x", 4097), 413}, {"https://app.example.test", `{"email":"absent@example.test"}`, 202}, {"https://app.example.test", `{"email":"absent@example.test"}`, 202}, {"https://app.example.test", `{"email":"existing@example.test"}`, 429}} {
		r := httptest.NewRequest("POST", "https://app.example.test/signup", strings.NewReader(tc.body))
		r.RemoteAddr = "192.0.2.5:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d", called+30))
		r.Header.Set("CF-Connecting-IP", "192.0.2.99")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("status=%d want=%d", w.Code, tc.want)
		}
		if tc.want == 429 && w.Header().Get("Retry-After") == "" {
			t.Fatal("missing retry")
		}
	}
	if called != 2 {
		t.Fatalf("expensive calls=%d", called)
	}
}
func TestT3_7_CrossOriginPasswordChangeLeavesCredentialUnchanged(t *testing.T) {
	l, s := fixture(t, 20)
	person, _ := uuid.NewV7()
	credential, _ := uuid.NewV7()
	verifier := fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(make([]byte, 16)), base64.RawStdEncoding.EncodeToString(make([]byte, 32)))
	err := l.cfg.DB.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, person, l.cfg.InstallationID, l.cfg.ApplicationID)
		if e != nil {
			return e
		}
		_, e = tx.Exec(`INSERT INTO identity_credentials(id,person_id,method,verifier_hash) VALUES($1,$2,'email_password',$3)`, credential, person, verifier)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := authproof.NewVerifiedCredential(person, l.cfg.InstallationID, l.cfg.ApplicationID, l.cfg.EnvironmentID, 0, "email_password", time.Now().UTC(), "aal1", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.Issue(context.Background(), proof, "")
	if err != nil {
		t.Fatal(err)
	}
	mutate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e := l.cfg.DB.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
			_, e := tx.Exec(`UPDATE identity_credentials SET verifier_hash=$1 WHERE id=$2`, strings.Replace(verifier, "p=1", "p=2", 1), credential)
			return e
		})
		if e != nil {
			t.Error(e)
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	})
	h, err := (Guard{l, s}).CookieMutation(PasswordChange, mutate)
	if err != nil {
		t.Fatal(err)
	}
	// Identity-subtree test fixture supplies verified context to isolate this
	// guard. Production composition uses authoritative session middleware.
	for _, tc := range []struct {
		origin, token string
		want          int
	}{{"https://evil.example.test", issued.CSRFToken, 403}, {"https://app.example.test", "", 403}, {"https://app.example.test", "wrong", 403}, {"https://app.example.test", issued.CSRFToken, 204}} {
		r := httptest.NewRequest("POST", "https://app.example.test/account/password", strings.NewReader("{}"))
		r = r.WithContext(identity.ContextWithVerifiedCredential(r.Context(), proof))
		r.RemoteAddr = "192.0.2.5:1234"
		r.AddCookie(issued.Cookie)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-CSRF-Token", tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("mutation=%d want=%d", w.Code, tc.want)
		}
		var actual string
		e := l.cfg.DB.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
			return tx.QueryRow(`SELECT verifier_hash FROM identity_credentials WHERE id=$1`, credential).Scan(&actual)
		})
		if e != nil {
			t.Fatal(e)
		}
		if tc.want != 204 && actual != verifier {
			t.Fatal("denied mutation changed credential")
		}
		if tc.want == 204 && actual == verifier {
			t.Fatal("accepted mutation did not execute")
		}
	}
}
func TestT3_7_RedirectAndPeerInputs(t *testing.T) {
	for _, target := range []string{"https://evil.example", "//evil.example", "/\\evil", "/%2f%2fevil", "/a/../b", "/%252f%252fevil", "/a//b", "/a\r\nLocation: evil", "/x#fragment", "/%00"} {
		if _, e := RelativeRedirect(target); e == nil {
			t.Errorf("accepted %q", target)
		}
	}
	for _, target := range []string{"/", "/todos", "/account?tab=billing"} {
		if got, e := RelativeRedirect(target); e != nil || got != target {
			t.Errorf("valid=%q %v", got, e)
		}
	}
	r := httptest.NewRequest("POST", "/signup", nil)
	r.RemoteAddr = "[::ffff:192.0.2.5]:456"
	r.Header.Set("X-Forwarded-For", "198.51.100.2")
	if ip, e := PeerIP(r); e != nil || ip != "192.0.2.5" {
		t.Fatal(ip, e)
	}
}

func TestT3_7_ExpiryScopeAndConfiguration(t *testing.T) {
	l, _ := fixture(t, 1)
	ctx := context.Background()
	first, e := l.Allow(ctx, Signin, "192.0.2.9", "x@example.test")
	if e != nil || !first.Allowed {
		t.Fatal(first, e)
	}
	denied, e := l.Allow(ctx, Signin, "192.0.2.9", "x@example.test")
	if e != nil || denied.Allowed {
		t.Fatal(denied, e)
	}
	// Expiry is driven by database state, not a wall-clock sleep.
	e = l.cfg.DB.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, e := tx.Exec(`UPDATE identity_auth_limits SET window_start=transaction_timestamp()-interval '2 hour',window_end=transaction_timestamp()-interval '1 hour'`)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	renewed, e := l.Allow(ctx, Signin, "192.0.2.9", "x@example.test")
	if e != nil || !renewed.Allowed {
		t.Fatal(renewed, e)
	}
	cfg := l.cfg
	cfg.EnvironmentID, _ = uuid.NewV7()
	other, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	admitted, e := other.Allow(ctx, Signin, "192.0.2.9", "x@example.test")
	if e != nil || !admitted.Allowed {
		t.Fatal("environment collision", admitted, e)
	}
	for _, change := range []func(*Config){func(c *Config) { c.Key = []byte("short") }, func(c *Config) { c.Window = time.Millisecond }, func(c *Config) { c.Window = time.Minute + 1 }, func(c *Config) { c.IPLimit = 0 }, func(c *Config) { c.EnvironmentID = uuid.Nil }} {
		bad := l.cfg
		change(&bad)
		if _, e := New(bad); e != ErrConfiguration {
			t.Fatal("invalid configuration accepted")
		}
	}
	if _, e = l.Allow(ctx, Operation("callback"), "192.0.2.9", "x"); e != ErrConfiguration {
		t.Fatal("unbound callback admitted")
	}
}

func TestT3_7_PruneIsBoundedScopedAndPreservesLiveBudgets(t *testing.T) {
	l, _ := fixture(t, 2)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, e := l.Allow(ctx, Signin, fmt.Sprintf("192.0.2.%d", 100+i), fmt.Sprintf("prune%d@example.test", i)); e != nil {
			t.Fatal(e)
		}
	}
	cfg := l.cfg
	cfg.EnvironmentID, _ = uuid.NewV7()
	other, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.Allow(ctx, Signin, "192.0.2.200", "other@example.test"); e != nil {
		t.Fatal(e)
	}
	e = l.cfg.DB.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, e := tx.Exec(`UPDATE identity_auth_limits SET window_start=transaction_timestamp()-interval '2 hour',window_end=transaction_timestamp()-interval '1 hour' WHERE dimension='account'`)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	n, e := l.PruneExpired(ctx, 2)
	if e != nil || n != 2 {
		t.Fatal(n, e)
	}
	n, e = l.PruneExpired(ctx, 2)
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
	n, e = l.PruneExpired(ctx, 2)
	if e != nil || n != 0 {
		t.Fatal(n, e)
	}
	n, e = other.PruneExpired(ctx, 2)
	if e != nil || n != 1 {
		t.Fatal("cross-scope prune", n, e)
	}
	var count int
	e = l.cfg.DB.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM identity_auth_limits WHERE dimension='ip'`).Scan(&count)
	})
	if e != nil || count != 4 {
		t.Fatal("live budget deleted", count, e)
	}
	if _, e = l.PruneExpired(ctx, 1001); e != ErrConfiguration {
		t.Fatal("unbounded prune accepted")
	}
}

func TestT3_7_PasswordWorkNeedsOneMatchingDurableAdmission(t *testing.T) {
	l, s := fixture(t, 2)
	budget := l.PasswordBudget()
	if e := budget.Allow(context.Background(), "signin:x@example.test"); e != ErrUnavailable {
		t.Fatal("unadmitted expensive work accepted")
	}
	called := 0
	h, e := (Guard{l, s}).PublicJSON(Signin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if e := budget.Allow(r.Context(), "register:x@example.test"); e != ErrUnavailable {
			t.Fatal("wrong operation admitted")
		}
		if e := budget.Allow(r.Context(), "signin:other@example.test"); e != ErrUnavailable {
			t.Fatal("wrong account admitted")
		}
		if e := budget.Allow(r.Context(), "signin:x@example.test"); e != nil {
			t.Fatal("matching work denied", e)
		}
		if e := budget.Allow(r.Context(), "signin:x@example.test"); e != ErrUnavailable {
			t.Fatal("repeated expensive work admitted")
		}
		w.WriteHeader(204)
	}))
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "https://app.example.test/auth", strings.NewReader(`{"email":" X@example.test "}`))
	r.RemoteAddr = "192.0.2.8:987"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://app.example.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || called != 1 {
		t.Fatal(w.Code, called)
	}
}

func TestPasswordMutationAdmissionIsPurposeBoundAndFinite(t *testing.T) {
	limiter := &Limiter{}
	guard := Guard{Limiter: limiter}
	budget := limiter.PasswordBudget()
	personID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	person := personID.String()
	proof := guard.admittedContext(context.Background(), PasswordChange, person)
	if e := budget.Allow(proof, "change-new:"+person); e == nil {
		t.Fatal("replacement hashing admitted before current verification budget")
	}
	if e := budget.Allow(proof, "change-current:"+person); e != nil {
		t.Fatal(e)
	}
	if e := budget.Allow(proof, "change-current:"+person); e == nil {
		t.Fatal("current verifier work repeated")
	}
	if e := budget.Allow(proof, "change-new:"+person); e != nil {
		t.Fatal(e)
	}
	if e := budget.Allow(proof, "change-new:"+person); e == nil {
		t.Fatal("replacement hashing repeated")
	}
	reset := guard.admittedContext(context.Background(), Recovery, person)
	if e := budget.Allow(reset, "register:"+person); e == nil {
		t.Fatal("recovery admitted registration work")
	}
	if e := budget.Allow(reset, "reset:"+person); e != nil {
		t.Fatal(e)
	}
	if e := budget.Allow(reset, "reset:"+person); e == nil {
		t.Fatal("reset hashing repeated")
	}
}

func TestMFAAdmissionIsBoundToCurrentPersonAndUsedOnce(t *testing.T) {
	limiter := &Limiter{}
	guard := Guard{Limiter: limiter}
	budget := limiter.PasswordBudget()
	person, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	proof := guard.admittedContext(context.Background(), MFA, person.String())
	if err := budget.Allow(proof, "mfa-current:another-person"); err == nil {
		t.Fatal("foreign primary verification admitted")
	}
	if err := budget.Allow(proof, "signin:"+person.String()); err == nil {
		t.Fatal("MFA admitted a different operation")
	}
	if err := budget.Allow(proof, "mfa-current:"+person.String()); err != nil {
		t.Fatal(err)
	}
	if err := budget.Allow(proof, "mfa-current:"+person.String()); err == nil {
		t.Fatal("primary password verification repeated")
	}
}
