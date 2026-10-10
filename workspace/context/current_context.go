package context

import (
	"context"
	"database/sql"

	"github.com/ajent-social/amos/identity"
	"github.com/google/uuid"
)

// ResolveCurrentContextTx resolves current facts on the caller's retained
// transaction and binds a private copy for attribution. The already-admitted
// context principal must match the freshly rechecked principal in every exposed
// field. This is neither a raw selection setter nor a session-provenance proof.
// The caller still owns current rechecks, permission evaluation and finalization;
// retaining the returned context does not authorize a later transaction.
func (r *Resolver) ResolveCurrentContextTx(ctx context.Context, tx *sql.Tx, principal identity.Principal, workspaceID uuid.UUID) (context.Context, Selection, error) {
	if r == nil || !validConfig(r.cfg) || ctx == nil || tx == nil || ctx.Err() != nil {
		return nil, Selection{}, ErrUnavailable
	}
	admitted, ok := identity.PrincipalFromContext(ctx)
	if !ok || !sameCurrentPrincipal(admitted, principal) {
		return nil, Selection{}, ErrDenied
	}
	selection, err := r.ResolveCurrentTx(ctx, tx, principal, workspaceID)
	if ctx.Err() != nil {
		return nil, Selection{}, ErrUnavailable
	}
	if err != nil {
		return nil, Selection{}, err
	}
	return context.WithValue(ctx, selectionKey{}, copySelection(selection)), copySelection(selection), nil
}

func sameCurrentPrincipal(a, b identity.Principal) bool {
	return a.InstallationID() == b.InstallationID() && a.ApplicationID() == b.ApplicationID() && a.EnvironmentID() == b.EnvironmentID() && a.PersonID() == b.PersonID() && a.SecurityEpoch() == b.SecurityEpoch() && a.AuthenticationMethod() == b.AuthenticationMethod() && a.AuthenticatedAt().Equal(b.AuthenticatedAt()) && a.Actor().Kind() == b.Actor().Kind() && a.Actor().PersonID() == b.Actor().PersonID() && a.Actor().MachineID() == b.Actor().MachineID() && a.Assurance().Level() == b.Assurance().Level() && a.Assurance().ExpiresAt().Equal(b.Assurance().ExpiresAt())
}
