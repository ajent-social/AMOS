// Package store provides transaction-scoped PostgreSQL persistence for
// personal and organization workspaces. Authorization belongs to callers;
// selectors and IDs passed here are never treated as proof of authority.
// Account disablement may retain an active membership row for audit and
// recovery, but reads used for business access reject disabled people and
// callers must always check current person state at the authorization boundary.
// Mutations acquire identity-person rows before workspace rows. Callers that
// compose several resources in one transaction must use stable ascending ID
// order; external SQL with a different order can deadlock and PostgreSQL will
// abort one whole transaction. The person-disable owner trigger also locks
// affected workspaces in UUID order.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrInvalidInput            = errors.New("invalid workspace store input")
	ErrWorkspaceUnavailable    = errors.New("workspace unavailable")
	ErrWorkspaceExists         = errors.New("workspace already exists")
	ErrPersonUnavailable       = errors.New("person unavailable")
	ErrPersonalWorkspaceExists = errors.New("personal workspace already exists")
	ErrMembershipExists        = errors.New("workspace membership already exists")
	ErrMembershipUnavailable   = errors.New("workspace membership unavailable")
	ErrPersistence             = errors.New("workspace persistence failed")
)

const (
	KindPersonal             = "personal"
	KindOrganization         = "organization"
	WorkspaceActive          = "active"
	WorkspaceSuspended       = "suspended"
	WorkspaceDeletionPending = "deletion_pending"
	MembershipActive         = "active"
	MembershipSuspended      = "suspended"
	MembershipLeft           = "left"
	RoleOwner                = "owner"
	RoleAdmin                = "admin"
	RoleMember               = "member"
)

type Store struct{ tx *sql.Tx }

func New(tx *sql.Tx) (*Store, error) {
	if tx == nil {
		return nil, ErrInvalidInput
	}
	return &Store{tx: tx}, nil
}

type Scope struct{ InstallationID, ApplicationID uuid.UUID }
type Workspace struct {
	ID                   uuid.UUID
	Scope                Scope
	Kind, State          string
	PersonalOwnerID      uuid.UUID
	Epoch                int64
	CreatedAt, UpdatedAt time.Time
}
type Membership struct {
	ID                    uuid.UUID
	Scope                 Scope
	WorkspaceID, PersonID uuid.UUID
	Role                  string
	RoleVersion           int
	State                 string
	Epoch                 int64
	CreatedAt, UpdatedAt  time.Time
}
type CreatePersonalInput struct {
	ID            uuid.UUID
	Scope         Scope
	OwnerPersonID uuid.UUID
}
type CreateOrganizationInput struct {
	ID                uuid.UUID
	OwnerMembershipID uuid.UUID
	Scope             Scope
	OwnerPersonID     uuid.UUID
}
type AddMembershipInput struct {
	ID                    uuid.UUID
	Scope                 Scope
	WorkspaceID, PersonID uuid.UUID
	Role                  string
	RoleVersion           int
}
type UpdateMembershipInput struct {
	Scope                 Scope
	WorkspaceID, PersonID uuid.UUID
	Role, State           string
	RoleVersion           int
}

// CreatePersonalWorkspace writes the unique personal resource for an active
// person. The owner FK and partial unique index enforce tenant-local ownership.
func (s *Store) CreatePersonalWorkspace(ctx context.Context, in CreatePersonalInput) (Workspace, error) {
	if !s.valid(ctx) || !validID(in.ID) || !validScope(in.Scope) || !validID(in.OwnerPersonID) {
		return Workspace{}, ErrInvalidInput
	}
	if err := s.lockActivePerson(ctx, in.Scope, in.OwnerPersonID); err != nil {
		return Workspace{}, err
	}
	var w Workspace
	err := s.tx.QueryRowContext(ctx, `INSERT INTO workspaces (id,installation_id,application_id,kind,state,personal_owner_id)
		SELECT $1,p.installation_id,p.application_id,'personal','active',p.id FROM identity_persons p
		WHERE p.id=$2 AND p.installation_id=$3 AND p.application_id=$4 AND p.state='active'
		RETURNING id,installation_id,application_id,kind,state,personal_owner_id,workspace_epoch,created_at,updated_at`,
		in.ID, in.OwnerPersonID, in.Scope.InstallationID, in.Scope.ApplicationID).Scan(&w.ID, &w.Scope.InstallationID, &w.Scope.ApplicationID, &w.Kind, &w.State, &w.PersonalOwnerID, &w.Epoch, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return Workspace{}, s.mapCreateError(err)
	}
	return w, nil
}

// CreateOrganizationWorkspace and its active owner membership are one SQL
// transaction. The deferred database constraint rejects a committed ownerless
// active organization even if a future caller bypasses this method.
func (s *Store) CreateOrganizationWorkspace(ctx context.Context, in CreateOrganizationInput) (Workspace, Membership, error) {
	if !s.valid(ctx) || !validID(in.ID) || !validID(in.OwnerMembershipID) || !validScope(in.Scope) || !validID(in.OwnerPersonID) {
		return Workspace{}, Membership{}, ErrInvalidInput
	}
	if err := s.lockActivePerson(ctx, in.Scope, in.OwnerPersonID); err != nil {
		return Workspace{}, Membership{}, err
	}
	var w Workspace
	err := s.tx.QueryRowContext(ctx, `INSERT INTO workspaces (id,installation_id,application_id,kind,state)
		SELECT $1,p.installation_id,p.application_id,'organization','active' FROM identity_persons p
		WHERE p.id=$2 AND p.installation_id=$3 AND p.application_id=$4 AND p.state='active'
		RETURNING id,installation_id,application_id,kind,state,workspace_epoch,created_at,updated_at`,
		in.ID, in.OwnerPersonID, in.Scope.InstallationID, in.Scope.ApplicationID).Scan(&w.ID, &w.Scope.InstallationID, &w.Scope.ApplicationID, &w.Kind, &w.State, &w.Epoch, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return Workspace{}, Membership{}, s.mapCreateError(err)
	}
	m := Membership{ID: in.OwnerMembershipID, Scope: in.Scope, WorkspaceID: in.ID, PersonID: in.OwnerPersonID, Role: RoleOwner, RoleVersion: 1, State: MembershipActive}
	if err = s.insertMembership(ctx, m); err != nil {
		return Workspace{}, Membership{}, err
	}
	m, err = s.getMembership(ctx, in.Scope, in.ID, in.OwnerPersonID)
	if err != nil {
		return Workspace{}, Membership{}, err
	}
	return w, m, nil
}

// AddMembership adds a current human membership to an active organization.
// Callers must perform current actor/permission checks before this persistence
// operation; membership selection alone grants nothing.
func (s *Store) AddMembership(ctx context.Context, in AddMembershipInput) (Membership, error) {
	if !s.valid(ctx) || !validID(in.ID) || !validScope(in.Scope) || !validID(in.WorkspaceID) || !validID(in.PersonID) || !validRole(in.Role) || in.RoleVersion < 1 {
		return Membership{}, ErrInvalidInput
	}
	if err := s.lockActivePerson(ctx, in.Scope, in.PersonID); err != nil {
		return Membership{}, err
	}
	if err := s.lockActiveOrganization(ctx, in.Scope, in.WorkspaceID); err != nil {
		return Membership{}, err
	}
	m := Membership{ID: in.ID, Scope: in.Scope, WorkspaceID: in.WorkspaceID, PersonID: in.PersonID, Role: in.Role, RoleVersion: in.RoleVersion, State: MembershipActive}
	if err := s.insertMembership(ctx, m); err != nil {
		return Membership{}, err
	}
	return s.getMembership(ctx, in.Scope, in.WorkspaceID, in.PersonID)
}

func (s *Store) insertMembership(ctx context.Context, m Membership) error {
	result, err := s.tx.ExecContext(ctx, `INSERT INTO workspace_memberships (id,installation_id,application_id,workspace_id,person_id,role_key,role_version,state)
		SELECT $1,w.installation_id,w.application_id,w.id,p.id,$6,$7,'active'
		FROM workspaces w JOIN identity_persons p ON p.id=$5 AND p.installation_id=w.installation_id AND p.application_id=w.application_id
		WHERE w.id=$4 AND w.installation_id=$2 AND w.application_id=$3 AND w.kind='organization' AND w.state='active' AND p.state='active'`,
		m.ID, m.Scope.InstallationID, m.Scope.ApplicationID, m.WorkspaceID, m.PersonID, m.Role, m.RoleVersion)
	if err != nil {
		return mapConflict(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return ErrPersistence
	}
	if n != 1 {
		return ErrWorkspaceUnavailable
	}
	return nil
}

// UpdateMembership serializes all membership transitions on the workspace
// row. Deferred owner constraints preserve at least one active org owner.
func (s *Store) UpdateMembership(ctx context.Context, in UpdateMembershipInput) (Membership, error) {
	if !s.valid(ctx) || !validScope(in.Scope) || !validID(in.WorkspaceID) || !validID(in.PersonID) || !validRole(in.Role) || in.RoleVersion < 1 || !validMembershipState(in.State) {
		return Membership{}, ErrInvalidInput
	}
	if err := s.lockPerson(ctx, in.Scope, in.PersonID); err != nil {
		return Membership{}, err
	}
	if err := s.lockWorkspace(ctx, in.Scope, in.WorkspaceID); err != nil {
		return Membership{}, err
	}
	var id uuid.UUID
	err := s.tx.QueryRowContext(ctx, `UPDATE workspace_memberships SET role_key=$5,role_version=$6,state=$7,membership_epoch=membership_epoch+1,updated_at=transaction_timestamp()
		WHERE installation_id=$1 AND application_id=$2 AND workspace_id=$3 AND person_id=$4
		RETURNING id`, in.Scope.InstallationID, in.Scope.ApplicationID, in.WorkspaceID, in.PersonID, in.Role, in.RoleVersion, in.State).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, ErrMembershipUnavailable
	}
	if err != nil {
		return Membership{}, mapConflict(err)
	}
	return s.getMembership(ctx, in.Scope, in.WorkspaceID, in.PersonID)
}

func (s *Store) SetWorkspaceState(ctx context.Context, scope Scope, id uuid.UUID, state string) (Workspace, error) {
	if !s.valid(ctx) || !validScope(scope) || !validID(id) || !validWorkspaceState(state) {
		return Workspace{}, ErrInvalidInput
	}
	if err := s.lockWorkspace(ctx, scope, id); err != nil {
		return Workspace{}, err
	}
	var w Workspace
	err := s.tx.QueryRowContext(ctx, `UPDATE workspaces SET state=$4,workspace_epoch=workspace_epoch+1,updated_at=transaction_timestamp()
		WHERE installation_id=$1 AND application_id=$2 AND id=$3
		RETURNING id,installation_id,application_id,kind,state,personal_owner_id,workspace_epoch,created_at,updated_at`, scope.InstallationID, scope.ApplicationID, id, state).Scan(&w.ID, &w.Scope.InstallationID, &w.Scope.ApplicationID, &w.Kind, &w.State, &w.PersonalOwnerID, &w.Epoch, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrWorkspaceUnavailable
	}
	if err != nil {
		return Workspace{}, ErrPersistence
	}
	return w, nil
}

// FindWorkspace always requires the installation/application scope alongside
// the resource ID, so a valid identifier from another tenant is not returned.
func (s *Store) FindWorkspace(ctx context.Context, scope Scope, id uuid.UUID) (Workspace, error) {
	if !s.valid(ctx) || !validScope(scope) || !validID(id) {
		return Workspace{}, ErrInvalidInput
	}
	var w Workspace
	err := s.tx.QueryRowContext(ctx, `SELECT id,installation_id,application_id,kind,state,personal_owner_id,workspace_epoch,created_at,updated_at
		FROM workspaces WHERE id=$1 AND installation_id=$2 AND application_id=$3`, id, scope.InstallationID, scope.ApplicationID).Scan(&w.ID, &w.Scope.InstallationID, &w.Scope.ApplicationID, &w.Kind, &w.State, &w.PersonalOwnerID, &w.Epoch, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrWorkspaceUnavailable
	}
	if err != nil {
		return Workspace{}, ErrPersistence
	}
	return w, nil
}

// FindPersonalWorkspace selects the active personal resource of a currently
// active person inside an explicit application realm. IDs are selectors only;
// callers must obtain ownerID from the authenticated person principal.
func (s *Store) FindPersonalWorkspace(ctx context.Context, scope Scope, ownerID uuid.UUID) (Workspace, error) {
	if !s.valid(ctx) || !validScope(scope) || !validID(ownerID) {
		return Workspace{}, ErrInvalidInput
	}
	var w Workspace
	err := s.tx.QueryRowContext(ctx, `SELECT w.id,w.installation_id,w.application_id,w.kind,w.state,w.personal_owner_id,w.workspace_epoch,w.created_at,w.updated_at
 FROM workspaces w JOIN identity_persons p ON p.id=w.personal_owner_id AND p.installation_id=w.installation_id AND p.application_id=w.application_id
 WHERE w.installation_id=$1 AND w.application_id=$2 AND w.personal_owner_id=$3 AND w.kind='personal' AND w.state='active' AND p.state='active'`, scope.InstallationID, scope.ApplicationID, ownerID).Scan(&w.ID, &w.Scope.InstallationID, &w.Scope.ApplicationID, &w.Kind, &w.State, &w.PersonalOwnerID, &w.Epoch, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrWorkspaceUnavailable
	}
	if err != nil {
		return Workspace{}, ErrPersistence
	}
	return w, nil
}

func (s *Store) FindMembership(ctx context.Context, scope Scope, workspaceID, personID uuid.UUID) (Membership, error) {
	if !s.valid(ctx) || !validScope(scope) || !validID(workspaceID) || !validID(personID) {
		return Membership{}, ErrInvalidInput
	}
	var m Membership
	err := s.tx.QueryRowContext(ctx, `SELECT m.id,m.installation_id,m.application_id,m.workspace_id,m.person_id,m.role_key,m.role_version,m.state,m.membership_epoch,m.created_at,m.updated_at
		FROM workspace_memberships m JOIN identity_persons p ON p.id=m.person_id AND p.installation_id=m.installation_id AND p.application_id=m.application_id
		WHERE m.installation_id=$1 AND m.application_id=$2 AND m.workspace_id=$3 AND m.person_id=$4 AND p.state='active'`, scope.InstallationID, scope.ApplicationID, workspaceID, personID).Scan(&m.ID, &m.Scope.InstallationID, &m.Scope.ApplicationID, &m.WorkspaceID, &m.PersonID, &m.Role, &m.RoleVersion, &m.State, &m.Epoch, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, ErrMembershipUnavailable
	}
	if err != nil {
		return Membership{}, ErrPersistence
	}
	return m, nil
}

func (s *Store) ReadWorkspaceEpoch(ctx context.Context, scope Scope, id uuid.UUID) (int64, error) {
	if !s.valid(ctx) || !validScope(scope) || !validID(id) {
		return 0, ErrInvalidInput
	}
	var epoch int64
	err := s.tx.QueryRowContext(ctx, `SELECT workspace_epoch FROM workspaces WHERE id=$1 AND installation_id=$2 AND application_id=$3`, id, scope.InstallationID, scope.ApplicationID).Scan(&epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrWorkspaceUnavailable
	}
	if err != nil {
		return 0, ErrPersistence
	}
	return epoch, nil
}

func (s *Store) ReadMembershipEpoch(ctx context.Context, scope Scope, workspaceID, personID uuid.UUID) (int64, error) {
	if !s.valid(ctx) || !validScope(scope) || !validID(workspaceID) || !validID(personID) {
		return 0, ErrInvalidInput
	}
	var epoch int64
	err := s.tx.QueryRowContext(ctx, `SELECT m.membership_epoch FROM workspace_memberships m JOIN identity_persons p ON p.id=m.person_id AND p.installation_id=m.installation_id AND p.application_id=m.application_id WHERE m.installation_id=$1 AND m.application_id=$2 AND m.workspace_id=$3 AND m.person_id=$4 AND m.state='active' AND p.state='active'`, scope.InstallationID, scope.ApplicationID, workspaceID, personID).Scan(&epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrMembershipUnavailable
	}
	if err != nil {
		return 0, ErrPersistence
	}
	return epoch, nil
}

func (s *Store) getMembership(ctx context.Context, scope Scope, workspaceID, personID uuid.UUID) (Membership, error) {
	var m Membership
	err := s.tx.QueryRowContext(ctx, `SELECT id,installation_id,application_id,workspace_id,person_id,role_key,role_version,state,membership_epoch,created_at,updated_at
		FROM workspace_memberships WHERE installation_id=$1 AND application_id=$2 AND workspace_id=$3 AND person_id=$4`, scope.InstallationID, scope.ApplicationID, workspaceID, personID).Scan(&m.ID, &m.Scope.InstallationID, &m.Scope.ApplicationID, &m.WorkspaceID, &m.PersonID, &m.Role, &m.RoleVersion, &m.State, &m.Epoch, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, ErrMembershipUnavailable
	}
	if err != nil {
		return Membership{}, ErrPersistence
	}
	return m, nil
}

func (s *Store) lockWorkspace(ctx context.Context, scope Scope, id uuid.UUID) error {
	var found uuid.UUID
	err := s.tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE id=$1 AND installation_id=$2 AND application_id=$3 FOR UPDATE`, id, scope.InstallationID, scope.ApplicationID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWorkspaceUnavailable
	}
	if err != nil {
		return ErrPersistence
	}
	return nil
}
func (s *Store) lockPerson(ctx context.Context, scope Scope, id uuid.UUID) error {
	var state string
	err := s.tx.QueryRowContext(ctx, `SELECT state FROM identity_persons WHERE id=$1 AND installation_id=$2 AND application_id=$3 FOR SHARE`, id, scope.InstallationID, scope.ApplicationID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPersonUnavailable
	}
	if err != nil {
		return ErrPersistence
	}
	return nil
}
func (s *Store) lockActivePerson(ctx context.Context, scope Scope, id uuid.UUID) error {
	var state string
	err := s.tx.QueryRowContext(ctx, `SELECT state FROM identity_persons WHERE id=$1 AND installation_id=$2 AND application_id=$3 FOR SHARE`, id, scope.InstallationID, scope.ApplicationID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPersonUnavailable
	}
	if err != nil {
		return ErrPersistence
	}
	if state != "active" {
		return ErrPersonUnavailable
	}
	return nil
}
func (s *Store) lockActiveOrganization(ctx context.Context, scope Scope, id uuid.UUID) error {
	var found uuid.UUID
	err := s.tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE id=$1 AND installation_id=$2 AND application_id=$3 AND kind='organization' AND state='active' FOR UPDATE`, id, scope.InstallationID, scope.ApplicationID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWorkspaceUnavailable
	}
	if err != nil {
		return ErrPersistence
	}
	return nil
}
func (s *Store) valid(ctx context.Context) bool { return s != nil && s.tx != nil && ctx != nil }
func (s *Store) mapCreateError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "workspaces_personal_owner_unique":
			return ErrPersonalWorkspaceExists
		case "workspaces_pkey":
			return ErrWorkspaceExists
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPersonUnavailable
	}
	return fmt.Errorf("create workspace: %w", err)
}
func mapConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "workspace_memberships_scope_person_key":
			return ErrMembershipExists
		case "workspace_memberships_pkey":
			return ErrMembershipExists
		case "workspace_memberships_role_fk":
			return ErrInvalidInput
		}
	}
	return fmt.Errorf("persist workspace membership: %w", err)
}
func validScope(s Scope) bool { return validID(s.InstallationID) && validID(s.ApplicationID) }
func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == fmt.Sprint(id)
}
func validRole(v string) bool { return v == RoleOwner || v == RoleAdmin || v == RoleMember }
func validWorkspaceState(v string) bool {
	return v == WorkspaceActive || v == WorkspaceSuspended || v == WorkspaceDeletionPending
}
func validMembershipState(v string) bool {
	return v == MembershipActive || v == MembershipSuspended || v == MembershipLeft
}
