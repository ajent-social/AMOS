package context

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity/session"
	"github.com/google/uuid"
)

// Required-service SOURCE only: ordinary synthetic AAL1 admission through the
// same native service and retained runtime transaction. No assurance transition,
// login-producer, invocation-audit or complete executor qualification is claimed.
func TestWorkspaceCurrentContextRuntimeRequiredService(t *testing.T) {
	f := newCurrentRuntimeFixture(t)
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal("synthetic credential unavailable")
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	sid := newID(t)
	// Use the ordinary native AAL1 maximum lifetime from initial creation so the
	// admitted and freshly read principal expose the same expiry. This never
	// changes a stored assurance level or an existing credential's authority.
	currentRuntimeTx(t, f.db, nil, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO public.identity_sessions(id,person_id,installation_id,application_id,environment_id,token_digest,security_epoch,authentication_method,issued_at,authenticated_at,last_seen_at,expires_at,idle_expires_at)
 SELECT $1,$2,$3,$4,$5,$6,0,'email_password',at,at,at,at+interval '12 hours',at+interval '20 minutes' FROM(SELECT pg_catalog.clock_timestamp() AS at) sample`, sid, f.person, f.cfg.InstallationID, f.cfg.ApplicationID, f.cfg.EnvironmentID, digest[:])
		return err
	})
	// The existing fixture cleanup removes only this newly allocated realm,
	// including this added session, and never changes shared role/schema rows.
	svc, err := session.NewWithTxRunner(f.db, session.Config{InstallationID: f.cfg.InstallationID, ApplicationID: f.cfg.ApplicationID, EnvironmentID: f.cfg.EnvironmentID, AllowedOrigins: []string{"https://workspace.example.test"}, CookieSecure: true})
	if err != nil {
		t.Fatal("native service construction failed")
	}
	for _, selector := range []uuid.UUID{uuid.Nil, f.personal, f.organization, f.foreignPersonal} {
		t.Run("native_binding", func(t *testing.T) {
			called := false
			var resolveErr error
			handler := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				called = true
				resolveErr = f.db.WithTx(req.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
					fresh, err := svc.RecheckCurrentTx(req.Context(), tx)
					if err != nil {
						return err
					}
					bound, selected, err := f.resolver.ResolveCurrentContextTx(req.Context(), tx, fresh, selector)
					if selector == f.foreignPersonal {
						if err != ErrDenied || bound != nil || !reflect.DeepEqual(selected, Selection{}) {
							return ErrUnavailable
						}
						return nil
					}
					if err != nil {
						return err
					}
					want := selector
					if want == uuid.Nil {
						want = f.personal
					}
					held, ok := FromContext(bound)
					if !ok || selected.Workspace.ID != want || held.Workspace.ID != want {
						return ErrUnavailable
					}
					// The same service still recognizes the original admission/deadline after
					// binding; no public principal/selection setter or second pool is used.
					after, err := svc.RecheckCurrentTx(bound, tx)
					if err != nil || !sameCurrentPrincipal(after, fresh) {
						return ErrUnavailable
					}
					selected.Workspace.ID = uuid.Nil
					if selector == f.organization {
						selected.Permissions[0] = "mutated"
						selected.Membership.Role = "mutated"
						again, _ := FromContext(bound)
						if again.Permissions[0] == "mutated" || again.Membership.Role == "mutated" {
							return ErrUnavailable
						}
					}
					return nil
				})
				w.WriteHeader(http.StatusNoContent)
			}))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, "https://workspace.example.test/", nil).WithContext(ctx)
			req.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: token})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if !called || rec.Code != http.StatusNoContent || resolveErr != nil {
				t.Fatal("same-instance native current context binding failed")
			}
		})
	}
}
