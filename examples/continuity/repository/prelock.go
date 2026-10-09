package repository

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"

	"github.com/ajent-social/amos/examples/continuity/domain"
)

// LockedResource observes a row or the case-protected draft key in one successful
// PrelockMutation. It is not authority or a transferable transaction capability.
type LockedResource struct {
	Kind string
	ID   string
}

const maxMutationResources = 35

type mutationSnapshot struct {
	caseValue   domain.Case
	application domain.Application
	procedure   domain.Procedure
}

// PrelockMutation discovers and locks the complete resource closure for one of
// the four existing mutations. The caller must already hold current authority,
// use one writable READ COMMITTED transaction, and call this once before any
// domain/capacity locks or mutation. Roll back on every error. Returned selectors
// are observations only; the caller owns transaction custody and callback scope.
// This primitive neither invokes a mutation nor qualifies the full writer graph.
func (r *Repository) PrelockMutation(ctx context.Context, action, id string) ([]LockedResource, error) {
	if !validAction(action) || !validID(id) {
		return nil, ErrInvalid
	}
	if ctx == nil || ctx.Err() != nil || r.ready(ctx) != nil {
		return nil, ErrUnavailable
	}
	var isolation, readOnly string
	if err := r.tx.QueryRowContext(ctx, `SELECT pg_catalog.current_setting('transaction_isolation'), pg_catalog.current_setting('transaction_read_only')`).Scan(&isolation, &readOnly); err != nil || isolation != "read committed" || readOnly != "off" {
		return nil, ErrUnavailable
	}
	before, err := r.mutationSnapshot(ctx, action, id)
	if err != nil {
		return nil, err
	}
	resources, err := mutationResources(action, id, before)
	if err != nil {
		return nil, err
	}
	for _, resource := range resources {
		if err := r.lockMutationResource(ctx, resource); err != nil {
			return nil, err
		}
	}
	// Do not use caseRecord here: it could lock a changed source relation before
	// detecting that discovery no longer describes the acquired closure.
	after, err := r.mutationSnapshot(ctx, action, id)
	if err != nil {
		return nil, err
	}
	if !sameMutationSnapshot(action, before, after) {
		return nil, ErrConflict
	}
	for _, resource := range resources {
		switch resource.Kind {
		case "property":
			_, err = r.Property(ctx, resource.ID)
		case "source":
			_, err = r.source(ctx, resource.ID, false)
		}
		if err != nil {
			return nil, err
		}
	}
	if ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	return append([]LockedResource(nil), resources...), nil
}

func (r *Repository) mutationSnapshot(ctx context.Context, action, id string) (mutationSnapshot, error) {
	var value mutationSnapshot
	var err error
	switch action {
	case "case.changed", "draft.saved":
		value.caseValue, err = r.caseRow(ctx, id, false)
		if err == nil && value.caseValue.ID != id {
			err = ErrUnavailable
		}
	case "checklist.changed":
		value.application, err = r.applicationRow(ctx, id, false)
		if err == nil && value.application.ID != id {
			err = ErrUnavailable
		}
	case "procedure.changed":
		value.procedure, err = r.procedure(ctx, id, false)
		if err == nil && value.procedure.ID != id {
			err = ErrUnavailable
		}
	default:
		err = ErrInvalid
	}
	if err != nil {
		return mutationSnapshot{}, err
	}
	return value, nil
}

func mutationResources(action, id string, value mutationSnapshot) ([]LockedResource, error) {
	var resources []LockedResource
	switch action {
	case "case.changed", "draft.saved":
		resources = append(resources, LockedResource{"case", id}, LockedResource{"property", value.caseValue.PropertyID})
		for _, source := range value.caseValue.SourceIDs {
			resources = append(resources, LockedResource{"source", source})
		}
		if action == "draft.saved" {
			resources = append(resources, LockedResource{"draft", id})
		}
	case "checklist.changed":
		resources = append(resources, LockedResource{"application", id}, LockedResource{"property", value.application.PropertyID})
	case "procedure.changed":
		resources = append(resources, LockedResource{"procedure", id})
	default:
		return nil, ErrInvalid
	}
	if len(resources) == 0 || len(resources) > maxMutationResources {
		return nil, ErrUnavailable
	}
	seen := make(map[LockedResource]bool, len(resources))
	for _, resource := range resources {
		if !validID(resource.ID) || resourceRank(resource.Kind) == 0 || seen[resource] {
			return nil, ErrUnavailable
		}
		seen[resource] = true
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].ID == resources[j].ID {
			return resourceRank(resources[i].Kind) < resourceRank(resources[j].Kind)
		}
		return resources[i].ID < resources[j].ID
	})
	return resources, nil
}

func resourceRank(kind string) int {
	switch kind {
	case "property":
		return 1
	case "source":
		return 2
	case "case":
		return 3
	case "application":
		return 4
	case "procedure":
		return 5
	case "draft":
		return 6
	default:
		return 0
	}
}

func (r *Repository) lockMutationResource(ctx context.Context, resource LockedResource) error {
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	var query string
	switch resource.Kind {
	case "property":
		query = `SELECT id FROM public.continuity_properties WHERE ` + scoped + ` AND id=$5 FOR SHARE`
	case "source":
		query = `SELECT id FROM public.continuity_sources WHERE ` + scoped + ` AND id=$5 FOR SHARE`
	case "case":
		query = `SELECT id FROM public.continuity_cases WHERE ` + scoped + ` AND id=$5 FOR UPDATE`
	case "application":
		query = `SELECT id FROM public.continuity_applications WHERE ` + scoped + ` AND id=$5 FOR UPDATE`
	case "procedure":
		query = `SELECT id FROM public.continuity_procedures WHERE ` + scoped + ` AND id=$5 FOR UPDATE`
	case "draft":
		query = `SELECT case_id FROM public.continuity_drafts WHERE ` + scoped + ` AND case_id=$5 FOR UPDATE`
	default:
		return ErrUnavailable
	}
	var found string
	err := r.tx.QueryRowContext(ctx, query, r.args(resource.ID)...).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) && resource.Kind == "draft" {
		// There is no row lock for absence. The earlier same-ID case UPDATE
		// protects this key only under the complete cooperating writer profile.
		return nil
	}
	if err != nil {
		return readError(err)
	}
	if found != resource.ID {
		return ErrUnavailable
	}
	return nil
}

func sameMutationSnapshot(action string, a, b mutationSnapshot) bool {
	switch action {
	case "case.changed", "draft.saved":
		x, y := a.caseValue, b.caseValue
		return x.ID == y.ID && x.PropertyID == y.PropertyID && x.Title == y.Title && x.Status == y.Status && x.Revision == y.Revision && slices.Equal(x.SourceIDs, y.SourceIDs)
	case "checklist.changed":
		x, y := a.application, b.application
		return x.ID == y.ID && x.PropertyID == y.PropertyID && x.Revision == y.Revision && slices.Equal(x.Items, y.Items)
	case "procedure.changed":
		return a.procedure == b.procedure
	default:
		return false
	}
}
