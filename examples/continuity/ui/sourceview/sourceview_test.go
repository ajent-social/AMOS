package sourceview_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ajent-social/amos/examples/continuity/repository"
	"github.com/ajent-social/amos/examples/continuity/ui/sourceview"
	"golang.org/x/net/html"
)

const id = "01900000-0000-7000-8000-000000000001"

func source() repository.Source {
	s := repository.Source{ID: id, Kind: "document", Title: `  <img src=x onerror="alert(1)"> & 源  `, Body: "\n\t <script>alert('x')</script>\n& <a href=javascript:alert(1)>x</a>\n{{.Secret}} [link](https://example.invalid)  "}
	s.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(s.Body)))
	return s
}
func summary(s repository.Source) repository.SourceSummary {
	return repository.SourceSummary{ID: s.ID, Kind: s.Kind, Title: s.Title, SHA256: s.SHA256}
}
func parse(t *testing.T, b []byte) *html.Node {
	t.Helper()
	n, e := html.Parse(bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	return n
}
func nodes(n *html.Node, tag string) []*html.Node {
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
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func content(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var s strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		s.WriteString(content(c))
	}
	return s.String()
}
func one(t *testing.T, n *html.Node, tag string) *html.Node {
	t.Helper()
	ns := nodes(n, tag)
	if len(ns) != 1 {
		t.Fatalf("%s count=%d", tag, len(ns))
	}
	return ns[0]
}
func invalid(t *testing.T, b []byte, e error) {
	t.Helper()
	if b != nil || e != sourceview.ErrInvalid {
		t.Fatalf("want nil/exact ErrInvalid, got %q/%v", b, e)
	}
}
func structure(t *testing.T, b []byte, fragment bool) *html.Node {
	t.Helper()
	n := parse(t, b)
	s := one(t, n, "section")
	if attr(s, "id") != "continuity-source-content" {
		t.Fatal("section target")
	}
	for _, tag := range []string{"script", "img", "iframe"} {
		if len(nodes(n, tag)) != 0 {
			t.Fatalf("injected %s", tag)
		}
	}
	if fragment {
		body := one(t, n, "body")
		if body.FirstChild != s || s.NextSibling != nil {
			t.Fatal("fragment must contain only section")
		}
		for _, tag := range []string{"main", "link", "style", "meta", "title"} {
			if len(nodes(n, tag)) != 0 {
				t.Fatalf("fragment includes %s", tag)
			}
		}
		if !bytes.HasPrefix(b, []byte("<section ")) {
			t.Fatal("not bare section")
		}
	} else {
		if attr(one(t, n, "html"), "lang") != "en" || attr(one(t, n, "main"), "id") != "main-content" {
			t.Fatal("document landmarks")
		}
		if attr(one(t, n, "link"), "href") != "/assets/base.css" {
			t.Fatal("asset")
		}
		if len(nodes(n, "meta")) != 2 {
			t.Fatal("metadata")
		}
		if attr(nodes(n, "a")[0], "href") != "#main-content" {
			t.Fatal("skip link")
		}
	}
	for _, a := range nodes(n, "a") {
		u, e := url.Parse(attr(a, "href"))
		if e != nil || u.IsAbs() || u.Host != "" || (u.Path != "" && !strings.HasPrefix(u.Path, "/continuity/sources")) {
			t.Fatal("unsafe link")
		}
	}
	return n
}
func TestDetailPlainBodyAndStructure(t *testing.T) {
	t.Parallel()
	for _, fragment := range []bool{false, true} {
		for _, kind := range []string{"document", "correspondence"} {
			s := source()
			s.Kind = kind
			original := s
			b, e := sourceview.RenderSource(s, fragment)
			if e != nil {
				t.Fatal(e)
			}
			n := structure(t, b, fragment)
			if content(one(t, n, "pre")) != s.Body {
				t.Fatalf("original body changed: %q", content(one(t, n, "pre")))
			}
			if content(one(t, n, "h1")) != strings.TrimSpace(s.Title) || content(one(t, n, "code")) != s.SHA256 {
				t.Fatal("source text changed")
			}
			if s != original {
				t.Fatal("input mutated")
			}
		}
	}
}
func TestListFormAndPagination(t *testing.T) {
	t.Parallel()
	s := source()
	q := `  <svg onload="x"> + & /?% 源  `
	for _, fragment := range []bool{false, true} {
		m := sourceview.SourceList{Query: q, Kind: "document", After: "01900000-0000-7000-8000-000000000000", Limit: 1, Sources: []repository.SourceSummary{summary(s)}}
		before := m.Sources[0]
		b, e := sourceview.RenderSources(m, fragment)
		if e != nil {
			t.Fatal(e)
		}
		n := structure(t, b, fragment)
		f := one(t, n, "form")
		if attr(f, "method") != "get" || attr(f, "action") != "/continuity/sources" {
			t.Fatal("GET search")
		}
		values := map[string]string{}
		for _, in := range nodes(f, "input") {
			values[attr(in, "name")] = attr(in, "value")
			if attr(in, "name") == "q" && attr(in, "maxlength") != "120" {
				t.Fatal("maxlength")
			}
		}
		if !reflect.DeepEqual(values, map[string]string{"q": strings.TrimSpace(q), "limit": "1"}) {
			t.Fatalf("form cursor/reset values: %#v", values)
		}
		sel := one(t, n, "select")
		if attr(sel, "name") != "kind" {
			t.Fatal("kind name")
		}
		selected := ""
		for _, o := range nodes(sel, "option") {
			for _, a := range o.Attr {
				if a.Key == "selected" {
					selected = attr(o, "value")
				}
			}
		}
		if selected != "document" {
			t.Fatal("selected kind")
		}
		for _, field := range append(nodes(f, "input")[:1], sel) {
			found := false
			for _, label := range nodes(f, "label") {
				if attr(label, "for") == attr(field, "id") {
					found = true
				}
			}
			if !found {
				t.Fatal("unlabelled field")
			}
		}
		next := false
		detail := false
		for _, a := range nodes(n, "a") {
			if content(a) == "Try next page" {
				next = true
				u, e := url.Parse(attr(a, "href"))
				if e != nil {
					t.Fatal(e)
				}
				want := url.Values{"q": {strings.TrimSpace(q)}, "kind": {"document"}, "limit": {"1"}, "after": {id}}
				if u.Path != "/continuity/sources" || !reflect.DeepEqual(u.Query(), want) {
					t.Fatalf("next query %v", u)
				}
			}
			if content(a) == strings.TrimSpace(s.Title) {
				detail = attr(a, "href") == "/continuity/sources/"+id
			}
		}
		if !next || !detail {
			t.Fatal("missing navigation")
		}
		if m.Sources[0] != before || m.Query != q {
			t.Fatal("input mutated")
		}
	}
}
func TestEmptyAndErrors(t *testing.T) {
	t.Parallel()
	for _, fragment := range []bool{false, true} {
		b, e := sourceview.RenderSources(sourceview.SourceList{Limit: 2}, fragment)
		if e != nil {
			t.Fatal(e)
		}
		n := structure(t, b, fragment)
		if !strings.Contains(content(n), "No sources in this page.") || strings.Contains(content(n), "Try next page") {
			t.Fatal("empty page")
		}
		for kind, want := range map[sourceview.SourceError]string{sourceview.SourceNotFound: "Source not found.", sourceview.SourceUnavailable: "Sources are temporarily unavailable."} {
			b, e = sourceview.RenderSourceError(kind, fragment)
			if e != nil {
				t.Fatal(e)
			}
			n = structure(t, b, fragment)
			if !strings.Contains(content(n), want) || len(nodes(n, "form")) != 0 || len(nodes(n, "pre")) != 0 {
				t.Fatal("error semantics")
			}
		}
		b, e = sourceview.RenderSourceError("diagnostic <secret>", fragment)
		invalid(t, b, e)
	}
}
func TestInvalidDetail(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*repository.Source){
		"wrong digest":              func(s *repository.Source) { s.SHA256 = strings.Repeat("0", 64) },
		"body whitespace digest":    func(s *repository.Source) { s.Body += " " },
		"digest uppercase":          func(s *repository.Source) { s.SHA256 = strings.ToUpper(s.SHA256) },
		"digest short":              func(s *repository.Source) { s.SHA256 = "abc" },
		"digest huge":               func(s *repository.Source) { s.SHA256 = strings.Repeat("a", 65) },
		"id uppercase":              func(s *repository.Source) { s.ID = "01900000-0000-7000-A000-000000000001" },
		"id version":                func(s *repository.Source) { s.ID = "01900000-0000-4000-8000-000000000001" },
		"id variant":                func(s *repository.Source) { s.ID = "01900000-0000-7000-c000-000000000001" },
		"id hyphen":                 func(s *repository.Source) { s.ID = "01900000x0000-7000-8000-000000000001" },
		"id huge":                   func(s *repository.Source) { s.ID = id + " " },
		"kind":                      func(s *repository.Source) { s.Kind = "Document" },
		"kind huge":                 func(s *repository.Source) { s.Kind = strings.Repeat("a", 15) },
		"title empty":               func(s *repository.Source) { s.Title = " " },
		"title rune limit":          func(s *repository.Source) { s.Title = strings.Repeat("界", 201) },
		"title raw limit":           func(s *repository.Source) { s.Title = strings.Repeat(" ", 801) },
		"title control before trim": func(s *repository.Source) { s.Title = "\tvalid" },
		"title unicode control":     func(s *repository.Source) { s.Title = "valid\u0085" },
		"title invalid utf8":        func(s *repository.Source) { s.Title = "\xff" },
		"body empty":                func(s *repository.Source) { s.Body = "" },
		"body blank":                func(s *repository.Source) { s.Body = " \n\t" },
		"body raw limit":            func(s *repository.Source) { s.Body = strings.Repeat("a", 65537) },
		"body carriage return":      func(s *repository.Source) { s.Body = "a\r" },
		"body del":                  func(s *repository.Source) { s.Body = "a\x7f" },
		"body nul":                  func(s *repository.Source) { s.Body = "a\x00" },
		"body invalid utf8":         func(s *repository.Source) { s.Body = "a\xff" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := source()
			change(&s)
			if strings.HasPrefix(name, "body ") && name != "body whitespace digest" {
				s.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(s.Body)))
			}
			for _, f := range []bool{false, true} {
				b, e := sourceview.RenderSource(s, f)
				invalid(t, b, e)
			}
		})
	}
}
func TestInvalidLists(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*sourceview.SourceList){
		"zero limit": func(m *sourceview.SourceList) { m.Limit = 0 }, "over limit": func(m *sourceview.SourceList) { m.Limit = 101 },
		"too many":  func(m *sourceview.SourceList) { m.Sources = append(m.Sources, m.Sources[0]) },
		"query raw": func(m *sourceview.SourceList) { m.Query = strings.Repeat(" ", 481) }, "query runes": func(m *sourceview.SourceList) { m.Query = strings.Repeat("a", 121) },
		"query blank": func(m *sourceview.SourceList) { m.Query = " " }, "query control": func(m *sourceview.SourceList) { m.Query = "a\n" }, "query utf8": func(m *sourceview.SourceList) { m.Query = "\xff" },
		"after": func(m *sourceview.SourceList) { m.After = "bad" }, "after raw": func(m *sourceview.SourceList) { m.After = id + " " }, "after equal": func(m *sourceview.SourceList) { m.After = id }, "after greater": func(m *sourceview.SourceList) { m.After = "01900000-0000-7000-8000-000000000002" },
		"filter unknown": func(m *sourceview.SourceList) { m.Kind = "other" }, "filter raw": func(m *sourceview.SourceList) { m.Kind = strings.Repeat("x", 15) }, "filter mismatch": func(m *sourceview.SourceList) { m.Kind = "correspondence" },
		"row id": func(m *sourceview.SourceList) { m.Sources[0].ID = "bad" }, "row raw id": func(m *sourceview.SourceList) { m.Sources[0].ID = id + " " },
		"row kind": func(m *sourceview.SourceList) { m.Sources[0].Kind = "other" }, "row title": func(m *sourceview.SourceList) { m.Sources[0].Title = "\n" }, "row digest": func(m *sourceview.SourceList) { m.Sources[0].SHA256 = strings.Repeat("g", 64) },
		"duplicates": func(m *sourceview.SourceList) { m.Limit = 2; m.Sources = append(m.Sources, m.Sources[0]) },
		"descending": func(m *sourceview.SourceList) {
			m.Limit = 2
			m.Sources = append(m.Sources, m.Sources[0])
			m.Sources[0].ID = "01900000-0000-7000-8000-000000000002"
		},
		"later invalid": func(m *sourceview.SourceList) {
			m.Limit = 2
			m.Sources = append(m.Sources, m.Sources[0])
			m.Sources[1].ID = "01900000-0000-7000-8000-000000000002"
			m.Sources[1].Title = "\xff"
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			m := sourceview.SourceList{Limit: 1, Sources: []repository.SourceSummary{summary(source())}}
			change(&m)
			for _, f := range []bool{false, true} {
				b, e := sourceview.RenderSources(m, f)
				invalid(t, b, e)
			}
		})
	}
}
func TestMaximumBounds(t *testing.T) {
	t.Parallel()
	s := source()
	s.Title = strings.Repeat("🦀", 200)
	s.Body = strings.Repeat("'", 65536)
	s.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(s.Body)))
	for _, f := range []bool{false, true} {
		b, e := sourceview.RenderSource(s, f)
		if e != nil {
			t.Fatal(e)
		}
		if len(b) > 512*1024 || content(one(t, parse(t, b), "pre")) != s.Body {
			t.Fatal("maximum body")
		}
	}
	m := sourceview.SourceList{Query: strings.Repeat("🦀", 120), Limit: 100}
	for i := 1; i <= 100; i++ {
		r := summary(s)
		r.ID = fmt.Sprintf("01900000-0000-7000-8000-%012x", i)
		m.Sources = append(m.Sources, r)
	}
	b, e := sourceview.RenderSources(m, false)
	if e != nil {
		t.Fatal(e)
	}
	if len(b) > 512*1024 || len(nodes(parse(t, b), "li")) != 100 {
		t.Fatal("maximum page")
	}
}
func TestCopiesAndConcurrency(t *testing.T) {
	t.Parallel()
	s := source()
	m := sourceview.SourceList{Limit: 1, Sources: []repository.SourceSummary{summary(s)}}
	before := m.Sources[0]
	renders := []func() ([]byte, error){func() ([]byte, error) { return sourceview.RenderSource(s, false) }, func() ([]byte, error) { return sourceview.RenderSources(m, true) }, func() ([]byte, error) { return sourceview.RenderSourceError(sourceview.SourceNotFound, false) }}
	for _, render := range renders {
		expected, e := render()
		if e != nil {
			t.Fatal(e)
		}
		b, e := render()
		if e != nil {
			t.Fatal(e)
		}
		b[0] = '!'
		again, e := render()
		if e != nil || !bytes.Equal(expected, again) {
			t.Fatal("shared output")
		}
		var wg sync.WaitGroup
		for i := 0; i < 24; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				b, e := render()
				if e != nil || !bytes.Equal(b, expected) {
					t.Error("concurrent rendering")
				}
			}()
		}
		wg.Wait()
	}
	if m.Sources[0] != before {
		t.Fatal("shared input changed")
	}
}

// TestStaticArtifacts is an opt-in synthetic static browser fixture generator.
// Pure unit runs do not claim browser qualification or access any service.
func TestStaticArtifacts(t *testing.T) {
	dir := os.Getenv("SOURCEVIEW_STATIC_DIR")
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute output directory required")
	}
	s := source()
	s.Body = "\n" + strings.Repeat("long<&>text", 6000) + "\n"
	s.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(s.Body)))
	detail, e := sourceview.RenderSource(s, false)
	if e != nil {
		t.Fatal(e)
	}
	list, e := sourceview.RenderSources(sourceview.SourceList{Query: `<script> & +`, Limit: 1, Sources: []repository.SourceSummary{summary(s)}}, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	for name, b := range map[string][]byte{"detail.html": detail, "list.html": list} {
		if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
