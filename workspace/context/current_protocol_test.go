package context

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/session"
	"github.com/google/uuid"
)

// These driver scripts test data handling and statement/argument protocol only.
// They do not model PostgreSQL locks or qualify a native credential producer.
type currentQuery struct {
	contains string
	args     []any
	rows     [][]driver.Value
	columns  int
	err      error
	after    func()
}
type currentScript struct {
	t       *testing.T
	queries []currentQuery
}
type currentConnector struct{ script *currentScript }
type currentDriver struct{}
type currentConn struct{ script *currentScript }
type currentDriverTx struct{}
type currentRows struct {
	values  [][]driver.Value
	columns int
}

func (c currentConnector) Connect(context.Context) (driver.Conn, error) {
	return &currentConn{c.script}, nil
}
func (currentConnector) Driver() driver.Driver { return currentDriver{} }
func (currentDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("script requires connector")
}
func (*currentConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*currentConn) Close() error              { return nil }
func (*currentConn) Begin() (driver.Tx, error) { return currentDriverTx{}, nil }
func (currentDriverTx) Commit() error          { return nil }
func (currentDriverTx) Rollback() error        { return nil }
func (c *currentConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if len(c.script.queries) == 0 {
		c.script.t.Error("unexpected extra SQL query")
		return nil, errors.New("unexpected query")
	}
	q := c.script.queries[0]
	c.script.queries = c.script.queries[1:]
	normalized := strings.Join(strings.Fields(query), " ")
	if !strings.Contains(normalized, q.contains) {
		c.script.t.Errorf("SQL protocol: got %q want %q", normalized, q.contains)
	}
	if q.args != nil {
		got := make([]any, len(args))
		for i, a := range args {
			got[i] = a.Value
		}
		if !reflect.DeepEqual(got, q.args) {
			c.script.t.Errorf("SQL argument scope mismatch: got %v want %v", got, q.args)
		}
	}
	if q.after != nil {
		q.after()
	}
	if q.err != nil {
		return nil, q.err
	}
	n := q.columns
	if len(q.rows) > 0 {
		n = len(q.rows[0])
	}
	return &currentRows{q.rows, n}, nil
}
func (r *currentRows) Columns() []string { return make([]string, r.columns) }
func (*currentRows) Close() error        { return nil }
func (r *currentRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}
func currentScriptDB(t *testing.T, queries []currentQuery) *sql.DB {
	t.Helper()
	script := &currentScript{t: t, queries: queries}
	db := sql.OpenDB(currentConnector{script})
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
		if len(script.queries) != 0 {
			t.Errorf("%d required SQL queries not executed", len(script.queries))
		}
	})
	return db
}

type currentSQLRunner struct{ db *sql.DB }

func (r currentSQLRunner) WithTx(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func currentPrincipalFromSession(t *testing.T, runner session.TxRunner, cfg Config, token string) identity.Principal {
	t.Helper()
	svc, err := session.NewWithTxRunner(runner, session.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, AllowedOrigins: []string{"https://workspace.example.test"}, CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	var p identity.Principal
	reached := false
	h := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		p, ok = identity.PrincipalFromContext(r.Context())
		reached = ok
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "https://workspace.example.test/", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !reached || rec.Code != http.StatusNoContent {
		t.Fatalf("synthetic session principal unavailable (status %d)", rec.Code)
	}
	return p
}
func currentSyntheticPrincipal(t *testing.T, cfg Config, person uuid.UUID, epoch int64) identity.Principal {
	t.Helper()
	now := time.Now().UTC().Add(-time.Minute)
	sid := newID(t)
	db := currentScriptDB(t, []currentQuery{
		{contains: "SHOW transaction_isolation", rows: [][]driver.Value{{"read committed"}}},
		{contains: "FROM identity_sessions", rows: [][]driver.Value{{sid.String()}}},
		{contains: "SELECT clock_timestamp()", rows: [][]driver.Value{{now}}},
		{contains: "UPDATE identity_sessions s", rows: [][]driver.Value{{sid.String(), person.String(), cfg.InstallationID.String(), cfg.ApplicationID.String(), cfg.EnvironmentID.String(), epoch, "email_password", now}}},
	})
	return currentPrincipalFromSession(t, currentSQLRunner{db}, cfg, base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
}

func TestCurrentTxProtocolAndFailureMapping(t *testing.T) {
	cfg := Config{newID(t), newID(t), newID(t)}
	person, wid, mid := newID(t), newID(t), newID(t)
	p := currentSyntheticPrincipal(t, cfg, person, 4)
	r, err := NewForTransactions(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	mode := currentQuery{contains: "SELECT pg_catalog.current_setting('transaction_isolation'), pg_catalog.current_setting('transaction_read_only')", rows: [][]driver.Value{{"read committed", "off"}}}
	personRow := currentQuery{contains: "FROM public.identity_persons WHERE id=$1 AND installation_id=$2 AND application_id=$3 FOR SHARE", args: []any{person.String(), cfg.InstallationID.String(), cfg.ApplicationID.String()}, rows: [][]driver.Value{{"active", int64(4)}}}
	discover := currentQuery{contains: "FROM public.workspaces WHERE installation_id=$1 AND application_id=$2 AND personal_owner_id=$3 AND kind='personal' LIMIT 2", args: []any{cfg.InstallationID.String(), cfg.ApplicationID.String(), person.String()}, rows: [][]driver.Value{{wid.String()}}}
	workspace := func(kind, state string, owner driver.Value) currentQuery {
		return currentQuery{contains: "FROM public.workspaces WHERE id=$1 AND installation_id=$2 AND application_id=$3 FOR SHARE", args: []any{wid.String(), cfg.InstallationID.String(), cfg.ApplicationID.String()}, rows: [][]driver.Value{{wid.String(), cfg.InstallationID.String(), cfg.ApplicationID.String(), kind, state, owner, int64(2), now, now}}}
	}
	member := currentQuery{contains: "FROM public.workspace_memberships WHERE installation_id=$1 AND application_id=$2 AND workspace_id=$3 AND person_id=$4 FOR SHARE", args: []any{cfg.InstallationID.String(), cfg.ApplicationID.String(), wid.String(), person.String()}, rows: [][]driver.Value{{mid.String(), cfg.InstallationID.String(), cfg.ApplicationID.String(), wid.String(), person.String(), "member", int64(1), "active", int64(3), now, now}}}
	role := currentQuery{contains: "SELECT pg_catalog.array_to_json(permissions) FROM public.workspace_role_versions WHERE role_key=$1 AND version=$2", args: []any{"member", int64(1)}, rows: [][]driver.Value{{`["workspace.members.read"]`}}}
	absent := func(q currentQuery) currentQuery { q.columns = len(q.rows[0]); q.rows = nil; return q }
	change := func(q currentQuery, index int, value driver.Value) currentQuery {
		q.rows = [][]driver.Value{append([]driver.Value(nil), q.rows[0]...)}
		q.rows[0][index] = value
		return q
	}
	cases := []struct {
		name     string
		selector uuid.UUID
		queries  []currentQuery
		want     error
	}{
		{"personal explicit", wid, []currentQuery{mode, personRow, workspace("personal", "active", person.String())}, nil},
		{"personal default", uuid.Nil, []currentQuery{mode, personRow, discover, workspace("personal", "active", person.String())}, nil},
		{"organization", wid, []currentQuery{mode, personRow, workspace("organization", "active", nil), member, role}, nil},
		{"missing person", wid, []currentQuery{mode, absent(personRow)}, ErrDenied},
		{"stale person", wid, []currentQuery{mode, change(personRow, 1, int64(5))}, ErrDenied},
		{"disabled person", wid, []currentQuery{mode, change(personRow, 0, "self_disabled")}, ErrDenied},
		{"missing default", uuid.Nil, []currentQuery{mode, personRow, absent(discover)}, ErrDenied},
		{"missing workspace", wid, []currentQuery{mode, personRow, absent(workspace("personal", "active", person.String()))}, ErrDenied},
		{"foreign owner", wid, []currentQuery{mode, personRow, workspace("personal", "active", newID(t).String())}, ErrDenied},
		{"inactive workspace", wid, []currentQuery{mode, personRow, workspace("personal", "suspended", person.String())}, ErrDenied},
		{"unknown workspace", wid, []currentQuery{mode, personRow, workspace("unexpected", "active", nil)}, ErrUnavailable},
		{"changed default kind", uuid.Nil, []currentQuery{mode, personRow, discover, workspace("organization", "active", nil)}, ErrUnavailable},
		{"missing membership", wid, []currentQuery{mode, personRow, workspace("organization", "active", nil), absent(member)}, ErrDenied},
		{"suspended membership", wid, []currentQuery{mode, personRow, workspace("organization", "active", nil), change(member, 7, "suspended")}, ErrDenied},
		{"left membership", wid, []currentQuery{mode, personRow, workspace("organization", "active", nil), change(member, 7, "left")}, ErrDenied},
		{"unknown role", wid, []currentQuery{mode, personRow, workspace("organization", "active", nil), change(member, 5, "unexpected")}, ErrUnavailable},
		{"missing role", wid, []currentQuery{mode, personRow, workspace("organization", "active", nil), member, absent(role)}, ErrUnavailable},
		{"null permissions", wid, []currentQuery{mode, personRow, workspace("organization", "active", nil), member, change(role, 0, nil)}, ErrUnavailable},
		{"malformed permissions", wid, []currentQuery{mode, personRow, workspace("organization", "active", nil), member, change(role, 0, `["admin"]`)}, ErrUnavailable},
		{"repeatable read", wid, []currentQuery{change(mode, 0, "repeatable read")}, ErrUnavailable},
		{"read only", wid, []currentQuery{change(mode, 1, "on")}, ErrUnavailable},
	}
	duplicate := discover
	duplicate.rows = append(append([][]driver.Value(nil), discover.rows...), []driver.Value{newID(t).String()})
	cases = append(cases, struct {
		name     string
		selector uuid.UUID
		queries  []currentQuery
		want     error
	}{"ambiguous default", uuid.Nil, []currentQuery{mode, personRow, duplicate}, ErrUnavailable})
	for i := range 6 {
		sequence := []currentQuery{mode, personRow, workspace("organization", "active", nil), member, role}
		if i == 5 {
			sequence = []currentQuery{mode, personRow, discover}
		}
		index := i
		if i == 5 {
			index = 2
		}
		sequence[index].err = errors.New("injected query failure")
		sequence = sequence[:index+1]
		cases = append(cases, struct {
			name     string
			selector uuid.UUID
			queries  []currentQuery
			want     error
		}{"query failure " + string(rune('0'+i)), map[bool]uuid.UUID{true: uuid.Nil, false: wid}[i == 5], sequence, ErrUnavailable})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := currentScriptDB(t, tc.queries)
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			s, err := r.ResolveCurrentTx(context.Background(), tx, p, tc.selector)
			if tc.want != nil {
				assertCurrentError(t, s, err, tc.want)
				return
			}
			if err != nil || s.Workspace.ID != wid || s.Workspace.Epoch != 2 {
				t.Fatalf("current selection failed: %v", err)
			}
			if s.Workspace.Kind == "organization" && (s.Membership == nil || s.Membership.ID != mid || s.MembershipEpoch != 3 || !reflect.DeepEqual(s.Permissions, []string{"workspace.members.read"})) {
				t.Fatal("organization authority omitted")
			}
		})
	}
	t.Run("cancellation after final query", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		last := workspace("personal", "active", person.String())
		last.after = cancel
		db := currentScriptDB(t, []currentQuery{mode, personRow, last})
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		s, err := r.ResolveCurrentTx(ctx, tx, p, wid)
		assertCurrentError(t, s, err, ErrUnavailable)
	})
	t.Run("invalid selector and realm before SQL", func(t *testing.T) {
		db := currentScriptDB(t, nil)
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		s, err := r.ResolveCurrentTx(context.Background(), tx, p, uuid.New())
		assertCurrentError(t, s, err, ErrInvalidSelector)
		wrong := cfg
		wrong.EnvironmentID = newID(t)
		other, err := NewForTransactions(wrong)
		if err != nil {
			t.Fatal(err)
		}
		s, err = other.ResolveCurrentTx(context.Background(), tx, p, wid)
		assertCurrentError(t, s, err, ErrDenied)
		s, err = r.ResolveCurrentTx(context.Background(), tx, identity.Principal{}, wid)
		assertCurrentError(t, s, err, ErrUnavailable)
	})
	t.Run("closed transaction", func(t *testing.T) {
		db := currentScriptDB(t, nil)
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		s, err := r.ResolveCurrentTx(context.Background(), tx, p, wid)
		assertCurrentError(t, s, err, ErrUnavailable)
	})
}
