package host

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ajent-social/amos/audit"
	auditstore "github.com/ajent-social/amos/audit/sqlstore"
	"github.com/google/uuid"
)

// This primitive check uses actual same-instance native admission and one
// retained RuntimeDB transaction for selection and audit attribution. It is not
// an operation executor, an authority writer, a permission grant, a business
// mutation route, or a complete signup journey. No assurance transition occurs.
func TestPrivateInvocationAuditContextRuntimeRequiredService(t *testing.T) {
	f := privateReadFixture(t)
	if f == nil {
		return
	}
	var person uuid.UUID
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal("synthetic credential unavailable")
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	sid := testID(t)
	// A new ordinary AAL1 session starts with the native twelve-hour maximum.
	// No existing session, epoch, method or assurance is changed. Base cleanup
	// owns all synthetic rows in this newly allocated realm, including this one.
	err := f.core.database.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(f.ctx, `SELECT personal_owner_id FROM public.workspaces WHERE id=$1 AND installation_id=$2 AND application_id=$3`, f.workspaceID, f.cfg.InstallationID, f.cfg.ApplicationID).Scan(&person); err != nil {
			return err
		}
		_, err := tx.ExecContext(f.ctx, `INSERT INTO public.identity_sessions(id,person_id,installation_id,application_id,environment_id,token_digest,security_epoch,authentication_method,issued_at,authenticated_at,last_seen_at,expires_at,idle_expires_at) SELECT $1,$2,$3,$4,$5,$6,0,'email_password',at,at,at,at+interval '12 hours',at+interval '20 minutes' FROM(SELECT pg_catalog.clock_timestamp() AS at) sample`, sid, person, f.cfg.InstallationID, f.cfg.ApplicationID, f.cfg.EnvironmentID, digest[:])
		return err
	})
	if err != nil {
		t.Fatal("owned ordinary session setup unavailable")
	}
	writer := auditstore.NewInvocationWriter()
	rollback := errors.New("synthetic audit rollback")
	count := func(id uuid.UUID) int {
		t.Helper()
		var n int
		err := f.core.database.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(f.ctx, `SELECT count(*) FROM public.amos_security_audit_events WHERE resource_id=$1`, id).Scan(&n)
		})
		if err != nil {
			t.Fatal("audit observation unavailable")
		}
		return n
	}
	for _, outcome := range []audit.Outcome{audit.OutcomeSucceeded, audit.OutcomeDenied, audit.OutcomeUnavailable} {
		for _, commit := range []bool{true, false} {
			label := "rollback_"
			if commit {
				label = "commit_"
			}
			t.Run(label+string(outcome), func(t *testing.T) {
				id, correlation := testID(t), testID(t)
				called := false
				var result error
				handler := f.core.sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					result = f.core.database.WithTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
						fresh, err := f.core.sessions.RecheckCurrentTx(r.Context(), tx)
						if err != nil {
							return err
						}
						bound, selection, err := f.core.workspaces.ResolveCurrentContextTx(r.Context(), tx, fresh, f.workspaceID)
						if err != nil {
							return err
						}
						if selection.Workspace.ID != f.workspaceID {
							return errors.New("synthetic selection mismatch")
						}
						if err = writer.AppendInvocationTx(bound, tx, id, "synthetic.attribution", outcome, correlation); err != nil {
							return err
						}
						var n int
						err = tx.QueryRowContext(bound, `SELECT count(*) FROM public.amos_security_audit_events WHERE resource_id=$1 AND installation_id=$2 AND application_id=$3 AND environment_id=$4 AND workspace_id=$5 AND actor_kind='person' AND actor_id=$6 AND action='operation.invoked' AND resource_type='invocation' AND operation_id='synthetic.attribution' AND outcome=$7 AND correlation_id=$8 AND attributes='[]'::jsonb`, id, f.cfg.InstallationID, f.cfg.ApplicationID, f.cfg.EnvironmentID, f.workspaceID, person, outcome, correlation).Scan(&n)
						if err != nil {
							return err
						}
						if n != 1 {
							return errors.New("synthetic attribution mismatch")
						}
						if !commit {
							return rollback
						}
						return nil
					})
					w.WriteHeader(http.StatusNoContent)
				}))
				request := httptest.NewRequest(http.MethodGet, f.cfg.Origin+"/synthetic-audit-check", nil).WithContext(f.ctx)
				request.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: token})
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if !called || response.Code != http.StatusNoContent {
					t.Fatal("native admission unavailable")
				}
				if commit {
					if result != nil || count(id) != 1 {
						t.Fatal("native attributed audit did not commit")
					}
				} else if !errors.Is(result, rollback) || count(id) != 0 {
					t.Fatal("native attributed audit did not roll back")
				}
			})
		}
	}
	t.Run("missing_native_admission_never_reaches_audit", func(t *testing.T) {
		called := false
		h := f.core.sessions.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
		r := httptest.NewRequest(http.MethodGet, f.cfg.Origin+"/synthetic-audit-check", nil).WithContext(f.ctx)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if called || w.Code != http.StatusUnauthorized {
			t.Fatal("missing native admission accepted")
		}
	})
	t.Run("unbound_native_context_cannot_append", func(t *testing.T) {
		id := testID(t)
		called := false
		var result error
		h := f.core.sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			result = f.core.database.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
				return writer.AppendInvocationTx(r.Context(), tx, id, "synthetic.attribution", audit.OutcomeDenied, testID(t))
			})
			w.WriteHeader(http.StatusNoContent)
		}))
		r := httptest.NewRequest(http.MethodGet, f.cfg.Origin+"/synthetic-audit-check", nil).WithContext(f.ctx)
		r.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if !called || !errors.Is(result, audit.ErrForbidden) || count(id) != 0 {
			t.Fatal("unbound native context appended audit")
		}
	})
}
