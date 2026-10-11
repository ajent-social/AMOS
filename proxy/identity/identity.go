package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const (
	Issuer          = "https://amos.invalid/proxy-issuer"
	Audience        = "business-proxy-v1"
	AssertionHeader = "X-AMOS-Identity-Assertion"
	MaxLifetime     = 60 * time.Second
	ClockSkew       = 5 * time.Second
	maxTokenBytes   = 8 << 10
	maxReference    = 256
	maxScopes       = 32
	maxScopeBytes   = 128
)

var (
	ErrInvalidAssertion = errors.New("invalid private-service identity assertion")
	ErrReplay           = errors.New("private-service identity assertion replayed")
	ErrReplayStore      = errors.New("private-service replay store unavailable")
)

type Identity struct {
	Subject               string
	Installation          string
	Application           string
	Environment           string
	Workspace             string
	AuthorizationDecision string
	Scopes                []string
	TokenID               string
}

type RequestBinding struct {
	Method string
	Path   string
}

type SigningKey struct {
	ID         string
	PrivateKey ed25519.PrivateKey
}

type VerificationKey struct {
	PublicKey   ed25519.PublicKey
	RetiredAt   time.Time
	AcceptUntil time.Time
}

// ReplayStore must atomically consume an issuer/token-ID pair and retain it
// through retainUntil. A false result means that pair was already consumed.
type ReplayStore interface {
	Consume(ctx context.Context, issuer, tokenID string, retainUntil time.Time) (bool, error)
}

type Signer struct {
	signer jose.Signer
	now    func() time.Time
}

type Verifier struct {
	keys   map[string]VerificationKey
	replay ReplayStore
	now    func() time.Time
}

type assertionClaims struct {
	jwt.Claims
	Installation          string   `json:"installation"`
	Application           string   `json:"application"`
	Environment           string   `json:"environment"`
	Workspace             string   `json:"workspace"`
	AuthorizationDecision string   `json:"authorization_decision"`
	Scopes                []string `json:"scopes"`
	Method                string   `json:"method"`
	Path                  string   `json:"path"`
}

type SignerConfig struct {
	Key SigningKey
	Now func() time.Time
}

type VerifierConfig struct {
	Keys   map[string]VerificationKey
	Replay ReplayStore
	Now    func() time.Time
}

func NewSigner(config SignerConfig) (*Signer, error) {
	if !validKeyID(config.Key.ID) || len(config.Key.PrivateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid signer key: %w", ErrInvalidAssertion)
	}
	key := append(ed25519.PrivateKey(nil), config.Key.PrivateKey...)
	options := (&jose.SignerOptions{}).
		WithType("JWT").
		WithHeader(jose.HeaderKey("kid"), config.Key.ID)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: key}, options)
	if err != nil {
		return nil, fmt.Errorf("create assertion signer: %w", err)
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Signer{signer: signer, now: now}, nil
}

func NewVerifier(config VerifierConfig) (*Verifier, error) {
	if config.Replay == nil || len(config.Keys) == 0 {
		return nil, fmt.Errorf("verifier requires trusted keys and an atomic replay store: %w", ErrInvalidAssertion)
	}
	keys := make(map[string]VerificationKey, len(config.Keys))
	for id, key := range config.Keys {
		if !validKeyID(id) || len(key.PublicKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid verifier key: %w", ErrInvalidAssertion)
		}
		if !key.RetiredAt.IsZero() {
			if key.AcceptUntil.IsZero() || key.AcceptUntil.Before(key.RetiredAt) || key.AcceptUntil.After(key.RetiredAt.Add(24*time.Hour)) {
				return nil, fmt.Errorf("retired key overlap exceeds 24 hours: %w", ErrInvalidAssertion)
			}
		}
		key.PublicKey = append(ed25519.PublicKey(nil), key.PublicKey...)
		keys[id] = key
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Verifier{keys: keys, replay: config.Replay, now: now}, nil
}

// Sign issues a compact EdDSA JWT for one canonical method/path pair. The
// caller must supply identity already authorized for this private hop.
func (s *Signer) Sign(identity Identity, binding RequestBinding) (string, error) {
	if s == nil || s.signer == nil || !validIdentity(identity) || !validBinding(binding) {
		return "", ErrInvalidAssertion
	}
	id, err := newTokenID()
	if err != nil {
		return "", fmt.Errorf("generate assertion ID: %w", err)
	}
	now := s.now().UTC().Truncate(time.Second)
	scopes := append([]string(nil), identity.Scopes...)
	// Stable claim encoding avoids semantically equivalent scope lists.
	for i := 0; i < len(scopes); i++ {
		for j := i + 1; j < len(scopes); j++ {
			if scopes[j] < scopes[i] {
				scopes[i], scopes[j] = scopes[j], scopes[i]
			}
		}
	}
	claims := assertionClaims{
		Claims: jwt.Claims{
			Issuer:    Issuer,
			Subject:   identity.Subject,
			Audience:  jwt.Audience{Audience},
			Expiry:    jwt.NewNumericDate(now.Add(MaxLifetime)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        id,
		},
		Installation:          identity.Installation,
		Application:           identity.Application,
		Environment:           identity.Environment,
		Workspace:             identity.Workspace,
		AuthorizationDecision: identity.AuthorizationDecision,
		Scopes:                scopes,
		Method:                binding.Method,
		Path:                  binding.Path,
	}
	serialized, err := jwt.Signed(s.signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("sign assertion: %w", err)
	}
	if len(serialized) > maxTokenBytes {
		return "", ErrInvalidAssertion
	}
	return serialized, nil
}

// Verify checks a trusted signature, fixed issuer/audience, lifetime, key
// overlap, request binding and one-time token ID. Replay storage is mandatory
// and failures are closed.
func (v *Verifier) Verify(ctx context.Context, assertion string, binding RequestBinding) (Identity, error) {
	if v == nil || v.replay == nil || ctx == nil || len(assertion) == 0 || len(assertion) > maxTokenBytes || strings.Count(assertion, ".") != 2 || !validBinding(binding) {
		return Identity{}, ErrInvalidAssertion
	}
	for _, r := range assertion {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return Identity{}, ErrInvalidAssertion
		}
	}
	token, err := jwt.ParseSigned(assertion, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil || len(token.Headers) != 1 {
		return Identity{}, ErrInvalidAssertion
	}
	header := token.Headers[0]
	if header.Algorithm != string(jose.EdDSA) || !validKeyID(header.KeyID) {
		return Identity{}, ErrInvalidAssertion
	}
	if typ, ok := header.ExtraHeaders[jose.HeaderKey("typ")].(string); !ok || typ != "JWT" {
		return Identity{}, ErrInvalidAssertion
	}
	key, ok := v.keys[header.KeyID]
	if !ok {
		return Identity{}, ErrInvalidAssertion
	}
	now := v.now().UTC()
	if !key.AcceptUntil.IsZero() && !now.Before(key.AcceptUntil) {
		return Identity{}, ErrInvalidAssertion
	}
	var claims assertionClaims
	if err := token.Claims(key.PublicKey, &claims); err != nil {
		return Identity{}, ErrInvalidAssertion
	}
	if claims.Issuer != Issuer || len(claims.Audience) != 1 || claims.Audience[0] != Audience {
		return Identity{}, ErrInvalidAssertion
	}
	if err := claims.Claims.ValidateWithLeeway(jwt.Expected{
		Issuer:      Issuer,
		AnyAudience: jwt.Audience{Audience},
		Time:        now,
	}, ClockSkew); err != nil {
		return Identity{}, ErrInvalidAssertion
	}
	if claims.Expiry == nil || claims.NotBefore == nil || claims.IssuedAt == nil || !validTokenID(claims.ID) {
		return Identity{}, ErrInvalidAssertion
	}
	expiresAt := claims.Expiry.Time().UTC()
	issuedAt := claims.IssuedAt.Time().UTC()
	notBefore := claims.NotBefore.Time().UTC()
	if !expiresAt.After(now) || !issuedAt.Before(expiresAt) || expiresAt.Sub(issuedAt) > MaxLifetime || issuedAt.After(now.Add(ClockSkew)) || notBefore.After(now.Add(ClockSkew)) || (!key.RetiredAt.IsZero() && issuedAt.After(key.RetiredAt)) {
		return Identity{}, ErrInvalidAssertion
	}
	identity := Identity{
		Subject:               claims.Subject,
		Installation:          claims.Installation,
		Application:           claims.Application,
		Environment:           claims.Environment,
		Workspace:             claims.Workspace,
		AuthorizationDecision: claims.AuthorizationDecision,
		Scopes:                append([]string(nil), claims.Scopes...),
		TokenID:               claims.ID,
	}
	if !validIdentity(identity) || !equalScopes(claims.Scopes) || claims.Method != binding.Method || claims.Path != binding.Path {
		return Identity{}, ErrInvalidAssertion
	}
	consumed, err := v.replay.Consume(ctx, Issuer, claims.ID, expiresAt.Add(ClockSkew))
	if err != nil {
		return Identity{}, ErrReplayStore
	}
	if !consumed {
		return Identity{}, ErrReplay
	}
	return identity, nil
}

func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values(AssertionHeader)
		assertion := ""
		if len(values) == 1 {
			assertion = values[0]
		}
		StripCallerIdentityHeaders(r.Header)
		if len(values) != 1 || assertion == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		binding, err := RequestBindingFromRequest(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		identity, err := v.Verify(r.Context(), assertion, binding)
		if err != nil {
			status := http.StatusUnauthorized
			if errors.Is(err, ErrReplayStore) {
				status = http.StatusServiceUnavailable
			}
			http.Error(w, "unauthorized", status)
			return
		}
		ctx := context.WithValue(r.Context(), identityContextKey{}, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	if ctx == nil {
		return Identity{}, false
	}
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	identity.Scopes = append([]string(nil), identity.Scopes...)
	return identity, ok
}

type identityContextKey struct{}

// SetAssertionHeader removes client-supplied identity headers before adding
// the server-generated assertion to a private-hop request.
func SetAssertionHeader(headers http.Header, assertion string) error {
	if headers == nil || assertion == "" || len(assertion) > maxTokenBytes {
		return ErrInvalidAssertion
	}
	for _, r := range assertion {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return ErrInvalidAssertion
		}
	}
	StripCallerIdentityHeaders(headers)
	headers.Set(AssertionHeader, assertion)
	return nil
}

func StripCallerIdentityHeaders(headers http.Header) {
	if headers == nil {
		return
	}
	blocked := map[string]struct{}{
		strings.ToLower(AssertionHeader): {},
		"x-amos-identity":                {},
		"x-amos-subject":                 {},
		"x-amos-workspace":               {},
		"x-amos-scopes":                  {},
		"x-amos-authorization-decision":  {},
		"x-authenticated-user":           {},
		"x-authenticated-user-id":        {},
		"x-authenticated-email":          {},
		"x-authenticated-groups":         {},
		"x-auth-request-user":            {},
		"x-auth-request-user-id":         {},
		"x-auth-request-email":           {},
		"x-auth-request-groups":          {},
		"x-forwarded-user":               {},
		"x-forwarded-user-id":            {},
		"x-forwarded-remote-user":        {},
		"x-forwarded-email":              {},
		"x-forwarded-preferred-username": {},
		"x-forwarded-groups":             {},
		"x-remote-user":                  {},
		"x-remote-user-id":               {},
		"x-remote-email":                 {},
		"x-remote-groups":                {},
		"x-original-user":                {},
		"x-original-user-id":             {},
		"x-original-email":               {},
		"x-original-groups":              {},
		"x-user":                         {},
		"x-user-id":                      {},
		"x-user-email":                   {},
		"x-user-groups":                  {},
		"x-email":                        {},
		"x-groups":                       {},
		"remote-user":                    {},
	}
	for name := range headers {
		if _, ok := blocked[strings.ToLower(name)]; ok {
			delete(headers, name)
		}
	}
}

func RequestBindingFromRequest(r *http.Request) (RequestBinding, error) {
	if r == nil || r.URL == nil {
		return RequestBinding{}, ErrInvalidAssertion
	}
	binding := RequestBinding{Method: r.Method, Path: r.URL.EscapedPath()}
	if !validBinding(binding) {
		return RequestBinding{}, ErrInvalidAssertion
	}
	return binding, nil
}

func validIdentity(identity Identity) bool {
	if !validReference(identity.Subject) || !validReference(identity.Installation) || !validReference(identity.Application) || !validReference(identity.Environment) || !validReference(identity.Workspace) || !validReference(identity.AuthorizationDecision) {
		return false
	}
	if len(identity.Scopes) == 0 || len(identity.Scopes) > maxScopes {
		return false
	}
	seen := make(map[string]struct{}, len(identity.Scopes))
	for _, scope := range identity.Scopes {
		if !validScope(scope) {
			return false
		}
		if _, exists := seen[scope]; exists {
			return false
		}
		seen[scope] = struct{}{}
	}
	return true
}

func validReference(value string) bool {
	if value == "" || len(value) > maxReference || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validScope(scope string) bool {
	if scope == "" || len(scope) > maxScopeBytes {
		return false
	}
	for _, r := range scope {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/:._-", r)) {
			return false
		}
	}
	return true
}

func equalScopes(scopes []string) bool {
	if len(scopes) == 0 || len(scopes) > maxScopes {
		return false
	}
	for i, scope := range scopes {
		if !validScope(scope) || (i > 0 && scopes[i-1] >= scope) {
			return false
		}
	}
	return true
}

func validBinding(binding RequestBinding) bool {
	if binding.Method == "" || binding.Method != strings.ToUpper(binding.Method) || len(binding.Method) > 32 {
		return false
	}
	for _, r := range binding.Method {
		if !isHTTPTokenRune(r) {
			return false
		}
	}
	return validPath(binding.Path)
}

func validPath(value string) bool {
	if value == "" || len(value) > 2048 || value[0] != '/' || strings.ContainsAny(value, "?#\\") || !utf8.ValidString(value) {
		return false
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return false
	}
	decoded, err := url.PathUnescape(value)
	if err != nil || !utf8.ValidString(decoded) || strings.ContainsAny(decoded, "?#\\") || strings.Contains(decoded, "//") || containsEscapedOctet(decoded) {
		return false
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return (&url.URL{Path: decoded}).EscapedPath() == value
}

func containsEscapedOctet(value string) bool {
	for i := 0; i+2 < len(value); i++ {
		if value[i] == '%' && isHexDigit(value[i+1]) && isHexDigit(value[i+2]) {
			return true
		}
	}
	return false
}

func isHexDigit(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func isHTTPTokenRune(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return true
	}
	return strings.ContainsRune("!#$%&'*+-.^_`|~", r)
}

func validKeyID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
			return false
		}
	}
	return true
}

func newTokenID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func validTokenID(id string) bool {
	if len(id) != 22 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(decoded) == 16
}
