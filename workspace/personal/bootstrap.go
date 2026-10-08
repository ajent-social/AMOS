// Package personal creates and repairs the single personal workspace bound to
// a verified person. Callers must run it in the same transaction as signup or
// account verification.
package personal

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ajent-social/amos/identity"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

var (
	ErrInvalidInput            = errors.New("invalid personal workspace bootstrap input")
	ErrOwnerMismatch           = errors.New("personal workspace owner does not match verified person")
	ErrPersonUnavailable       = errors.New("verified person is unavailable")
	ErrWorkspaceRepairRequired = errors.New("personal workspace requires operator repair")
	ErrPersistence             = errors.New("personal workspace bootstrap persistence failed")
)

// Input contains a requested resource ID and owner. The owner and scope are
// checked against the authenticated principal before any write occurs.
type Input struct {
	WorkspaceID uuid.UUID
	Scope       workspacestore.Scope
	OwnerID     uuid.UUID
}

// Result reports whether this call created the workspace or found the
// existing owner-bound workspace. Existing is useful for idempotent signup
// retries and repair of an earlier incomplete bootstrap.
type Result struct {
	Workspace workspacestore.Workspace
	Existing  bool
}

type Participant struct {
	tx       *sql.Tx
	writer   *aw.Attempt
	finished bool
}

func New(tx *sql.Tx) (*Participant, error) {
	if tx == nil || aw.SelectLegacy() != nil {
		return nil, ErrInvalidInput
	}
	return &Participant{tx: tx}, nil
}

// Bootstrap creates exactly one personal workspace for principal. Its caller
// owns transaction commit/rollback, so the workspace and account transition
// either both commit or both roll back.
func (p *Participant) Bootstrap(ctx context.Context, principal identity.Principal, in Input) (result Result, err error) {
	if p != nil && p.writer != nil {
		defer p.finishFailure(&err)
		if err = p.prepare(ctx, in.Scope, in.OwnerID); err != nil {
			return Result{}, err
		}
	}
	if p == nil || p.tx == nil || ctx == nil || in.WorkspaceID == uuid.Nil ||
		in.WorkspaceID.Version() != 7 || in.WorkspaceID.Variant() != uuid.RFC4122 ||
		in.OwnerID == uuid.Nil || in.Scope.InstallationID == uuid.Nil || in.Scope.ApplicationID == uuid.Nil {
		return Result{}, ErrInvalidInput
	}
	actor := principal.Actor()
	personID := actor.PersonID()
	if actor.Kind() != "person" || personID == uuid.Nil ||
		personID != in.OwnerID || principal.InstallationID() != in.Scope.InstallationID ||
		principal.ApplicationID() != in.Scope.ApplicationID || principal.SecurityEpoch() < 0 {
		return Result{}, ErrOwnerMismatch
	}
	return p.bootstrapActivePerson(ctx, in, personID, principal.SecurityEpoch())
}

func (p *Participant) bootstrapActivePerson(ctx context.Context, in Input, personID uuid.UUID, securityEpoch int64) (Result, error) {
	var currentEpoch int64
	err := p.tx.QueryRowContext(ctx, `
		SELECT security_epoch FROM identity_persons
		WHERE id = $1 AND installation_id = $2 AND application_id = $3 AND state = 'active'
		FOR UPDATE`, personID, in.Scope.InstallationID, in.Scope.ApplicationID).Scan(&currentEpoch)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && currentEpoch != securityEpoch) {
		return Result{}, ErrPersonUnavailable
	}
	if err != nil {
		return Result{}, ErrPersistence
	}

	var existing workspacestore.Workspace
	err = p.tx.QueryRowContext(ctx, `
		SELECT id, installation_id, application_id, kind, state, personal_owner_id,
		       workspace_epoch, created_at, updated_at
		FROM workspaces
		WHERE installation_id = $1 AND application_id = $2
		  AND personal_owner_id = $3 AND kind = 'personal'`,
		in.Scope.InstallationID, in.Scope.ApplicationID, personID).Scan(
		&existing.ID, &existing.Scope.InstallationID, &existing.Scope.ApplicationID,
		&existing.Kind, &existing.State, &existing.PersonalOwnerID,
		&existing.Epoch, &existing.CreatedAt, &existing.UpdatedAt)
	if err == nil {
		if existing.State != workspacestore.WorkspaceActive || existing.PersonalOwnerID != personID {
			return Result{}, ErrWorkspaceRepairRequired
		}
		if p.writer != nil {
			if err := p.checkWorkspace(ctx, existing.ID, false); err != nil {
				return Result{}, err
			}
		}
		return Result{Workspace: existing, Existing: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrPersistence
	}
	var store *workspacestore.Store
	if p.writer != nil {
		store, err = workspacestore.NewWriter(p.writer)
	} else {
		store, err = workspacestore.New(p.tx)
	}
	if err != nil {
		return Result{}, ErrPersistence
	}
	created, err := store.CreatePersonalWorkspace(ctx, workspacestore.CreatePersonalInput{
		ID: in.WorkspaceID, Scope: in.Scope, OwnerPersonID: personID,
	})
	if err != nil {
		// The mutating store participant has already marked its terminal outcome.
		if p.writer != nil {
			p.finished = true
		}
		return Result{}, err
	}
	return Result{Workspace: created}, nil
}
