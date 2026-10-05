// Package telemetry provides privacy-preserving HTTP request logging.
package telemetry

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// RouteTemplate resolves a request to a registered route pattern. Implementations
// must return a template, never a path containing request values.
type RouteTemplate func(*http.Request) string

var (
	// Request IDs are opaque 128-bit lowercase hex values. This matches the
	// runtime's generated IDs and prevents caller-controlled log content.
	validID    = regexp.MustCompile(`^[a-f0-9]{32}$`)
	validRoute = regexp.MustCompile(`^/[A-Za-z0-9_./{}:*~-]{0,255}$`)
	knownCodes = map[string]struct{}{
		"auth.denied": {}, "dependency.unavailable": {}, "handler.panic": {},
		"method.not_allowed": {}, "path.invalid": {}, "route.not_found": {}, "route.unavailable": {},
	}
)

var fallbackIDSequence atomic.Uint64

// Logging returns middleware that emits one structured record for each request.
// It validates client correlation headers and never logs request
// headers, query values, bodies, response bodies, or panic values.
func Logging(logger *slog.Logger, route RouteTemplate, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rw := &responseWriter{ResponseWriter: w}
		requestID := r.Header.Get("X-Request-ID")
		if !validID.MatchString(requestID) {
			requestID = newID()
		}
		rw.Header().Set("X-Request-ID", requestID)
		panicked := false
		func() {
			defer func() {
				if recover() != nil {
					panicked = true
					if !rw.wroteHeader {
						http.Error(rw, "internal server error", http.StatusInternalServerError)
					}
				}
			}()
			next.ServeHTTP(rw, r)
		}()
		if !rw.wroteHeader {
			rw.WriteHeader(http.StatusOK)
		}
		id := rw.requestID
		routeValue := "unmatched"
		if route != nil {
			if candidate := route(r); validRoute.MatchString(candidate) && !strings.Contains(candidate, "..") {
				routeValue = candidate
			}
		}
		code := responseCode(rw.body.Bytes())
		if panicked {
			code = "handler.panic"
		}
		logger.Info("http request", "request_id", id, "route", routeValue, "method", r.Method,
			"status", rw.status, "duration_ms", time.Since(started).Milliseconds(), "error_code", code)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	requestID   string
	body        bytes.Buffer
}

func (w *responseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	if id := w.Header().Get("X-Request-ID"); !validID.MatchString(id) {
		w.Header().Set("X-Request-ID", newID())
	}
	w.requestID = w.Header().Get("X-Request-ID")
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.status >= 400 && w.body.Len() < 4096 {
		remaining := 4096 - w.body.Len()
		captured := p
		if len(captured) > remaining {
			captured = captured[:remaining]
		}
		_, _ = w.body.Write(captured)
	}
	return w.ResponseWriter.Write(p)
}

func (w *responseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

func (w *responseWriter) Push(target string, opts *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func responseCode(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(body, &payload) == nil {
		if _, ok := knownCodes[payload.Code]; ok {
			return payload.Code
		}
	}
	return "http.error"
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixNano()))
		binary.BigEndian.PutUint64(b[8:], fallbackIDSequence.Add(1))
	}
	return hex.EncodeToString(b[:])
}
