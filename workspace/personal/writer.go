package personal

import (
	"context"
	"errors"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

// NewWriter binds bootstrap to the native attempt. For active bootstrap the
// caller must supply the same attempt's privately refreshed session principal;
// accepting a public Principal alone is not current-session authentication.
func NewWriter(a *aw.Attempt) (*Participant, error) {
	if _, err := a.Binding(); err != nil {
		return nil, ErrPersistence
	}
	return &Participant{writer: a}, nil
}
func (p *Participant) planned(table aw.Table, id uuid.UUID, reserved bool) (aw.Row, error) {
	rows, err := p.writer.PlannedRows(table)
	if err != nil {
		return aw.Row{}, ErrPersistence
	}
	for _, r := range rows {
		if r.ID == id && (reserved && r.Access == aw.ReservedInsert || !reserved && r.Access == aw.ExistingUpdate) {
			return r, nil
		}
	}
	return aw.Row{}, ErrPersistence
}
func (p *Participant) prepare(ctx context.Context, scope workspacestore.Scope, person uuid.UUID) error {
	realm, err := p.writer.Realm()
	if err != nil || realm.Installation != scope.InstallationID || realm.Application != scope.ApplicationID {
		return ErrPersistence
	}
	rows, err := p.writer.PlannedRows(aw.Persons)
	if err != nil {
		return ErrPersistence
	}
	for _, r := range rows {
		if r.ID == person && (r.Access == aw.ExistingUpdate || r.Access == aw.ReservedInsert) {
			tx, e := p.writer.ParticipantTx(ctx, aw.W, []aw.Row{r})
			if e != nil {
				return ErrPersistence
			}
			p.tx = tx
			return nil
		}
	}
	return ErrPersistence
}
func (p *Participant) checkWorkspace(ctx context.Context, id uuid.UUID, reserved bool) error {
	r, err := p.planned(aw.Workspaces, id, reserved)
	if err != nil {
		return err
	}
	_, err = p.writer.ParticipantTx(ctx, aw.W, []aw.Row{r})
	if err != nil {
		return ErrPersistence
	}
	return nil
}
func (p *Participant) finishFailure(err *error) {
	if *err == nil || p.finished {
		return
	}
	outcome := aw.UnavailableRollback
	if errors.Is(*err, ErrPersonUnavailable) || errors.Is(*err, ErrWorkspaceRepairRequired) || errors.Is(*err, ErrOwnerMismatch) {
		outcome = aw.DeniedRollback
	}
	p.finished = true
	if e := p.writer.Finish(outcome); e != nil {
		*err = ErrPersistence
	}
}
