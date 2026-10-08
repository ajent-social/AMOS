package personal

import (
	"context"
	"database/sql"
	"errors"

	identitystore "github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

// BootstrapPending provisions only the newly created pending account named by
// registration, in the same transaction. It neither activates the person nor
// creates a principal/session. Existing authenticated bootstrap stays separate.
func (p *Participant) BootstrapPending(ctx context.Context, registration identitystore.PendingRegistration, workspaceID uuid.UUID) (result Result, err error) {
	if p != nil && p.writer != nil {
		defer p.finishFailure(&err)
		if !registration.InAttempt(p.writer) {
			return Result{}, ErrInvalidInput
		}
		scope := workspacestore.Scope{InstallationID: registration.InstallationID(), ApplicationID: registration.ApplicationID()}
		if err = p.prepare(ctx, scope, registration.PersonID()); err != nil {
			return Result{}, err
		}
		if err = p.checkWorkspace(ctx, workspaceID, true); err != nil {
			return Result{}, err
		}
	}
	if p == nil || p.tx == nil || ctx == nil || (p.writer == nil && !registration.InTransaction(p.tx)) || workspaceID == uuid.Nil || workspaceID.Version() != 7 || workspaceID.Variant() != uuid.RFC4122 {
		return Result{}, ErrInvalidInput
	}
	personID := registration.PersonID()
	scope := workspacestore.Scope{InstallationID: registration.InstallationID(), ApplicationID: registration.ApplicationID()}
	var state string
	err = p.tx.QueryRowContext(ctx, `SELECT state FROM identity_persons WHERE id=$1 AND installation_id=$2 AND application_id=$3 FOR UPDATE`, personID, scope.InstallationID, scope.ApplicationID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && state != identitystore.AccountPendingVerification) {
		return Result{}, ErrPersonUnavailable
	}
	if err != nil {
		return Result{}, ErrPersistence
	}
	if p.writer != nil {
		if err = p.writer.RecordMutation(aw.WorkspaceWrite, []aw.Row{{Table: aw.Workspaces, ID: workspaceID, Access: aw.ReservedInsert}}); err != nil {
			return Result{}, ErrPersistence
		}
	}
	var w workspacestore.Workspace
	err = p.tx.QueryRowContext(ctx, `INSERT INTO workspaces (id,installation_id,application_id,kind,state,personal_owner_id)
 VALUES ($1,$2,$3,'personal','active',$4)
 ON CONFLICT (installation_id,application_id,personal_owner_id) WHERE kind='personal' DO NOTHING
 RETURNING id,installation_id,application_id,kind,state,personal_owner_id,workspace_epoch,created_at,updated_at`, workspaceID, scope.InstallationID, scope.ApplicationID, personID).Scan(&w.ID, &w.Scope.InstallationID, &w.Scope.ApplicationID, &w.Kind, &w.State, &w.PersonalOwnerID, &w.Epoch, &w.CreatedAt, &w.UpdatedAt)
	existing := errors.Is(err, sql.ErrNoRows)
	if existing {
		err = p.tx.QueryRowContext(ctx, `SELECT id,installation_id,application_id,kind,state,personal_owner_id,workspace_epoch,created_at,updated_at FROM workspaces WHERE installation_id=$1 AND application_id=$2 AND personal_owner_id=$3 AND kind='personal'`, scope.InstallationID, scope.ApplicationID, personID).Scan(&w.ID, &w.Scope.InstallationID, &w.Scope.ApplicationID, &w.Kind, &w.State, &w.PersonalOwnerID, &w.Epoch, &w.CreatedAt, &w.UpdatedAt)
	}
	if err != nil {
		return Result{}, ErrPersistence
	}
	if p.writer != nil && existing {
		if err = p.checkWorkspace(ctx, w.ID, false); err != nil {
			return Result{}, err
		}
	}
	if w.State != workspacestore.WorkspaceActive {
		return Result{}, ErrWorkspaceRepairRequired
	}
	return Result{Workspace: w, Existing: existing}, nil
}
