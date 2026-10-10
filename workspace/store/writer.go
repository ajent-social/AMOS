package store

import (
	"context"
	"errors"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

// NewWriter binds persistence to an already sealed native root. Every operation
// repeats lifetime, realm, acquisition and exact row checks before SQL.
func NewWriter(a *aw.Attempt) (*Store, error) {
	if _, err := a.Realm(); err != nil {
		return nil, ErrPersistence
	}
	if _, err := a.Binding(); err != nil {
		return nil, ErrPersistence
	}
	return &Store{writer: a}, nil
}

type writerRow struct {
	table   aw.Table
	id      uuid.UUID
	access  aw.Access
	mutable bool
}

func (s *Store) planned(row writerRow) (aw.Row, error) {
	rows, err := s.writer.PlannedRows(row.table)
	if err != nil {
		return aw.Row{}, ErrPersistence
	}
	for _, planned := range rows {
		if planned.ID == row.id && (row.access == 0 || row.access == planned.Access) && (!row.mutable || planned.Access != aw.ExistingShare) {
			return planned, nil
		}
	}
	return aw.Row{}, ErrPersistence
}
func (s *Store) prepareWriter(ctx context.Context, scope Scope, phase aw.Phase, want ...writerRow) (result error) {
	defer s.finishFailure(&result)
	if s == nil || s.writer == nil {
		return ErrPersistence
	}
	realm, err := s.writer.Realm()
	if err != nil || realm.Installation != scope.InstallationID || realm.Application != scope.ApplicationID {
		return ErrPersistence
	}
	rows := make([]aw.Row, 0, len(want))
	for _, row := range want {
		planned, e := s.planned(row)
		if e != nil {
			return e
		}
		rows = append(rows, planned)
	}
	tx, err := s.writer.ParticipantTx(ctx, phase, rows)
	if err != nil {
		return ErrPersistence
	}
	s.tx = tx
	return nil
}

// End a failed mutating participant even if a native caller ignores its error.
// Domain absence is a denial; invalid plan/input/dependency is unavailable.
func (s *Store) finishFailure(err *error) {
	if s == nil || s.writer == nil || s.writerFinished || *err == nil {
		return
	}
	out := aw.UnavailableRollback
	if errors.Is(*err, ErrPersonUnavailable) || errors.Is(*err, ErrWorkspaceUnavailable) || errors.Is(*err, ErrMembershipUnavailable) {
		out = aw.DeniedRollback
	}
	s.writerFinished = true
	if e := s.writer.Finish(out); e != nil {
		*err = ErrPersistence
	}
}

func (s *Store) CreatePersonalWorkspace(ctx context.Context, in CreatePersonalInput) (w Workspace, err error) {
	if s != nil && s.writer != nil {
		defer s.finishFailure(&err)
		if err = s.prepareWriter(ctx, in.Scope, aw.W, writerRow{mutable: true, table: aw.Persons, id: in.OwnerPersonID}, writerRow{mutable: true, table: aw.Workspaces, id: in.ID, access: aw.ReservedInsert}); err != nil {
			return Workspace{}, err
		}
		if err = s.writer.RecordMutation(aw.WorkspaceWrite, []aw.Row{{Table: aw.Workspaces, ID: in.ID, Access: aw.ReservedInsert}}); err != nil {
			return Workspace{}, ErrPersistence
		}
	}
	return s.createPersonalWorkspace(ctx, in)
}
func (s *Store) CreateOrganizationWorkspace(ctx context.Context, in CreateOrganizationInput) (w Workspace, m Membership, err error) {
	if s != nil && s.writer != nil {
		defer s.finishFailure(&err)
		if err = s.prepareWriter(ctx, in.Scope, aw.W, writerRow{mutable: true, table: aw.Persons, id: in.OwnerPersonID}, writerRow{mutable: true, table: aw.Workspaces, id: in.ID, access: aw.ReservedInsert}, writerRow{mutable: true, table: aw.Memberships, id: in.OwnerMembershipID, access: aw.ReservedInsert}); err != nil {
			return Workspace{}, Membership{}, err
		}
		if err = s.writer.RecordMutation(aw.WorkspaceWrite, []aw.Row{{Table: aw.Workspaces, ID: in.ID, Access: aw.ReservedInsert}}); err != nil {
			return Workspace{}, Membership{}, ErrPersistence
		}
	}
	return s.createOrganizationWorkspace(ctx, in)
}
func (s *Store) AddMembership(ctx context.Context, in AddMembershipInput) (m Membership, err error) {
	if s != nil && s.writer != nil {
		defer s.finishFailure(&err)
		if err = s.prepareWriter(ctx, in.Scope, aw.W, writerRow{mutable: true, table: aw.Persons, id: in.PersonID}, writerRow{mutable: true, table: aw.Workspaces, id: in.WorkspaceID}, writerRow{mutable: true, table: aw.Memberships, id: in.ID, access: aw.ReservedInsert}); err != nil {
			return Membership{}, err
		}
	}
	return s.addMembership(ctx, in)
}

func (s *Store) UpdateMembership(ctx context.Context, in UpdateMembershipInput) (m Membership, err error) {
	if s != nil && s.writer != nil {
		defer s.finishFailure(&err)
		if err = s.prepareWriter(ctx, in.Scope, aw.W, writerRow{mutable: true, table: aw.Persons, id: in.PersonID}, writerRow{mutable: true, table: aw.Workspaces, id: in.WorkspaceID, access: aw.ExistingUpdate}); err != nil {
			return Membership{}, err
		}
		// Plain scoped lookup only; the exact returned ID must already be held.
		m, err = s.getMembership(ctx, in.Scope, in.WorkspaceID, in.PersonID)
		if err != nil {
			return Membership{}, err
		}
		row, e := s.planned(writerRow{mutable: true, table: aw.Memberships, id: m.ID, access: aw.ExistingUpdate})
		if e != nil {
			return Membership{}, e
		}
		if _, e = s.writer.ParticipantTx(ctx, aw.W, []aw.Row{row}); e != nil {
			return Membership{}, ErrPersistence
		}
		if e = s.writer.RecordMutation(aw.WorkspaceWrite, []aw.Row{row}); e != nil {
			return Membership{}, ErrPersistence
		}
	}
	return s.updateMembership(ctx, in)
}

func (s *Store) SetWorkspaceState(ctx context.Context, scope Scope, id uuid.UUID, state string) (w Workspace, err error) {
	if s != nil && s.writer != nil {
		defer s.finishFailure(&err)
		if err = s.prepareWriter(ctx, scope, aw.W, writerRow{mutable: true, table: aw.Workspaces, id: id, access: aw.ExistingUpdate}); err != nil {
			return Workspace{}, err
		}
		if err = s.checkAffectedPersons(ctx, scope, id); err != nil {
			return Workspace{}, err
		}
		if err = s.writer.RecordMutation(aw.WorkspaceWrite, []aw.Row{{Table: aw.Workspaces, ID: id, Access: aw.ExistingUpdate}}); err != nil {
			return Workspace{}, ErrPersistence
		}
	}
	return s.setWorkspaceState(ctx, scope, id, state)
}
func (s *Store) checkAffectedPersons(ctx context.Context, scope Scope, id uuid.UUID) (result error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT personal_owner_id FROM public.workspaces WHERE id=$1 AND installation_id=$2 AND application_id=$3 AND kind='personal'
 UNION SELECT person_id FROM public.workspace_memberships WHERE workspace_id=$1 AND installation_id=$2 AND application_id=$3 AND state<>'left'`, id, scope.InstallationID, scope.ApplicationID)
	if err != nil {
		return ErrPersistence
	}
	defer func() {
		if err := rows.Close(); err != nil {
			result = ErrPersistence
		}
	}()
	for rows.Next() {
		var person uuid.UUID
		if err := rows.Scan(&person); err != nil {
			return ErrPersistence
		}
		planned, err := s.planned(writerRow{mutable: true, table: aw.Persons, id: person, access: aw.ExistingUpdate})
		if err != nil {
			return err
		}
		if err := s.writer.CheckRows(aw.P, []aw.Row{planned}); err != nil {
			return ErrPersistence
		}
	}
	if rows.Err() != nil {
		return ErrPersistence
	}
	return nil
}

func (s *Store) FindWorkspace(ctx context.Context, scope Scope, id uuid.UUID) (Workspace, error) {
	if s != nil && s.writer != nil {
		if err := s.prepareWriter(ctx, scope, aw.W, writerRow{table: aw.Workspaces, id: id}); err != nil {
			return Workspace{}, err
		}
	}
	return s.findWorkspace(ctx, scope, id)
}
func (s *Store) FindPersonalWorkspace(ctx context.Context, scope Scope, owner uuid.UUID) (Workspace, error) {
	if s != nil && s.writer != nil {
		if err := s.prepareWriter(ctx, scope, aw.W, writerRow{table: aw.Persons, id: owner}); err != nil {
			return Workspace{}, err
		}
	}
	w, err := s.findPersonalWorkspace(ctx, scope, owner)
	if err == nil && s.writer != nil {
		if err := s.prepareWriter(ctx, scope, aw.W, writerRow{table: aw.Workspaces, id: w.ID}); err != nil {
			return Workspace{}, err
		}
	}
	return w, err
}
func (s *Store) FindMembership(ctx context.Context, scope Scope, workspace, person uuid.UUID) (Membership, error) {
	if s != nil && s.writer != nil {
		if err := s.prepareWriter(ctx, scope, aw.W, writerRow{table: aw.Persons, id: person}, writerRow{table: aw.Workspaces, id: workspace}); err != nil {
			return Membership{}, err
		}
	}
	m, err := s.findMembership(ctx, scope, workspace, person)
	if err == nil && s.writer != nil {
		if err := s.prepareWriter(ctx, scope, aw.W, writerRow{table: aw.Memberships, id: m.ID}); err != nil {
			return Membership{}, err
		}
	}
	return m, err
}
func (s *Store) ReadWorkspaceEpoch(ctx context.Context, scope Scope, id uuid.UUID) (int64, error) {
	if s != nil && s.writer != nil {
		if err := s.prepareWriter(ctx, scope, aw.W, writerRow{table: aw.Workspaces, id: id}); err != nil {
			return 0, err
		}
	}
	return s.readWorkspaceEpoch(ctx, scope, id)
}
func (s *Store) ReadMembershipEpoch(ctx context.Context, scope Scope, workspace, person uuid.UUID) (int64, error) {
	if s != nil && s.writer != nil {
		m, err := s.FindMembership(ctx, scope, workspace, person)
		if err != nil {
			return 0, err
		}
		if m.State != MembershipActive {
			return 0, ErrMembershipUnavailable
		}
		return m.Epoch, nil
	}
	return s.readMembershipEpoch(ctx, scope, workspace, person)
}
