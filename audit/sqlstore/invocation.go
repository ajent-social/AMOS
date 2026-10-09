package sqlstore

import (
	"context"
	"database/sql"

	"github.com/ajent-social/amos/app/operation"
	"github.com/ajent-social/amos/audit"
	"github.com/ajent-social/amos/identity"
	workspacecontext "github.com/ajent-social/amos/workspace/context"
)

// InvocationWriter is trusted internal persistence, not an authorization
// service. Its owning executor must recheck current authority in the same
// transaction. It can record a denial without granting the attempted action.
type InvocationWriter struct{}

var _ operation.InvocationAuditWriter = (*InvocationWriter)(nil)

func NewInvocationWriter() *InvocationWriter { return &InvocationWriter{} }

// AppendInvocationTx derives attribution only from existing trusted middleware
// contexts. Registered operation ID and server correlation are supplied by the
// executor, never forwarded from request payload attribution. No commit occurs.
func (w *InvocationWriter) AppendInvocationTx(ctx context.Context, tx *sql.Tx, invocationID identity.ID, operationID string, outcome audit.Outcome, correlationID identity.ID) error {
	if w == nil || ctx == nil || tx == nil {
		return audit.ErrInvalidEvent
	}
	principal, ok := identity.PrincipalFromContext(ctx)
	if !ok {
		return audit.ErrForbidden
	}
	selection, ok := workspacecontext.FromContext(ctx)
	if !ok || selection.Workspace.Scope.InstallationID != principal.InstallationID() || selection.Workspace.Scope.ApplicationID != principal.ApplicationID() || principal.Actor().Kind() != "person" {
		return audit.ErrForbidden
	}
	actor := audit.Actor{Kind: audit.ActorPerson, ID: principal.Actor().PersonID()}
	event := audit.Event{
		InstallationID: principal.InstallationID(), ApplicationID: principal.ApplicationID(), EnvironmentID: principal.EnvironmentID(), WorkspaceID: selection.Workspace.ID,
		Action: audit.ActionOperationInvoked, ResourceType: audit.ResourceInvocation, ResourceID: invocationID,
		Outcome: outcome, CorrelationID: correlationID, OperationID: operationID, Attributes: []audit.Attribute{},
	}
	if !audit.ValidActor(actor) || audit.Validate(event) != nil {
		return audit.ErrInvalidEvent
	}
	_, err := insertEvent(ctx, tx, event, actor)
	return err
}
