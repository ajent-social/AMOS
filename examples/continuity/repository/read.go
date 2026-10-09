package repository

import (
	"context"
	"database/sql"
	"sort"

	"github.com/ajent-social/amos/examples/continuity/domain"
)

func (r *Repository) Property(ctx context.Context, id string) (Property, error) {
	if r.ready(ctx) != nil || !validID(id) {
		return Property{}, ErrInvalid
	}
	var p Property
	var date sql.NullTime
	err := r.tx.QueryRowContext(ctx, `SELECT id,name,area,owner_label,occupant_label,occupancy,inspection_date FROM public.continuity_properties WHERE `+scoped+` AND id=$5`, r.args(id)...).Scan(&p.ID, &p.Name, &p.Area, &p.OwnerLabel, &p.OccupantLabel, &p.Occupancy, &date)
	if err != nil {
		return Property{}, readError(err)
	}
	if date.Valid {
		p.InspectionDate = date.Time.Format("2006-01-02")
	}
	p, ok := propertyValue(p)
	if !ok {
		return Property{}, ErrUnavailable
	}
	return p, nil
}
func (r *Repository) Source(ctx context.Context, id string) (Source, error) {
	return r.source(ctx, id, false)
}
func (r *Repository) source(ctx context.Context, id string, lock bool) (Source, error) {
	if r.ready(ctx) != nil || !validID(id) {
		return Source{}, ErrInvalid
	}
	var s Source
	q := `SELECT id,kind,title,body,sha256 FROM public.continuity_sources WHERE ` + scoped + ` AND id=$5`
	if lock {
		q += ` FOR SHARE`
	}
	err := r.tx.QueryRowContext(ctx, q, r.args(id)...).Scan(&s.ID, &s.Kind, &s.Title, &s.Body, &s.SHA256)
	if err != nil {
		return Source{}, readError(err)
	}
	s, ok := sourceValue(s)
	if !ok {
		return Source{}, ErrUnavailable
	}
	return s, nil
}
func (r *Repository) references(ctx context.Context, ids []string) error {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for _, id := range sorted {
		if _, err := r.source(ctx, id, true); err != nil {
			return err
		}
	}
	return nil
}
func (r *Repository) Case(ctx context.Context, id string) (domain.Case, error) {
	return r.caseRecord(ctx, id, false)
}
func (r *Repository) caseRow(ctx context.Context, id string, lock bool) (domain.Case, error) {
	if r.ready(ctx) != nil || !validID(id) {
		return domain.Case{}, ErrInvalid
	}
	var c domain.Case
	var raw []byte
	q := `SELECT id,property_id,title,status,revision,CASE WHEN octet_length(source_ids::text)<=2048 THEN source_ids ELSE NULL END FROM public.continuity_cases WHERE ` + scoped + ` AND id=$5`
	if lock {
		q += ` FOR UPDATE`
	}
	err := r.tx.QueryRowContext(ctx, q, r.args(id)...).Scan(&c.ID, &c.PropertyID, &c.Title, &c.Status, &c.Revision, &raw)
	if err != nil {
		return domain.Case{}, readError(err)
	}
	if !strictJSON(raw, &c.SourceIDs) {
		return domain.Case{}, ErrUnavailable
	}
	c, ok := caseValue(c)
	if !ok {
		return domain.Case{}, ErrUnavailable
	}
	return c, nil
}

// caseRecord preserves legacy relation checks and source SHARE locks. Discovery
// uses caseRow instead so it cannot acquire resources before global ordering.
func (r *Repository) caseRecord(ctx context.Context, id string, lock bool) (domain.Case, error) {
	c, err := r.caseRow(ctx, id, lock)
	if err != nil {
		return domain.Case{}, err
	}
	if _, err = r.Property(ctx, c.PropertyID); err != nil {
		return domain.Case{}, err
	}
	if err = r.references(ctx, c.SourceIDs); err != nil {
		return domain.Case{}, err
	}
	return c, nil
}
func (r *Repository) Application(ctx context.Context, id string) (domain.Application, error) {
	return r.application(ctx, id, false)
}
func (r *Repository) applicationRow(ctx context.Context, id string, lock bool) (domain.Application, error) {
	if r.ready(ctx) != nil || !validID(id) {
		return domain.Application{}, ErrInvalid
	}
	var a domain.Application
	var raw []byte
	q := `SELECT id,property_id,revision,CASE WHEN octet_length(items::text)<=65536 THEN items ELSE NULL END FROM public.continuity_applications WHERE ` + scoped + ` AND id=$5`
	if lock {
		q += ` FOR UPDATE`
	}
	err := r.tx.QueryRowContext(ctx, q, r.args(id)...).Scan(&a.ID, &a.PropertyID, &a.Revision, &raw)
	if err != nil {
		return domain.Application{}, readError(err)
	}
	if !checklistJSON(raw, &a.Items) {
		return domain.Application{}, ErrUnavailable
	}
	a, ok := appValue(a)
	if !ok {
		return domain.Application{}, ErrUnavailable
	}
	return a, nil
}

func (r *Repository) application(ctx context.Context, id string, lock bool) (domain.Application, error) {
	a, err := r.applicationRow(ctx, id, lock)
	if err != nil {
		return domain.Application{}, err
	}
	if _, err = r.Property(ctx, a.PropertyID); err != nil {
		return domain.Application{}, err
	}
	return a, nil
}
func (r *Repository) Procedure(ctx context.Context, id string) (domain.Procedure, error) {
	return r.procedure(ctx, id, false)
}
func (r *Repository) procedure(ctx context.Context, id string, lock bool) (domain.Procedure, error) {
	if r.ready(ctx) != nil || !validID(id) {
		return domain.Procedure{}, ErrInvalid
	}
	var p domain.Procedure
	q := `SELECT id,title,body,revision FROM public.continuity_procedures WHERE ` + scoped + ` AND id=$5`
	if lock {
		q += ` FOR UPDATE`
	}
	err := r.tx.QueryRowContext(ctx, q, r.args(id)...).Scan(&p.ID, &p.Title, &p.Body, &p.Revision)
	if err != nil {
		return domain.Procedure{}, readError(err)
	}
	p, ok := procedureValue(p)
	if !ok {
		return domain.Procedure{}, ErrUnavailable
	}
	return p, nil
}
func (r *Repository) Draft(ctx context.Context, caseID string) (domain.Draft, error) {
	if r.ready(ctx) != nil || !validID(caseID) {
		return domain.Draft{}, ErrInvalid
	}
	var d domain.Draft
	var raw []byte
	err := r.tx.QueryRowContext(ctx, `SELECT case_id,case_revision,body,CASE WHEN octet_length(source_ids::text)<=2048 THEN source_ids ELSE NULL END FROM public.continuity_drafts WHERE `+scoped+` AND case_id=$5`, r.args(caseID)...).Scan(&d.CaseID, &d.CaseRevision, &d.Body, &raw)
	if err != nil {
		return domain.Draft{}, readError(err)
	}
	if !strictJSON(raw, &d.SourceIDs) {
		return domain.Draft{}, ErrUnavailable
	}
	// A saved draft remains a historical snapshot after later case edits; validate
	// its own positive revision and references without rebinding it to newer state.
	// Once the draft row exists, a missing dependency is unavailable storage,
	// never an absent draft that a caller may safely omit from its presentation.
	c, err := r.Case(ctx, caseID)
	if err != nil {
		return domain.Draft{}, ErrUnavailable
	}
	if d.CaseRevision > c.Revision {
		return domain.Draft{}, ErrUnavailable
	}
	c.Revision = d.CaseRevision
	c.SourceIDs = d.SourceIDs
	valid, err := domain.PrepareDraft(c, d.Body)
	if err != nil {
		return domain.Draft{}, ErrUnavailable
	}
	if err = r.references(ctx, d.SourceIDs); err != nil {
		return domain.Draft{}, ErrUnavailable
	}
	return valid, nil
}
