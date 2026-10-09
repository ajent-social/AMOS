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

	"github.com/google/uuid"
)

const sourcePath = "/continuity/sources"
const sourceHTTPMax = 512 * 1024

// sourceResponse is deliberately not a Flusher, Hijacker or unwrapping writer.
// Header's mutable map is bounded at publication, not at intermediate allocation.
type sourceResponse struct {
	header    http.Header
	body      bytes.Buffer
	status    int
	failed    bool
	committed bool
}

func (b *sourceResponse) Header() http.Header { return b.header }
func (b *sourceResponse) WriteHeader(status int) {
	if b.status != 0 || b.failed {
		return
	}
	if status < 200 || status > 599 {
		b.failed = true
		return
	}
	b.status = status
}
func (b *sourceResponse) Write(p []byte) (int, error) {
	if b.failed || len(p) > sourceHTTPMax-b.body.Len() {
		b.failed = true
		b.body.Reset()
		return 0, errUnavailable
	}
	if b.status == 0 {
		b.WriteHeader(http.StatusOK)
	}
	return b.body.Write(p)
}

// Only the closed native middleware chain reaches this helper in production.
func (b *sourceResponse) collect(run func()) {
	defer func() {
		if recover() != nil {
			b.failed = true
			b.body.Reset()
		}
	}()
	run()
}

func sourceFragment(h http.Header) bool {
	a, b := h.Values("HX-Request"), h.Values("HX-Target")
	return len(a) == 1 && a[0] == "true" && len(b) == 1 && b[0] == "continuity-source-content"
}

func sourceID(s string) bool {
	if len(s) != 36 {
		return false
	}
	id, err := uuid.Parse(s)
	return err == nil && id.String() == s && id.Version() == 7 && id.Variant() == uuid.RFC4122
}

func sourceInput(r *http.Request) (sourceQuery, int) {
	q := sourceQuery{Fragment: sourceFragment(r.Header)}
	u := r.URL
	if u == nil || len(u.Path) > 128 || len(u.RawQuery) > 2048 || u.RawPath != "" || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return q, http.StatusBadRequest
	}
	if u.Path != sourcePath {
		if !strings.HasPrefix(u.Path, sourcePath+"/") {
			return q, http.StatusNotFound
		}
		q.ID = strings.TrimPrefix(u.Path, sourcePath+"/")
		if !sourceID(q.ID) {
			return q, http.StatusBadRequest
		}
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return q, http.StatusMethodNotAllowed
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil || (q.ID != "" && (u.RawQuery != "" || u.ForceQuery)) {
		return q, http.StatusBadRequest
	}
	if q.ID != "" {
		return q, 0
	}
	q.Limit = 25
	for k, values := range values {
		if len(values) != 1 {
			return q, http.StatusBadRequest
		}
		v := values[0]
		switch k {
		case "q":
			if len(v) > 480 || !utf8.ValidString(v) || strings.ContainsFunc(v, unicode.IsControl) {
				return q, http.StatusBadRequest
			}
			q.Query = strings.TrimSpace(v)
			if utf8.RuneCountInString(q.Query) > 120 || (v != "" && q.Query == "") {
				return q, http.StatusBadRequest
			}
		case "kind":
			if v != "" && v != "document" && v != "correspondence" {
				return q, http.StatusBadRequest
			}
			q.Kind = v
		case "after":
			if v != "" && !sourceID(v) {
				return q, http.StatusBadRequest
			}
			q.After = v
		case "limit":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 100 || strconv.Itoa(n) != v {
				return q, http.StatusBadRequest
			}
			q.Limit = n
		default:
			return q, http.StatusBadRequest
		}
	}
	return q, 0
}

// sourceHandler remains unexported and captures the complete private custody
// chain. It neither opens a public host nor accepts a replacement authority.
func (c *readCore) sourceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		q, status := sourceInput(r)
		response := &sourceResponse{header: make(http.Header), status: status}
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
				body, err := c.source(admitted.Context(), q)
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
				_, _ = w.Write(body) // The collector retains and checks any write failure.
			})).ServeHTTP(response, r)
		})
	})
}

func (b *sourceResponse) validHeaders() bool {
	bytes, values := 0, 0
	for k, vv := range b.header {
		bytes += len(k)
		values += len(vv)
		if values > 32 || bytes > 8192 {
			return false
		}
		for _, v := range vv {
			bytes += len(v)
			if bytes > 8192 {
				return false
			}
		}
	}
	return true
}

func sourceError(status int) (string, string) {
	switch status {
	case http.StatusBadRequest:
		return "request.invalid", "Invalid source request."
	case http.StatusUnauthorized:
		return "auth.unauthenticated", "Sign in to view sources."
	case http.StatusForbidden:
		return "request.forbidden", "Request not permitted."
	case http.StatusNotFound:
		return "resource.not_found", "Source not found."
	case http.StatusMethodNotAllowed:
		return "method.not_allowed", "Method not allowed."
	default:
		return "dependency.unavailable", "Sources are temporarily unavailable."
	}
}

const sourceErrorHTML = `{{if not .Fragment}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Sources</title><link rel="stylesheet" href="/assets/base.css"></head><body><a href="#main-content">Skip to content</a><main id="main-content">{{end}}<section id="continuity-source-content"><h1>Sources</h1><p role="status">{{.Message}}</p><dl><dt>Code</dt><dd>{{.Code}}</dd><dt>Request ID</dt><dd>{{.ID}}</dd></dl><a href="/continuity/sources">Back to sources</a></section>{{if not .Fragment}}</main></body></html>{{end}}`

func sourceErrorBody(status int, id string, fragment bool) []byte {
	code, message := sourceError(status)
	view := struct {
		Code, Message, ID string
		Fragment          bool
	}{code, message, id, fragment}
	t, err := template.New("source-error").Parse(sourceErrorHTML)
	if err != nil {
		return []byte("Sources are temporarily unavailable.")
	}
	var b bytes.Buffer
	if err = t.Execute(&b, view); err != nil {
		return []byte("Sources are temporarily unavailable.")
	}
	return b.Bytes()
}

func (b *sourceResponse) publish(w http.ResponseWriter, r *http.Request, id string, fragment bool) {
	status := b.status
	if b.failed || !b.validHeaders() || r.Context().Err() != nil {
		status = http.StatusServiceUnavailable
	}
	if status != 200 && status != 400 && status != 401 && status != 403 && status != 404 && status != 405 && status != 503 {
		status = http.StatusServiceUnavailable
	}
	if status == 200 && (!b.committed || b.body.Len() == 0 || len(b.header) != 1 || b.header.Get("Content-Type") != "text/html; charset=utf-8" || len(b.header.Values("Content-Type")) != 1) {
		status = http.StatusServiceUnavailable
	}
	body := b.body.Bytes()
	if status != 200 {
		body = sourceErrorBody(status, id, fragment)
	}
	h := w.Header()
	for _, name := range []string{"ETag", "Last-Modified", "Set-Cookie", "Location", "X-Request-ID", "Allow"} {
		h.Del(name)
	}
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Vary", "HX-Request, HX-Target")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self' 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if id != "" {
		h.Set("X-Request-ID", id)
	}
	if status == http.StatusMethodNotAllowed {
		h.Set("Allow", "GET, HEAD")
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body) // Publication has begun; network failure cannot be undone.
	}
}
