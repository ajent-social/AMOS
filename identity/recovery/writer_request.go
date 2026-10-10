package recovery

import (
	"context"
	"errors"
	deliveryemail "github.com/ajent-social/amos/delivery/email"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
	"time"
)

func (s *Service) resetEvidence(a *aw.Attempt, action wp.Action, v accountSnapshot, r resetSnapshot, verified time.Time) (wp.Evidence, error) {
	var digest [32]byte
	copy(digest[:], r.digest)
	return wp.Challenge(a, action, wp.ChallengeCheck{Subject: wp.Subject{Person: v.person, Realm: s.realm(), Epoch: v.epoch}, Contact: wp.Contact{ID: v.email, ComparisonKey: v.key, VerifiedAt: v.verified}, ID: r.id, TokenDigest: digest, CreatedAt: r.created, ExpiresAt: r.expires, VerifiedAt: verified})
}
func (s *Service) requestWriter(ctx context.Context, address string) error {
	if ctx == nil {
		return ErrUnavailable
	}
	id, e := store.NewID()
	if e != nil {
		return ErrUnavailable
	}
	request, e := store.NewID()
	if e != nil {
		return ErrUnavailable
	}
	raw, digest, e := newToken()
	if e != nil {
		return ErrUnavailable
	}
	var permit wp.Permit
	var binding aw.Binding
	noop := false
	completion, e := s.root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		fail := func(e error) aw.Outcome { return recoveryOutcome(recoveryFailure(a, e)) }
		tx, e := a.DiscoveryTx(ctx)
		if e != nil {
			return fail(ErrUnavailable)
		}
		v, e := s.account(ctx, tx, uuid.Nil, uuid.Nil, address)
		if errors.Is(e, ErrChallengeUnavailable) {
			noop = true
			return fail(e)
		}
		if e != nil {
			return fail(e)
		}
		rows := accountRows(v)
		prior := []uuid.UUID{}
		query, e := tx.QueryContext(ctx, `SELECT id FROM identity_challenges WHERE person_id=$1 AND email_id=$2 AND purpose=$3 AND consumed_at IS NULL ORDER BY id LIMIT 1025`, v.person, v.email, passwordResetPurpose)
		if e != nil {
			return fail(ErrUnavailable)
		}
		for query.Next() {
			var old uuid.UUID
			if e = query.Scan(&old); e != nil {
				_ = query.Close()
				return fail(ErrUnavailable)
			}
			prior = append(prior, old)
			rows = append(rows, aw.Row{Table: aw.Challenges, ID: old, Access: aw.ExistingUpdate})
		}
		e = query.Err()
		closed := query.Close()
		if e != nil || closed != nil || len(prior) > 1024 {
			return fail(ErrUnavailable)
		}
		rows = append(rows, aw.Row{Table: aw.Challenges, ID: id, Access: aw.ReservedInsert})
		policy, e := s.policyRows(ctx, tx, v.person)
		if e != nil {
			return fail(e)
		}
		rows = append(rows, policy...)
		delivery := aw.Delivery{MaterialID: id, JobKey: "password-reset:" + request.String()}
		if e = sealAcquire(ctx, a, s.realm(), rows, []aw.Delivery{delivery}); e != nil {
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
		if !sameAccount(v, held) {
			return fail(ErrChallengeUnavailable)
		}
		b, e := a.StartedAt()
		if e != nil {
			return fail(ErrUnavailable)
		}
		var recent int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_challenges WHERE person_id=$1 AND email_id=$2 AND purpose=$3 AND created_at >= $4::timestamptz - interval '1 hour'`, v.person, v.email, passwordResetPurpose, b).Scan(&recent); e != nil {
			return fail(ErrUnavailable)
		}
		if recent >= MaxIssueRequestsPerHour {
			noop = true
			return fail(ErrChallengeUnavailable)
		}
		for _, old := range prior {
			row := aw.Row{Table: aw.Challenges, ID: old, Access: aw.ExistingUpdate}
			if e = a.RecordMutation(aw.ChallengeWrite, []aw.Row{row}); e != nil {
				return fail(ErrUnavailable)
			}
			result, e := tx.ExecContext(ctx, `UPDATE identity_challenges SET consumed_at=clock_timestamp() WHERE id=$1 AND person_id=$2 AND email_id=$3 AND purpose=$4 AND consumed_at IS NULL`, old, v.person, v.email, passwordResetPurpose)
			if e != nil {
				return fail(ErrUnavailable)
			}
			n, e := result.RowsAffected()
			if e != nil || n != 1 {
				return fail(ErrUnavailable)
			}
		}
		st, e := store.NewWriter(a)
		if e != nil {
			return fail(ErrUnavailable)
		}
		expiry := b.Add(s.cfg.ChallengeLifetime)
		if e = st.CreateChallenge(ctx, store.Challenge{ID: id, PersonID: v.person, EmailID: v.email, Purpose: passwordResetPurpose, Digest: digest[:], ExpiresAt: expiry}); e != nil {
			return recoveryOutcome(recoveryStoreError(e))
		}
		r, e := s.reset(ctx, tx, id, digest[:])
		if e != nil {
			return fail(e)
		}
		var verification time.Time
		if e = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&verification); e != nil {
			return fail(ErrUnavailable)
		}
		proof, e := s.resetEvidence(a, wp.ResetRequest, v, r, verification)
		if e != nil {
			return fail(ErrUnavailable)
		}
		ref := deliveryemail.SecretReference("material:" + id.String())
		material := deliveryemail.PrivateMaterial{Recipient: v.address, ActionURL: s.actionURL(id, raw)}
		if e = s.writerMaterials.PutPasswordResetWriter(ctx, a, delivery, ref, material, expiry); e != nil {
			return aw.UnavailableRollback
		}
		if _, e = deliveryemail.EnqueueWriter(ctx, a, delivery, s.writerOutbox, s.cfg.Renderer, s.cfg.InstallationID, s.cfg.ApplicationID, delivery.JobKey, deliveryemail.Request{Template: deliveryemail.TemplatePasswordReset, MaterialRef: ref, ExpiresInSeconds: int64(s.cfg.ChallengeLifetime / time.Second)}, expiry); e != nil {
			return aw.UnavailableRollback
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
		current, e := s.reset(ctx, tx, id, digest[:])
		if e != nil {
			return fail(e)
		}
		if !sameAccount(v, final) || !sameReset(r, current) || r.consumed.Valid || !f.Before(expiry) {
			return fail(ErrChallengeUnavailable)
		}
		for _, old := range prior {
			var at time.Time
			if e = tx.QueryRowContext(ctx, `SELECT consumed_at FROM identity_challenges WHERE id=$1 AND person_id=$2 AND email_id=$3 AND purpose=$4`, old, v.person, v.email, passwordResetPurpose).Scan(&at); e != nil || at.IsZero() || f.Before(at) {
				return fail(ErrUnavailable)
			}
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
		if errors.Is(e, aw.ErrDenied) && noop {
			return nil
		}
		return ErrUnavailable
	}
	return releaseRecovery(completion, permit, binding)
}
