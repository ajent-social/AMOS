// Package telemetry provides privacy-preserving HTTP request logging.
package telemetry

import (
	"bufio"
	"bytes"
	"context"
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
	// runtime's server-generated IDs and prevents caller-controlled log content.
	validID    = regexp.MustCompile(`^[a-f0-9]{32}$`)
	validRoute = regexp.MustCompile(`^/[A-Za-z0-9_./{}:*~-]{0,255}$`)
	knownCodes = map[string]struct{}{
		"auth.denied": {}, "dependency.unavailable": {}, "handler.panic": {},
		"method.not_allowed": {}, "path.invalid": {}, "route.not_found": {}, "route.unavailable": {},
	}
)

type requestIDContextKey struct{}

// RequestID returns the server-generated request ID installed by Logging.
// It returns an empty string when the context did not originate in the middleware.
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDContextKey{}).(string)
	return id
}

var fallbackIDSequence atomic.Uint64

// Logging returns middleware that emits one structured record for each request.
// It creates a server-generated request ID and never logs request headers,
// query values, bodies, response bodies, or panic values.
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
		requestID := newID()
		r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, requestID))
		rw.requestID = requestID
		if flusher, ok := w.(http.Flusher); ok {
			rw.flusher = flusher
		}
		if hijacker, ok := w.(http.Hijacker); ok {
			rw.hijacker = hijacker
		}
		if pusher, ok := w.(http.Pusher); ok {
			rw.pusher = pusher
		}
		rw.Header().Set("X-Request-ID", requestID)
		panicked := false
		func() {
			defer func() {
				if recover() != nil {
					panicked = true
					if !rw.wroteHeader && !rw.hijacked {
						http.Error(rw, "internal server error", http.StatusInternalServerError)
					}
				}
			}()
			next.ServeHTTP(wrapResponseWriter(rw), r)
		}()
		if !rw.wroteHeader && !rw.hijacked {
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
		attrs := []any{"request_id", id, "route", routeValue, "method", r.Method,
			"duration_ms", time.Since(started).Milliseconds(), "error_code", code}
		if rw.hijacked {
			attrs = append(attrs, "hijacked", true)
		} else {
			attrs = append(attrs, "status", rw.status)
		}
		logger.Info("http request", attrs...)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	requestID   string
	hijacked    bool
	flusher     http.Flusher
	hijacker    http.Hijacker
	pusher      http.Pusher
	body        bytes.Buffer
}

func (w *responseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	// Restore the trusted ID before every committed header block, including
	// informational responses, which are sent before a final status.
	w.Header().Set("X-Request-ID", w.requestID)
	// Informational responses do not finalize the response. Forward them while
	// preserving the chance to capture and log the eventual final status.
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.wroteHeader = true
	w.status = status
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

func (w *responseWriter) flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.flusher.Flush()
}

func (w *responseWriter) hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := w.hijacker.Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, rw, err
}

func (w *responseWriter) push(target string, opts *http.PushOptions) error {
	return w.pusher.Push(target, opts)
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Optional writer adapters expose only the capabilities supported by the
// underlying ResponseWriter. Embedding the base keeps ResponseController's
// Unwrap path available without advertising unsupported interfaces.
type flushResponseWriter struct{ *responseWriter }

func (w *flushResponseWriter) Flush() { w.flush() }

type hijackResponseWriter struct{ *responseWriter }

func (w *hijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}

type pushResponseWriter struct{ *responseWriter }

func (w *pushResponseWriter) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

type flushHijackResponseWriter struct{ *responseWriter }

func (w *flushHijackResponseWriter) Flush() { w.flush() }
func (w *flushHijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}

type flushPushResponseWriter struct{ *responseWriter }

func (w *flushPushResponseWriter) Flush() { w.flush() }
func (w *flushPushResponseWriter) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

type hijackPushResponseWriter struct{ *responseWriter }

func (w *hijackPushResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}
func (w *hijackPushResponseWriter) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

type flushHijackPushResponseWriter struct{ *responseWriter }

func (w *flushHijackPushResponseWriter) Flush() { w.flush() }
func (w *flushHijackPushResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}
func (w *flushHijackPushResponseWriter) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

func wrapResponseWriter(w *responseWriter) http.ResponseWriter {
	switch {
	case w.flusher != nil && w.hijacker != nil && w.pusher != nil:
		return &flushHijackPushResponseWriter{w}
	case w.flusher != nil && w.hijacker != nil:
		return &flushHijackResponseWriter{w}
	case w.flusher != nil && w.pusher != nil:
		return &flushPushResponseWriter{w}
	case w.hijacker != nil && w.pusher != nil:
		return &hijackPushResponseWriter{w}
	case w.flusher != nil:
		return &flushResponseWriter{w}
	case w.hijacker != nil:
		return &hijackResponseWriter{w}
	case w.pusher != nil:
		return &pushResponseWriter{w}
	default:
		return w
	}
}

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
