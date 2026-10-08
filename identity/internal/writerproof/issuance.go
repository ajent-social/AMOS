package writerproof

import (
	"time"

	"github.com/ajent-social/amos/identity/internal/authproof"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
)

func issueFields(d *evidenceData) (string, time.Time, string, time.Time, bool) {
	switch {
	case d.kind == passwordKind && d.action == PasswordSignIn:
		v := d.passwordCheck.VerifiedAt
		return "email_password", v, "aal1", v.Add(12 * time.Hour), true
	case d.kind == challengeKind && d.action == MagicConfirm:
		v := d.challenge.VerifiedAt
		return "email_magic_link", v, "aal1", v.Add(12 * time.Hour), true
	case d.kind == totpKind && (d.action == MFAConfirm || d.action == MFAChallenge):
		v := d.factor.VerifiedAt
		return d.actor.Method, v, "aal2", v.Add(15 * time.Minute), true
	case d.kind == providerKind && d.action == FederationCallbackLogin:
		v := d.provider.ValidatedAt
		return d.provider.Provider, v, "aal1", v.Add(12 * time.Hour), true
	default:
		return "", time.Time{}, "", time.Time{}, false
	}
}
func ForIssue(a *aw.Attempt, evidence Evidence) (Issuance, error) {
	d := evidence.data
	if d == nil || !d.binding.Matches(a) || !validSubject(d.subject) {
		return Issuance{}, ErrUnavailable
	}
	if err := checkRows(a, aw.S, personRow(d.subject)); err != nil {
		return Issuance{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finalized || d.issued != nil {
		return Issuance{}, reject(a)
	}
	method, v, level, expiry, ok := issueFields(d)
	if !ok {
		return Issuance{}, ErrUnavailable
	}
	credential, err := authproof.NewVerifiedCredential(d.subject.Person, d.subject.Realm.Installation, d.subject.Realm.Application, d.subject.Realm.Environment, d.subject.Epoch, method, v, level, expiry)
	if err != nil {
		return Issuance{}, ErrUnavailable
	}
	i := &issuanceData{evidence: d, verifiedCredential: credential}
	d.issued = i
	return Issuance{data: i}, nil
}

// Check validates a live issuance without consuming its credential getter.
// Store callers must separately check each actual session row and S acquisition.
func (i Issuance) Check(a *aw.Attempt) error {
	if i.data == nil || i.data.evidence == nil {
		return ErrUnavailable
	}
	d := i.data.evidence
	if !d.binding.Matches(a) || !validSubject(d.subject) {
		return ErrUnavailable
	}
	if err := checkRows(a, aw.S, personRow(d.subject)); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, _, _, _, issuing := issueFields(d)
	if !issuing || d.finalized || d.issued != i.data {
		return ErrUnavailable
	}
	return nil
}
func (i Issuance) Credential(a *aw.Attempt) (authproof.VerifiedCredential, error) {
	if err := i.Check(a); err != nil {
		return authproof.VerifiedCredential{}, err
	}
	d := i.data.evidence
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finalized || d.issued != i.data || i.data.consumed {
		return authproof.VerifiedCredential{}, reject(a)
	}
	i.data.consumed = true
	return i.data.verifiedCredential, nil
}

func actorLive(c ActorCheck, f time.Time) bool {
	return actorShape(c) && !f.Before(c.AuthenticatedAt) && f.Before(c.IdleUntil) && f.Before(c.AbsoluteUntil) && f.Before(c.AssuranceUntil)
}
func (d *evidenceData) finalBounds(f time.Time) bool {
	if f.IsZero() || f.Before(d.started) {
		return false
	}
	switch d.kind {
	case passwordKind:
		return !f.Before(d.passwordCheck.VerifiedAt) && f.Before(d.passwordCheck.ValidUntil)
	case challengeKind:
		return !f.Before(d.challenge.VerifiedAt) && !f.Before(d.challenge.CreatedAt) && f.Before(d.challenge.ExpiresAt)
	case actorKind:
		return actorLive(d.actor, f)
	case enrollmentKind:
		return actorLive(d.actor, f) && d.primary != nil && d.primary.finalBounds(f) && f.Before(d.factor.PendingUntil)
	case totpKind:
		return actorLive(d.actor, f) && d.primary != nil && d.primary.finalBounds(f) && !f.Before(d.factor.VerifiedAt) && f.Before(d.factor.WindowUntil) && f.Before(d.factor.VerifiedAt.Add(15*time.Minute)) && (d.action != MFAConfirm || f.Before(d.factor.PendingUntil))
	case counterKind:
		return actorLive(d.actor, f) && d.primary != nil && d.primary.finalBounds(f) && !f.Before(d.counter.UpdatedAt) && f.Before(d.factor.VerifiedAt.Add(30*time.Second)) && (d.action != MFAConfirm || f.Before(d.factor.PendingUntil))
	case flowKind, providerKind:
		if f.Before(d.flow.CreatedAt) || !f.Before(d.flow.ExpiresAt) {
			return false
		}
		if d.action == FederationBeginLink || d.action == FederationCallbackLink {
			if !actorLive(d.actor, f) || !f.Before(d.actor.AuthenticatedAt.Add(15*time.Minute)) {
				return false
			}
		}
		if d.kind == providerKind {
			return !f.Before(d.provider.ValidatedAt) && f.Before(d.provider.ProviderValidUntil) && f.Before(d.provider.ReceiptUntil)
		}
		return true
	default:
		return false
	}
}

func (d *evidenceData) finalChronology(f time.Time) bool {
	if d == nil || f.IsZero() || d.started.IsZero() || f.Before(d.started) {
		return false
	}
	notBefore := func(t time.Time) bool { return !t.IsZero() && !f.Before(t) }
	switch d.kind {
	case passwordKind:
		return notBefore(d.passwordCheck.VerifiedAt)
	case challengeKind:
		return notBefore(d.challenge.CreatedAt) && notBefore(d.challenge.VerifiedAt)
	case actorKind:
		return notBefore(d.actor.AuthenticatedAt)
	case enrollmentKind:
		return notBefore(d.actor.AuthenticatedAt) && d.primary.finalChronology(f)
	case totpKind:
		return notBefore(d.actor.AuthenticatedAt) && notBefore(d.factor.VerifiedAt) && d.primary.finalChronology(f)
	case counterKind:
		return notBefore(d.actor.AuthenticatedAt) && notBefore(d.factor.VerifiedAt) && notBefore(d.counter.UpdatedAt) && d.primary.finalChronology(f)
	case flowKind, providerKind:
		if !notBefore(d.flow.CreatedAt) {
			return false
		}
		if (d.action == FederationBeginLink || d.action == FederationCallbackLink) && !notBefore(d.actor.AuthenticatedAt) {
			return false
		}
		return d.kind != providerKind || notBefore(d.provider.ValidationStartedAt) && notBefore(d.provider.ValidatedAt)
	default:
		return false
	}
}

func (d *evidenceData) finalError(f time.Time) error {
	if !d.finalChronology(f) {
		return ErrUnavailable
	}
	if !d.finalBounds(f) {
		return ErrDenied
	}
	return nil
}

// Finalize checks immutable evidence bounds at the root's exact recorded F.
// Native callers must first compare every held row to the intended transition;
// this factory cannot infer those SQL predicates from copied snapshots.
func Finalize(a *aw.Attempt, evidence Evidence, finalDBTime time.Time) (Permit, error) {
	d := evidence.data
	if d == nil || !d.binding.Matches(a) || !a.IsFinalSample(finalDBTime) {
		return Permit{}, ErrUnavailable
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finalized {
		return Permit{}, reject(a)
	}
	if err := d.finalError(finalDBTime); err != nil {
		return Permit{}, err
	}
	_, v, _, _, issuing := issueFields(d)
	if issuing {
		if d.issued == nil || !d.issued.consumed || !finalDBTime.Before(v.Add(30*time.Minute)) || !finalDBTime.Before(v.Add(12*time.Hour)) {
			return Permit{}, ErrUnavailable
		}
	} else if d.issued != nil {
		return Permit{}, ErrUnavailable
	}
	outcome := aw.Success
	if d.kind == counterKind {
		outcome = aw.CounterOnlyDenied
	}
	d.finalized = true
	return Permit{data: &finalizationData{binding: d.binding, action: d.action, outcome: outcome, finalTime: finalDBTime, issuance: d.issued}}, nil
}
func (p Permit) Outcome() (aw.Outcome, error) {
	if p.data == nil || p.data.finalTime.IsZero() || (p.data.outcome != aw.Success && p.data.outcome != aw.CounterOnlyDenied) {
		return 0, ErrUnavailable
	}
	return p.data.outcome, nil
}
func (p Permit) Matches(a *aw.Attempt, issuance Issuance) bool {
	return p.data != nil && p.data.outcome == aw.Success && p.data.issuance != nil && p.data.issuance == issuance.data && p.data.binding.Matches(a)
}
func (p Permit) MatchesRelease(release aw.Release) bool {
	if _, err := p.Outcome(); err != nil {
		return false
	}
	outcome, err := release.Outcome()
	return err == nil && outcome == p.data.outcome && release.Matches(p.data.binding)
}

func reject(a *aw.Attempt) error {
	// Terminal rollback also handles an ignored duplicate-call error. A closed
	// or already-poisoned attempt is already incapable of publishing output.
	if err := a.Finish(aw.UnavailableRollback); err != nil {
		return ErrUnavailable
	}
	return ErrUnavailable
}
