// Package render provides the default escaped server-rendered page shell and
// the public seam used by applications that supply another renderer.
package render

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const ContractVersion = "1.0"
const MainTarget = "main-content"

var (
	ErrInvalidView = errors.New("invalid UI view model")
	ErrUnknownPage = errors.New("unknown UI page")
	localPath      = regexp.MustCompile(`^/(?:[A-Za-z0-9._~-]+(?:/[A-Za-z0-9._~-]+)*)?(?:\?[A-Za-z0-9._~%=&+-]*)?$`)
)

// Page identifies a versioned shared page. Business landing pages remain
// application-owned and should be rendered by their own handler.
type Page struct {
	ID      string
	Version string
}

// ActionURL is a local form or navigation destination. Path must be an
// application route beginning with one slash; external and protocol-relative
// destinations are rejected.
type ActionURL struct {
	Name string
	Path string
}

// ViewModel contains only display data. All strings are treated as untrusted
// text and escaped by html/template.
type ViewModel struct {
	Page        Page
	Title       string
	Heading     string
	ProfileName string
	RequestID   string
	Status      string
	Navigation  []Link
	Actions     []ActionURL
	FieldErrors []FieldError
	Flash       []Flash
	Form        *Form
}

type Link struct{ Label, Path string }
type FieldError struct{ Field, Message string }
type Flash struct{ Kind, Message string }
type Form struct {
	Action    string
	Method    string
	Label     string
	Field     string
	Value     string
	Submit    string
	CSRFToken string // operation-bound form token; never a session or cookie credential
}

// Renderer renders a named shared page for a validated immutable view model.
// Implementations must preserve escaping, status semantics and no-store headers.
type Renderer interface {
	Render(context.Context, http.ResponseWriter, *http.Request, Page, ViewModel) error
}

// ActionResolver supplies safe local URLs for named actions. Implementations
// must not derive permission from presentation state.
type ActionResolver interface {
	ResolveAction(context.Context, string) (ActionURL, bool)
}

// Default is the html/template renderer. It has no unsafe HTML escape hatch.
type Default struct{ templates *template.Template }

func NewDefault() (*Default, error) {
	t, err := template.New("shell").Option("missingkey=error").Parse(documentTemplate)
	if err != nil {
		return nil, err
	}
	return &Default{templates: t}, nil
}

func (d *Default) Render(_ context.Context, w http.ResponseWriter, r *http.Request, page Page, model ViewModel) error {
	if d == nil || d.templates == nil || r == nil {
		return ErrInvalidView
	}
	model.Page = page
	if !validPage(page) || !validModel(model) {
		return ErrInvalidView
	}
	var body bytes.Buffer
	fragment := isMainFragment(r)
	name := "document"
	if fragment {
		name = "fragment"
	}
	if err := d.templates.ExecuteTemplate(&body, name, model); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "HX-Request")
	w.Header().Add("Vary", "HX-Target")
	w.WriteHeader(http.StatusOK)
	_, err := io.Copy(w, &body)
	return err
}

func isMainFragment(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("HX-Request")), "true") &&
		r.Header.Get("HX-Target") == MainTarget
}

func validPage(p Page) bool {
	return p.ID != "" && p.Version == ContractVersion && !strings.ContainsAny(p.ID, "<>\"' /")
}

func validModel(m ViewModel) bool {
	if m.Title == "" || m.Heading == "" {
		return false
	}
	for _, link := range m.Navigation {
		if link.Label == "" || !safeLocalPath(link.Path) {
			return false
		}
	}
	for _, action := range m.Actions {
		if action.Name == "" || !safeLocalPath(action.Path) {
			return false
		}
	}
	for _, e := range m.FieldErrors {
		if e.Field == "" || e.Message == "" {
			return false
		}
	}
	for _, f := range m.Flash {
		if f.Kind != "status" && f.Kind != "error" {
			return false
		}
	}
	if m.Form != nil {
		if !safeLocalPath(m.Form.Action) || m.Form.Method != http.MethodPost || m.Form.Label == "" || m.Form.Field == "" || m.Form.Submit == "" {
			return false
		}
	}
	return true
}

func safeLocalPath(path string) bool {
	if !localPath.MatchString(path) || strings.HasPrefix(path, "//") {
		return false
	}
	pathOnly, _, _ := strings.Cut(path, "?")
	for _, segment := range strings.Split(pathOnly, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

const documentTemplate = `{{define "document"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}}</title><link rel="stylesheet" href="/assets/base.css"></head>
<body><a class="skip-link" href="#main-content">Skip to main content</a><header class="site-header"><a href="/" class="brand">AMOS</a><nav aria-label="Primary">{{range .Navigation}}<a href="{{.Path}}">{{.Label}}</a>{{end}}</nav></header>
<main id="main-content" tabindex="-1">{{template "fragment" .}}</main><footer><p>Request reference: <span>{{.RequestID}}</span></p></footer></body></html>{{end}}
{{define "fragment"}}<h1>{{.Heading}}</h1>{{if .ProfileName}}<p class="profile-name">{{.ProfileName}}</p>{{end}}
{{range .Flash}}<p class="flash flash-{{.Kind}}" role="{{if eq .Kind "error"}}alert{{else}}status{{end}}" aria-live="polite">{{.Message}}</p>{{end}}
{{if .FieldErrors}}<section class="error-summary" tabindex="-1" aria-labelledby="error-title"><h2 id="error-title">Please correct these errors</h2><ul>{{range .FieldErrors}}<li><a href="#{{.Field}}">{{.Message}}</a></li>{{end}}</ul></section>{{end}}
{{if .Form}}<form action="{{.Form.Action}}" method="post"><label for="{{.Form.Field}}">{{.Form.Label}}</label>{{range .FieldErrors}}<span class="field-error" id="{{.Field}}-error">{{.Message}}</span>{{end}}<input id="{{.Form.Field}}" name="{{.Form.Field}}" value="{{.Form.Value}}" aria-describedby="{{range $i, $e := .FieldErrors}}{{if eq $e.Field $.Form.Field}}{{$e.Field}}-error{{end}}{{end}}"><button type="submit">{{.Form.Submit}}</button>{{if .Form.CSRFToken}}<input type="hidden" name="_csrf" value="{{.Form.CSRFToken}}">{{end}}</form>{{end}}
{{if .Status}}<p role="status" aria-live="polite">{{.Status}}</p>{{end}}{{end}}`
