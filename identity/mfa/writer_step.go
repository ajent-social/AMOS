package mfa

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/ajent-social/amos/identity"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
)

func (s *Service) stepWriter(ctx context.Context, principal identity.Principal, r *http.Request, input proofRequest, pending bool) (session.Issued, error) {
	if ctx == nil || r == nil || ctx != r.Context() || input.CurrentPassword == "" || len(input.CurrentPassword) > 512 || !validTOTPCode(input.Code) || pending && !validID(input.FactorID) {
		return session.Issued{}, ErrBadCode
	}
	request, err := s.writerSessions.AdmitWriterRequest(r)
	if err != nil {
		if errors.Is(err, session.ErrUnauthenticated) {
			return session.Issued{}, ErrDenied
		}
		return session.Issued{}, ErrUnavailable
	}
	if !s.writerSessions.WriterPrincipalMatches(request, principal) {
		return session.Issued{}, ErrDenied
	}
	ctx = r.Context()
	scope := scopeFor(principal)
	if !validScope(scope) {
		return session.Issued{}, ErrDenied
	}
	action, policyAction := wp.MFAChallenge, "totp.challenge"
	if pending {
		action = wp.MFAConfirm
		policyAction = "totp.confirm"
	}
	var staged session.Staged
	var permit wp.Permit
	var output aw.Binding
	counter := false
	counterResult := ErrBadCode
	reason := ErrDenied
	completion, err := s.root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		tx, e := a.DiscoveryTx(ctx)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		rows, e := primaryRows(ctx, tx, scope)
		if errors.Is(e, sql.ErrNoRows) {
			reason = ErrBadProof
			return mfaFinish(a, aw.DeniedRollback)
		}
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		prior, e := s.writerSessions.DiscoverPrior(ctx, a, request, action)
		if e != nil {
			return mfaParticipantFailure(e)
		}
		rows = append(rows, prior...)
		// Discovery is plain, non-authorizing reuse of the factor decoder. The
		// resulting exact factor ID joins the immutable H plan before any mutation.
		reader, e := NewReadStore(tx)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		var discovered Factor
		if pending {
			discovered, e = reader.Find(ctx, scope, input.FactorID)
		} else {
			discovered, e = reader.FindCurrent(ctx, scope, FactorActive)
		}
		if errors.Is(e, ErrFactorAbsent) {
			return mfaFinish(a, aw.DeniedRollback)
		}
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		rows = append(rows, factorRow(discovered.ID, aw.ExistingUpdate))
		dependencies, e := mfaPolicyRows(ctx, tx, scope)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		rows = append(rows, dependencies...)
		rows = mfaRows(rows)
		plan, e := aw.NewPlan(mfaRealm(scope), rows, nil)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		if e = a.SealPlan(plan); e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		if e = mfaAcquire(ctx, a); e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		tx, e = a.ParticipantTx(ctx, aw.W, rows)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		actor, e := s.writerSessions.ActorForWriter(ctx, a, request)
		if e != nil {
			return mfaFinish(a, mfaParticipantFailure(e))
		}
		currentPrincipal, e := s.currentPolicyPrincipal(ctx, tx, actor)
		if e != nil {
			return policyOutcome(a, e)
		}

		if e = s.policy(ctx, tx, currentPrincipal, policyAction); e != nil {
			return policyOutcome(a, e)
		}
		primary, e := s.writerPrimary.VerifyCurrentPasswordWriter(ctx, a, action, actor, input.CurrentPassword)
		if e != nil {
			reason = mapPrimaryError(e)
			return mfaParticipantFailure(e)
		}
		snapshot, e := primary.PasswordSnapshot(a, action)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		st, e := NewWriter(a)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		factor, e := st.Find(ctx, scope, discovered.ID)
		if errors.Is(e, ErrFactorAbsent) {
			return mfaFinish(a, aw.DeniedRollback)
		}
		if e != nil {
			return mfaParticipantFailure(e)
		}
		if e = factorShape(factor); e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		if !sameFactor(discovered, factor) || pending && factor.State != FactorPending || !pending && factor.State != FactorActive {
			return mfaFinish(a, aw.DeniedRollback)
		}
		if pending && *factor.PendingSecurityEpoch != actor.Subject.Epoch {
			return mfaFinish(a, aw.DeniedRollback)
		}
		var aadExpiry time.Time
		if pending {
			aadExpiry = *factor.ExpiresAt
		}
		seed, e := s.cfg.Vault.Open(ctx, tx, scope, factor.ID, factor.State, aadExpiry, seedPurpose, factor.SeedCiphertext)
		if e != nil || len(seed) == 0 || len(seed) > 256 {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		defer wipe(seed)
		var activeCiphertext []byte
		if pending {
			activeCiphertext, e = s.cfg.Vault.Seal(ctx, tx, scope, factor.ID, FactorActive, time.Time{}, seedPurpose, seed)
			if e != nil || len(activeCiphertext) < 32 || len(activeCiphertext) > 4096 {
				return mfaFinish(a, aw.UnavailableRollback)
			}
		}
		// T follows all vault work and remains the original TOTP/window deadline.
		t, e := mfaClock(ctx, tx)
		if e != nil || t.Before(snapshot.VerifiedAt) || t.Before(factor.CreatedAt) {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		if !t.Before(snapshot.ValidUntil) || pending && !t.Before(*factor.ExpiresAt) {
			return mfaFinish(a, aw.DeniedRollback)
		}
		if st.Locked(factor, t) {
			reason = ErrBadCode
			return mfaFinish(a, aw.DeniedRollback)
		}
		step, matched := matchingStep(input.Code, string(seed), t)
		if !matched {
			step = -1
		}
		check := factorCheck(factor, actor.Subject.Epoch, step, t)
		var evidence wp.Evidence
		expected := factor
		if !matched || step <= factor.LastUsedStep {
			counter = true
			counterResult = ErrBadCode
			counterReason := wp.BadCode
			if matched {
				counterResult = ErrReplay
				counterReason = wp.Replay
			}
			if e = a.RestrictCounter(factor.ID); e != nil {
				return mfaFinish(a, aw.UnavailableRollback)
			}
			transition := wp.CounterTransition{FactorID: factor.ID, Reason: counterReason, BeforeAttempts: factor.FailedAttempts, AfterAttempts: min(factor.FailedAttempts+1, DefaultMaxAttempts), UpdatedAt: t}
			if factor.LockedUntil != nil {
				transition.BeforeLockedUntil = *factor.LockedUntil
				transition.AfterAttempts = 1
			}
			if transition.AfterAttempts == DefaultMaxAttempts {
				transition.AfterLockedUntil = t.Add(DefaultCodeLockout)
			}
			evidence, e = wp.Counter(a, actor, primary, check, transition)
			if e != nil {
				return mfaProofFailure(a, e)
			}
			if e = st.RecordFailure(ctx, scope, factor.ID, t); e != nil {
				return mfaParticipantFailure(e)
			}
			expected.FailedAttempts = transition.AfterAttempts
			expected.LockedUntil = nil
			if !transition.AfterLockedUntil.IsZero() {
				until := transition.AfterLockedUntil
				expected.LockedUntil = &until
			}
		} else {
			evidence, e = wp.TOTP(a, action, actor, primary, check)
			if e != nil {
				return mfaProofFailure(a, e)
			}
			if pending {
				if e = st.ActivatePendingAndConsumeStep(ctx, scope, factor.ID, actor.Subject.Epoch, step, activeCiphertext, t); e != nil {
					return mfaParticipantFailure(e)
				}
				expected.State = FactorActive
				expected.PendingSecurityEpoch = nil
				expected.ExpiresAt = nil
				expected.ActivatedAt = &t
				expected.SeedCiphertext = activeCiphertext
			} else {
				accepted, e := st.AcceptStep(ctx, scope, factor.ID, step, t)
				if e != nil {
					return mfaParticipantFailure(e)
				}
				if !accepted {
					return mfaFinish(a, aw.UnavailableRollback)
				}
			}
			expected.LastUsedStep = step
			expected.FailedAttempts = 0
			expected.LockedUntil = nil
			issuance, e := wp.ForIssue(a, evidence)
			if e != nil {
				return mfaProofFailure(a, e)
			}
			staged, e = s.writerSessions.StageWriter(ctx, a, issuance, request)
			if e != nil {
				return mfaParticipantFailure(e)
			}
		}
		output, e = a.Binding()
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		f, e := a.DrainAndSample(ctx)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		if e = checkPrimaryFinal(ctx, tx, snapshot); e != nil {
			return policyOutcome(a, e)
		}
		if e = checkActorFinal(ctx, tx, actor, !counter, f); e != nil {
			return policyOutcome(a, e)
		}
		if e = s.policy(ctx, tx, currentPrincipal, policyAction); e != nil {
			return policyOutcome(a, e)
		}
		if e = checkFactorFinal(ctx, tx, expected, t); e != nil {
			return policyOutcome(a, e)
		}
		if !counter {
			if e = s.writerSessions.CheckStagedWriter(ctx, a, staged, f); e != nil {
				return mfaParticipantFailure(e)
			}
		}
		permit, e = wp.Finalize(a, evidence, f)
		if e != nil {
			return mfaProofFailure(a, e)
		}
		if counter {
			return mfaFinish(a, aw.CounterOnlyDenied)
		}
		return mfaFinish(a, aw.Success)
	})
	if err != nil {
		if errors.Is(err, aw.ErrDenied) {
			return session.Issued{}, classify(reason)
		}
		return session.Issued{}, ErrUnavailable
	}
	if counter {
		release, e := completion.TakeRelease(output)
		if e != nil || !permit.MatchesRelease(release) {
			return session.Issued{}, ErrUnavailable
		}
		outcome, e := release.Outcome()
		if e != nil || outcome != aw.CounterOnlyDenied {
			return session.Issued{}, ErrUnavailable
		}
		return session.Issued{}, counterResult
	}
	issued, err := s.writerSessions.PublishWriter(completion, permit, staged)
	if err != nil {
		return session.Issued{}, ErrUnavailable
	}
	return issued, nil
}
