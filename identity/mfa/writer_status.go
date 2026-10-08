package mfa

import (
	"context"
	"database/sql"
	"errors"

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
		last, err := s.writerSessions.RecheckCurrentTx(ctx, tx)
		if err != nil {
			if errors.Is(err, session.ErrUnauthenticated) {
				return ErrDenied
			}
			return ErrUnavailable
		}
		if last != final {
			return ErrDenied
		}
		at, err := mfaClock(ctx, tx)
		if err != nil {
			return ErrUnavailable
		}
		if activeErr == nil {
			if err = factorShape(active); err != nil {
				return err
			}
			result.Enabled = true
		}
		if pendingErr == nil {
			if err = factorShape(pending); err != nil {
				return err
			}
			if pending.ExpiresAt.After(at) {
				id, expires := pending.ID, *pending.ExpiresAt
				result.Pending = true
				result.PendingFactorID = &id
				result.ExpiresAt = &expires
			}
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
