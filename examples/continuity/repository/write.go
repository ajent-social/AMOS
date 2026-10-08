package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/google/uuid"
)

func changed(result sql.Result, err error) error {
	if err != nil {
		return ErrUnavailable
	}
	n, err := result.RowsAffected()
	if err != nil {
		return ErrUnavailable
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func (r *Repository) activity(ctx context.Context, actor, action, id string, revision int64) error {
	event, err := uuid.NewV7()
	if err != nil {
		return ErrUnavailable
	}
	_, err = r.tx.ExecContext(ctx, `INSERT INTO public.continuity_activity (installation_id,application_id,environment_id,workspace_id,id,actor_id,action,resource_id,revision) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, r.args(event, actor, action, id, revision)...)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
func (r *Repository) ChangeCase(ctx context.Context, id string, expected int64, next domain.CaseStatus, actorID string) (domain.Case, error) {
	if r.ready(ctx) != nil || !validID(id) || !validID(actorID) || expected <= 0 {
		return domain.Case{}, ErrInvalid
	}
	switch next {
	case domain.AwaitingOwner, domain.ReadyToArrange, domain.InProgress, domain.NeedsAssessment, domain.Completed:
	default:
		return domain.Case{}, ErrInvalid
	}
	current, err := r.caseRecord(ctx, id, true)
	if err != nil {
		return domain.Case{}, err
	}
	v, err := domain.ChangeCase(current, expected, next)
	if err != nil {
		return domain.Case{}, domainError(err)
	}
	err = changed(r.tx.ExecContext(ctx, `UPDATE public.continuity_cases SET status=$7,revision=$8,updated_at=transaction_timestamp() WHERE `+scoped+` AND id=$5 AND revision=$6`, r.args(id, current.Revision, v.Status, v.Revision)...))
	if err != nil {
		return domain.Case{}, err
	}
	if err = r.activity(ctx, actorID, "case.changed", id, v.Revision); err != nil {
		return domain.Case{}, err
	}
	return v, nil
}
func (r *Repository) SetChecklist(ctx context.Context, id string, expected int64, itemID string, done bool, actorID string) (domain.Application, error) {
	if r.ready(ctx) != nil || !validID(id) || !validID(itemID) || !validID(actorID) || expected <= 0 {
		return domain.Application{}, ErrInvalid
	}
	current, err := r.application(ctx, id, true)
	if err != nil {
		return domain.Application{}, err
	}
	v, err := domain.SetChecklist(current, expected, itemID, done)
	if err != nil {
		return domain.Application{}, domainError(err)
	}
	raw, err := json.Marshal(v.Items)
	if err != nil {
		return domain.Application{}, ErrUnavailable
	}
	err = changed(r.tx.ExecContext(ctx, `UPDATE public.continuity_applications SET items=$7,revision=$8,updated_at=transaction_timestamp() WHERE `+scoped+` AND id=$5 AND revision=$6`, r.args(id, current.Revision, raw, v.Revision)...))
	if err != nil {
		return domain.Application{}, err
	}
	if err = r.activity(ctx, actorID, "checklist.changed", id, v.Revision); err != nil {
		return domain.Application{}, err
	}
	return v, nil
}
func (r *Repository) EditProcedure(ctx context.Context, id string, expected int64, body, actorID string) (domain.Procedure, error) {
	if _, ok := textValue(body, 6000, true); r.ready(ctx) != nil || !ok || !validID(id) || !validID(actorID) || expected <= 0 {
		return domain.Procedure{}, ErrInvalid
	}
	current, err := r.procedure(ctx, id, true)
	if err != nil {
		return domain.Procedure{}, err
	}
	v, err := domain.EditProcedure(current, expected, body)
	if err != nil {
		return domain.Procedure{}, domainError(err)
	}
	err = changed(r.tx.ExecContext(ctx, `UPDATE public.continuity_procedures SET body=$7,revision=$8,updated_at=transaction_timestamp() WHERE `+scoped+` AND id=$5 AND revision=$6`, r.args(id, current.Revision, v.Body, v.Revision)...))
	if err != nil {
		return domain.Procedure{}, err
	}
	if err = r.activity(ctx, actorID, "procedure.changed", id, v.Revision); err != nil {
		return domain.Procedure{}, err
	}
	return v, nil
}
func (r *Repository) SaveDraft(ctx context.Context, caseID string, expectedCase int64, body, actorID string) (domain.Draft, error) {
	if _, ok := textValue(body, 4000, true); r.ready(ctx) != nil || !ok || !validID(caseID) || !validID(actorID) || expectedCase <= 0 {
		return domain.Draft{}, ErrInvalid
	}
	c, err := r.caseRecord(ctx, caseID, true)
	if err != nil {
		return domain.Draft{}, err
	}
	if c.Revision != expectedCase {
		return domain.Draft{}, ErrConflict
	}
	v, err := domain.PrepareDraft(c, body)
	if err != nil {
		return domain.Draft{}, domainError(err)
	}
	raw, err := json.Marshal(v.SourceIDs)
	if err != nil {
		return domain.Draft{}, ErrUnavailable
	}
	err = changed(r.tx.ExecContext(ctx, `INSERT INTO public.continuity_drafts (installation_id,application_id,environment_id,workspace_id,case_id,case_revision,body,source_ids) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (installation_id,application_id,environment_id,workspace_id,case_id) DO UPDATE SET case_revision=EXCLUDED.case_revision,body=EXCLUDED.body,source_ids=EXCLUDED.source_ids,updated_at=transaction_timestamp()`, r.args(caseID, v.CaseRevision, v.Body, raw)...))
	if err != nil {
		return domain.Draft{}, err
	}
	if err = r.activity(ctx, actorID, "draft.saved", caseID, v.CaseRevision); err != nil {
		return domain.Draft{}, err
	}
	return v, nil
}
