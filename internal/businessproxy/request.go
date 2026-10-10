package businessproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxRequestBodyBytes = 1 << 20
	maxUpstreamTimeout  = 10 * time.Second
	maxQueryBytes       = 8 << 10
	maxQueryKeys        = 64
	maxQueryKeyBytes    = 128
	maxQueryValueBytes  = 2 << 10
)

var (
	ErrInvalidRequest   = errors.New("invalid proxy request")
	ErrNoRoute          = errors.New("proxy route is not allowed")
	ErrMethodNotAllowed = errors.New("proxy method is not allowed")
	ErrBodyTooLarge     = errors.New("proxy request body exceeds limit")
	ErrUpstreamRedirect = errors.New("proxy upstream redirect is forbidden")
)

// QueryKey declares one query key accepted by a fixed route. Repeated values
// are rejected unless AllowMultiple is true for that key.
type QueryKey struct {
	Name          string
	AllowMultiple bool
}

// Route maps one public path prefix to a fixed upstream path prefix. Route
// values are deployment-owned configuration; they must never come from a
// request. Prefixes may not overlap, and the public prefix may not cover an
// AMOS-reserved route.
type Route struct {
	PublicPrefix        string
	UpstreamPrefix      string
	Methods             []string
	QueryKeys           []QueryKey
	RequestContentTypes []string
}

// Config contains the fixed origin and allowlisted routes selected at
// startup. Origin is not derived from the incoming Host or URL.
type Config struct {
	Origin string
	Routes []Route
}

type route struct {
	publicPrefix   string
	upstreamPrefix string
	methods        map[string]struct{}
	queryKeys      map[string]bool
	contentTypes   map[string]struct{}
}

// RequestBuilder creates a bounded request for the configured origin. It
// filters input only; callers must perform current authorization before Build
// and must apply verified service identity in the separately reviewed identity
// propagation boundary.
type RequestBuilder struct {
	origin *url.URL
	routes []route
}

// NewRequestBuilder validates immutable startup configuration. It does not
// resolve or qualify the private origin; deployment composition must provide
// a private, TLS-verified, client-authenticated transport before exposing the
// proxy.
func NewRequestBuilder(config Config) (*RequestBuilder, error) {
	origin, err := url.Parse(config.Origin)
	if err != nil || origin == nil || origin.Scheme != "https" || origin.Host == "" || origin.Opaque != "" || origin.User != nil || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || origin.RawFragment != "" || (origin.Path != "" && origin.Path != "/") {
		return nil, fmt.Errorf("invalid fixed proxy origin: %w", ErrInvalidRequest)
	}
	if len(config.Routes) == 0 {
		return nil, fmt.Errorf("proxy route allowlist is empty: %w", ErrInvalidRequest)
	}

	builder := &RequestBuilder{origin: origin, routes: make([]route, 0, len(config.Routes))}
	registered := make(map[string]struct{})
	for _, candidate := range config.Routes {
		publicPrefix, err := canonicalConfiguredPrefix(candidate.PublicPrefix)
		if err != nil || isReservedPath(publicPrefix) {
			return nil, fmt.Errorf("invalid or reserved public route prefix %q: %w", candidate.PublicPrefix, ErrInvalidRequest)
		}
		upstreamPrefix, err := canonicalConfiguredPrefix(candidate.UpstreamPrefix)
		if err != nil {
			return nil, fmt.Errorf("invalid upstream route prefix %q: %w", candidate.UpstreamPrefix, ErrInvalidRequest)
		}
		if len(candidate.Methods) == 0 {
			return nil, fmt.Errorf("route %q has no allowed methods: %w", publicPrefix, ErrInvalidRequest)
		}

		compiled := route{
			publicPrefix:   publicPrefix,
			upstreamPrefix: upstreamPrefix,
			methods:        make(map[string]struct{}, len(candidate.Methods)),
			queryKeys:      make(map[string]bool, len(candidate.QueryKeys)),
			contentTypes:   make(map[string]struct{}, len(candidate.RequestContentTypes)),
		}
		for _, method := range candidate.Methods {
			if method == "" || method != strings.ToUpper(method) || !allowedMethod(method) {
				return nil, fmt.Errorf("route %q has invalid method %q: %w", publicPrefix, method, ErrInvalidRequest)
			}
			if _, duplicate := compiled.methods[method]; duplicate {
				return nil, fmt.Errorf("route %q repeats method %q: %w", publicPrefix, method, ErrInvalidRequest)
			}
			if _, duplicate := registered[method+" "+publicPrefix]; duplicate {
				return nil, fmt.Errorf("duplicate route registration %q: %w", method+" "+publicPrefix, ErrInvalidRequest)
			}
			registered[method+" "+publicPrefix] = struct{}{}
			compiled.methods[method] = struct{}{}
		}
		for _, queryKey := range candidate.QueryKeys {
			if !validQueryKey(queryKey.Name) {
				return nil, fmt.Errorf("route %q has invalid query key %q: %w", publicPrefix, queryKey.Name, ErrInvalidRequest)
			}
			if _, duplicate := compiled.queryKeys[queryKey.Name]; duplicate {
				return nil, fmt.Errorf("route %q repeats query key %q: %w", publicPrefix, queryKey.Name, ErrInvalidRequest)
			}
			compiled.queryKeys[queryKey.Name] = queryKey.AllowMultiple
		}
		for _, contentType := range candidate.RequestContentTypes {
			mediaType, _, parseErr := mime.ParseMediaType(contentType)
			if parseErr != nil || mediaType == "" || mediaType != strings.ToLower(mediaType) {
				return nil, fmt.Errorf("route %q has invalid request content type %q: %w", publicPrefix, contentType, ErrInvalidRequest)
			}
			if _, duplicate := compiled.contentTypes[mediaType]; duplicate {
				return nil, fmt.Errorf("route %q repeats request content type %q: %w", publicPrefix, mediaType, ErrInvalidRequest)
			}
			compiled.contentTypes[mediaType] = struct{}{}
		}
		builder.routes = append(builder.routes, compiled)
	}

	sort.Slice(builder.routes, func(i, j int) bool {
		return builder.routes[i].publicPrefix < builder.routes[j].publicPrefix
	})
	for i := range builder.routes {
		for j := i + 1; j < len(builder.routes); j++ {
			left, right := builder.routes[i].publicPrefix, builder.routes[j].publicPrefix
			if (pathPrefixMatches(left, right) || pathPrefixMatches(right, left)) && routesShareMethod(builder.routes[i], builder.routes[j]) {
				return nil, fmt.Errorf("overlapping public route prefixes %q and %q: %w", left, right, ErrInvalidRequest)
			}
			left, right = builder.routes[i].upstreamPrefix, builder.routes[j].upstreamPrefix
			if (pathPrefixMatches(left, right) || pathPrefixMatches(right, left)) && routesShareMethod(builder.routes[i], builder.routes[j]) {
				return nil, fmt.Errorf("overlapping upstream route prefixes %q and %q: %w", left, right, ErrInvalidRequest)
			}
		}
	}

	return builder, nil
}

// NewHTTPClient requires an explicitly configured transport and prevents the
// client from following an upstream redirect to a different origin. The
// transport must enforce the deployment's private-address, TLS-root and
// client-certificate profile.
func NewHTTPClient(transport http.RoundTripper) (*http.Client, error) {
	if transport == nil {
		return nil, fmt.Errorf("proxy transport is required: %w", ErrInvalidRequest)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   maxUpstreamTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return ErrUpstreamRedirect
		},
	}, nil
}

// Build returns a fresh request for the configured origin and route. It never
// forwards request-supplied origin, Host, credentials, cookies, identity,
// forwarding or hop-by-hop headers. Callers must invoke it only after their
// current authorization decision and before the bounded response layer.
func (b *RequestBuilder) Build(in *http.Request) (*http.Request, context.CancelFunc, error) {
	if b == nil || b.origin == nil || in == nil || in.URL == nil || in.Method == "" || in.URL.Opaque != "" || in.URL.User != nil || in.URL.Fragment != "" {
		return nil, nil, ErrInvalidRequest
	}
	if in.URL.IsAbs() || in.URL.Host != "" || (in.RequestURI != "" && !strings.HasPrefix(in.RequestURI, "/")) {
		return nil, nil, fmt.Errorf("absolute request target is forbidden: %w", ErrInvalidRequest)
	}
	if err := validateHeaderSyntax(in.Header); err != nil {
		return nil, nil, err
	}
	if err := validateHopHeaders(in); err != nil {
		return nil, nil, err
	}

	path, err := canonicalRequestPath(in.URL)
	if err != nil || isReservedPath(path) {
		return nil, nil, fmt.Errorf("request path is not eligible for proxying: %w", ErrInvalidRequest)
	}
	selected, suffix, pathMatched := b.findRoute(path, in.Method)
	if selected == nil {
		if pathMatched {
			return nil, nil, ErrMethodNotAllowed
		}
		return nil, nil, ErrNoRoute
	}

	query, err := canonicalQuery(in.URL.RawQuery, selected.queryKeys)
	if err != nil {
		return nil, nil, err
	}
	body, contentType, err := readRequestBody(in, selected.contentTypes)
	if err != nil {
		return nil, nil, err
	}

	target := *b.origin
	target.Path = selected.upstreamPrefix + suffix
	if target.Path == "" {
		target.Path = "/"
	}
	target.RawPath = ""
	target.RawQuery = query
	target.ForceQuery = false

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	ctx, cancel := context.WithTimeout(in.Context(), maxUpstreamTimeout)
	out, err := http.NewRequestWithContext(ctx, in.Method, target.String(), bodyReader)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("build fixed-origin proxy request: %w", err)
	}
	if len(body) > 0 {
		out.ContentLength = int64(len(body))
	}
	if contentType != "" {
		out.Header.Set("Content-Type", contentType)
	}
	copyAllowedHeaders(out.Header, in.Header)
	return out, cancel, nil
}

func (b *RequestBuilder) findRoute(path, method string) (*route, string, bool) {
	var selected *route
	pathMatched := false
	for i := range b.routes {
		candidate := &b.routes[i]
		if !pathPrefixMatches(candidate.publicPrefix, path) {
			continue
		}
		pathMatched = true
		if _, allowed := candidate.methods[method]; !allowed {
			continue
		}
		if selected == nil || len(candidate.publicPrefix) > len(selected.publicPrefix) {
			selected = candidate
		}
	}
	if selected == nil {
		return nil, "", pathMatched
	}
	return selected, strings.TrimPrefix(path, selected.publicPrefix), true
}

func routesShareMethod(left, right route) bool {
	for method := range left.methods {
		if _, ok := right.methods[method]; ok {
			return true
		}
	}
	return false
}

func canonicalConfiguredPrefix(value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, "//") || strings.ContainsAny(value, "%;?#") || hasControl(value) || !utf8.ValidString(value) {
		return "", ErrInvalidRequest
	}
	if value != "/" && strings.HasSuffix(value, "/") {
		value = strings.TrimSuffix(value, "/")
	}
	if value == "/" || isDotPath(value) {
		return "", ErrInvalidRequest
	}
	return value, nil
}

func canonicalRequestPath(requestURL *url.URL) (string, error) {
	escaped := requestURL.RawPath
	if escaped == "" {
		escaped = requestURL.EscapedPath()
	}
	if escaped == "" || !strings.HasPrefix(escaped, "/") || strings.Contains(escaped, "\\") || strings.Contains(escaped, "//") || strings.Contains(escaped, ";") {
		return "", ErrInvalidRequest
	}
	for i := 0; i < len(escaped); i++ {
		if escaped[i] != '%' {
			continue
		}
		if i+2 >= len(escaped) || !upperHex(escaped[i+1]) || !upperHex(escaped[i+2]) {
			return "", ErrInvalidRequest
		}
		value := fromHex(escaped[i+1])<<4 | fromHex(escaped[i+2])
		if value == '%' || value == '/' || value == '\\' || value < 0x20 || value == 0x7f || isUnreserved(value) {
			return "", ErrInvalidRequest
		}
		i += 2
	}
	decoded, err := url.PathUnescape(escaped)
	if err != nil || !utf8.ValidString(decoded) || decoded != requestURL.Path || hasControl(decoded) || strings.Contains(decoded, "\\") || strings.Contains(decoded, "//") || strings.Contains(decoded, ";") || isDotPath(decoded) {
		return "", ErrInvalidRequest
	}
	return decoded, nil
}

func canonicalQuery(raw string, allowed map[string]bool) (string, error) {
	if len(raw) > maxQueryBytes {
		return "", ErrInvalidRequest
	}
	values, err := url.ParseQuery(raw)
	if err != nil || len(values) > maxQueryKeys {
		return "", ErrInvalidRequest
	}
	for key, entries := range values {
		allowMultiple, ok := allowed[key]
		if !ok || !validQueryKey(key) || len(entries) == 0 || (!allowMultiple && len(entries) > 1) {
			return "", ErrInvalidRequest
		}
		for _, value := range entries {
			if len(value) > maxQueryValueBytes || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\x00") {
				return "", ErrInvalidRequest
			}
		}
	}
	return values.Encode(), nil
}

func readRequestBody(in *http.Request, allowedContentTypes map[string]struct{}) ([]byte, string, error) {
	if len(in.TransferEncoding) > 1 {
		return nil, "", ErrInvalidRequest
	}
	for _, encoding := range in.TransferEncoding {
		if !strings.EqualFold(encoding, "chunked") {
			return nil, "", ErrInvalidRequest
		}
	}
	if in.ContentLength > maxRequestBodyBytes {
		return nil, "", ErrBodyTooLarge
	}
	if in.Body == nil || in.Body == http.NoBody {
		return nil, "", nil
	}
	if in.Method == http.MethodGet || in.Method == http.MethodHead {
		return nil, "", ErrInvalidRequest
	}
	contentType := ""
	values := headerValues(in.Header, "Content-Type")
	if len(values) > 1 {
		return nil, "", ErrInvalidRequest
	}
	if len(values) == 1 {
		mediaType, _, err := mime.ParseMediaType(values[0])
		if err != nil {
			return nil, "", ErrInvalidRequest
		}
		if _, ok := allowedContentTypes[mediaType]; !ok {
			return nil, "", ErrInvalidRequest
		}
		contentType = values[0]
	} else {
		return nil, "", ErrInvalidRequest
	}
	if len(headerValues(in.Header, "Content-Encoding")) > 0 {
		for _, value := range headerValues(in.Header, "Content-Encoding") {
			if !strings.EqualFold(strings.TrimSpace(value), "identity") {
				return nil, "", ErrInvalidRequest
			}
		}
	}
	body, err := io.ReadAll(io.LimitReader(in.Body, maxRequestBodyBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read bounded proxy request body: %w", err)
	}
	if len(body) > maxRequestBodyBytes {
		return nil, "", ErrBodyTooLarge
	}
	return body, contentType, nil
}

func validateHeaderSyntax(headers http.Header) error {
	seen := make(map[string]struct{}, len(headers))
	for name, values := range headers {
		if !validHeaderName(name) {
			return fmt.Errorf("invalid request header name: %w", ErrInvalidRequest)
		}
		normalized := strings.ToLower(name)
		if _, duplicate := seen[normalized]; duplicate {
			return fmt.Errorf("ambiguous duplicate request header %q: %w", name, ErrInvalidRequest)
		}
		seen[normalized] = struct{}{}
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n\x00") {
				return fmt.Errorf("invalid value for request header %q: %w", name, ErrInvalidRequest)
			}
		}
	}
	return nil
}

func validateHopHeaders(in *http.Request) error {
	if len(headerValues(in.Header, "Proxy-Connection")) > 0 || len(headerValues(in.Header, "Proxy-Authenticate")) > 0 || len(headerValues(in.Header, "Proxy-Authorization")) > 0 || len(headerValues(in.Header, "Keep-Alive")) > 0 || len(headerValues(in.Header, "TE")) > 0 || len(headerValues(in.Header, "Trailer")) > 0 || len(headerValues(in.Header, "Upgrade")) > 0 || len(headerValues(in.Header, "Transfer-Encoding")) > 0 || len(in.Trailer) > 0 {
		return fmt.Errorf("hop-by-hop request headers are forbidden: %w", ErrInvalidRequest)
	}
	for _, value := range headerValues(in.Header, "Connection") {
		for _, token := range strings.Split(value, ",") {
			token = strings.TrimSpace(token)
			if token == "" || !validHeaderName(token) || (!strings.EqualFold(token, "close") && !strings.EqualFold(token, "keep-alive")) {
				return fmt.Errorf("connection-nominated headers are forbidden: %w", ErrInvalidRequest)
			}
		}
	}
	return nil
}

func copyAllowedHeaders(destination, source http.Header) {
	for _, name := range []string{"Accept", "Accept-Language", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since"} {
		for _, value := range headerValues(source, name) {
			destination.Add(name, value)
		}
	}
}

func headerValues(headers http.Header, name string) []string {
	for key, values := range headers {
		if strings.EqualFold(key, name) {
			return values
		}
	}
	return nil
}

func pathPrefixMatches(prefix, path string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func isReservedPath(path string) bool {
	lower := strings.ToLower(path)
	for _, prefix := range []string{"/signin", "/signup", "/signout", "/auth", "/verify-email", "/forgot-password", "/reset-password", "/oauth", "/.well-known", "/account", "/workspaces", "/billing", "/api", "/mcp", "/healthz", "/readyz", "/admin", "/internal", "/identity"} {
		if pathPrefixMatches(prefix, lower) {
			return true
		}
	}
	for _, segment := range strings.Split(strings.TrimPrefix(lower, "/"), "/") {
		if segment == "admin" || segment == "internal" || segment == "identity" {
			return true
		}
	}
	return false
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func isDotPath(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func validQueryKey(value string) bool {
	if value == "" || len(value) > maxQueryKeyBytes {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._~-", r) {
			continue
		}
		return false
	}
	return true
}

func validHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r) {
			continue
		}
		return false
	}
	return true
}

func allowedMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isUnreserved(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || strings.ContainsRune("-._~", rune(value))
}

func upperHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'A' && value <= 'F'
}

func fromHex(value byte) byte {
	if value >= '0' && value <= '9' {
		return value - '0'
	}
	return value - 'A' + 10
}
