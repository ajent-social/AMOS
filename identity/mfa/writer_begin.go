package mfa

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"

	"github.com/ajent-social/amos/identity"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func (s *Service) beginWriter(ctx context.Context, principal identity.Principal, supplied string) (Enrollment, error) {
	if ctx == nil || supplied == "" || len(supplied) > 512 {
		return Enrollment{}, ErrUnavailable
	}
	request, err := s.writerSessions.AdmitWriterContext(ctx, wp.MFABegin)
	if err != nil {
		if errors.Is(err, session.ErrUnauthenticated) {
			return Enrollment{}, ErrDenied
		}
		return Enrollment{}, ErrUnavailable
	}
	if !s.writerSessions.WriterPrincipalMatches(request, principal) {
		return Enrollment{}, ErrDenied
	}
	scope := scopeFor(principal)
	if !validScope(scope) {
		return Enrollment{}, ErrDenied
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: s.cfg.Issuer, AccountName: scope.PersonID.String(), Period: uint(TOTPPeriod / time.Second), SecretSize: 20, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return Enrollment{}, ErrUnavailable
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Enrollment{}, ErrUnavailable
	}
	seed := []byte(key.Secret())
	defer wipe(seed)
	var result Enrollment
	var output aw.Binding
	var permit wp.Permit
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
		prior, e := s.writerSessions.DiscoverPrior(ctx, a, request, wp.MFABegin)
		if e != nil {
			return mfaParticipantFailure(e)
		}
		rows = append(rows, prior...)
		factors, e := tx.QueryContext(ctx, `SELECT id FROM public.identity_totp_factors WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND person_id=$4 AND state IN ('pending','active')`, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.PersonID)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		for factors.Next() {
			var existing uuid.UUID
			if e = factors.Scan(&existing); e != nil {
				break
			}
			rows = append(rows, factorRow(existing, aw.ExistingUpdate))
		}
		if next := factors.Err(); e == nil {
			e = next
		}
		if next := factors.Close(); e == nil {
			e = next
		}
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		rows = append(rows, factorRow(id, aw.ReservedInsert))
		policyRows, e := mfaPolicyRows(ctx, tx, scope)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		rows = append(rows, policyRows...)
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

		if e = s.policy(ctx, tx, currentPrincipal, "totp.enroll"); e != nil {
			return policyOutcome(a, e)
		}
		primary, e := s.writerPrimary.VerifyCurrentPasswordWriter(ctx, a, wp.MFABegin, actor, supplied)
		if e != nil {
			reason = mapPrimaryError(e)
			return mfaParticipantFailure(e)
		}
		snapshot, e := primary.PasswordSnapshot(a, wp.MFABegin)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		expires := snapshot.VerifiedAt.Add(DefaultPendingLifetime)
		sealed, e := s.cfg.Vault.Seal(ctx, tx, scope, id, FactorPending, expires, seedPurpose, seed)
		if e != nil || len(sealed) < 32 || len(sealed) > 4096 {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		now, e := mfaClock(ctx, tx)
		if e != nil || now.Before(snapshot.VerifiedAt) {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		if !now.Before(expires) {
			return mfaFinish(a, aw.DeniedRollback)
		}
		st, e := NewWriter(a)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		if e = st.CreatePending(ctx, Factor{ID: id, Scope: scope, SeedCiphertext: sealed, State: FactorPending}, actor.Subject.Epoch, snapshot.VerifiedAt); e != nil {
			reason = e
			return mfaParticipantFailure(e)
		}
		factor := wp.FactorCheck{ID: id, State: string(FactorPending), Epoch: actor.Subject.Epoch, LastStep: -1, AcceptedStep: -1, CiphertextDigest: sha256.Sum256(sealed), PendingUntil: expires}
		evidence, e := wp.Enrollment(a, actor, primary, factor)
		if e != nil {
			return mfaProofFailure(a, e)
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
		if e = checkActorFinal(ctx, tx, actor, false, f); e != nil {
			return policyOutcome(a, e)
		}
		if e = s.policy(ctx, tx, currentPrincipal, "totp.enroll"); e != nil {
			return policyOutcome(a, e)
		}
		reader, e := NewReadStore(tx)
		if e != nil {
			return mfaFinish(a, aw.UnavailableRollback)
		}
		final, e := reader.Find(ctx, scope, id)
		if e != nil {
			return mfaFinish(a, mfaParticipantFailure(e))
		}
		if final.State != FactorPending || final.PendingSecurityEpoch == nil || *final.PendingSecurityEpoch != actor.Subject.Epoch || final.ExpiresAt == nil || !final.ExpiresAt.Equal(expires) || !final.CreatedAt.Equal(snapshot.VerifiedAt) || final.LastUsedStep != -1 || final.FailedAttempts != 0 || final.LockedUntil != nil || final.ActivatedAt != nil || !bytes.Equal(final.SeedCiphertext, sealed) {
			return mfaFinish(a, aw.DeniedRollback)
		}
		permit, e = wp.Finalize(a, evidence, f)
		if e != nil {
			return mfaProofFailure(a, e)
		}
		result = Enrollment{FactorID: id, Seed: key.Secret(), OTPAuthURI: key.URL(), ExpiresAt: expires}
		return mfaFinish(a, aw.Success)
	})
	if err != nil {
		if errors.Is(err, aw.ErrDenied) {
			return Enrollment{}, classify(reason)
		}
		return Enrollment{}, ErrUnavailable
	}
	release, err := completion.TakeRelease(output)
	if err != nil || !permit.MatchesRelease(release) {
		return Enrollment{}, ErrUnavailable
	}
	return result, nil
}
