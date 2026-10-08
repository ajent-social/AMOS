package repository

import (
	"context"

	"github.com/ajent-social/amos/examples/continuity/domain"
)

// IDs are buffered and rows closed before detail validation uses the same Tx.
func (r *Repository) ids(ctx context.Context, table, after, extra string, limit int, values ...any) ([]string, error) {
	args := r.args(after, limit)
	args = append(args, values...)
	q := `SELECT id FROM public.` + table + ` WHERE ` + scoped + ` AND ($5='' OR id>NULLIF($5,'')::uuid)` + extra + ` ORDER BY id ASC LIMIT $6`
	rows, err := r.tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, ErrUnavailable
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	rowErr := rows.Err()
	closeErr := rows.Close()
	if err != nil || rowErr != nil || closeErr != nil {
		return nil, ErrUnavailable
	}
	return ids, nil
}
func listValues[T any](ctx context.Context, ids []string, get func(context.Context, string) (T, error)) ([]T, error) {
	out := make([]T, 0, len(ids))
	for _, id := range ids {
		v, err := get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (r *Repository) Properties(ctx context.Context, after, query string, limit int) ([]Property, error) {
	q, ok := pageInput(after, query, limit)
	if r.ready(ctx) != nil || !ok {
		return nil, ErrInvalid
	}
	ids, err := r.ids(ctx, "continuity_properties", after, ` AND ($7='' OR name ILIKE $8 ESCAPE '\' OR area ILIKE $8 ESCAPE '\' OR owner_label ILIKE $8 ESCAPE '\' OR occupant_label ILIKE $8 ESCAPE '\')`, limit, q, searchPattern(q))
	if err != nil {
		return nil, err
	}
	return listValues(ctx, ids, r.Property)
}
func (r *Repository) Sources(ctx context.Context, after, query, kind string, limit int) ([]SourceSummary, error) {
	q, ok := pageInput(after, query, limit)
	if r.ready(ctx) != nil || !ok || (kind != "" && kind != "correspondence" && kind != "document") {
		return nil, ErrInvalid
	}
	ids, err := r.ids(ctx, "continuity_sources", after, ` AND ($7='' OR title ILIKE $8 ESCAPE '\' OR body ILIKE $8 ESCAPE '\') AND ($9='' OR kind=$9)`, limit, q, searchPattern(q), kind)
	if err != nil {
		return nil, err
	}
	return listValues(ctx, ids, func(ctx context.Context, id string) (SourceSummary, error) {
		s, err := r.Source(ctx, id)
		if err != nil {
			return SourceSummary{}, err
		}
		return SourceSummary{ID: s.ID, Kind: s.Kind, Title: s.Title, SHA256: s.SHA256}, nil
	})
}
func (r *Repository) Cases(ctx context.Context, after string, limit int) ([]domain.Case, error) {
	if _, ok := pageInput(after, "", limit); r.ready(ctx) != nil || !ok {
		return nil, ErrInvalid
	}
	ids, err := r.ids(ctx, "continuity_cases", after, "", limit)
	if err != nil {
		return nil, err
	}
	return listValues(ctx, ids, r.Case)
}
func (r *Repository) Applications(ctx context.Context, after string, limit int) ([]domain.Application, error) {
	if _, ok := pageInput(after, "", limit); r.ready(ctx) != nil || !ok {
		return nil, ErrInvalid
	}
	ids, err := r.ids(ctx, "continuity_applications", after, "", limit)
	if err != nil {
		return nil, err
	}
	return listValues(ctx, ids, r.Application)
}
func (r *Repository) Procedures(ctx context.Context, after string, limit int) ([]domain.Procedure, error) {
	if _, ok := pageInput(after, "", limit); r.ready(ctx) != nil || !ok {
		return nil, ErrInvalid
	}
	ids, err := r.ids(ctx, "continuity_procedures", after, "", limit)
	if err != nil {
		return nil, err
	}
	return listValues(ctx, ids, r.Procedure)
}
func (r *Repository) Activity(ctx context.Context, after string, limit int) ([]Activity, error) {
	if _, ok := pageInput(after, "", limit); r.ready(ctx) != nil || !ok {
		return nil, ErrInvalid
	}
	rows, err := r.tx.QueryContext(ctx, `SELECT id,actor_id,action,resource_id,revision,created_at FROM public.continuity_activity WHERE `+scoped+` AND ($5='' OR id>NULLIF($5,'')::uuid) ORDER BY id ASC LIMIT $6`, r.args(after, limit)...)
	if err != nil {
		return nil, ErrUnavailable
	}
	out := make([]Activity, 0)
	for rows.Next() {
		var a Activity
		err = rows.Scan(&a.ID, &a.ActorID, &a.Action, &a.ResourceID, &a.Revision, &a.At)
		if err != nil {
			break
		}
		if !validID(a.ID) || !validID(a.ActorID) || !validID(a.ResourceID) || !validAction(a.Action) || a.Revision <= 0 || a.At.IsZero() {
			err = ErrUnavailable
			break
		}
		out = append(out, a)
	}
	rowErr := rows.Err()
	closeErr := rows.Close()
	if err != nil || rowErr != nil || closeErr != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}
