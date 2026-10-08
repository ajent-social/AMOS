package writerproof

import (
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
)

func providerMethod(value string) bool {
	switch value {
	case "google", "github", "apple", "enterprise_oidc":
		return true
	default:
		return false
	}
}
func flowShape(flow FlowCheck) bool {
	return validID(flow.ID) && validID(flow.ConnectionID) && providerMethod(flow.Provider) && flow.Issuer != "" && len(flow.Issuer) <= 2048 && len(flow.Subject) <= 1024 && flow.ReturnTo != "" && len(flow.ReturnTo) <= 2048 && flow.CallbackURL != "" && len(flow.CallbackURL) <= 2048 && flow.CodeChallenge != "" && len(flow.CodeChallenge) <= 128 && flow.CodeChallengeMethod == "S256" && flow.AuthorizationURL != "" && len(flow.AuthorizationURL) <= 8192 && nonzero(flow.StateDigest) && nonzero(flow.BrowserDigest) && nonzero(flow.NonceDigest) && !flow.CreatedAt.IsZero() && flow.ExpiresAt.Equal(flow.CreatedAt.Add(10*time.Minute))
}
func flowEvidence(a *aw.Attempt, action Action, kind evidenceKind, flow FlowCheck, actor *ActorCheck) (*evidenceData, error) {
	if !flowShape(flow) {
		return nil, ErrUnavailable
	}
	b, start, err := realm(a, flow.Realm)
	if err != nil {
		return nil, err
	}
	d := &evidenceData{binding: b, started: start, action: action, kind: kind, flow: flow}
	link := action == FederationBeginLink || action == FederationCallbackLink
	if link {
		if actor == nil || actor.Subject.Realm != flow.Realm || actor.AuthenticatedAt.After(start) {
			return nil, ErrUnavailable
		}
		if err := actorRows(a, *actor); err != nil {
			return nil, err
		}
		d.actor = *actor
		d.subject = actor.Subject
	} else if actor != nil {
		return nil, ErrUnavailable
	}
	access := aw.ExistingUpdate
	if action == FederationBeginLogin || action == FederationBeginLink {
		access = aw.ReservedInsert
		if !flow.CreatedAt.Equal(start) || flow.Subject != "" {
			return nil, ErrUnavailable
		}
	} else if flow.CreatedAt.After(start) {
		return nil, ErrUnavailable
	}
	if err := checkRows(a, aw.H, aw.Row{Table: aw.Connections, ID: flow.ConnectionID, Access: aw.ExistingShare}, aw.Row{Table: aw.Flows, ID: flow.ID, Access: access}); err != nil {
		return nil, err
	}
	return d, nil
}
func FlowBegin(a *aw.Attempt, action Action, flow FlowCheck, actor *ActorCheck) (Evidence, error) {
	if action != FederationBeginLogin && action != FederationBeginLink {
		return Evidence{}, ErrUnavailable
	}
	d, err := flowEvidence(a, action, flowKind, flow, actor)
	if err != nil {
		return Evidence{}, err
	}
	return Evidence{data: d}, nil
}
func FlowCallback(a *aw.Attempt, action Action, flow FlowCheck, provider ProviderCheck, subject Subject, actor *ActorCheck) (Evidence, error) {
	if action != FederationCallbackLogin && action != FederationCallbackLink {
		return Evidence{}, ErrUnavailable
	}
	d, err := flowEvidence(a, action, providerKind, flow, actor)
	if err != nil {
		return Evidence{}, err
	}
	if !validSubject(subject) || subject.Realm != flow.Realm || action == FederationCallbackLink && !same(subject, d.subject) {
		return Evidence{}, ErrUnavailable
	}
	if provider.ConnectionID != flow.ConnectionID || provider.Provider != flow.Provider || provider.Issuer != flow.Issuer || provider.Subject == "" || provider.Subject != flow.Subject || provider.NonceDigest != flow.NonceDigest || provider.ValidationStartedAt.IsZero() || provider.ValidatedAt.IsZero() || provider.ValidatedAt.Before(provider.ValidationStartedAt) || provider.ValidatedAt.After(d.started) || !provider.ReceiptUntil.Equal(provider.ValidationStartedAt.Add(60*time.Second)) || !provider.ValidatedAt.Before(provider.ReceiptUntil) || !provider.ValidatedAt.Before(provider.ProviderValidUntil) {
		return Evidence{}, ErrUnavailable
	}
	if err := checkRows(a, aw.P, personRow(subject)); err != nil {
		return Evidence{}, err
	}
	d.subject = subject
	d.provider = provider
	return Evidence{data: d}, nil
}
