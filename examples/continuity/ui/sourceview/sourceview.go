// Package sourceview renders bounded, caller-supplied source values. The caller
// owns current authority, retrieval, transaction outcome and HTTP behavior.
package sourceview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ajent-social/amos/examples/continuity/repository"
)

// SourceList is one supplied page, not an inventory or an authority assertion.
type SourceList struct {
	Query, Kind, After string
	Limit              int
	Sources            []repository.SourceSummary
}

// SourceError selects fixed public wording; it carries no diagnostics.
type SourceError string

const (
	SourceNotFound    SourceError = "not_found"
	SourceUnavailable SourceError = "unavailable"
)

// ErrInvalid is returned with nil bytes for every validation or render failure.
var ErrInvalid = errors.New("invalid continuity source view")

const maxOutput = 512 * 1024

type view struct {
	Title, Mode, Message, Next string
	List                       SourceList
	Source                     repository.Source
}

// RenderSources renders a supplied page without retrieving or authorizing it.
func RenderSources(model SourceList, fragment bool) ([]byte, error) {
	if len(model.Query) > 480 || len(model.Kind) > 14 || len(model.After) > 36 || model.Limit < 1 || model.Limit > 100 || len(model.Sources) > model.Limit {
		return nil, ErrInvalid
	}
	if model.After != "" && !validID(model.After) {
		return nil, ErrInvalid
	}
	if model.Kind != "" && !validKind(model.Kind) {
		return nil, ErrInvalid
	}
	if model.Query != "" {
		q, ok := text(model.Query, 120)
		if !ok {
			return nil, ErrInvalid
		}
		model.Query = q
	}
	// Check every raw row before copying or scanning any row text.
	for _, s := range model.Sources {
		if !rawSummary(s) {
			return nil, ErrInvalid
		}
	}
	previous := model.After
	for _, s := range model.Sources {
		if !validID(s.ID) || s.ID <= previous || !validKind(s.Kind) || (model.Kind != "" && s.Kind != model.Kind) || !validDigest(s.SHA256) {
			return nil, ErrInvalid
		}
		if _, ok := text(s.Title, 200); !ok {
			return nil, ErrInvalid
		}
		previous = s.ID
	}
	rows := make([]repository.SourceSummary, len(model.Sources))
	copy(rows, model.Sources)
	for i := range rows {
		rows[i].Title = strings.TrimSpace(rows[i].Title)
	}
	model.Sources = rows
	v := view{Title: "Sources", Mode: "list", List: model}
	if len(rows) == model.Limit {
		q := url.Values{"q": {model.Query}, "kind": {model.Kind}, "limit": {strconv.Itoa(model.Limit)}, "after": {rows[len(rows)-1].ID}}
		v.Next = "/continuity/sources?" + q.Encode()
	}
	return render(v, fragment)
}

// RenderSource checks byte consistency, not authenticity or disclosure authority.
func RenderSource(source repository.Source, fragment bool) ([]byte, error) {
	if !rawSummary(repository.SourceSummary{ID: source.ID, Kind: source.Kind, Title: source.Title, SHA256: source.SHA256}) || len(source.Body) > 65536 {
		return nil, ErrInvalid
	}
	title, ok := text(source.Title, 200)
	if !ok || !validID(source.ID) || !validKind(source.Kind) || !validDigest(source.SHA256) || !utf8.ValidString(source.Body) || strings.TrimSpace(source.Body) == "" {
		return nil, ErrInvalid
	}
	for _, c := range source.Body {
		if (c < 32 || c == 127) && c != '\t' && c != '\n' {
			return nil, ErrInvalid
		}
	}
	sum := sha256.Sum256([]byte(source.Body))
	if source.SHA256 != hex.EncodeToString(sum[:]) {
		return nil, ErrInvalid
	}
	source.Title = title
	return render(view{Title: source.Title, Mode: "detail", Source: source}, fragment)
}

// RenderSourceError produces fixed, non-enumerating application wording.
func RenderSourceError(kind SourceError, fragment bool) ([]byte, error) {
	var message string
	switch kind {
	case SourceNotFound:
		message = "Source not found."
	case SourceUnavailable:
		message = "Sources are temporarily unavailable."
	default:
		return nil, ErrInvalid
	}
	return render(view{Title: "Sources", Mode: "error", Message: message}, fragment)
}

func rawSummary(s repository.SourceSummary) bool {
	return len(s.ID) <= 36 && len(s.Kind) <= 14 && len(s.Title) <= 800 && len(s.SHA256) <= 64
}
func validKind(s string) bool { return s == "document" || s == "correspondence" }
func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func validID(s string) bool {
	if len(s) != 36 || s[14] != '7' || !strings.ContainsRune("89ab", rune(s[19])) {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func text(s string, max int) (string, bool) {
	if !utf8.ValidString(s) {
		return "", false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return "", false
		}
	}
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n > 0 && n <= max
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > maxOutput-b.Len() {
		return 0, ErrInvalid
	}
	return b.Buffer.Write(p)
}
func render(v view, fragment bool) ([]byte, error) {
	var b boundedBuffer
	name := "full"
	if fragment {
		name = "section"
	}
	if err := templates.ExecuteTemplate(&b, name, v); err != nil {
		return nil, ErrInvalid
	}
	return b.Bytes(), nil
}

// Parsed once and never modified. Every execution has its own bounded writer.
var templates = template.Must(template.New("sourceview").Parse(`
{{define "full"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}} — Continuity</title><link rel="stylesheet" href="/assets/base.css"><style>#continuity-source-content{overflow-wrap:anywhere}#continuity-source-content pre{white-space:pre-wrap;overflow-wrap:anywhere}#continuity-source-content input,#continuity-source-content select{max-width:100%;box-sizing:border-box}</style></head><body><a href="#main-content">Skip to content</a><main id="main-content">{{template "section" .}}</main></body></html>{{end}}
{{define "section"}}<section id="continuity-source-content" aria-labelledby="source-view-heading">
{{if eq .Mode "list"}}<h1 id="source-view-heading">Sources</h1><p>This is a supplied page. Its retrieval scope is caller-owned.</p>
<form method="get" action="/continuity/sources"><label for="source-query">Search sources</label><input id="source-query" name="q" maxlength="120" value="{{.List.Query}}"><label for="source-kind">Source kind</label><select id="source-kind" name="kind"><option value=""{{if eq .List.Kind ""}} selected{{end}}>All kinds</option><option value="correspondence"{{if eq .List.Kind "correspondence"}} selected{{end}}>Correspondence</option><option value="document"{{if eq .List.Kind "document"}} selected{{end}}>Document</option></select><input type="hidden" name="limit" value="{{.List.Limit}}"><button type="submit">Search</button></form>
{{if .List.Sources}}<ul>{{range .List.Sources}}<li><a href="/continuity/sources/{{.ID}}">{{.Title}}</a><p>Kind: {{.Kind}}</p><p>Supplied SHA-256: <code>{{.SHA256}}</code></p></li>{{end}}</ul>{{else}}<p>No sources in this page.</p>{{end}}
{{if .Next}}<a href="{{.Next}}">Try next page</a>{{end}}
{{else if eq .Mode "detail"}}<h1 id="source-view-heading">{{.Source.Title}}</h1><p>Supplied source snapshot. Byte consistency does not establish authenticity or disclosure authority.</p><p>Kind: {{.Source.Kind}}</p><p>SHA-256: <code>{{.Source.SHA256}}</code></p><pre>{{"\n"}}{{.Source.Body}}</pre>
{{else}}<h1 id="source-view-heading">Sources</h1><p>{{.Message}}</p>{{end}}
<a href="/continuity/sources">Back to sources</a></section>{{end}}`))
