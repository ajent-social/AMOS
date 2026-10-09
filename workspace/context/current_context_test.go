package context

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/session"
	"github.com/google/uuid"
)

// Obtain context through the existing native middleware with a scripted SQL
// driver. This is a pure binding/protocol test, not login or live SQL evidence.
func contextBindingPrincipal(t *testing.T, cfg Config, person uuid.UUID, epoch int64, at time.Time) (context.Context, identity.Principal) {
	t.Helper()
	sid := newID(t)
	db := currentScriptDB(t, []currentQuery{
		{contains: "SHOW transaction_isolation", rows: [][]driver.Value{{"read committed"}}},
		{contains: "FROM identity_sessions", rows: [][]driver.Value{{sid.String()}}},
		{contains: "SELECT clock_timestamp()", rows: [][]driver.Value{{at.Add(time.Minute)}}},
		{contains: "UPDATE identity_sessions s", rows: [][]driver.Value{{sid.String(), person.String(), cfg.InstallationID.String(), cfg.ApplicationID.String(), cfg.EnvironmentID.String(), epoch, "email_password", at}}},
	})
	return contextBindingAdmission(t, currentSQLRunner{db}, cfg, base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
}
func contextBindingAdmission(t *testing.T, runner session.TxRunner, cfg Config, token string) (context.Context, identity.Principal) {
	t.Helper()
	svc, err := session.NewWithTxRunner(runner, session.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, AllowedOrigins: []string{"https://workspace.example.test"}, CookieSecure: true})
	if err != nil {
		t.Fatal("session setup failed")
	}
	var ctx context.Context
	var p identity.Principal
	handler := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		p, ok = identity.PrincipalFromContext(r.Context())
		if !ok {
			t.Error("native principal missing")
		}
		// The scripted unit harness outlives this synthetic HTTP call;
		// retain its values and supply explicit test cancellation below.
		ctx = context.WithoutCancel(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "https://workspace.example.test/", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || ctx == nil {
		t.Fatal("native context admission failed")
	}
	return ctx, p
}
func bindingQueries(cfg Config, person, wid uuid.UUID, organization bool) []currentQuery {
	now := time.Now().UTC()
	kind := "personal"
	var owner driver.Value = person.String()
	if organization {
		kind = "organization"
		owner = nil
	}
	q := []currentQuery{
		{contains: "SELECT pg_catalog.current_setting('transaction_isolation'), pg_catalog.current_setting('transaction_read_only')", rows: [][]driver.Value{{"read committed", "off"}}},
		{contains: "FROM public.identity_persons WHERE id=$1 AND installation_id=$2 AND application_id=$3 FOR SHARE", args: []any{person.String(), cfg.InstallationID.String(), cfg.ApplicationID.String()}, rows: [][]driver.Value{{"active", int64(4)}}},
		{contains: "FROM public.workspaces WHERE id=$1 AND installation_id=$2 AND application_id=$3 FOR SHARE", args: []any{wid.String(), cfg.InstallationID.String(), cfg.ApplicationID.String()}, rows: [][]driver.Value{{wid.String(), cfg.InstallationID.String(), cfg.ApplicationID.String(), kind, "active", owner, int64(2), now, now}}},
	}
	if organization {
		q = append(q, currentQuery{contains: "FROM public.workspace_memberships", rows: [][]driver.Value{{uuid.MustParse("01900000-0000-7000-8000-000000000501").String(), cfg.InstallationID.String(), cfg.ApplicationID.String(), wid.String(), person.String(), "member", int64(1), "active", int64(3), now, now}}}, currentQuery{contains: "SELECT pg_catalog.array_to_json(permissions)", rows: [][]driver.Value{{`["workspace.members.read"]`}}})
	}
	return q
}
func bindingTx(t *testing.T, q []currentQuery) *sql.Tx {
	t.Helper()
	db := currentScriptDB(t, q)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}
func requireNoBinding(t *testing.T, ctx context.Context, s Selection, err, want error) {
	t.Helper()
	if ctx != nil || !reflect.DeepEqual(s, Selection{}) || err != want {
		t.Fatalf("failure returned binding or wrong class: %v", err)
	}
}

func TestCurrentContextBindingValidation(t *testing.T) {
	cfg := Config{newID(t), newID(t), newID(t)}
	person, wid := newID(t), newID(t)
	at := time.Now().UTC().Add(-time.Minute)
	ctx, p := contextBindingPrincipal(t, cfg, person, 4, at)
	for _, name := range []string{"missing", "nil_context", "nil_tx", "nil_resolver", "bad_config", "canceled", "installation", "application", "environment", "person", "epoch", "authenticated_at", "zero_principal"} {
		t.Run(name, func(t *testing.T) {
			r, _ := NewForTransactions(cfg)
			request := ctx
			fresh := p
			tx := bindingTx(t, nil)
			want := ErrDenied
			changedCfg := cfg
			changedPerson := person
			epoch := int64(4)
			authAt := at
			switch name {
			case "missing":
				request = context.Background()
			case "nil_context":
				request = nil
				want = ErrUnavailable
			case "nil_tx":
				tx = nil
				want = ErrUnavailable
			case "nil_resolver":
				r = nil
				want = ErrUnavailable
			case "bad_config":
				r = &Resolver{}
				want = ErrUnavailable
			case "canceled":
				var cancel context.CancelFunc
				request, cancel = context.WithCancel(ctx)
				cancel()
				want = ErrUnavailable
			case "installation":
				changedCfg.InstallationID = newID(t)
			case "application":
				changedCfg.ApplicationID = newID(t)
			case "environment":
				changedCfg.EnvironmentID = newID(t)
			case "person":
				changedPerson = newID(t)
			case "epoch":
				epoch++
			case "authenticated_at":
				authAt = authAt.Add(-time.Second)
			case "zero_principal":
				fresh = identity.Principal{}
			}
			switch name {
			case "installation", "application", "environment", "person", "epoch", "authenticated_at":
				_, fresh = contextBindingPrincipal(t, changedCfg, changedPerson, epoch, authAt)
			}
			bound, s, err := r.ResolveCurrentContextTx(request, tx, fresh, wid)
			requireNoBinding(t, bound, s, err, want)
		})
	}
}
func TestCurrentContextBindingCopiesAndCancellation(t *testing.T) {
	for _, organization := range []bool{false, true} {
		t.Run("selection", func(t *testing.T) {
			cfg := Config{newID(t), newID(t), newID(t)}
			person, wid := newID(t), newID(t)
			ctx, p := contextBindingPrincipal(t, cfg, person, 4, time.Now().UTC().Add(-time.Minute))
			type valueKey struct{}
			ctx = context.WithValue(ctx, valueKey{}, "retained")
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			r, _ := NewForTransactions(cfg)
			tx := bindingTx(t, bindingQueries(cfg, person, wid, organization))
			bound, s, err := r.ResolveCurrentContextTx(ctx, tx, p, wid)
			if err != nil || bound == nil || s.Workspace.ID != wid || bound.Value(valueKey{}) != "retained" {
				t.Fatal("successful binding failed")
			}
			if _, ok := FromContext(ctx); ok {
				t.Fatal("input context changed")
			}
			native, ok := identity.PrincipalFromContext(bound)
			if !ok || !sameCurrentPrincipal(native, p) {
				t.Fatal("principal changed")
			}
			s.Workspace.ID = uuid.Nil
			if organization {
				s.Permissions[0] = "mutated"
				s.Membership.Role = "mutated"
			}
			first, ok := FromContext(bound)
			if !ok || first.Workspace.ID != wid {
				t.Fatal("returned selection aliases binding")
			}
			if organization {
				if first.Permissions[0] != "workspace.members.read" || first.Membership.Role != "member" {
					t.Fatal("nested return aliases binding")
				}
				first.Permissions[0] = "again"
				first.Membership.Role = "again"
				second, _ := FromContext(bound)
				if second.Permissions[0] != "workspace.members.read" || second.Membership.Role != "member" {
					t.Fatal("FromContext aliases binding")
				}
			}
			cancel()
			if bound.Err() != context.Canceled {
				t.Fatal("cancellation not retained")
			}
		})
	}
}
func TestCurrentContextBindingFailurePropagation(t *testing.T) {
	cfg := Config{newID(t), newID(t), newID(t)}
	person, wid := newID(t), newID(t)
	ctx, p := contextBindingPrincipal(t, cfg, person, 4, time.Now().UTC().Add(-time.Minute))
	r, _ := NewForTransactions(cfg)
	for _, name := range []string{"selector", "denied", "query", "late_cancel"} {
		t.Run(name, func(t *testing.T) {
			q := bindingQueries(cfg, person, wid, false)
			request := ctx
			selector := wid
			want := ErrUnavailable
			switch name {
			case "selector":
				q = nil
				selector = uuid.New()
				want = ErrInvalidSelector
			case "denied":
				q = q[:2]
				q[1].rows = nil
				q[1].columns = 2
				want = ErrDenied
			case "query":
				q = q[:1]
				q[0].err = errors.New("private SQL diagnostic")
			case "late_cancel":
				var cancel context.CancelFunc
				request, cancel = context.WithCancel(ctx)
				defer cancel()
				q[2].after = cancel
			}
			bound, s, err := r.ResolveCurrentContextTx(request, bindingTx(t, q), p, selector)
			requireNoBinding(t, bound, s, err, want)
		})
	}
}
func TestCurrentContextPrincipalInstantEquality(t *testing.T) {
	cfg := Config{newID(t), newID(t), newID(t)}
	person := newID(t)
	at := time.Now().UTC().Add(-time.Minute)
	_, a := contextBindingPrincipal(t, cfg, person, 4, at)
	_, b := contextBindingPrincipal(t, cfg, person, 4, at.In(time.FixedZone("synthetic", 3600)))
	if !sameCurrentPrincipal(a, b) {
		t.Fatal("same instant with another location rejected")
	}
}
