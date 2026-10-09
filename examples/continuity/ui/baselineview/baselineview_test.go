package baselineview

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/ajent-social/amos/examples/continuity/guide"
	"github.com/ajent-social/amos/examples/continuity/repository"
	"golang.org/x/net/html"
)

func testID(n int) string { return fmt.Sprintf("01900000-0000-7000-8000-%012x", n) }
func testToken() string   { return base64.RawURLEncoding.EncodeToString(make([]byte, 32)) }
func testProperty() repository.Property {
	return repository.Property{ID: testID(1), Name: "House", Area: "North", OwnerLabel: "Recorded owner", OccupantLabel: "Recorded occupant", Occupancy: "unknown", InspectionDate: "2024-02-29"}
}
func testCase() domain.Case {
	return domain.Case{ID: testID(2), PropertyID: testID(1), Title: "Inspect roof", Status: domain.AwaitingOwner, Revision: 7, SourceIDs: []string{testID(8)}}
}
func testApplication() domain.Application {
	return domain.Application{ID: testID(3), PropertyID: testID(1), Revision: 3, Items: []domain.ChecklistItem{{ID: testID(4), Label: "Supporting record"}, {ID: testID(5), Label: "Human decision", HumanDecision: true}}}
}
func testProcedure() domain.Procedure {
	return domain.Procedure{ID: testID(6), Title: "Procedure", Body: "First review records.\nThen ask a person.", Revision: 2}
}
func testActivity() repository.Activity {
	return repository.Activity{ID: testID(7), ActorID: testID(9), ResourceID: testID(2), Revision: 7, Action: "case.changed", At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("offset", 3600))}
}
func testSelection() guide.Selection {
	s := guide.Source{ID: testID(8), Title: "Original record", Body: "Supplied original text"}
	s.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(s.Body)))
	return guide.Selection{Cases: []domain.Case{testCase()}, Sources: []guide.Source{s}}
}
func testDraft() domain.Draft {
	return domain.Draft{CaseID: testID(2), CaseRevision: 6, Body: "Unsent notes", SourceIDs: []string{testID(8)}}
}
func elements(n *html.Node, tag string) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}
func attribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func textContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var s strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		s.WriteString(textContent(c))
	}
	return s.String()
}
func document(t *testing.T, b []byte, fragment bool) *html.Node {
	t.Helper()
	n, e := html.Parse(bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	sections := elements(n, "section")
	if len(sections) != 1 || attribute(sections[0], "id") != "continuity-baseline-content" {
		t.Fatal("single declared fragment section missing")
	}
	for _, tag := range []string{"script", "img", "iframe"} {
		if len(elements(n, tag)) != 0 {
			t.Fatalf("untrusted %s element", tag)
		}
	}
	if fragment {
		if bytes.Contains(b, []byte("<!doctype")) || len(elements(n, "main")) != 0 || len(elements(n, "link")) != 0 {
			t.Fatal("fragment has document wrapper")
		}
	} else {
		if len(elements(n, "main")) != 1 || len(elements(n, "title")) != 1 || !bytes.Contains(b, []byte(`href="#main-content"`)) {
			t.Fatal("full document landmarks missing")
		}
	}
	return n
}
func invalidOutput(t *testing.T, b []byte, e error) {
	t.Helper()
	if b != nil || e != ErrInvalid {
		t.Fatalf("want nil/exact ErrInvalid, got %d bytes/%v", len(b), e)
	}
}
func fields(n *html.Node, name string) []*html.Node {
	var found []*html.Node
	for _, tag := range []string{"input", "select", "textarea"} {
		for _, v := range elements(n, tag) {
			if attribute(v, "name") == name {
				found = append(found, v)
			}
		}
	}
	return found
}

func TestEveryViewFullAndFragment(t *testing.T) {
	p, c, a, proc, activity := testProperty(), testCase(), testApplication(), testProcedure(), testActivity()
	views := map[string]func(bool) ([]byte, error){
		"properties": func(f bool) ([]byte, error) {
			return RenderProperties(PropertyList{PageCursor: PageCursor{Limit: 10}, Properties: []repository.Property{p}}, f)
		},
		"property": func(f bool) ([]byte, error) { return RenderProperty(p, f) },
		"cases": func(f bool) ([]byte, error) {
			return RenderCases(CaseList{PageCursor: PageCursor{Limit: 10}, Cases: []domain.Case{c}}, f)
		},
		"case": func(f bool) ([]byte, error) { return RenderCase(CasePage{Case: c, CSRFToken: testToken()}, f) },
		"applications": func(f bool) ([]byte, error) {
			return RenderApplications(ApplicationList{PageCursor: PageCursor{Limit: 10}, Applications: []domain.Application{a}}, f)
		},
		"application": func(f bool) ([]byte, error) {
			return RenderApplication(ApplicationPage{Application: a, CSRFToken: testToken()}, f)
		},
		"procedures": func(f bool) ([]byte, error) {
			return RenderProcedures(ProcedureList{PageCursor: PageCursor{Limit: 10}, Procedures: []domain.Procedure{proc}}, f)
		},
		"procedure": func(f bool) ([]byte, error) {
			return RenderProcedure(ProcedurePage{Procedure: proc, CSRFToken: testToken()}, f)
		},
		"activity": func(f bool) ([]byte, error) {
			return RenderActivity(ActivityList{PageCursor: PageCursor{Limit: 10}, Activity: []repository.Activity{activity}}, f)
		},
		"guide": func(f bool) ([]byte, error) {
			return RenderGuide(guide.Request{Topic: guide.Attention}, testSelection(), f)
		},
	}
	for name, render := range views {
		for _, fragment := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fragment=%v", name, fragment), func(t *testing.T) {
				b, e := render(fragment)
				if e != nil {
					t.Fatal(e)
				}
				n := document(t, b, fragment)
				if len(elements(n, "h1")) != 1 {
					t.Fatal("missing labelled heading")
				}
				for _, form := range elements(n, "form") {
					if attribute(form, "method") == "post" {
						if len(fields(form, "_csrf")) != 1 || attribute(fields(form, "_csrf")[0], "value") != testToken() || len(fields(form, "expected")) != 1 {
							t.Fatal("ordinary POST custody fields missing")
						}
					}
				}
				for _, tag := range []string{"select", "textarea"} {
					for _, field := range elements(n, tag) {
						id := attribute(field, "id")
						labels := 0
						for _, label := range elements(n, "label") {
							if attribute(label, "for") == id {
								labels++
							}
						}
						if id == "" || labels != 1 {
							t.Fatal("field lacks associated label")
						}
					}
				}
			})
		}
	}
}

func TestOrdinaryFormsAndHumanDecisionUnavailable(t *testing.T) {
	a := testApplication()
	b, e := RenderApplication(ApplicationPage{Application: a, CSRFToken: testToken()}, false)
	if e != nil {
		t.Fatal(e)
	}
	n := document(t, b, false)
	forms := elements(n, "form")
	if len(forms) != 1 || attribute(forms[0], "action") != "/continuity/applications/"+a.ID+"/checklist" {
		t.Fatal("supporting form missing or human decision became actionable")
	}
	if len(fields(n, "item_id")) != 1 || attribute(fields(n, "item_id")[0], "value") != a.Items[0].ID {
		t.Fatal("human-decision ID exposed as mutable input")
	}
	if !strings.Contains(textContent(n), "Final human decision is disabled") {
		t.Fatal("disabled decision explanation missing")
	}
	c := testCase()
	d := testDraft()
	b, e = RenderCase(CasePage{Case: c, Draft: &d, CSRFToken: testToken()}, true)
	if e != nil {
		t.Fatal(e)
	}
	n = document(t, b, true)
	if len(elements(n, "form")) != 2 || !strings.Contains(textContent(n), "The case changed after this draft was saved") {
		t.Fatal("ordinary forms or stale draft warning missing")
	}
	for _, f := range elements(n, "form") {
		if attribute(fields(f, "expected")[0], "value") != "7" {
			t.Fatal("stale draft reused an old expected revision")
		}
	}
	if len(fields(n, "recipient")) != 0 || strings.Contains(string(b), "/send") {
		t.Fatal("draft exposed sending")
	}
	statuses := fields(n, "status")
	if len(statuses) != 1 || len(elements(statuses[0], "option")) != 5 {
		t.Fatal("finite status choices missing")
	}
}

func TestEscapedValuesAndOwnedOutput(t *testing.T) {
	attack := `</textarea><script>boom</script><img src=x onerror="boom"> & {{.Secret}}`
	p := testProcedure()
	p.Title = attack
	p.Body = attack
	b, e := RenderProcedure(ProcedurePage{Procedure: p, CSRFToken: testToken()}, false)
	if e != nil {
		t.Fatal(e)
	}
	n := document(t, b, false)
	area := elements(n, "textarea")
	if len(area) != 1 || strings.TrimPrefix(textContent(area[0]), "\n") != attack {
		t.Fatal("procedure text altered or escaped markup executed")
	}
	c := testCase()
	c.Title = attack
	d := testDraft()
	d.Body = attack
	b, e = RenderCase(CasePage{Case: c, Draft: &d, CSRFToken: testToken()}, true)
	if e != nil {
		t.Fatal(e)
	}
	document(t, b, true)
	copyOfOutput := append([]byte(nil), b...)
	c.SourceIDs[0] = testID(99)
	d.SourceIDs[0] = testID(98)
	d.Body = "changed"
	if !bytes.Equal(b, copyOfOutput) {
		t.Fatal("output aliases input")
	}
	p2 := testProcedure()
	next, e := RenderProcedure(ProcedurePage{Procedure: p2, CSRFToken: testToken()}, true)
	if e != nil {
		t.Fatal(e)
	}
	for i := range b {
		b[i] = '!'
	}
	if bytes.Contains(next, []byte("!!!!")) {
		t.Fatal("calls share output buffers")
	}
}

func TestLastRevisionAndInputIsolation(t *testing.T) {
	c := testCase()
	c.Revision = math.MaxInt64
	c.Title = "  Case  "
	before := append([]string(nil), c.SourceIDs...)
	if _, e := RenderCase(CasePage{Case: c, CSRFToken: testToken()}, false); e != nil {
		t.Fatal("readable last revision rejected", e)
	}
	if c.Title != "  Case  " || !reflect.DeepEqual(c.SourceIDs, before) {
		t.Fatal("input normalized in place")
	}
	a := testApplication()
	a.Revision = math.MaxInt64
	a.Items[0].Label = "  Label  "
	if _, e := RenderApplication(ApplicationPage{Application: a, CSRFToken: testToken()}, false); e != nil {
		t.Fatal(e)
	}
	if a.Items[0].Label != "  Label  " {
		t.Fatal("nested item mutated")
	}
	p := testProcedure()
	p.Revision = math.MaxInt64
	if _, e := RenderProcedure(ProcedurePage{Procedure: p, CSRFToken: testToken()}, false); e != nil {
		t.Fatal(e)
	}
}

func TestPaginationFiltersEmptyAndOrdering(t *testing.T) {
	p := testProperty()
	b, e := RenderProperties(PropertyList{PageCursor: PageCursor{Limit: 1}, Query: "  roof & window  ", Properties: []repository.Property{p}}, true)
	if e != nil {
		t.Fatal(e)
	}
	n := document(t, b, true)
	found := false
	for _, a := range elements(n, "a") {
		if textContent(a) == "Try next page" {
			u, e := url.Parse(attribute(a, "href"))
			if e != nil || u.Query().Get("q") != "roof & window" || u.Query().Get("after") != p.ID || u.Query().Get("limit") != "1" {
				t.Fatal("pagination lost bounded filters")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("next-page suggestion absent")
	}
	if len(fields(elements(n, "form")[0], "after")) != 0 {
		t.Fatal("search retains stale cursor")
	}
	for _, q := range []string{"", "   "} {
		b, e := RenderProperties(PropertyList{PageCursor: PageCursor{Limit: 10}, Query: q}, true)
		if e != nil || !strings.Contains(string(b), "No properties in this page") {
			t.Fatal("empty search/state unavailable", e)
		}
	}
	for _, cursor := range []PageCursor{{Limit: 0}, {Limit: 101}, {Limit: 1, After: p.ID}, {Limit: 1, After: "bad"}} {
		b, e := RenderProperties(PropertyList{PageCursor: cursor, Properties: []repository.Property{p}}, false)
		invalidOutput(t, b, e)
	}
	b, e = RenderProperties(PropertyList{PageCursor: PageCursor{Limit: 2}, Properties: []repository.Property{p, p}}, false)
	invalidOutput(t, b, e)
	later := p
	later.ID = testID(10)
	b, e = RenderProperties(PropertyList{PageCursor: PageCursor{Limit: 2}, Properties: []repository.Property{later, p}}, false)
	invalidOutput(t, b, e)
}

func TestInvalidModelsReturnNoPartialOutput(t *testing.T) {
	for _, bad := range []string{"", strings.Repeat("x", 801), "\xff", "\ntrimmed control", "\u0085control"} {
		t.Run(fmt.Sprintf("property/%q", bad[:min(len(bad), 12)]), func(t *testing.T) {
			p := testProperty()
			p.Name = bad
			b, e := RenderProperty(p, false)
			invalidOutput(t, b, e)
		})
	}
	for _, change := range []func(*repository.Property){func(p *repository.Property) { p.ID = strings.ToUpper(testID(11)) }, func(p *repository.Property) { p.ID = "01900000-0000-4000-8000-000000000001" }, func(p *repository.Property) { p.Occupancy = "leased" }, func(p *repository.Property) { p.InspectionDate = "2025-02-29" }, func(p *repository.Property) { p.OccupantLabel = "\t" }} {
		p := testProperty()
		change(&p)
		b, e := RenderProperty(p, false)
		invalidOutput(t, b, e)
	}
	for _, change := range []func(*domain.Case){func(c *domain.Case) { c.Revision = 0 }, func(c *domain.Case) { c.Status = "approved" }, func(c *domain.Case) { c.SourceIDs = nil }, func(c *domain.Case) { c.SourceIDs = append(c.SourceIDs, c.SourceIDs[0]) }, func(c *domain.Case) { c.SourceIDs = []string{strings.Repeat("a", 37)} }} {
		c := testCase()
		change(&c)
		b, e := RenderCase(CasePage{Case: c, CSRFToken: testToken()}, false)
		invalidOutput(t, b, e)
	}
	for _, token := range []string{"", testToken() + "=", strings.Repeat("!", 43), strings.Repeat("A", 42) + "B"} {
		b, e := RenderCase(CasePage{Case: testCase(), CSRFToken: token}, false)
		invalidOutput(t, b, e)
	}
	for _, change := range []func(*domain.Draft){func(d *domain.Draft) { d.CaseID = testID(90) }, func(d *domain.Draft) { d.CaseRevision = 8 }, func(d *domain.Draft) { d.CaseRevision = 0 }, func(d *domain.Draft) { d.Body = "\rbody" }, func(d *domain.Draft) { d.SourceIDs = nil }, func(d *domain.Draft) { d.Body = strings.Repeat("a", 16001) }} {
		d := testDraft()
		change(&d)
		b, e := RenderCase(CasePage{Case: testCase(), Draft: &d, CSRFToken: testToken()}, false)
		invalidOutput(t, b, e)
	}
	for _, change := range []func(*domain.Application){func(a *domain.Application) { a.Items[1].Done = true }, func(a *domain.Application) { a.Items[1].HumanDecision = false }, func(a *domain.Application) { a.Items[0].HumanDecision = true }, func(a *domain.Application) { a.Items[1].ID = a.Items[0].ID }, func(a *domain.Application) { a.Items[0].Label = "\nlabel" }} {
		a := testApplication()
		change(&a)
		b, e := RenderApplication(ApplicationPage{Application: a, CSRFToken: testToken()}, false)
		invalidOutput(t, b, e)
	}
	for _, q := range []string{"\t", strings.Repeat("a", 481), strings.Repeat("a", 121), "\xff"} {
		b, e := RenderProperties(PropertyList{PageCursor: PageCursor{Limit: 10}, Query: q}, false)
		invalidOutput(t, b, e)
	}
	p := testProcedure()
	p.Body = "\x00bad"
	b, e := RenderProcedure(ProcedurePage{Procedure: p, CSRFToken: testToken()}, false)
	invalidOutput(t, b, e)
	activity := testActivity()
	activity.Action = "application.approved"
	b, e = RenderActivity(ActivityList{PageCursor: PageCursor{Limit: 10}, Activity: []repository.Activity{activity}}, false)
	invalidOutput(t, b, e)
	activity = testActivity()
	activity.At = time.Time{}
	b, e = RenderActivity(ActivityList{PageCursor: PageCursor{Limit: 10}, Activity: []repository.Activity{activity}}, false)
	invalidOutput(t, b, e)
}

func TestGuideFiniteTopicsReferencesAndRawPreflight(t *testing.T) {
	for _, topic := range []guide.Topic{guide.Attention, guide.ExplainCase, guide.SpendingAuthority, guide.Handover, guide.OwnerDraft} {
		r := guide.Request{Topic: topic}
		if topic == guide.ExplainCase || topic == guide.OwnerDraft {
			r.CaseID = testID(2)
		}
		b, e := RenderGuide(r, testSelection(), false)
		if e != nil {
			t.Fatal(topic, e)
		}
		n := document(t, b, false)
		if len(elements(n, "form")) != 1 || attribute(elements(n, "form")[0], "method") != "get" || len(elements(fields(n, "topic")[0], "option")) != 5 {
			t.Fatal("guide ordinary finite selection form missing")
		}
		if topic == guide.OwnerDraft && !strings.Contains(textContent(n), "not been saved or sent") {
			t.Fatal("guide preview claimed a send/save")
		}
	}
	for _, change := range []func(*guide.Selection){func(s *guide.Selection) { s.Sources[0].SHA256 = strings.Repeat("0", 64) }, func(s *guide.Selection) { s.Sources = nil }, func(s *guide.Selection) { s.Sources[0].Body = strings.Repeat("a", 65537) }, func(s *guide.Selection) { s.Cases[0].Title = "\xff" }, func(s *guide.Selection) { s.Sources = append(s.Sources, s.Sources[0]) }} {
		s := testSelection()
		change(&s)
		b, e := RenderGuide(guide.Request{Topic: guide.SpendingAuthority}, s, true)
		invalidOutput(t, b, e)
	}
	b, e := RenderGuide(guide.Request{Topic: "invented"}, testSelection(), true)
	invalidOutput(t, b, e)
	selection := testSelection()
	original := append([]string(nil), selection.Cases[0].SourceIDs...)
	if _, e := RenderGuide(guide.Request{Topic: guide.Attention}, selection, true); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(original, selection.Cases[0].SourceIDs) {
		t.Fatal("guide mutates caller references")
	}
	// A last oversized source rejects before guide hashes/copies any earlier data.
	selection.Sources = append(selection.Sources, guide.Source{ID: testID(11), Title: "Too large", Body: strings.Repeat("a", 65537), SHA256: strings.Repeat("0", 64)})
	if allocations := testing.AllocsPerRun(10, func() {
		b, e := RenderGuide(guide.Request{Topic: guide.Attention}, selection, true)
		invalidOutput(t, b, e)
	}); allocations != 0 {
		t.Fatalf("raw guide preflight allocated before rejecting: %v", allocations)
	}
}

func TestDraftRawPreflightPrecedesCaseCopy(t *testing.T) {
	d := testDraft()
	d.Body = strings.Repeat("a", 16001)
	v := CasePage{Case: testCase(), Draft: &d, CSRFToken: testToken()}
	if allocations := testing.AllocsPerRun(10, func() { b, e := RenderCase(v, true); invalidOutput(t, b, e) }); allocations != 0 {
		t.Fatalf("oversized draft accepted case copying first: %v allocations", allocations)
	}
}

func TestBoundedOutputAndConcurrentRendering(t *testing.T) {
	rows := make([]domain.Application, 100)
	for i := range rows {
		a := testApplication()
		a.ID = testID(100 + i)
		a.Items = make([]domain.ChecklistItem, 32)
		for j := range a.Items {
			a.Items[j] = domain.ChecklistItem{ID: testID(300 + j), Label: strings.Repeat("&", 200), HumanDecision: j == 31}
		}
		rows[i] = a
	}
	// A list shows only summaries and must still validate every nested field.
	rows[99].Items[0].Label = "\xff"
	b, e := RenderApplications(ApplicationList{PageCursor: PageCursor{Limit: 100}, Applications: rows}, true)
	invalidOutput(t, b, e)
	a := testApplication()
	a.Items = make([]domain.ChecklistItem, 32)
	for j := range a.Items {
		a.Items[j] = domain.ChecklistItem{ID: testID(300 + j), Label: strings.Repeat("&", 200), HumanDecision: j == 31}
	}
	if _, e := RenderApplication(ApplicationPage{Application: a, CSRFToken: testToken()}, false); e != nil {
		t.Fatal(e)
	}
	// Guide may legitimately expand 100 cases x 32 distinct source links past cap.
	selection := guide.Selection{}
	for i := 0; i < 32; i++ {
		body := "source"
		selection.Sources = append(selection.Sources, guide.Source{ID: testID(500 + i), Title: "Source", Body: body, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(body)))})
	}
	for i := 0; i < 100; i++ {
		c := testCase()
		c.ID = testID(100 + i)
		c.Title = strings.Repeat("&", 200)
		c.SourceIDs = nil
		for _, s := range selection.Sources {
			c.SourceIDs = append(c.SourceIDs, s.ID)
		}
		selection.Cases = append(selection.Cases, c)
	}
	b, e = RenderGuide(guide.Request{Topic: guide.Attention}, selection, false)
	invalidOutput(t, b, e)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			for j := 0; j < 10; j++ {
				b, e := RenderCase(CasePage{Case: testCase(), CSRFToken: testToken()}, true)
				if e != nil || !bytes.Contains(b, []byte("Unsent draft")) {
					t.Error("concurrent renderer failure", e)
				}
			}
		})
	}
	wg.Wait()
}

func TestErrorsAndActivityUTC(t *testing.T) {
	for _, kind := range []ViewError{Invalid, NotFound, Conflict, Unavailable, Unsupported} {
		for _, fragment := range []bool{false, true} {
			b, e := RenderError(kind, fragment)
			if e != nil {
				t.Fatal(e)
			}
			n := document(t, b, fragment)
			if len(elements(n, "a")) == 0 || !bytes.Contains(b, []byte(`role="alert"`)) {
				t.Fatal("error recovery unavailable")
			}
		}
	}
	b, e := RenderError("raw database diagnostics", false)
	invalidOutput(t, b, e)
	b, e = RenderActivity(ActivityList{PageCursor: PageCursor{Limit: 10}, Activity: []repository.Activity{testActivity()}}, true)
	if e != nil || !bytes.Contains(b, []byte("2026-01-02T02:04:05Z")) {
		t.Fatal("timestamp not supplied UTC", e)
	}
}

func TestEmptyPagesAndLargeValidGuideSource(t *testing.T) {
	views := []func() ([]byte, error){
		func() ([]byte, error) { return RenderCases(CaseList{PageCursor: PageCursor{Limit: 10}}, true) },
		func() ([]byte, error) {
			return RenderApplications(ApplicationList{PageCursor: PageCursor{Limit: 10}}, true)
		},
		func() ([]byte, error) {
			return RenderProcedures(ProcedureList{PageCursor: PageCursor{Limit: 10}}, true)
		},
		func() ([]byte, error) { return RenderActivity(ActivityList{PageCursor: PageCursor{Limit: 10}}, true) },
	}
	for _, view := range views {
		b, e := view()
		if e != nil || !strings.Contains(string(b), "in this page") {
			t.Fatal("empty page missing", e)
		}
		document(t, b, true)
	}
	s := testSelection()
	s.Sources[0].Body = strings.Repeat("a", 65536)
	s.Sources[0].SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(s.Sources[0].Body)))
	if _, e := RenderGuide(guide.Request{Topic: guide.SpendingAuthority}, s, true); e != nil {
		t.Fatal("valid maximum source rejected", e)
	}
}

// These files are synthetic presentation artifacts, never a server or session.
// With no output directory this test still checks every rendered artifact.
func TestStaticArtifacts(t *testing.T) {
	out := os.Getenv("AMOS_BASELINE_ARTIFACT_DIR")
	if out != "" {
		if e := os.Mkdir(out, 0700); e != nil {
			t.Fatal(e)
		}
	}
	p := testProperty()
	p.Name = "House <script>not executable</script>"
	c := testCase()
	c.Title = "Record <img src=x onerror=alert(1)>"
	d := testDraft()
	d.Body = "<script>not executable</script>\nUnsent synthetic text"
	artifacts := map[string]func() ([]byte, error){
		"properties": func() ([]byte, error) {
			return RenderProperties(PropertyList{PageCursor: PageCursor{Limit: 1}, Query: "roof & window", Properties: []repository.Property{p}}, false)
		},
		"case": func() ([]byte, error) { return RenderCase(CasePage{Case: c, Draft: &d, CSRFToken: testToken()}, false) },
		"application": func() ([]byte, error) {
			return RenderApplication(ApplicationPage{Application: testApplication(), CSRFToken: testToken()}, false)
		},
		"procedure": func() ([]byte, error) {
			return RenderProcedure(ProcedurePage{Procedure: testProcedure(), CSRFToken: testToken()}, false)
		},
		"guide": func() ([]byte, error) {
			return RenderGuide(guide.Request{Topic: guide.OwnerDraft, CaseID: testID(2)}, testSelection(), false)
		},
		"conflict": func() ([]byte, error) { return RenderError(Conflict, false) },
		"empty": func() ([]byte, error) {
			return RenderProperties(PropertyList{PageCursor: PageCursor{Limit: 10}}, false)
		},
	}
	for name, render := range artifacts {
		b, e := render()
		if e != nil {
			t.Fatal(e)
		}
		document(t, b, false)
		if out != "" {
			if e := os.WriteFile(filepath.Join(out, name+".html"), b, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
}
