// Package baselineview renders supplied continuity values. It performs no I/O,
// authority checks, mutations or sending. Callers own committed publication.
package baselineview

import (
	"bytes"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/ajent-social/amos/examples/continuity/guide"
	"github.com/ajent-social/amos/examples/continuity/repository"
)

type PageCursor struct {
	After string
	Limit int
}
type PropertyList struct {
	PageCursor
	Query      string
	Properties []repository.Property
}
type CaseList struct {
	PageCursor
	Cases []domain.Case
}
type ApplicationList struct {
	PageCursor
	Applications []domain.Application
}
type ProcedureList struct {
	PageCursor
	Procedures []domain.Procedure
}
type ActivityList struct {
	PageCursor
	Activity []repository.Activity
}
type CasePage struct {
	Case      domain.Case
	Draft     *domain.Draft
	CSRFToken string
}
type ApplicationPage struct {
	Application domain.Application
	CSRFToken   string
}
type ProcedurePage struct {
	Procedure domain.Procedure
	CSRFToken string
}
type ViewError string

const (
	Invalid     ViewError = "invalid"
	NotFound    ViewError = "not_found"
	Conflict    ViewError = "conflict"
	Unavailable ViewError = "unavailable"
	Unsupported ViewError = "unsupported"
)

var ErrInvalid = errors.New("invalid continuity baseline view")

const maxOutput = 512 * 1024

type page struct {
	Title string
	Data  any
	Next  string
}
type guidePage struct {
	Request  guide.Request
	Cases    []domain.Case
	Response guide.Response
}
type boundedBuffer struct{ buf bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > maxOutput-b.buf.Len() {
		return 0, ErrInvalid
	}
	return b.buf.Write(p)
}
func render(name, title string, data any, next string, fragment bool) ([]byte, error) {
	b := new(boundedBuffer)
	v := page{title, data, next}
	if !fragment {
		if err := templates.ExecuteTemplate(b, "header", v); err != nil {
			return nil, ErrInvalid
		}
	}
	for _, part := range []string{"start", name, "end"} {
		if err := templates.ExecuteTemplate(b, part, v); err != nil {
			return nil, ErrInvalid
		}
	}
	if !fragment {
		if err := templates.ExecuteTemplate(b, "footer", v); err != nil {
			return nil, ErrInvalid
		}
	}
	return b.buf.Bytes(), nil
}
func nextURL(route string, p PageCursor, n int, last, query string) string {
	if n != p.Limit {
		return ""
	}
	v := url.Values{"after": {last}, "limit": {strconv.Itoa(p.Limit)}}
	if query != "" {
		v.Set("q", query)
	}
	return "/continuity/" + route + "?" + v.Encode()
}
func listNext[T any](route string, p PageCursor, rows []T, id func(T) string, query string) string {
	if len(rows) == 0 {
		return ""
	}
	return nextURL(route, p, len(rows), id(rows[len(rows)-1]), query)
}
func propertyID(v repository.Property) string   { return v.ID }
func caseID(v domain.Case) string               { return v.ID }
func applicationID(v domain.Application) string { return v.ID }
func procedureID(v domain.Procedure) string     { return v.ID }
func activityID(v repository.Activity) string   { return v.ID }

func RenderProperties(v PropertyList, fragment bool) ([]byte, error) {
	if len(v.Query) > 480 || !rawList(v.PageCursor, v.Properties, rawProperty) {
		return nil, ErrInvalid
	}
	if !utf8.ValidString(v.Query) {
		return nil, ErrInvalid
	}
	for _, r := range v.Query {
		if unicode.IsControl(r) {
			return nil, ErrInvalid
		}
	}
	v.Query = strings.TrimSpace(v.Query)
	if utf8.RuneCountInString(v.Query) > 120 {
		return nil, ErrInvalid
	}
	rows, ok := listValues(v.PageCursor, v.Properties, rawProperty, propertyValue, propertyID)
	if !ok {
		return nil, ErrInvalid
	}
	v.Properties = rows
	return render("properties", "Properties", v, listNext("properties", v.PageCursor, rows, propertyID, v.Query), fragment)
}
func RenderProperty(v repository.Property, fragment bool) ([]byte, error) {
	if !rawProperty(v) {
		return nil, ErrInvalid
	}
	v, ok := propertyValue(v)
	if !ok {
		return nil, ErrInvalid
	}
	return render("property", v.Name, v, "", fragment)
}
func RenderCases(v CaseList, fragment bool) ([]byte, error) {
	rows, ok := listValues(v.PageCursor, v.Cases, rawCase, caseValue, caseID)
	if !ok {
		return nil, ErrInvalid
	}
	v.Cases = rows
	return render("cases", "Cases", v, listNext("cases", v.PageCursor, rows, caseID, ""), fragment)
}
func RenderCase(v CasePage, fragment bool) ([]byte, error) {
	if !rawCase(v.Case) || (v.Draft != nil && !rawDraft(*v.Draft)) || !tokenOK(v.CSRFToken) {
		return nil, ErrInvalid
	}
	c, ok := caseValue(v.Case)
	if !ok {
		return nil, ErrInvalid
	}
	v.Case = c
	if v.Draft != nil {
		d, ok := draftValue(*v.Draft, c)
		if !ok {
			return nil, ErrInvalid
		}
		v.Draft = &d
	}
	return render("case", c.Title, v, "", fragment)
}
func RenderApplications(v ApplicationList, fragment bool) ([]byte, error) {
	rows, ok := listValues(v.PageCursor, v.Applications, rawApplication, applicationValue, applicationID)
	if !ok {
		return nil, ErrInvalid
	}
	v.Applications = rows
	return render("applications", "Applications", v, listNext("applications", v.PageCursor, rows, applicationID, ""), fragment)
}
func RenderApplication(v ApplicationPage, fragment bool) ([]byte, error) {
	if !rawApplication(v.Application) || !tokenOK(v.CSRFToken) {
		return nil, ErrInvalid
	}
	a, ok := applicationValue(v.Application)
	if !ok {
		return nil, ErrInvalid
	}
	v.Application = a
	return render("application", "Application checklist", v, "", fragment)
}
func RenderProcedures(v ProcedureList, fragment bool) ([]byte, error) {
	rows, ok := listValues(v.PageCursor, v.Procedures, rawProcedure, procedureValue, procedureID)
	if !ok {
		return nil, ErrInvalid
	}
	v.Procedures = rows
	return render("procedures", "Procedures", v, listNext("procedures", v.PageCursor, rows, procedureID, ""), fragment)
}
func RenderProcedure(v ProcedurePage, fragment bool) ([]byte, error) {
	if !rawProcedure(v.Procedure) || !tokenOK(v.CSRFToken) {
		return nil, ErrInvalid
	}
	p, ok := procedureValue(v.Procedure)
	if !ok {
		return nil, ErrInvalid
	}
	v.Procedure = p
	return render("procedure", p.Title, v, "", fragment)
}
func RenderActivity(v ActivityList, fragment bool) ([]byte, error) {
	rows, ok := listValues(v.PageCursor, v.Activity, rawActivity, activityValue, activityID)
	if !ok {
		return nil, ErrInvalid
	}
	v.Activity = rows
	return render("activity", "Activity", v, listNext("activity", v.PageCursor, rows, activityID, ""), fragment)
}
func RenderGuide(request guide.Request, selection guide.Selection, fragment bool) ([]byte, error) {
	// Complete raw preflight before Answer scans, hashes or copies any content.
	if len(request.Topic) > 18 || len(request.CaseID) > 36 || len(selection.Cases) > 100 || len(selection.Sources) > 100 {
		return nil, ErrInvalid
	}
	for _, c := range selection.Cases {
		if !rawCase(c) {
			return nil, ErrInvalid
		}
	}
	for _, s := range selection.Sources {
		if len(s.ID) > 36 || len(s.Title) > 800 || len(s.Body) > 65536 || len(s.SHA256) != 64 {
			return nil, ErrInvalid
		}
	}
	answer, err := guide.Answer(request, selection)
	if err != nil {
		return nil, ErrInvalid
	}
	cases := make([]domain.Case, len(selection.Cases))
	for i, c := range selection.Cases {
		v, ok := caseValue(c)
		if !ok {
			return nil, ErrInvalid
		}
		cases[i] = v
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return render("guide", "Guide", guidePage{request, cases, answer}, "", fragment)
}
func RenderError(kind ViewError, fragment bool) ([]byte, error) {
	var message string
	switch kind {
	case Invalid:
		message = "The supplied request is invalid. Review the fields and try again."
	case NotFound:
		message = "The requested record is not available."
	case Conflict:
		message = "The record changed. Reload it before submitting again. Your changes have not been saved."
	case Unavailable:
		message = "The service is unavailable. Your changes have not been saved. Try again later."
	case Unsupported:
		message = "This request is not supported by the current application."
	default:
		return nil, ErrInvalid
	}
	return render("error", "Unable to complete request", message, "", fragment)
}
