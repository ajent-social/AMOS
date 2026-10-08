package writerproof

import (
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
)

func primaryFor(a *aw.Attempt, primary Evidence, action Action, actor ActorCheck) (*evidenceData, error) {
	d := primary.data
	if d == nil || d.kind != passwordKind || d.action != action || !d.binding.Matches(a) || !same(d.subject, actor.Subject) {
		return nil, ErrUnavailable
	}
	if err := actorRows(a, actor); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finalized || actor.AuthenticatedAt.After(d.started) {
		return nil, ErrUnavailable
	}
	return d, nil
}
func factorShape(f FactorCheck) bool {
	return validID(f.ID) && nonzero(f.CiphertextDigest) && f.Epoch >= 0 && f.LastStep >= -1 && f.AcceptedStep >= -1 && f.FailedAttempts >= 0 && f.FailedAttempts <= 5 && (f.State == "pending" || f.State == "active")
}
func Enrollment(a *aw.Attempt, actor ActorCheck, primary Evidence, factor FactorCheck) (Evidence, error) {
	p, err := primaryFor(a, primary, MFABegin, actor)
	if err != nil {
		return Evidence{}, err
	}
	if !factorShape(factor) || factor.State != "pending" || factor.Epoch != actor.Subject.Epoch || !factor.PendingUntil.Equal(p.passwordCheck.VerifiedAt.Add(30*time.Minute)) || factor.LastStep != -1 || factor.AcceptedStep != -1 || factor.FailedAttempts != 0 || !factor.LockedUntil.IsZero() || !factor.WindowUntil.IsZero() {
		return Evidence{}, ErrUnavailable
	}
	if err := checkRows(a, aw.H, aw.Row{Table: aw.Factors, ID: factor.ID, Access: aw.ReservedInsert}); err != nil {
		return Evidence{}, err
	}
	d, err := newEvidence(a, MFABegin, enrollmentKind, actor.Subject)
	if err != nil {
		return Evidence{}, err
	}
	d.primary = p
	d.actor = actor
	d.factor = factor
	return Evidence{data: d}, nil
}
func TOTP(a *aw.Attempt, action Action, actor ActorCheck, primary Evidence, factor FactorCheck) (Evidence, error) {
	if action != MFAConfirm && action != MFAChallenge {
		return Evidence{}, ErrUnavailable
	}
	p, err := primaryFor(a, primary, action, actor)
	if err != nil {
		return Evidence{}, err
	}
	if !factorShape(factor) || factor.VerifiedAt.IsZero() || factor.VerifiedAt.Before(p.passwordCheck.VerifiedAt) || factor.AcceptedStep < 0 || factor.AcceptedStep <= factor.LastStep || !factor.LockedUntil.IsZero() && factor.LockedUntil.After(factor.VerifiedAt) {
		return Evidence{}, ErrUnavailable
	}
	// Reject arithmetic overflow before deriving the immutable accepted window.
	if factor.AcceptedStep > (1<<63-1)/30-2 {
		return Evidence{}, ErrUnavailable
	}
	window := time.Unix((factor.AcceptedStep+2)*30, 0).UTC()
	if !factor.WindowUntil.Equal(window) || factor.VerifiedAt.Before(time.Unix((factor.AcceptedStep-1)*30, 0)) || !factor.VerifiedAt.Before(window) {
		return Evidence{}, ErrUnavailable
	}
	if action == MFAConfirm {
		if factor.State != "pending" || factor.Epoch != actor.Subject.Epoch || factor.PendingUntil.IsZero() || !factor.VerifiedAt.Before(factor.PendingUntil) {
			return Evidence{}, ErrUnavailable
		}
	} else if factor.State != "active" || !factor.PendingUntil.IsZero() {
		return Evidence{}, ErrUnavailable
	}
	if err := checkRows(a, aw.H, aw.Row{Table: aw.Factors, ID: factor.ID, Access: aw.ExistingUpdate}); err != nil {
		return Evidence{}, err
	}
	d, err := newEvidence(a, action, totpKind, actor.Subject)
	if err != nil {
		return Evidence{}, err
	}
	d.primary = p
	d.actor = actor
	d.factor = factor
	return Evidence{data: d}, nil
}
func Counter(a *aw.Attempt, actor ActorCheck, primary Evidence, factor FactorCheck, transition CounterTransition) (Evidence, error) {
	if primary.data == nil {
		return Evidence{}, ErrUnavailable
	}
	action := primary.data.action
	if action != MFAConfirm && action != MFAChallenge {
		return Evidence{}, ErrUnavailable
	}
	p, err := primaryFor(a, primary, action, actor)
	if err != nil {
		return Evidence{}, err
	}
	if !factorShape(factor) || factor.VerifiedAt.IsZero() || factor.VerifiedAt.Before(p.passwordCheck.VerifiedAt) || transition.FactorID != factor.ID || transition.BeforeAttempts != factor.FailedAttempts || !transition.BeforeLockedUntil.Equal(factor.LockedUntil) || !transition.UpdatedAt.Equal(factor.VerifiedAt) || !factor.LockedUntil.IsZero() && factor.LockedUntil.After(factor.VerifiedAt) || p.passwordCheck.StagedHash != "" {
		return Evidence{}, ErrUnavailable
	}
	if action == MFAConfirm {
		if factor.State != "pending" || factor.Epoch != actor.Subject.Epoch || factor.PendingUntil.IsZero() || !factor.VerifiedAt.Before(factor.PendingUntil) {
			return Evidence{}, ErrUnavailable
		}
	} else if factor.State != "active" || !factor.PendingUntil.IsZero() {
		return Evidence{}, ErrUnavailable
	}
	switch transition.Reason {
	case BadCode:
		if factor.AcceptedStep != -1 {
			return Evidence{}, ErrUnavailable
		}
	case Replay:
		if factor.AcceptedStep < 0 || factor.AcceptedStep > factor.LastStep {
			return Evidence{}, ErrUnavailable
		}
	default:
		return Evidence{}, ErrUnavailable
	}
	next := min(factor.FailedAttempts+1, 5)
	if !factor.LockedUntil.IsZero() {
		next = 1
	}
	if transition.AfterAttempts != next {
		return Evidence{}, ErrUnavailable
	}
	if next == 5 {
		if !transition.AfterLockedUntil.Equal(factor.VerifiedAt.Add(15 * time.Minute)) {
			return Evidence{}, ErrUnavailable
		}
	} else if !transition.AfterLockedUntil.IsZero() {
		return Evidence{}, ErrUnavailable
	}
	if err := checkRows(a, aw.H, aw.Row{Table: aw.Factors, ID: factor.ID, Access: aw.ExistingUpdate}); err != nil {
		return Evidence{}, err
	}
	d, err := newEvidence(a, action, counterKind, actor.Subject)
	if err != nil {
		return Evidence{}, err
	}
	d.primary = p
	d.actor = actor
	d.factor = factor
	d.counter = transition
	return Evidence{data: d}, nil
}
