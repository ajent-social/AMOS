package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type traceEndpoint struct {
	authority, host, dial string
	tls                   *tls.Config
	client                *x509.Certificate
}

func newTraceEndpoint(authority, dial string, ca, cert, key []byte, client bool) (traceEndpoint, error) {
	var ep traceEndpoint
	host, port, err := net.SplitHostPort(authority)
	if err != nil || host == "" || len(host) > 253 {
		return ep, ErrTraceConfig
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port || net.JoinHostPort(host, port) != authority {
		return ep, ErrTraceConfig
	}
	if ip, e := netip.ParseAddr(host); e == nil {
		if !validTraceIP(ip) || ip.String() != host {
			return ep, ErrTraceConfig
		}
	} else {
		if host != strings.ToLower(host) || strings.HasSuffix(host, ".") || strings.Trim(host, "0123456789.") == "" {
			return ep, ErrTraceConfig
		}
		for _, part := range strings.Split(host, ".") {
			if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
				return ep, ErrTraceConfig
			}
			for _, c := range []byte(part) {
				if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
					return ep, ErrTraceConfig
				}
			}
		}
	}
	ip, e := netip.ParseAddr(dial)
	if e != nil || !validTraceIP(ip) || ip.String() != dial {
		return ep, ErrTraceConfig
	}
	if len(ca) == 0 || len(ca) > 65536 || len(cert) > 65536 || len(key) > 65536 {
		return ep, ErrTraceConfig
	}
	roots := x509.NewCertPool()
	rest := bytes.Clone(ca)
	count := 0
	for len(bytes.TrimSpace(rest)) > 0 {
		rest = bytes.TrimSpace(rest)
		if !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) {
			return ep, ErrTraceConfig
		}
		block, tail := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return ep, ErrTraceConfig
		}
		cert, e := x509.ParseCertificate(block.Bytes)
		if e != nil || !cert.IsCA || !cert.BasicConstraintsValid {
			return ep, ErrTraceConfig
		}
		roots.AddCert(cert)
		count++
		rest = tail
	}
	if count == 0 {
		return ep, ErrTraceConfig
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots, ServerName: host, NextProtos: []string{"http/1.1"}}
	ep = traceEndpoint{authority: authority, host: host, dial: net.JoinHostPort(dial, port), tls: tc}
	if client {
		if len(cert) == 0 || len(key) == 0 || !validTracePEM(cert, false) || !validTracePEM(key, true) {
			return traceEndpoint{}, ErrTraceConfig
		}
		pair, e := tls.X509KeyPair(bytes.Clone(cert), bytes.Clone(key))
		if e != nil || len(pair.Certificate) == 0 {
			return traceEndpoint{}, ErrTraceConfig
		}
		leaf, e := x509.ParseCertificate(pair.Certificate[0])
		if e != nil || !validTraceClient(leaf, time.Now()) {
			return traceEndpoint{}, ErrTraceConfig
		}
		pair.Leaf = leaf
		ep.client = leaf
		tc.Certificates = []tls.Certificate{pair}
	}
	return ep, nil
}

// PEM parsing is exact: do not silently ignore prefixed/trailing material.
func validTracePEM(input []byte, key bool) bool {
	rest := input
	count := 0
	for len(bytes.TrimSpace(rest)) > 0 {
		rest = bytes.TrimSpace(rest)
		if !bytes.HasPrefix(rest, []byte("-----BEGIN ")) {
			return false
		}
		block, tail := pem.Decode(rest)
		if block == nil || len(block.Headers) != 0 {
			return false
		}
		if key {
			if count != 0 || (block.Type != "PRIVATE KEY" && block.Type != "EC PRIVATE KEY" && block.Type != "RSA PRIVATE KEY") {
				return false
			}
		} else if block.Type != "CERTIFICATE" {
			return false
		}
		count++
		rest = tail
	}
	return count > 0
}

func validTraceIP(ip netip.Addr) bool {
	return (ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) && !ip.Is4In6() && ip.Zone() == ""
}
func validTraceClient(c *x509.Certificate, now time.Time) bool {
	if c == nil || now.Before(c.NotBefore) || !now.Before(c.NotAfter) || c.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return false
	}
	for _, u := range c.ExtKeyUsage {
		if u == x509.ExtKeyUsageClientAuth {
			return true
		}
	}
	return false
}
func (ep traceEndpoint) transport(oneDial bool) *http.Transport {
	var mu sync.Mutex
	dialed := false
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" || addr != ep.authority {
			return nil, errTraceExport
		}
		if oneDial {
			mu.Lock()
			if dialed {
				mu.Unlock()
				return nil, errTraceExport
			}
			dialed = true
			mu.Unlock()
		}
		d := net.Dialer{Timeout: 2 * time.Second}
		return d.DialContext(ctx, "tcp", ep.dial)
	}
	return &http.Transport{Proxy: nil, DialContext: dial, TLSClientConfig: ep.tls.Clone(), TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 2 * time.Second, MaxConnsPerHost: 4, MaxIdleConns: 4, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, DisableKeepAlives: true, DisableCompression: true, ForceAttemptHTTP2: false, TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper), MaxResponseHeaderBytes: 8192}
}

// ObserveTracing establishes a fresh ingress decision regardless of correlation
// IDs or caller SDK context. A panic is rethrown unchanged after finite completion.
func ObserveTracing(t *Tracer, route RouteTemplate, next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	if t == nil || t.sdk == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		label := "unmatched"
		if route != nil {
			value := route(r)
			if value != "" && value != "unmatched" {
				label = "other"
				if ordinal, ok := t.routes[value]; ok {
					label = ordinal
				}
			}
		}
		parent, _ := t.incoming(r)
		ctx, _, end := t.startTrace(r.Context(), "http.server", trace.SpanKindServer, parent, nil, attribute.String("amos.route", label))
		rw := &traceResponseWriter{ResponseWriter: w}
		normal := false
		defer func() {
			if !normal {
				end("panic")
				return
			}
			v := "ok"
			if !rw.wroteHeader {
				rw.status = http.StatusOK
			}
			if ctx.Err() != nil {
				v = "cancelled"
			} else if rw.status >= 400 || rw.status < 100 || rw.status >= 600 {
				v = "error"
			}
			end(v)
		}()
		next.ServeHTTP(wrapTraceResponseWriter(rw), r.WithContext(ctx))
		normal = true
	})
}
func (t *Tracer) incoming(r *http.Request) (trace.SpanContext, bool) {
	var zero trace.SpanContext
	state := r.TLS
	if state == nil || !state.HandshakeComplete || state.Version != tls.VersionTLS13 || len(state.PeerCertificates) == 0 || len(state.VerifiedChains) == 0 {
		return zero, false
	}
	leaf := state.PeerCertificates[0]
	if !validTraceClient(leaf, time.Now()) {
		return zero, false
	}
	digest := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	allowed := false
	for _, p := range t.ingress {
		if p == digest {
			allowed = true
		}
	}
	if !allowed {
		return zero, false
	}
	verified := false
	for _, chain := range state.VerifiedChains {
		if len(chain) == 0 || !bytes.Equal(chain[0].Raw, leaf.Raw) {
			continue
		}
		roots := x509.NewCertPool()
		roots.AddCert(chain[len(chain)-1])
		inter := x509.NewCertPool()
		if len(chain) > 1 {
			for _, c := range chain[1 : len(chain)-1] {
				inter.AddCert(c)
			}
		}
		if _, e := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter, CurrentTime: time.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); e == nil {
			verified = true
			break
		}
	}
	if !verified {
		return zero, false
	}
	value := ""
	count := 0
	for k, vs := range r.Header {
		if strings.EqualFold(k, "traceparent") {
			if k != "Traceparent" || len(vs) != 1 {
				return zero, false
			}
			count++
			value = vs[0]
		}
	}
	if count != 1 || len(value) != 55 || value[:3] != "00-" || value[35] != '-' || value[52] != '-' || (value[53:] != "00" && value[53:] != "01") {
		return zero, false
	}
	for i, c := range []byte(value) {
		if i == 2 || i == 35 || i == 52 {
			continue
		}
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return zero, false
		}
	}
	var tid trace.TraceID
	var sid trace.SpanID
	if _, e := hex.Decode(tid[:], []byte(value[3:35])); e != nil {
		return zero, false
	}
	if _, e := hex.Decode(sid[:], []byte(value[36:52])); e != nil {
		return zero, false
	}
	if !tid.IsValid() || !sid.IsValid() {
		return zero, false
	}
	flags := trace.TraceFlags(0)
	if value[54] == '1' {
		flags = trace.FlagsSampled
	}
	return trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: flags, Remote: true}), flags == 0
}

// TraceTransport is an owned, authority-bound mutual-TLS RoundTripper. Each
// destination has a shared finite connection pool and a two-second body budget.
type TraceTransport struct {
	owner     *Tracer
	endpoint  traceEndpoint
	ordinal   string
	transport *http.Transport
}

func newTraceTransport(t *Tracer, ep traceEndpoint, i int) *TraceTransport {
	return &TraceTransport{owner: t, endpoint: ep, ordinal: fmtTraceOrdinal(i), transport: ep.transport(false)}
}
func fmtTraceOrdinal(i int) string { return "o" + string([]byte{'0' + byte(i/10), '0' + byte(i%10)}) }
func (t *Tracer) Transport(i int) (*TraceTransport, error) {
	if t == nil || i < 0 || i >= len(t.destinations) {
		return nil, ErrTraceConfig
	}
	if t.stopped() {
		return nil, ErrTraceClosed
	}
	return t.destinations[i], nil
}
func (t *TraceTransport) CloseIdleConnections() {
	if t != nil && t.transport != nil {
		t.transport.CloseIdleConnections()
	}
}
func (t *TraceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t == nil || t.owner.stopped() {
		return nil, ErrTraceClosed
	}
	if r == nil || r.URL == nil || r.URL.Scheme != "https" || r.URL.Host != t.endpoint.authority || r.Host != "" && r.Host != t.endpoint.authority || r.URL.User != nil || r.URL.Fragment != "" || r.URL.Opaque != "" {
		return nil, ErrTraceConfig
	}
	if !validTraceClient(t.endpoint.client, time.Now()) {
		return nil, ErrTraceConfig
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	parent := ownedTraceScope(ctx)
	end := func(string) {}
	var child *traceScope
	if parent != nil && parent.owner == t.owner {
		ctx, child, end = t.owner.startTrace(ctx, "http.client", trace.SpanKindClient, parent.sc, &parent.sampled, attribute.String("amos.destination", t.ordinal))
	}
	clone := r.Clone(ctx)
	if clone.Header == nil {
		clone.Header = make(http.Header)
	}
	clone.GetBody = nil
	for key := range clone.Header {
		if strings.EqualFold(key, "traceparent") || strings.EqualFold(key, "tracestate") || strings.EqualFold(key, "baggage") {
			delete(clone.Header, key)
		}
	}
	if child != nil {
		sc := child.sc
		flags := "00"
		if child.sampled {
			flags = "01"
		}
		clone.Header.Set("Traceparent", "00-"+sc.TraceID().String()+"-"+sc.SpanID().String()+"-"+flags)
	}
	resp, err := t.transport.RoundTrip(clone)
	finish := func() {
		value := "ok"
		if ctx.Err() != nil {
			value = "cancelled"
		} else if err != nil && resp == nil {
			value = "unavailable"
		} else if resp == nil || resp.StatusCode < 100 || resp.StatusCode >= 400 {
			value = "error"
		}
		end(value)
		cancel()
	}
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		finish()
		return resp, err
	}
	body := &traceBody{ReadCloser: resp.Body, finish: finish}
	body.stop = context.AfterFunc(ctx, func() { body.once.Do(finish) })
	resp.Body = body
	return resp, nil
}

type traceBody struct {
	io.ReadCloser
	once   sync.Once
	finish func()
	stop   func() bool
}

func (b *traceBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if e != nil {
		b.complete()
	}
	return n, e
}
func (b *traceBody) Close() error { e := b.ReadCloser.Close(); b.complete(); return e }

func (b *traceBody) complete() {
	if b.stop != nil {
		b.stop()
	}
	b.once.Do(b.finish)
}

// The status adapter never copies bodies or changes correlation headers.
type traceResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *traceResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *traceResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *traceResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type traceFlush struct {
	writer  *traceResponseWriter
	flusher http.Flusher
}

func (f traceFlush) Flush() {
	if !f.writer.wroteHeader {
		f.writer.WriteHeader(http.StatusOK)
	}
	f.flusher.Flush()
}

type traceHijack struct{ h http.Hijacker }

func (h traceHijack) Hijack() (net.Conn, *bufio.ReadWriter, error) { return h.h.Hijack() }
func wrapTraceResponseWriter(w *traceResponseWriter) http.ResponseWriter {
	f, hasF := w.ResponseWriter.(http.Flusher)
	h, hasH := w.ResponseWriter.(http.Hijacker)
	p, hasP := w.ResponseWriter.(http.Pusher)
	fl := traceFlush{w, f}
	hi := traceHijack{h}
	switch {
	case hasF && hasH && hasP:
		return struct {
			*traceResponseWriter
			http.Flusher
			http.Hijacker
			http.Pusher
		}{w, fl, hi, p}
	case hasF && hasH:
		return struct {
			*traceResponseWriter
			http.Flusher
			http.Hijacker
		}{w, fl, hi}
	case hasF && hasP:
		return struct {
			*traceResponseWriter
			http.Flusher
			http.Pusher
		}{w, fl, p}
	case hasH && hasP:
		return struct {
			*traceResponseWriter
			http.Hijacker
			http.Pusher
		}{w, hi, p}
	case hasF:
		return struct {
			*traceResponseWriter
			http.Flusher
		}{w, fl}
	case hasH:
		return struct {
			*traceResponseWriter
			http.Hijacker
		}{w, hi}
	case hasP:
		return struct {
			*traceResponseWriter
			http.Pusher
		}{w, p}
	default:
		return w
	}
}
