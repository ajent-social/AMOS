package domain_test

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/ajent-social/amos/examples/continuity/domain"
)

const id = "01900000-0000-7000-8000-000000000001"
const propertyID = "01900000-0000-7000-8000-000000000002"
const sourceID = "01900000-0000-7000-8000-000000000003"
const decisionID = "01900000-0000-7000-8000-000000000004"

func sampleCase() domain.Case {
	return domain.Case{ID: id, PropertyID: propertyID, Title: "  Inspection  ", Status: domain.AwaitingOwner, Revision: 2, SourceIDs: []string{sourceID}}
}
func sampleApplication() domain.Application {
	return domain.Application{ID: id, PropertyID: propertyID, Revision: 2, Items: []domain.ChecklistItem{{ID: sourceID, Label: "  Supporting document  "}, {ID: decisionID, Label: "  Human decision  ", HumanDecision: true}}}
}
func sampleProcedure() domain.Procedure {
	return domain.Procedure{ID: id, Title: "  Procedure  ", Body: "  Existing text  ", Revision: 2}
}

func checkError[T any](t *testing.T, got T, err, want error) {
	t.Helper()
	var zero T
	if err != want || !reflect.DeepEqual(got, zero) {
		t.Fatalf("got (%+v, %v), want zero and %v", got, err, want)
	}
}

func TestAllCaseTransitions(t *testing.T) {
	t.Parallel()
	states := []domain.CaseStatus{domain.AwaitingOwner, domain.ReadyToArrange, domain.InProgress, domain.NeedsAssessment, domain.Completed}
	for _, from := range states {
		for _, to := range states {
			t.Run(string(from)+"/"+string(to), func(t *testing.T) {
				current := sampleCase()
				current.Status = from
				got, err := domain.ChangeCase(current, 2, to)
				want := current
				want.Title = "Inspection"
				want.Status = to
				want.Revision = 3
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("got %+v, %v; want %+v", got, err, want)
				}
				if current.Title != "  Inspection  " || current.Status != from || current.Revision != 2 {
					t.Fatal("mutated input")
				}
			})
		}
	}
}

func TestCaseValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		alter func(*domain.Case)
	}{
		{"id", func(c *domain.Case) { c.ID = "bad" }},
		{"property", func(c *domain.Case) { c.PropertyID = "" }},
		{"title empty", func(c *domain.Case) { c.Title = " \u2003 " }},
		{"title long", func(c *domain.Case) { c.Title = strings.Repeat("界", 201) }},
		{"title utf8", func(c *domain.Case) { c.Title = "\xff" }},
		{"title control", func(c *domain.Case) { c.Title = "\nTitle" }},
		{"title unicode control", func(c *domain.Case) { c.Title = "Title\u0085" }},
		{"status", func(c *domain.Case) { c.Status = "approved" }},
		{"status utf8", func(c *domain.Case) { c.Status = "\xff" }},
		{"zero revision", func(c *domain.Case) { c.Revision = 0 }},
		{"negative revision", func(c *domain.Case) { c.Revision = -1 }},
		{"nil sources", func(c *domain.Case) { c.SourceIDs = nil }},
		{"empty sources", func(c *domain.Case) { c.SourceIDs = []string{} }},
		{"duplicate source", func(c *domain.Case) { c.SourceIDs = []string{sourceID, sourceID} }},
		{"invalid source", func(c *domain.Case) { c.SourceIDs = []string{"bad"} }},
		{"too many sources", func(c *domain.Case) { c.SourceIDs = ids(33) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := sampleCase()
			tt.alter(&c)
			got, err := domain.ChangeCase(c, 1, domain.Completed)
			checkError(t, got, err, domain.ErrInvalid)
			draft, err := domain.PrepareDraft(c, "Update")
			checkError(t, draft, err, domain.ErrInvalid)
		})
	}
}

func TestUUIDValidation(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "\xff", strings.ToUpper("01900000-abcd-7000-8000-000000000001"), "01900000000070008000000000000001", "{" + id + "}", "urn:uuid:" + id, " " + id, "01900000-0000-4000-8000-000000000001", "01900000-0000-7000-c000-000000000001", "01900000-0000-7000-0000-000000000001"} {
		t.Run(fmt.Sprintf("%q", bad), func(t *testing.T) {
			c := sampleCase()
			c.ID = bad
			got, err := domain.ChangeCase(c, 2, domain.Completed)
			checkError(t, got, err, domain.ErrInvalid)
			a := sampleApplication()
			a.Items[0].ID = bad
			app, err := domain.SetChecklist(a, 2, bad, true)
			checkError(t, app, err, domain.ErrInvalid)
			p := sampleProcedure()
			p.ID = bad
			proc, err := domain.EditProcedure(p, 2, "new")
			checkError(t, proc, err, domain.ErrInvalid)
		})
	}
}

func ids(n int) []string {
	result := make([]string, n)
	for i := range result {
		result[i] = fmt.Sprintf("01900000-0000-7000-8000-%012x", i+1)
	}
	return result
}

func TestBoundariesAndPlainText(t *testing.T) {
	t.Parallel()
	c := sampleCase()
	c.Title = " \u2003" + strings.Repeat("界", 200) + " "
	c.SourceIDs = ids(32)
	got, err := domain.ChangeCase(c, 2, domain.Completed)
	if err != nil || got.Title != strings.Repeat("界", 200) || len(got.SourceIDs) != 32 {
		t.Fatalf("case boundary: %+v %v", got, err)
	}
	for _, body := range []string{"x", strings.Repeat("界", 4000), "\n <script>untrusted()</script>\t\ntext \n", "x\u0085y"} {
		draft, err := domain.PrepareDraft(c, body)
		if err != nil || draft.Body != strings.TrimSpace(body) || draft.CaseID != c.ID || draft.CaseRevision != c.Revision || !reflect.DeepEqual(draft.SourceIDs, c.SourceIDs) {
			t.Fatalf("draft: %+v %v", draft, err)
		}
	}
	p := sampleProcedure()
	p.Title = strings.Repeat("界", 200)
	p.Body = strings.Repeat("界", 6000)
	proc, err := domain.EditProcedure(p, 2, "  "+p.Body+"  ")
	if err != nil || proc.Body != p.Body || proc.Revision != 3 {
		t.Fatalf("procedure boundary: %+v %v", proc, err)
	}
	a := sampleApplication()
	a.Items = nil
	for i, itemID := range ids(32) {
		a.Items = append(a.Items, domain.ChecklistItem{ID: itemID, Label: strings.Repeat("界", 200), HumanDecision: i == 31})
	}
	app, err := domain.SetChecklist(a, 2, a.Items[0].ID, true)
	if err != nil || len(app.Items) != 32 || !app.Items[0].Done {
		t.Fatalf("application boundary: %+v %v", app, err)
	}
}

func TestBodies(t *testing.T) {
	t.Parallel()
	bad := []string{"", " \n\t ", "\xff", "a\x00b", "\rtext", "text\x7f", strings.Repeat("界", 6001)}
	for control := rune(0); control < 32; control++ {
		if control != '\t' && control != '\n' {
			bad = append(bad, "x"+string(control)+"y")
		}
	}
	for i, body := range bad {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			got, err := domain.EditProcedure(sampleProcedure(), 1, body)
			checkError(t, got, err, domain.ErrInvalid)
			p := sampleProcedure()
			p.Body = body
			got, err = domain.EditProcedure(p, 2, "new")
			checkError(t, got, err, domain.ErrInvalid)
			draft, err := domain.PrepareDraft(sampleCase(), body)
			checkError(t, draft, err, domain.ErrInvalid)
		})
	}
	draft, err := domain.PrepareDraft(sampleCase(), strings.Repeat("x", 4001))
	checkError(t, draft, err, domain.ErrInvalid)
}

func TestRevisionGuards(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name              string
		current, expected int64
		want              error
	}{
		{"stale", 2, 1, domain.ErrConflict}, {"future", 2, 3, domain.ErrConflict},
		{"zero expected", 2, 0, domain.ErrInvalid}, {"negative expected", 2, -1, domain.ErrInvalid},
		{"zero current", 0, 1, domain.ErrInvalid}, {"negative current", -1, 1, domain.ErrInvalid},
		{"overflow", math.MaxInt64, math.MaxInt64, domain.ErrInvalid}, {"overflow before conflict", math.MaxInt64, 1, domain.ErrInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := sampleCase()
			c.Revision = tt.current
			got, err := domain.ChangeCase(c, tt.expected, domain.Completed)
			checkError(t, got, err, tt.want)
			a := sampleApplication()
			a.Revision = tt.current
			app, err := domain.SetChecklist(a, tt.expected, sourceID, true)
			checkError(t, app, err, tt.want)
			p := sampleProcedure()
			p.Revision = tt.current
			proc, err := domain.EditProcedure(p, tt.expected, "new")
			checkError(t, proc, err, tt.want)
		})
	}
	c := sampleCase()
	c.Revision = math.MaxInt64
	if _, err := domain.PrepareDraft(c, "update"); err != nil {
		t.Fatalf("draft does not increment: %v", err)
	}
	c.Revision = math.MaxInt64 - 1
	if got, err := domain.ChangeCase(c, c.Revision, c.Status); err != nil || got.Revision != math.MaxInt64 {
		t.Fatalf("last revision: %+v %v", got, err)
	}
}

func TestChecklistValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		alter func(*domain.Application)
	}{
		{"id", func(a *domain.Application) { a.ID = "bad" }},
		{"property", func(a *domain.Application) { a.PropertyID = "bad" }},
		{"nil", func(a *domain.Application) { a.Items = nil }},
		{"empty", func(a *domain.Application) { a.Items = []domain.ChecklistItem{} }},
		{"oversized", func(a *domain.Application) {
			for _, v := range ids(33) {
				a.Items = append(a.Items, domain.ChecklistItem{ID: v, Label: "item"})
			}
		}},
		{"duplicate", func(a *domain.Application) { a.Items[1].ID = a.Items[0].ID }},
		{"bad other id", func(a *domain.Application) { a.Items[1].ID = "bad" }},
		{"empty label", func(a *domain.Application) { a.Items[1].Label = " " }},
		{"long label", func(a *domain.Application) { a.Items[1].Label = strings.Repeat("界", 201) }},
		{"utf8 label", func(a *domain.Application) { a.Items[1].Label = "\xff" }},
		{"control label", func(a *domain.Application) { a.Items[1].Label = "label\t" }},
		{"no decision", func(a *domain.Application) { a.Items[1].HumanDecision = false }},
		{"two decisions", func(a *domain.Application) { a.Items[0].HumanDecision = true }},
		{"decision done", func(a *domain.Application) { a.Items[1].Done = true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := sampleApplication()
			tt.alter(&a)
			got, err := domain.SetChecklist(a, 1, sourceID, true)
			checkError(t, got, err, domain.ErrInvalid)
		})
	}
	for _, target := range []string{id, "bad", "\xff", ""} {
		got, err := domain.SetChecklist(sampleApplication(), 1, target, true)
		checkError(t, got, err, domain.ErrInvalid)
	}
}

func TestHumanDecisionGuard(t *testing.T) {
	t.Parallel()
	for _, done := range []bool{false, true} {
		got, err := domain.SetChecklist(sampleApplication(), 2, decisionID, done)
		checkError(t, got, err, domain.ErrDecisionDisabled)
		got, err = domain.SetChecklist(sampleApplication(), 1, decisionID, done)
		checkError(t, got, err, domain.ErrConflict)
	}
}

func TestChecklistAndProcedureChanges(t *testing.T) {
	t.Parallel()
	for _, before := range []bool{false, true} {
		for _, after := range []bool{false, true} {
			a := sampleApplication()
			a.Items[0].Done = before
			got, err := domain.SetChecklist(a, 2, sourceID, after)
			want := sampleApplication()
			want.Revision = 3
			want.Items[0].Done = after
			want.Items[0].Label = "Supporting document"
			want.Items[1].Label = "Human decision"
			if err != nil || !reflect.DeepEqual(got, want) || a.Items[0].Done != before || a.Items[0].Label != "  Supporting document  " {
				t.Fatalf("checklist: %+v %v", got, err)
			}
		}
	}
	p := sampleProcedure()
	for _, body := range []string{p.Body, " new\n\ttext "} {
		got, err := domain.EditProcedure(p, 2, body)
		want := p
		want.Title = "Procedure"
		want.Body = strings.TrimSpace(body)
		want.Revision = 3
		if err != nil || got != want || p != sampleProcedure() {
			t.Fatalf("procedure: %+v %v", got, err)
		}
	}
	for _, title := range []string{"", strings.Repeat("x", 201), "\xff", "x\n"} {
		p := sampleProcedure()
		p.Title = title
		got, err := domain.EditProcedure(p, 1, "new")
		checkError(t, got, err, domain.ErrInvalid)
	}
	got, err := domain.ChangeCase(sampleCase(), 1, "unknown")
	checkError(t, got, err, domain.ErrInvalid)
}

func TestNoSliceAliasing(t *testing.T) {
	t.Parallel()
	c := sampleCase()
	changed, err := domain.ChangeCase(c, 2, domain.Completed)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := domain.PrepareDraft(c, "update")
	if err != nil {
		t.Fatal(err)
	}
	draft2, err := domain.PrepareDraft(c, "update")
	if err != nil {
		t.Fatal(err)
	}
	c.SourceIDs[0] = id
	if changed.SourceIDs[0] != sourceID || draft.SourceIDs[0] != sourceID || draft2.SourceIDs[0] != sourceID {
		t.Fatal("input aliases outputs")
	}
	changed.SourceIDs[0] = propertyID
	draft.SourceIDs[0] = decisionID
	if c.SourceIDs[0] != id || draft2.SourceIDs[0] != sourceID {
		t.Fatal("outputs alias retained slices")
	}
	a := sampleApplication()
	changedApp, err := domain.SetChecklist(a, 2, sourceID, true)
	if err != nil {
		t.Fatal(err)
	}
	otherApp, err := domain.SetChecklist(a, 2, sourceID, false)
	if err != nil {
		t.Fatal(err)
	}
	a.Items[0].Label = "caller mutation"
	if changedApp.Items[0].Label != "Supporting document" || otherApp.Items[0].Label != "Supporting document" {
		t.Fatal("input aliases application output")
	}
	changedApp.Items[0].Label = "output mutation"
	if a.Items[0].Label != "caller mutation" || otherApp.Items[0].Label != "Supporting document" {
		t.Fatal("output aliases other application")
	}
}
