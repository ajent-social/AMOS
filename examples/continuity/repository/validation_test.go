package repository

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"testing"

	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/google/uuid"
)

const testID = "01900000-0000-7000-8000-000000000001"

func TestInvalidBeforeSQL(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse(testID)
	scope := Scope{id, id, id, id}
	if _, err := New(nil, scope); err != ErrInvalid {
		t.Fatal("nil transaction admitted")
	}
	if _, err := New(new(sql.Tx), Scope{}); err != ErrInvalid {
		t.Fatal("invalid scope admitted")
	}
	r, err := New(new(sql.Tx), scope)
	if err != nil {
		t.Fatal(err)
	}
	// The zero sql.Tx cannot execute SQL: invalid calls must return before touching it.
	ctx := context.Background()
	var absentContext context.Context
	tests := []struct {
		name string
		call func() error
	}{
		{"property", func() error { _, e := r.Property(ctx, "bad"); return e }},
		{"source", func() error { _, e := r.Source(ctx, "bad"); return e }},
		{"case", func() error { _, e := r.Case(ctx, "bad"); return e }},
		{"application", func() error { _, e := r.Application(ctx, "bad"); return e }},
		{"procedure", func() error { _, e := r.Procedure(ctx, "bad"); return e }},
		{"draft", func() error { _, e := r.Draft(ctx, "bad"); return e }},
		{"status", func() error { _, e := r.ChangeCase(ctx, testID, 1, "unknown", testID); return e }},
		{"actor", func() error { _, e := r.ChangeCase(ctx, testID, 1, domain.Completed, "bad"); return e }},
		{"checklist", func() error { _, e := r.SetChecklist(ctx, testID, 1, "bad", true, testID); return e }},
		{"body", func() error { _, e := r.EditProcedure(ctx, testID, 1, "\x00", testID); return e }},
		{"draft body", func() error { _, e := r.SaveDraft(ctx, testID, 1, "\r", testID); return e }},
		{"nil context", func() error { _, e := r.Property(absentContext, testID); return e }},
		{"property search", func() error { _, e := r.Properties(ctx, "", strings.Repeat("界", 121), 1); return e }},
		{"source kind", func() error { _, e := r.Sources(ctx, "", "", "bad", 1); return e }},
		{"cases cursor", func() error { _, e := r.Cases(ctx, "bad", 1); return e }},
		{"application limit", func() error { _, e := r.Applications(ctx, "", 101); return e }},
		{"procedure limit", func() error { _, e := r.Procedures(ctx, "", 0); return e }},
		{"activity cursor", func() error { _, e := r.Activity(ctx, "bad", 1); return e }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if e := tc.call(); e != ErrInvalid {
				t.Fatalf("got %v", e)
			}
		})
	}
}
func TestPersistedValidation(t *testing.T) {
	t.Parallel()
	c := domain.Case{ID: testID, PropertyID: testID, Title: "valid", Status: domain.Completed, Revision: math.MaxInt64, SourceIDs: []string{testID}}
	if _, ok := caseValue(c); !ok {
		t.Fatal("last valid revision rejected on read")
	}
	p := domain.Procedure{ID: testID, Title: "valid", Body: "valid", Revision: math.MaxInt64}
	if _, ok := procedureValue(p); !ok {
		t.Fatal("last procedure revision rejected")
	}
	a := domain.Application{ID: testID, PropertyID: testID, Revision: math.MaxInt64, Items: []domain.ChecklistItem{{ID: testID, Label: "Decision", HumanDecision: true}}}
	if _, ok := appValue(a); !ok {
		t.Fatal("decision-only application rejected")
	}
	a.Items[0].Done = true
	if _, ok := appValue(a); ok {
		t.Fatal("completed decision admitted")
	}
	c.SourceIDs = append(c.SourceIDs, testID)
	if _, ok := caseValue(c); ok {
		t.Fatal("duplicate source admitted")
	}
	var items []domain.ChecklistItem
	if strictJSON([]byte(`[{"ID":"x","extra":true}]`), &items) || strictJSON([]byte(`[] {}`), &items) {
		t.Fatal("noncontract JSON admitted")
	}
	if searchPattern(`a%_\b`) != `%a\%\_\\b%` {
		t.Fatal("literal wildcard escape differs")
	}
}

func TestExactChecklistJSON(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`[{"ID":"x","Label":"y","Done":false}]`,
		`[{"id":"x","Label":"y","Done":false,"HumanDecision":true}]`,
		`[{"ID":"x","Label":"y","Done":null,"HumanDecision":true}]`,
		`[{"ID":"x","Label":"y","Done":false,"HumanDecision":true,"extra":0}]`,
	} {
		var out []domain.ChecklistItem
		if checklistJSON([]byte(raw), &out) {
			t.Fatal("noncanonical checklist JSON admitted")
		}
	}
	var out []domain.ChecklistItem
	if !checklistJSON([]byte(`[{"ID":"x","Label":"y","Done":false,"HumanDecision":true}]`), &out) {
		t.Fatal("exact wire shape rejected")
	}
}
