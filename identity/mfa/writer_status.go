package mfa

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
)

func (s *Service) statusWriter(ctx context.Context, principal identity.Principal) (FactorStatus, error) {
	if ctx == nil {
		return FactorStatus{}, ErrUnavailable
	}
	admitted, ok := identity.PrincipalFromContext(ctx)
	if !ok || admitted != principal {
		return FactorStatus{}, ErrDenied
	}
	var result FactorStatus
	err := s.root.Read(ctx, func(ctx context.Context, tx *sql.Tx) (readErr error) {
		defer func() {
			if errors.Is(readErr, ErrDenied) {
				readErr = aw.ErrDenied
			}
		}()
		current, err := s.writerSessions.RecheckCurrentTx(ctx, tx)
		if err != nil {
			if errors.Is(err, session.ErrUnauthenticated) {
				return ErrDenied
			}
			return ErrUnavailable
		}
		scope := scopeFor(current)
		if !validScope(scope) {
			return ErrDenied
		}
		if err = s.policy(ctx, tx, current, "totp.read"); err != nil {
			return err
		}
		st, err := NewReadStore(tx)
		if err != nil {
			return ErrUnavailable
		}
		active, activeErr := st.FindCurrent(ctx, scope, FactorActive)
		if activeErr != nil && !errors.Is(activeErr, ErrFactorAbsent) {
			return ErrUnavailable
		}
		pending, pendingErr := st.FindCurrent(ctx, scope, FactorPending)
		if pendingErr != nil && !errors.Is(pendingErr, ErrFactorAbsent) {
			return ErrUnavailable
		}
		// Reader holds P SHARE, so a conforming factor writer cannot change these
		// rows. Drain declared deferred work and recheck the same private session.
		if _, err = tx.ExecContext(ctx, `SET CONSTRAINTS public.workspace_active_org_owner_workspace, public.workspace_active_org_owner_membership, public.workspace_person_state_owner_guard IMMEDIATE`); err != nil {
			return ErrUnavailable
		}
		final, err := s.writerSessions.RecheckCurrentTx(ctx, tx)
		if err != nil {
			if errors.Is(err, session.ErrUnauthenticated) {
				return ErrDenied
			}
			return ErrUnavailable
		}
		if err = s.policy(ctx, tx, final, "totp.read"); err != nil {
			return err
		}
		last, at, err := s.writerSessions.RecheckCurrentSampleTx(ctx, tx)
		if err != nil {
			if errors.Is(err, session.ErrUnauthenticated) {
				return ErrDenied
			}
			return ErrUnavailable
		}
		if last != final {
			return ErrDenied
		}
		// No SQL, policy callback or clock follows the exact actor sample.
		var liveActive, livePending *Factor
		if activeErr == nil {
			liveActive = &active
		}
		if pendingErr == nil {
			livePending = &pending
		}
		result, err = statusAt(liveActive, livePending, at)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, aw.ErrDenied) {
			return FactorStatus{}, ErrDenied
		}
		return FactorStatus{}, ErrUnavailable
	}
	return result, nil
}

// statusAt performs only bounded value checks against the reader's final sample.
// Expiry changes the reported pending flag, never the stored factor state.
func statusAt(active, pending *Factor, at time.Time) (FactorStatus, error) {
	if at.IsZero() {
		return FactorStatus{}, ErrUnavailable
	}
	result := FactorStatus{}
	if active != nil {
		if factorShape(*active) != nil || active.State != FactorActive || active.CreatedAt.After(at) || active.ActivatedAt.After(at) {
			return FactorStatus{}, ErrUnavailable
		}
		result.Enabled = true
	}
	if pending != nil {
		if factorShape(*pending) != nil || pending.State != FactorPending || pending.CreatedAt.After(at) {
			return FactorStatus{}, ErrUnavailable
		}
		if pending.ExpiresAt.After(at) {
			id, expiry := pending.ID, *pending.ExpiresAt
			result.Pending = true
			result.PendingFactorID = &id
			result.ExpiresAt = &expiry
		}
	}
	return result, nil
}
