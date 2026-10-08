package context

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/ajent-social/amos/identity"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

// NewForTransactions constructs a pool-free resolver for caller-owned transactions.
// The realm is copied. No connection, session, or credential is acquired here.
func NewForTransactions(cfg Config) (*Resolver, error) {
	if !validConfig(cfg) {
		return nil, ErrInvalidConfiguration
	}
	return &Resolver{cfg: cfg}, nil
}

// ResolveCurrentTx reads current workspace facts while retaining P SHARE, then
// W SHARE, then (for organizations) M SHARE until the caller ends tx. The caller
// must supply its explicit writable READ COMMITTED transaction and the principal
// refreshed by its exact private session service. This method does not establish
// session provenance or attest which database the supplied transaction uses.
//
// Absence is fenced only when every affected authority writer follows the complete
// W1 person UPDATE protocol. This method alone does not admit a host or resource.
// After downstream waits, callers must recheck the session and resolve again with
// the pinned returned workspace ID, evaluate fresh facts, and commit before output.
func (r *Resolver) ResolveCurrentTx(ctx context.Context, tx *sql.Tx, principal identity.Principal, workspaceID uuid.UUID) (Selection, error) {
	if r == nil || !validConfig(r.cfg) || ctx == nil || tx == nil || ctx.Err() != nil {
		return Selection{}, ErrUnavailable
	}
	if workspaceID != uuid.Nil && !validID(workspaceID) {
		return Selection{}, ErrInvalidSelector
	}
	if principal.Actor().Kind() != "person" || !validID(principal.Actor().PersonID()) || principal.SecurityEpoch() < 0 {
		return Selection{}, ErrUnavailable
	}
	if principal.InstallationID() != r.cfg.InstallationID || principal.ApplicationID() != r.cfg.ApplicationID || principal.EnvironmentID() != r.cfg.EnvironmentID {
		return Selection{}, ErrDenied
	}
	var isolation, readOnly string
	if err := tx.QueryRowContext(ctx, `SELECT pg_catalog.current_setting('transaction_isolation'), pg_catalog.current_setting('transaction_read_only')`).Scan(&isolation, &readOnly); err != nil || isolation != "read committed" || readOnly != "off" {
		return Selection{}, ErrUnavailable
	}
	selection, err := r.resolveCurrent(ctx, tx, principal, workspaceID)
	// Cancellation wins even if a driver delivered its last row concurrently.
	if ctx.Err() != nil {
		return Selection{}, ErrUnavailable
	}
	if err != nil {
		if errors.Is(err, ErrDenied) {
			return Selection{}, ErrDenied
		}
		return Selection{}, ErrUnavailable
	}
	return copySelection(selection), nil
}

func (r *Resolver) resolveCurrent(ctx context.Context, tx *sql.Tx, principal identity.Principal, workspaceID uuid.UUID) (Selection, error) {
	defaultPersonal := workspaceID == uuid.Nil
	personID := principal.Actor().PersonID()
	var state string
	var epoch int64
	err := tx.QueryRowContext(ctx, `SELECT state, security_epoch FROM public.identity_persons
 WHERE id=$1 AND installation_id=$2 AND application_id=$3 FOR SHARE`, personID, r.cfg.InstallationID, r.cfg.ApplicationID).Scan(&state, &epoch)
	if err != nil {
		return Selection{}, currentRowError(err)
	}
	if err = validateCurrentPerson(state, epoch, principal.SecurityEpoch()); err != nil {
		return Selection{}, err
	}
	if workspaceID == uuid.Nil {
		workspaceID, err = r.personalWorkspaceID(ctx, tx, personID)
		if err != nil {
			return Selection{}, err
		}
	}
	var w workspacestore.Workspace
	var owner uuid.NullUUID
	err = tx.QueryRowContext(ctx, `SELECT id, installation_id, application_id, kind, state,
 personal_owner_id, workspace_epoch, created_at, updated_at FROM public.workspaces
 WHERE id=$1 AND installation_id=$2 AND application_id=$3 FOR SHARE`, workspaceID, r.cfg.InstallationID, r.cfg.ApplicationID).Scan(
		&w.ID, &w.Scope.InstallationID, &w.Scope.ApplicationID, &w.Kind, &w.State, &owner, &w.Epoch, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return Selection{}, currentRowError(err)
	}
	w.PersonalOwnerID = owner.UUID
	if err = validateCurrentWorkspace(w, owner.Valid, r.cfg, workspaceID, personID); err != nil {
		return Selection{}, err
	}
	if defaultPersonal && w.Kind != workspacestore.KindPersonal {
		return Selection{}, ErrUnavailable
	}
	selection := Selection{Workspace: w}
	if w.Kind == workspacestore.KindPersonal {
		return selection, nil
	}
	m, permissions, err := r.currentMembership(ctx, tx, workspaceID, personID)
	if err != nil {
		return Selection{}, err
	}
	selection.Membership, selection.MembershipEpoch, selection.Permissions = &m, m.Epoch, permissions
	return selection, nil
}

// Discovery uses the same scoped personal_owner_id relation as store's
// FindPersonalWorkspace. P is already held; W is locked separately and all
// authority fields are revalidated after that lock (never a join lock upgrade).
func (r *Resolver) personalWorkspaceID(ctx context.Context, tx *sql.Tx, personID uuid.UUID) (uuid.UUID, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM public.workspaces
 WHERE installation_id=$1 AND application_id=$2 AND personal_owner_id=$3 AND kind='personal' LIMIT 2`, r.cfg.InstallationID, r.cfg.ApplicationID, personID)
	if err != nil {
		return uuid.Nil, ErrUnavailable
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if rows.Err() != nil {
			return uuid.Nil, ErrUnavailable
		}
		return uuid.Nil, ErrDenied
	}
	var id uuid.UUID
	if err = rows.Scan(&id); err != nil || !validID(id) {
		return uuid.Nil, ErrUnavailable
	}
	if rows.Next() || rows.Err() != nil {
		return uuid.Nil, ErrUnavailable
	}
	if err = rows.Close(); err != nil {
		return uuid.Nil, ErrUnavailable
	}
	return id, nil
}

func (r *Resolver) currentMembership(ctx context.Context, tx *sql.Tx, workspaceID, personID uuid.UUID) (workspacestore.Membership, []string, error) {
	var m workspacestore.Membership
	err := tx.QueryRowContext(ctx, `SELECT id, installation_id, application_id, workspace_id, person_id,
 role_key, role_version, state, membership_epoch, created_at, updated_at FROM public.workspace_memberships
 WHERE installation_id=$1 AND application_id=$2 AND workspace_id=$3 AND person_id=$4 FOR SHARE`, r.cfg.InstallationID, r.cfg.ApplicationID, workspaceID, personID).Scan(
		&m.ID, &m.Scope.InstallationID, &m.Scope.ApplicationID, &m.WorkspaceID, &m.PersonID, &m.Role, &m.RoleVersion, &m.State, &m.Epoch, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return workspacestore.Membership{}, nil, currentRowError(err)
	}
	if err = validateCurrentMembership(m, r.cfg, workspaceID, personID); err != nil {
		return workspacestore.Membership{}, nil, err
	}
	// No join: an absent membership is denied, but an absent immutable role is
	// malformed dependency state and must never become a successful empty grant.
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT pg_catalog.array_to_json(permissions) FROM public.workspace_role_versions WHERE role_key=$1 AND version=$2`, m.Role, m.RoleVersion).Scan(&raw)
	if err != nil {
		return workspacestore.Membership{}, nil, ErrUnavailable
	}
	permissions, err := decodeCurrentPermissions(raw)
	if err != nil {
		return workspacestore.Membership{}, nil, err
	}
	return m, permissions, nil
}

func validConfig(cfg Config) bool {
	return validID(cfg.InstallationID) && validID(cfg.ApplicationID) && validID(cfg.EnvironmentID)
}
func currentRowError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDenied
	}
	return ErrUnavailable
}
func validateCurrentPerson(state string, epoch, supplied int64) error {
	if epoch < 0 || supplied < 0 {
		return ErrUnavailable
	}
	switch state {
	case "active":
		if epoch != supplied {
			return ErrDenied
		}
		return nil
	case "pending_verification", "self_disabled", "administratively_disabled", "deletion_pending":
		return ErrDenied
	default:
		return ErrUnavailable
	}
}
func validateCurrentWorkspace(w workspacestore.Workspace, ownerPresent bool, cfg Config, id, personID uuid.UUID) error {
	if !validID(w.ID) || w.ID != id || w.Scope.InstallationID != cfg.InstallationID || w.Scope.ApplicationID != cfg.ApplicationID || w.Epoch < 0 {
		return ErrUnavailable
	}
	switch w.Kind {
	case workspacestore.KindPersonal:
		if !ownerPresent || !validID(w.PersonalOwnerID) {
			return ErrUnavailable
		}
	case workspacestore.KindOrganization:
		if ownerPresent || w.PersonalOwnerID != uuid.Nil {
			return ErrUnavailable
		}
	default:
		return ErrUnavailable
	}
	switch w.State {
	case workspacestore.WorkspaceActive:
	case workspacestore.WorkspaceSuspended, workspacestore.WorkspaceDeletionPending:
		return ErrDenied
	default:
		return ErrUnavailable
	}
	if w.Kind == workspacestore.KindPersonal && !personalOwner(w, personID) {
		return ErrDenied
	}
	return nil
}
func validateCurrentMembership(m workspacestore.Membership, cfg Config, workspaceID, personID uuid.UUID) error {
	if !validID(m.ID) || m.Scope.InstallationID != cfg.InstallationID || m.Scope.ApplicationID != cfg.ApplicationID || m.WorkspaceID != workspaceID || m.PersonID != personID || m.Epoch < 0 || m.RoleVersion < 1 {
		return ErrUnavailable
	}
	switch m.Role {
	case workspacestore.RoleOwner, workspacestore.RoleAdmin, workspacestore.RoleMember:
	default:
		return ErrUnavailable
	}
	switch m.State {
	case workspacestore.MembershipActive:
		return nil
	case workspacestore.MembershipLeft, workspacestore.MembershipSuspended:
		return ErrDenied
	default:
		return ErrUnavailable
	}
}
func decodeCurrentPermissions(raw []byte) ([]string, error) {
	var permissions []string
	if err := json.Unmarshal(raw, &permissions); err != nil || len(permissions) == 0 || len(permissions) > 7 {
		return nil, ErrUnavailable
	}
	seen := make(map[string]bool, len(permissions))
	for _, p := range permissions {
		switch p {
		case "workspace.members.read", "workspace.members.manage", "workspace.owners.manage", "workspace.authentication.manage", "workspace.delete", "billing.manage", "billing.read":
		default:
			return nil, ErrUnavailable
		}
		if seen[p] {
			return nil, ErrUnavailable
		}
		seen[p] = true
	}
	return permissions, nil
}
func personalOwner(w workspacestore.Workspace, personID uuid.UUID) bool {
	return w.PersonalOwnerID == personID
}
func copySelection(s Selection) Selection {
	s.Permissions = append([]string(nil), s.Permissions...)
	if s.Membership != nil {
		m := *s.Membership
		s.Membership = &m
	}
	return s
}
