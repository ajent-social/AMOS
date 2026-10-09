package host

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ajent-social/amos/examples/continuity/guide"
	"github.com/google/uuid"
)

const baselinePath = "/continuity/properties"

func baselineFragment(h http.Header) bool {
	request, target := h.Values("HX-Request"), h.Values("HX-Target")
	return len(request) == 1 && request[0] == "true" && len(target) == 1 && target[0] == "continuity-baseline-content"
}

// baselineInput parses only transport selectors. Form tokens do not exist here:
// the same native session service supplies them after middleware admission.
func baselineInput(r *http.Request) (baselineQuery, int) {
	q := baselineQuery{Fragment: baselineFragment(r.Header)}
	u := r.URL
	if u == nil || len(u.Path) > 128 || len(u.RawQuery) > 2048 || u.RawPath != "" || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return q, http.StatusBadRequest
	}
	for _, route := range []struct {
		path         string
		list, detail baselinePage
	}{
		{baselinePath, pageProperties, pageProperty},
		{"/continuity/cases", pageCases, pageCase},
		{"/continuity/applications", pageApplications, pageApplication},
		{"/continuity/procedures", pageProcedures, pageProcedure},
		{"/continuity/activity", pageActivity, 0},
		{"/continuity/guide", pageGuide, 0},
	} {
		if u.Path == route.path {
			q.Page = route.list
			break
		}
		if strings.HasPrefix(u.Path, route.path+"/") {
			q.ID = strings.TrimPrefix(u.Path, route.path+"/")
			if route.detail == 0 || !sourceID(q.ID) {
				return q, http.StatusBadRequest
			}
			q.Page = route.detail
			break
		}
	}
	if q.Page == 0 {
		return q, http.StatusNotFound
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return q, http.StatusMethodNotAllowed
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return q, http.StatusBadRequest
	}
	if q.ID != "" {
		if u.RawQuery != "" || u.ForceQuery {
			return q, http.StatusBadRequest
		}
		return q, 0
	}
	if q.Page == pageGuide {
		return baselineGuideInput(q, values)
	}
	q.Limit = 25
	for key, vv := range values {
		if len(vv) != 1 {
			return q, http.StatusBadRequest
		}
		value := vv[0]
		switch key {
		case "q":
			if q.Page != pageProperties || len(value) > 480 || !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
				return q, http.StatusBadRequest
			}
			q.Query = strings.TrimSpace(value)
			if utf8.RuneCountInString(q.Query) > 120 {
				return q, http.StatusBadRequest
			}
		case "after":
			if value != "" && !sourceID(value) {
				return q, http.StatusBadRequest
			}
			q.After = value
		case "limit":
			n, ok := baselineLimit(value)
			if !ok {
				return q, http.StatusBadRequest
			}
			q.Limit = n
		default:
			return q, http.StatusBadRequest
		}
	}
	return q, 0
}

func baselineLimit(value string) (int, bool) {
	if len(value) < 1 || len(value) > 3 {
		return 0, false
	}
	n, err := strconv.Atoi(value)
	return n, err == nil && n >= 1 && n <= 100 && strconv.Itoa(n) == value
}

func baselineGuideInput(q baselineQuery, values url.Values) (baselineQuery, int) {
	q.Topic = guide.Attention
	limitPresent := false
	for key, vv := range values {
		if len(vv) != 1 {
			return q, http.StatusBadRequest
		}
		value := vv[0]
		switch key {
		case "topic":
			if len(value) > 18 {
				return q, http.StatusBadRequest
			}
			q.Topic = guide.Topic(value)
		case "case_id":
			if value != "" && !sourceID(value) {
				return q, http.StatusBadRequest
			}
			q.CaseID = value
		case "limit":
			n, ok := baselineLimit(value)
			if !ok {
				return q, http.StatusBadRequest
			}
			limitPresent, q.Limit = true, n
		default:
			return q, http.StatusBadRequest
		}
	}
	switch q.Topic {
	case guide.Attention, guide.Handover:
		if q.CaseID != "" {
			return q, http.StatusBadRequest
		}
		if !limitPresent {
			q.Limit = 25
		}
	case guide.ExplainCase, guide.OwnerDraft:
		if q.CaseID == "" || limitPresent {
			return q, http.StatusBadRequest
		}
	case guide.SpendingAuthority:
		if q.CaseID != "" || limitPresent {
			return q, http.StatusBadRequest
		}
	default:
		return q, http.StatusBadRequest
	}
	return q, 0
}

// baselineHandler is private composition only; no routes or listener are added.
func (c *readCore) baselineHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		q, status := baselineInput(r)
		response := &sourceResponse{header: make(http.Header), status: status, presentation: baselinePresentation}
		id, idErr := uuid.NewRandom()
		requestID := ""
		if idErr == nil {
			requestID = id.String()
		} else {
			response.failed = true
		}
		defer func() {
			if recover() != nil {
				response.failed = true
			}
			response.publish(w, r, requestID, q.Fragment)
		}()
		if ctx.Err() != nil || idErr != nil {
			response.failed = true
			return
		}
		if status != 0 {
			return
		}
		if c == nil || c.sessions == nil {
			response.failed = true
			return
		}
		response.collect(func() {
			c.sessions.Middleware(http.HandlerFunc(func(w http.ResponseWriter, admitted *http.Request) {
				switch q.Page {
				case pageCase, pageApplication, pageProcedure:
					token, ok := c.sessions.CSRFToken(admitted)
					if !ok {
						response.failed = true
						return
					}
					q.CSRFToken = token
				}
				body, err := c.baseline(admitted.Context(), q)
				if err != nil {
					switch {
					case errors.Is(err, errInvalid):
						w.WriteHeader(http.StatusBadRequest)
					case errors.Is(err, errDenied), errors.Is(err, errNotFound):
						w.WriteHeader(http.StatusNotFound)
					default:
						w.WriteHeader(http.StatusServiceUnavailable)
					}
					return
				}
				if len(body) == 0 || len(body) > sourceHTTPMax {
					response.failed = true
					return
				}
				response.committed = true
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write(body) // The shared collector retains every write failure.
			})).ServeHTTP(response, r)
		})
	})
}

func baselineError(status int) (string, string) {
	switch status {
	case http.StatusBadRequest:
		return "request.invalid", "Invalid continuity request."
	case http.StatusUnauthorized:
		return "auth.unauthenticated", "Sign in to view continuity records."
	case http.StatusForbidden:
		return "request.forbidden", "Request not permitted."
	case http.StatusNotFound:
		return "resource.not_found", "Continuity record not found."
	case http.StatusMethodNotAllowed:
		return "method.not_allowed", "Method not allowed."
	default:
		return "dependency.unavailable", "Continuity records are temporarily unavailable."
	}
}

const baselineErrorHTML = `{{if not .Fragment}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Continuity</title><link rel="stylesheet" href="/assets/base.css"></head><body><a href="#main-content">Skip to content</a><main id="main-content">{{end}}<section id="continuity-baseline-content"><h1>Continuity</h1><p role="status">{{.Message}}</p><dl><dt>Code</dt><dd>{{.Code}}</dd><dt>Request ID</dt><dd>{{.ID}}</dd></dl><a href="/continuity/properties">Back to properties</a></section>{{if not .Fragment}}</main></body></html>{{end}}`

func baselineErrorBody(status int, id string, fragment bool) []byte {
	code, message := baselineError(status)
	view := struct {
		Code, Message, ID string
		Fragment          bool
	}{code, message, id, fragment}
	t, err := template.New("baseline-error").Parse(baselineErrorHTML)
	if err != nil {
		return []byte("Continuity records are temporarily unavailable.")
	}
	var b bytes.Buffer
	if err = t.Execute(&b, view); err != nil {
		return []byte("Continuity records are temporarily unavailable.")
	}
	return b.Bytes()
}
