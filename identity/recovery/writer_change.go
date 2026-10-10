package recovery

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/passwordwork"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/primaryproof"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
	"math"
	"time"
)

func recoveryRows(rows []aw.Row) []aw.Row {
	seen := map[aw.Row]bool{}
	result := []aw.Row{}
	for _, r := range rows {
		if !seen[r] {
			seen[r] = true
			result = append(result, r)
		}
	}
	return result
}
func recoverySessionError(e error) error {
	if errors.Is(e, session.ErrUnauthenticated) {
		return ErrChallengeUnavailable
	}
	return ErrUnavailable
}
func (s *Service) policyPrincipal(ctx context.Context, tx *sql.Tx, actor wp.ActorCheck) (identity.Principal, error) {
	p, e := s.sessions.RecheckCurrentTx(ctx, tx)
	if e != nil {
		return identity.Principal{}, recoverySessionError(e)
	}
	if p.PersonID() != actor.Subject.Person || p.InstallationID() != actor.Subject.Realm.Installation || p.ApplicationID() != actor.Subject.Realm.Application || p.EnvironmentID() != actor.Subject.Realm.Environment || p.SecurityEpoch() != actor.Subject.Epoch || p.AuthenticationMethod() != actor.Method || !p.AuthenticatedAt().Equal(actor.AuthenticatedAt) || string(p.Assurance().Level()) != actor.Assurance || !p.Assurance().ExpiresAt().Equal(actor.AssuranceUntil) {
		return identity.Principal{}, ErrChallengeUnavailable
	}
	return p, nil
}
func changedActor(ctx context.Context, tx *sql.Tx, actor wp.ActorCheck, newEpoch int64, f time.Time) error {
	var matches bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identity_sessions s JOIN identity_persons p ON p.id=s.person_id AND p.installation_id=s.installation_id AND p.application_id=s.application_id WHERE s.id=$1 AND s.person_id=$2 AND s.installation_id=$3 AND s.application_id=$4 AND s.environment_id=$5 AND s.token_digest=$6 AND s.security_epoch=$7 AND p.security_epoch=$8 AND p.state='active' AND s.authentication_method=$9 AND s.authenticated_at=$10 AND s.idle_expires_at=$11 AND s.expires_at=$12 AND s.revoked_at IS NOT NULL AND s.revoked_at<=$15 AND (CASE WHEN s.assurance_expires_at IS NOT NULL AND s.assurance_expires_at<=$15 THEN 'aal1' ELSE s.assurance_level END)=$13 AND (CASE WHEN s.assurance_expires_at IS NULL OR s.assurance_expires_at<=$15 THEN s.expires_at ELSE s.assurance_expires_at END)=$14)`, actor.SessionID, actor.Subject.Person, actor.Subject.Realm.Installation, actor.Subject.Realm.Application, actor.Subject.Realm.Environment, actor.Digest[:], actor.Subject.Epoch, newEpoch, actor.Method, actor.AuthenticatedAt, actor.IdleUntil, actor.AbsoluteUntil, actor.Assurance, actor.AssuranceUntil, f).Scan(&matches)
	if e != nil {
		return ErrUnavailable
	}
	if !matches {
		return ErrChallengeUnavailable
	}
	return nil
}
func (s *Service) changeWriter(ctx context.Context, principal identity.Principal, current, next string) error {
	request, e := s.sessions.AdmitWriterContext(ctx, wp.PasswordChange)
	if e != nil {
		return recoverySessionError(e)
	}
	if !s.sessions.WriterPrincipalMatches(request, principal) {
		return ErrChallengeUnavailable
	}
	var binding aw.Binding
	var permit wp.Permit
	var reason error
	completion, e := s.root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		fail := func(e error) aw.Outcome { reason = recoveryFailure(a, e); return recoveryOutcome(reason) }
		actorRows, e := s.sessions.DiscoverPrior(ctx, a, request, wp.PasswordChange)
		if e != nil {
			reason = recoverySessionError(e)
			return recoveryOutcome(reason)
		}
		tx, e := a.DiscoveryTx(ctx)
		if e != nil {
			return fail(ErrUnavailable)
		}
		v, e := s.account(ctx, tx, principal.PersonID(), uuid.Nil, "")
		if e != nil {
			return fail(e)
		}
		if v.credential == uuid.Nil || v.hash == "" || v.epoch != principal.SecurityEpoch() || v.epoch == math.MaxInt64 {
			return fail(ErrChallengeUnavailable)
		}
		sessions, e := sessionRows(ctx, tx, v.person)
		if e != nil {
			return fail(e)
		}
		policy, e := s.policyRows(ctx, tx, v.person)
		if e != nil {
			return fail(e)
		}
		rows := append(accountRows(v), actorRows...)
		rows = append(rows, sessions...)
		rows = recoveryRows(append(rows, policy...))
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
		if !sameAccount(v, held) {
			return fail(ErrChallengeUnavailable)
		}
		actor, e := s.sessions.ActorForWriter(ctx, a, request)
		if e != nil {
			reason = recoveryFailure(a, recoverySessionError(e))
			return recoveryOutcome(reason)
		}
		if actor.Subject.Person != v.person || actor.Subject.Epoch != v.epoch {
			return fail(ErrChallengeUnavailable)
		}
		refreshed, e := s.policyPrincipal(ctx, tx, actor)
		if e != nil {
			return fail(e)
		}
		if e = s.cfg.Policy.AuthorizePasswordChange(ctx, tx, refreshed); e != nil {
			return fail(e)
		}
		primary, e := s.primary.VerifyCurrentPasswordWriter(ctx, a, wp.PasswordChange, actor, current)
		if e != nil {
			reason = ErrUnavailable
			if errors.Is(e, primaryproof.ErrUnauthenticated) {
				reason = ErrChallengeUnavailable
			}
			return recoveryOutcome(reason)
		}
		original, e := primary.PasswordSnapshot(a, wp.PasswordChange)
		if e != nil {
			return fail(ErrUnavailable)
		}
		if original.CredentialID != v.credential || original.VerifiedHash != v.hash || original.Contact.ID != v.email || original.Contact.ComparisonKey != v.key || !original.Contact.VerifiedAt.Equal(v.verified) {
			return fail(ErrChallengeUnavailable)
		}
		actorProof, e := wp.Actor(a, wp.PasswordChange, actor)
		if e != nil {
			return fail(ErrUnavailable)
		}
		nextHash, e := passwordwork.Hash(ctx, s.cfg.Passwords, "change-new:"+v.person.String(), next)
		if e != nil {
			return fail(e)
		}
		after, e := s.applyPassword(ctx, a, tx, v, nextHash, sessions)
		if e != nil {
			reason = e
			return recoveryOutcome(e)
		}
		if after.epoch != v.epoch+1 {
			return fail(ErrUnavailable)
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
		if !sameAccount(after, final) {
			return fail(ErrChallengeUnavailable)
		}
		if e = revokedAll(ctx, tx, v.person, sessions, f); e != nil {
			return fail(e)
		}
		if e = changedActor(ctx, tx, actor, after.epoch, f); e != nil {
			return fail(e)
		}
		if e = s.writerPolicy.AuthorizePasswordChangeCompletion(ctx, tx, refreshed, after.epoch); e != nil {
			return fail(e)
		}
		if _, e = wp.Finalize(a, actorProof, f); e != nil {
			return fail(recoveryProofError(e))
		}
		permit, e = wp.Finalize(a, primary, f)
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
