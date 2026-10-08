package session

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/ajent-social/amos/identity"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

func (s *Service) browserCheck(w http.ResponseWriter, r *http.Request) error {
	if !unsafeMethod(r.Method) {
		return nil
	}
	if !s.AllowsOrigin(r) {
		writeError(w, r, http.StatusForbidden, "request.origin_denied")
		return ErrUnauthenticated
	}
	cookie, err := singleCookie(r, s.cookieName)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "auth.unauthenticated")
		return ErrUnauthenticated
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(raw) != 32 || !hmac.Equal([]byte(csrfFromRequest(w, r)), []byte(csrf(raw))) {
		writeError(w, r, http.StatusForbidden, "request.csrf_denied")
		return ErrUnauthenticated
	}
	return nil
}
func (s *Service) writerMiddleware(next http.Handler, w http.ResponseWriter, r *http.Request) {
	// This lifetime covers both authentication and every downstream recheck.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	cookie, cookieErr := singleCookie(r, s.cookieName)
	if cookieErr != nil {
		writeError(w, r, http.StatusUnauthorized, "auth.unauthenticated")
		return
	}
	admittedDigest, ok := tokenDigest(cookie.Value)
	if !ok {
		sessionHTTPError(w, r, ErrUnauthenticated)
		return
	}
	// Parse a bounded body once on the original request so the private
	// renewal request does not consume downstream form input.
	if unsafeMethod(r.Method) {
		csrfFromRequest(w, r)
	}
	principal, id, err := s.runBrowser(r, wp.Renew)
	if err != nil {
		sessionHTTPError(w, r, err)
		return
	}
	// Admission is minted only after commit and request boundary checks.
	if err = s.browserCheck(w, r); err != nil {
		return
	}
	credential, err := credentialForPrincipal(principal)
	if err != nil {
		sessionHTTPError(w, r, err)
		return
	}
	ctx = identity.ContextWithVerifiedCredential(r.Context(), credential)
	ctx = s.admitCurrent(ctx, id, principal, admittedDigest)
	next.ServeHTTP(w, r.WithContext(ctx))
}
func sessionHTTPError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrUnauthenticated) {
		writeError(w, r, http.StatusUnauthorized, "auth.unauthenticated")
	} else {
		writeError(w, r, http.StatusServiceUnavailable, "dependency.unavailable")
	}
}
func (s *Service) writerSignOut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method.not_allowed")
		return
	}
	if !s.AllowsOrigin(r) {
		writeError(w, r, http.StatusForbidden, "request.origin_denied")
		return
	}
	if _, err := singleCookie(r, s.cookieName); errors.Is(err, http.ErrNoCookie) {
		http.SetCookie(w, s.expiredCookie())
		w.WriteHeader(http.StatusNoContent)
		return
	} else if err != nil {
		sessionHTTPError(w, r, ErrUnauthenticated)
		return
	}
	if err := s.browserCheck(w, r); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if _, _, err := s.runBrowser(r, wp.Signout); err != nil {
		sessionHTTPError(w, r, err)
		return
	}
	http.SetCookie(w, s.expiredCookie())
	w.WriteHeader(http.StatusNoContent)
}
func actorFromRow(id uuid.UUID, v currentRow, now time.Time) wp.ActorCheck {
	level, until := "aal1", v.absolute
	if v.level != "aal1" && now.Before(v.elevated.Time) {
		level, until = v.level, v.elevated.Time
	}
	return wp.ActorCheck{Subject: wp.Subject{Person: v.person, Realm: aw.Realm{Installation: v.installation, Application: v.application, Environment: v.environment}, Epoch: v.epoch}, SessionID: id, Digest: [32]byte(v.digest), Method: v.method, AuthenticatedAt: v.authenticated, IdleUntil: v.idle, AbsoluteUntil: v.absolute, Assurance: level, AssuranceUntil: until}
}
func sameSession(a, b currentRow) bool {
	return a.person == b.person && a.installation == b.installation && a.application == b.application && a.environment == b.environment && a.epoch == b.epoch && a.method == b.method && a.authenticated.Equal(b.authenticated) && a.issued.Equal(b.issued) && a.absolute.Equal(b.absolute) && a.idle.Equal(b.idle) && a.lastSeen.Equal(b.lastSeen) && a.revoked == b.revoked && bytes.Equal(a.digest, b.digest) && a.level == b.level && a.elevated == b.elevated
}
func (s *Service) runBrowser(r *http.Request, action wp.Action) (identity.Principal, uuid.UUID, error) {
	r = r.Clone(r.Context())
	request, err := s.AdmitWriterRequest(r)
	if err != nil {
		return identity.Principal{}, uuid.Nil, err
	}
	var principal identity.Principal
	var sessionID uuid.UUID
	var permit wp.Permit
	var binding aw.Binding
	reason := ErrUnavailable
	completion, err := s.root.Run(r.Context(), func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		deny := func(e error) aw.Outcome {
			reason = e
			out := aw.UnavailableRollback
			if errors.Is(e, ErrUnauthenticated) {
				out = aw.DeniedRollback
			}
			if err := a.Finish(out); err != nil {
				reason = ErrUnavailable
				return aw.UnavailableRollback
			}
			return out
		}
		rows, e := s.DiscoverPrior(ctx, a, request, action)
		if e != nil {
			return deny(e)
		}
		d := request.data
		if d.priorID == uuid.Nil {
			return deny(ErrUnauthenticated)
		}
		plan, e := aw.NewPlan(aw.Realm{Installation: s.cfg.InstallationID, Application: s.cfg.ApplicationID, Environment: s.cfg.EnvironmentID}, rows, nil)
		if e != nil {
			return deny(ErrUnavailable)
		}
		if e = a.SealPlan(plan); e != nil {
			return deny(ErrUnavailable)
		}
		for phase := aw.P; phase <= aw.W; phase++ {
			if e = a.Acquire(ctx, phase); e != nil {
				return deny(ErrUnavailable)
			}
		}
		tx, e := a.ParticipantTx(ctx, aw.S, rows)
		if e != nil {
			return deny(ErrUnavailable)
		}
		before, now, e := s.readCurrent(ctx, tx, d.priorPerson, d.priorID, false)
		if e != nil {
			return deny(e)
		}
		if !bytes.Equal(before.digest, d.digest) {
			return deny(ErrUnauthenticated)
		}
		actor := actorFromRow(d.priorID, before, now)
		initialPrincipal, e := freshPrincipal(before, actor.Assurance, actor.AssuranceUntil)
		if e != nil {
			return deny(e)
		}
		// Renewal and signout require a live base session, not an elevated proof.
		// Preserve the admitted elevation separately for the final no-upgrade table.
		actor.Assurance = "aal1"
		actor.AssuranceUntil = before.absolute
		evidence, e := wp.Actor(a, action, actor)
		if e != nil {
			return deny(ErrUnavailable)
		}
		st, e := store.NewWriter(a)
		if e != nil {
			return deny(ErrUnavailable)
		}
		scope := store.SessionScope{InstallationID: s.cfg.InstallationID, ApplicationID: s.cfg.ApplicationID, EnvironmentID: s.cfg.EnvironmentID}
		if action == wp.Renew {
			_, e = st.FindActiveSession(ctx, d.digest, scope)
		} else {
			e = st.RevokeSessionScoped(ctx, d.digest, scope)
		}
		if e != nil {
			if errors.Is(e, store.ErrSessionUnavailable) {
				reason = ErrUnauthenticated
				return aw.DeniedRollback
			}
			return deny(ErrUnavailable)
		}
		after, _, e := s.readCurrent(ctx, tx, d.priorPerson, d.priorID, false)
		if action == wp.Signout {
			if !errors.Is(e, ErrUnauthenticated) || !after.revoked.Valid {
				return deny(ErrUnavailable)
			}
		} else if e != nil {
			return deny(e)
		}
		// Compare every immutable field to the original actor; only the intentional
		// renewal/revocation fields may differ.
		expected := before
		if action == wp.Renew {
			expected.idle = after.idle
			expected.lastSeen = after.lastSeen
		} else {
			expected.revoked = after.revoked
		}
		if !sameSession(expected, after) {
			return deny(ErrUnavailable)
		}
		binding, e = a.Binding()
		if e != nil {
			return deny(ErrUnavailable)
		}
		final, e := a.DrainAndSample(ctx)
		if e != nil {
			return deny(ErrUnavailable)
		}
		finalRow, checkedAt, e := s.readCurrent(ctx, tx, d.priorPerson, d.priorID, false)
		if action == wp.Signout {
			if !errors.Is(e, ErrUnauthenticated) {
				return deny(ErrUnavailable)
			}
		} else if e != nil {
			return deny(e)
		}
		if !sameSession(after, finalRow) || !final.Before(before.idle) || !final.Before(before.absolute) {
			return deny(ErrUnauthenticated)
		}
		permit, e = wp.Finalize(a, evidence, final)
		if e != nil {
			if errors.Is(e, wp.ErrDenied) {
				return deny(ErrUnauthenticated)
			}
			return deny(ErrUnavailable)
		}
		if action == wp.Renew {
			level, until, assuranceErr := currentAssurance(initialPrincipal, finalRow, checkedAt)
			if assuranceErr != nil {
				return deny(assuranceErr)
			}
			principal, e = freshPrincipal(finalRow, level, until)
			if e != nil {
				return deny(e)
			}
		}
		sessionID = d.priorID
		if e = a.Finish(aw.Success); e != nil {
			return aw.UnavailableRollback
		}
		return aw.Success
	})
	if err != nil {
		if errors.Is(err, aw.ErrDenied) {
			return identity.Principal{}, uuid.Nil, reason
		}
		return identity.Principal{}, uuid.Nil, ErrUnavailable
	}
	release, err := completion.TakeRelease(binding)
	if err != nil || !permit.MatchesRelease(release) {
		return identity.Principal{}, uuid.Nil, ErrUnavailable
	}
	return principal, sessionID, nil
}
