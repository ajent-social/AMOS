package magiclink

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"time"

	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

func (s *Service) confirmWriter(ctx context.Context, id uuid.UUID, digest []byte, browser string, consent bool, r *http.Request) (session.Issued, error) {
	if ctx == nil || r == nil || ctx != r.Context() || !validID(id) || len(digest) != 32 {
		return session.Issued{}, ErrUnavailable
	}
	request, err := s.writerSessions.AdmitWriterRequest(r)
	if err != nil {
		return session.Issued{}, ErrUnavailable
	}
	// Admission installs private provenance on the original request, not a copy.
	ctx = r.Context()
	var supplied [32]byte
	if browser != "" {
		_, supplied, err = parseToken(browser)
		if err != nil {
			supplied = [32]byte{}
		}
	}
	var staged session.Staged
	var permit wp.Permit
	reason := ErrChallengeUnavailable
	completion, err := s.root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		tx, e := a.DiscoveryTx(ctx)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		initial, e := s.challenge(ctx, tx, id, digest)
		if errors.Is(e, sql.ErrNoRows) {
			return magicFinish(a, aw.DeniedRollback)
		}
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if !initial.shape() {
			return magicFinish(a, aw.UnavailableRollback)
		}
		c := initial.contact
		rows := append(c.rows(), aw.Row{Table: aw.Challenges, ID: id, Access: aw.ExistingUpdate})
		prior, e := s.writerSessions.DiscoverPrior(ctx, a, request, wp.MagicConfirm)
		if e != nil {
			return aw.UnavailableRollback
		}
		rows = append(rows, prior...)
		policy, e := s.policyRows(ctx, tx, c.person)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		rows = append(rows, policy...)
		rows = magicRows(rows)
		plan, e := aw.NewPlan(s.realm(), rows, nil)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if e = a.SealPlan(plan); e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if e = magicAcquire(ctx, a); e != nil {
			return magicRootFailure(a, e)
		}
		tx, e = a.ParticipantTx(ctx, aw.W, rows)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		held, e := s.challenge(ctx, tx, id, digest)
		if errors.Is(e, sql.ErrNoRows) {
			return magicFinish(a, aw.DeniedRollback)
		}
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if !held.shape() {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if !held.same(initial) || !c.live() || held.consumed.Valid {
			return magicFinish(a, aw.DeniedRollback)
		}
		b, e := a.StartedAt()
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if held.created.After(b) {
			return magicFinish(a, aw.UnavailableRollback)
		}
		var verified time.Time
		if e = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&verified); e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if verified.Before(b) || c.verified.Time.After(verified) {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if !verified.Before(held.expires) {
			return magicFinish(a, aw.DeniedRollback)
		}
		sameBrowser := supplied != [32]byte{} && subtle.ConstantTimeCompare(held.browser, supplied[:]) == 1
		if !sameBrowser && !consent {
			reason = ErrBrowserConfirmationRequired
			return magicFinish(a, aw.DeniedRollback)
		}
		if e = s.cfg.Policy.Ready(ctx, tx); e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if e = s.cfg.Policy.AuthorizeMagicLink(ctx, tx, c.person); e != nil {
			if errors.Is(e, ErrPolicyDenied) || errors.Is(e, ErrStepUpRequired) {
				reason = e
				return magicFinish(a, aw.DeniedRollback)
			}
			return magicFinish(a, aw.UnavailableRollback)
		}
		evidence, e := wp.Challenge(a, wp.MagicConfirm, wp.ChallengeCheck{Subject: wp.Subject{Person: c.person, Realm: s.realm(), Epoch: c.epoch}, Contact: wp.Contact{ID: c.email, ComparisonKey: c.key, VerifiedAt: c.verified.Time}, ID: id, TokenDigest: [32]byte(held.digest), BrowserDigest: [32]byte(held.browser), CreatedAt: held.created, ExpiresAt: held.expires, VerifiedAt: verified, DifferentDeviceConfirmed: !sameBrowser && consent})
		if e != nil {
			return magicRootFailure(a, e)
		}
		st, e := store.NewWriter(a)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		consumed, e := st.ConsumeChallenge(ctx, id, challengePurpose, digest)
		if e != nil {
			return magicStoreFailure(e)
		}
		if consumed.PersonID != c.person || consumed.EmailID != c.email {
			return magicFinish(a, aw.UnavailableRollback)
		}
		var consumedAt time.Time
		if e = tx.QueryRowContext(ctx, `SELECT consumed_at FROM public.identity_challenges WHERE id=$1`, id).Scan(&consumedAt); e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if consumedAt.Before(verified) || !consumedAt.Before(held.expires) {
			return magicFinish(a, aw.UnavailableRollback)
		}

		issuance, e := wp.ForIssue(a, evidence)
		if e != nil {
			return magicRootFailure(a, e)
		}
		staged, e = s.writerSessions.StageWriter(ctx, a, issuance, request)
		if e != nil {
			if errors.Is(e, session.ErrUnauthenticated) {
				return aw.DeniedRollback
			}
			return aw.UnavailableRollback
		}
		f, e := a.DrainAndSample(ctx)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		current, e := s.contact(ctx, tx, c.key)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if !c.same(current) {
			return magicFinish(a, aw.DeniedRollback)
		}
		if e = checkMagicRow(ctx, tx, id, c, [32]byte(held.digest), [32]byte(held.browser), held.created, held.expires, consumedAt); e != nil {
			return magicRootFailure(a, e)
		}
		if e = s.cfg.Policy.AuthorizeMagicLink(ctx, tx, c.person); e != nil {
			if errors.Is(e, ErrPolicyDenied) || errors.Is(e, ErrStepUpRequired) {
				reason = e
				return magicFinish(a, aw.DeniedRollback)
			}
			return magicFinish(a, aw.UnavailableRollback)
		}
		if e = s.writerSessions.CheckStagedWriter(ctx, a, staged, f); e != nil {
			if errors.Is(e, session.ErrUnauthenticated) {
				return aw.DeniedRollback
			}
			return aw.UnavailableRollback
		}
		if f.Before(consumedAt) {
			return magicFinish(a, aw.UnavailableRollback)
		}
		permit, e = wp.Finalize(a, evidence, f)
		if e != nil {
			return magicRootFailure(a, e)
		}
		return magicFinish(a, aw.Success)
	})
	if err != nil {
		if errors.Is(err, aw.ErrDenied) {
			return session.Issued{}, reason
		}
		return session.Issued{}, ErrUnavailable
	}
	issued, err := s.writerSessions.PublishWriter(completion, permit, staged)
	if err != nil {
		return session.Issued{}, ErrUnavailable
	}
	return issued, nil
}
