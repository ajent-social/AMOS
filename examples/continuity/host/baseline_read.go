package host

import (
	"context"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/ajent-social/amos/examples/continuity/guide"
	"github.com/ajent-social/amos/examples/continuity/repository"
	"github.com/ajent-social/amos/examples/continuity/ui/baselineview"
	"github.com/google/uuid"
)

type baselinePage uint8

const (
	pageProperties baselinePage = iota + 1
	pageProperty
	pageCases
	pageCase
	pageApplications
	pageApplication
	pageProcedures
	pageProcedure
	pageActivity
	pageGuide
)

type baselineQuery struct {
	WorkspaceID       uuid.UUID
	Page              baselinePage
	ID, After, Query  string
	Limit             int
	Topic             guide.Topic
	CaseID, CSRFToken string
	Fragment          bool
}

func baselineToken(s string) bool {
	if len(s) != 43 {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == s
}
func normalizeBaselineQuery(q baselineQuery) (baselineQuery, bool) {
	bad := func() (baselineQuery, bool) { return baselineQuery{}, false }
	if len(q.ID) > 36 || len(q.After) > 36 || len(q.Query) > 480 || len(q.Topic) > 18 || len(q.CaseID) > 36 || len(q.CSRFToken) > 43 {
		return bad()
	}
	if q.WorkspaceID != uuid.Nil && (q.WorkspaceID.Version() != 7 || q.WorkspaceID.Variant() != uuid.RFC4122) {
		return bad()
	}
	if !utf8.ValidString(q.Query) {
		return bad()
	}
	for _, r := range q.Query {
		if unicode.IsControl(r) {
			return bad()
		}
	}
	hasQuery := q.Query != ""
	q.Query = strings.TrimSpace(q.Query)
	if utf8.RuneCountInString(q.Query) > 120 {
		return bad()
	}
	switch q.Page {
	case pageProperties, pageCases, pageApplications, pageProcedures, pageActivity:
		if q.ID != "" || q.Topic != "" || q.CaseID != "" || q.CSRFToken != "" || q.Limit < 1 || q.Limit > 100 || (q.After != "" && !sourceID(q.After)) || (q.Page != pageProperties && hasQuery) {
			return bad()
		}
	case pageProperty, pageCase, pageApplication, pageProcedure:
		if !sourceID(q.ID) || q.After != "" || hasQuery || q.Limit != 0 || q.Topic != "" || q.CaseID != "" {
			return bad()
		}
		if q.Page == pageProperty {
			if q.CSRFToken != "" {
				return bad()
			}
		} else if !baselineToken(q.CSRFToken) {
			return bad()
		}
	case pageGuide:
		if q.ID != "" || q.After != "" || hasQuery || q.CSRFToken != "" {
			return bad()
		}
		switch q.Topic {
		case guide.ExplainCase, guide.OwnerDraft:
			if !sourceID(q.CaseID) || q.Limit != 0 {
				return bad()
			}
		case guide.Attention, guide.Handover:
			if q.CaseID != "" || q.Limit < 1 || q.Limit > 100 {
				return bad()
			}
		case guide.SpendingAuthority:
			if q.CaseID != "" || q.Limit != 0 {
				return bad()
			}
		default:
			return bad()
		}
	default:
		return bad()
	}
	return q, true
}

func (c *readCore) baseline(ctx context.Context, q baselineQuery) ([]byte, error) {
	return c.readPage(ctx, sourceQuery{WorkspaceID: q.WorkspaceID}, &q)
}

// renderBaseline receives only the repository already scoped by the private
// authority path. Its bytes are provisional; readPage alone owns publication.
func renderBaseline(ctx context.Context, repo *repository.Repository, q baselineQuery) ([]byte, error) {
	var out []byte
	var err error
	cursor := baselineview.PageCursor{After: q.After, Limit: q.Limit}
	switch q.Page {
	case pageProperties:
		values, e := repo.Properties(ctx, q.After, q.Query, q.Limit)
		if e != nil {
			return nil, readClass(e)
		}
		out, err = baselineview.RenderProperties(baselineview.PropertyList{PageCursor: cursor, Query: q.Query, Properties: values}, q.Fragment)
	case pageProperty:
		value, e := repo.Property(ctx, q.ID)
		if e != nil {
			return nil, readClass(e)
		}
		out, err = baselineview.RenderProperty(value, q.Fragment)
	case pageCases:
		values, e := repo.Cases(ctx, q.After, q.Limit)
		if e != nil {
			return nil, readClass(e)
		}
		out, err = baselineview.RenderCases(baselineview.CaseList{PageCursor: cursor, Cases: values}, q.Fragment)
	case pageCase:
		value, e := repo.Case(ctx, q.ID)
		if e != nil {
			return nil, readClass(e)
		}
		model := baselineview.CasePage{Case: value, CSRFToken: q.CSRFToken}
		draft, e := repo.Draft(ctx, q.ID)
		if e == nil {
			model.Draft = &draft
		} else if !errors.Is(e, repository.ErrNotFound) {
			return nil, readClass(e)
		}
		out, err = baselineview.RenderCase(model, q.Fragment)
	case pageApplications:
		values, e := repo.Applications(ctx, q.After, q.Limit)
		if e != nil {
			return nil, readClass(e)
		}
		out, err = baselineview.RenderApplications(baselineview.ApplicationList{PageCursor: cursor, Applications: values}, q.Fragment)
	case pageApplication:
		value, e := repo.Application(ctx, q.ID)
		if e != nil {
			return nil, readClass(e)
		}
		out, err = baselineview.RenderApplication(baselineview.ApplicationPage{Application: value, CSRFToken: q.CSRFToken}, q.Fragment)
	case pageProcedures:
		values, e := repo.Procedures(ctx, q.After, q.Limit)
		if e != nil {
			return nil, readClass(e)
		}
		out, err = baselineview.RenderProcedures(baselineview.ProcedureList{PageCursor: cursor, Procedures: values}, q.Fragment)
	case pageProcedure:
		value, e := repo.Procedure(ctx, q.ID)
		if e != nil {
			return nil, readClass(e)
		}
		out, err = baselineview.RenderProcedure(baselineview.ProcedurePage{Procedure: value, CSRFToken: q.CSRFToken}, q.Fragment)
	case pageActivity:
		values, e := repo.Activity(ctx, q.After, q.Limit)
		if e != nil {
			return nil, readClass(e)
		}
		out, err = baselineview.RenderActivity(baselineview.ActivityList{PageCursor: cursor, Activity: values}, q.Fragment)
	case pageGuide:
		selection, e := baselineGuideSelection(ctx, repo, q)
		if e != nil {
			return nil, e
		}
		out, err = baselineview.RenderGuide(guide.Request{Topic: q.Topic, CaseID: q.CaseID}, selection, q.Fragment)
	default:
		return nil, errInvalid
	}
	if err != nil {
		return nil, errUnavailable
	}
	return out, nil
}

func baselineGuideSelection(ctx context.Context, repo *repository.Repository, q baselineQuery) (guide.Selection, error) {
	var selection guide.Selection
	var err error
	switch q.Topic {
	case guide.ExplainCase, guide.OwnerDraft:
		var value domain.Case
		value, err = repo.Case(ctx, q.CaseID)
		if err == nil {
			selection.Cases = []domain.Case{value}
		}
	case guide.Attention, guide.Handover:
		selection.Cases, err = repo.Cases(ctx, "", q.Limit)
	case guide.SpendingAuthority:
		return selection, nil
	default:
		return guide.Selection{}, errInvalid
	}
	if err != nil {
		return guide.Selection{}, readClass(err)
	}
	ids, e := baselineGuideReferences(selection.Cases)
	if e != nil {
		return guide.Selection{}, e
	}
	for _, id := range ids {
		value, e := repo.Source(ctx, id)
		if e != nil {
			return guide.Selection{}, readClass(e)
		}
		selection.Sources = append(selection.Sources, guide.Source{ID: value.ID, Title: value.Title, Body: value.Body, SHA256: value.SHA256})
	}
	return selection, nil
}

// Reject an oversized reference set before building the guide's retained body
// collection. Repository case reads already validate their own source references.
// Every supplied case keeps all its references; none are truncated to fit.
func baselineGuideReferences(cases []domain.Case) ([]string, error) {
	if len(cases) > 100 {
		return nil, errUnavailable
	}
	seen := make(map[string]bool)
	for _, value := range cases {
		if len(value.SourceIDs) < 1 || len(value.SourceIDs) > 32 {
			return nil, errUnavailable
		}
		for _, id := range value.SourceIDs {
			if !sourceID(id) {
				return nil, errUnavailable
			}
			seen[id] = true
			if len(seen) > 100 {
				return nil, errUnavailable
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
