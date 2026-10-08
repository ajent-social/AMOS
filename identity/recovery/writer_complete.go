package recovery

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ajent-social/amos/identity/internal/passwordwork"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
	"time"
)

func (s *Service) completeWriter(ctx context.Context, id uuid.UUID, digest []byte, secret string) error {
	if ctx == nil || !validID(id) || len(digest) != 32 {
		return ErrChallengeUnavailable
	}
	// Preflight releases its read transaction before bounded pure password work.
	var original accountSnapshot
	e := s.root.Read(ctx, func(ctx context.Context, tx *sql.Tx) error {
		r, e := s.reset(ctx, tx, id, digest)
		if e != nil {
			return e
		}
		original, e = s.account(ctx, tx, r.person, r.email, "")
		if e != nil {
			return e
		}
		var now time.Time
		if e = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return ErrUnavailable
		}
		if original.credential == uuid.Nil || original.hash == "" || r.consumed.Valid || now.Before(r.created) || !now.Before(r.expires) {
			return ErrChallengeUnavailable
		}
		return nil
	})
	if e != nil {
		if errors.Is(e, ErrChallengeUnavailable) {
			return e
		}
		return ErrUnavailable
	}
	nextHash, e := passwordwork.Hash(ctx, s.cfg.Passwords, "reset:"+id.String(), secret)
	if e != nil {
		if errors.Is(e, password.ErrInvalid) {
			return e
		}
		return ErrUnavailable
	}
	var permit wp.Permit
	var binding aw.Binding
	var reason error
	completion, e := s.root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		fail := func(e error) aw.Outcome { reason = recoveryFailure(a, e); return recoveryOutcome(reason) }
		tx, e := a.DiscoveryTx(ctx)
		if e != nil {
			return fail(ErrUnavailable)
		}
		r, e := s.reset(ctx, tx, id, digest)
		if e != nil {
			return fail(e)
		}
		v, e := s.account(ctx, tx, r.person, r.email, "")
		if e != nil {
			return fail(e)
		}
		if !sameAccount(v, original) {
			return fail(ErrChallengeUnavailable)
		}
		sessions, e := sessionRows(ctx, tx, v.person)
		if e != nil {
			return fail(e)
		}
		rows := append(accountRows(v), aw.Row{Table: aw.Challenges, ID: id, Access: aw.ExistingUpdate})
		rows = append(rows, sessions...)
		policy, e := s.policyRows(ctx, tx, v.person)
		if e != nil {
			return fail(e)
		}
		rows = append(rows, policy...)
		if e = sealAcquire(ctx, a, s.realm(), rows, nil); e != nil {
			return fail(e)
		}
		tx, e = a.ParticipantTx(ctx, aw.W, rows)
		if e != nil {
			return fail(ErrUnavailable)
		}
		held, e := s.account(ctx, tx, v.person, v.email, "")
		if e != nil {
			return fail(e)
		}
		current, e := s.reset(ctx, tx, id, digest)
		if e != nil {
			return fail(e)
		}
		if !sameAccount(v, held) || !sameReset(r, current) || r.consumed.Valid {
			return fail(ErrChallengeUnavailable)
		}
		var verified time.Time
		if e = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&verified); e != nil {
			return fail(ErrUnavailable)
		}
		if verified.Before(r.created) || !verified.Before(r.expires) {
			return fail(ErrChallengeUnavailable)
		}
		proof, e := s.resetEvidence(a, wp.ResetComplete, v, r, verified)
		if e != nil {
			return fail(ErrUnavailable)
		}
		if e = s.cfg.Policy.AuthorizePasswordReset(ctx, tx, v.person); e != nil {
			return fail(e)
		}
		st, e := store.NewWriter(a)
		if e != nil {
			return fail(ErrUnavailable)
		}
		consumed, e := st.ConsumeChallenge(ctx, id, passwordResetPurpose, digest)
		if e != nil {
			reason = recoveryStoreError(e)
			return recoveryOutcome(reason)
		}
		if consumed.PersonID != v.person || consumed.EmailID != v.email {
			return fail(ErrChallengeUnavailable)
		}
		after, e := s.applyPassword(ctx, a, tx, v, nextHash, sessions)
		if e != nil {
			reason = e
			return recoveryOutcome(e)
		}
		binding, e = a.Binding()
		if e != nil {
			return fail(ErrUnavailable)
		}
		f, e := a.DrainAndSample(ctx)
		if e != nil {
			return fail(ErrUnavailable)
		}
		final, e := s.account(ctx, tx, v.person, v.email, "")
		if e != nil {
			return fail(e)
		}
		current, e = s.reset(ctx, tx, id, digest)
		if e != nil {
			return fail(e)
		}
		if !sameAccount(after, final) || !current.consumed.Valid || current.consumed.Time.Before(verified) || f.Before(current.consumed.Time) || !f.Before(r.expires) {
			return fail(ErrChallengeUnavailable)
		}
		expected := r
		expected.consumed = current.consumed
		if !sameReset(expected, current) {
			return fail(ErrUnavailable)
		}
		if e = revokedAll(ctx, tx, v.person, sessions, f); e != nil {
			return fail(e)
		}
		if e = s.cfg.Policy.AuthorizePasswordReset(ctx, tx, v.person); e != nil {
			return fail(e)
		}
		permit, e = wp.Finalize(a, proof, f)
		if e != nil {
			return fail(recoveryProofError(e))
		}
		if e = a.Finish(aw.Success); e != nil {
			return aw.UnavailableRollback
		}
		return aw.Success
	})
	if e != nil {
		if errors.Is(e, aw.ErrDenied) && reason != nil {
			return reason
		}
		return ErrUnavailable
	}
	return releaseRecovery(completion, permit, binding)
}
