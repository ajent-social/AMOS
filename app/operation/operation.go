// Package operation defines immutable typed operation descriptors and their
// startup registry. It does not execute operations or establish authority.
package operation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/jobs"
	"github.com/ajent-social/amos/policy"
	workspacecontext "github.com/ajent-social/amos/workspace/context"
)

var (
	ErrInvalidDefinition    = errors.New("invalid operation definition")
	ErrDuplicateOperation   = errors.New("duplicate operation ID")
	ErrRouteConflict        = errors.New("operation route conflicts with a registered route")
	ErrReservedRoute        = errors.New("operation route is reserved by AMOS")
	ErrSchemaDigestMismatch = errors.New("operation codec schema digest mismatch")
)

// SideEffect classifies an operation's domain and external effect behavior.
type SideEffect string

const (
	SideEffectRead     SideEffect = "read"
	SideEffectWrite    SideEffect = "write"
	SideEffectExternal SideEffect = "external"
)

// Idempotency declares whether a mutation requires, naturally provides, or
// forbids automatic retry semantics.
type Idempotency string

const (
	IdempotencyRequired  Idempotency = "required"
	IdempotencyNatural   Idempotency = "natural"
	IdempotencyForbidden Idempotency = "forbidden"
)

// OutputClass declares whether an operation output is eligible for replay
// storage or is sensitive and therefore excluded from it.
type OutputClass string

const (
	OutputReplaySafe OutputClass = "replay_safe"
	OutputSensitive  OutputClass = "sensitive"
	OutputSecret     OutputClass = "secret"
)

// NameValues represents one named path or query value with its cardinality
// preserved for strict decoding.
type NameValues struct {
	Name   string
	Values []string
}

// RawInput carries transport-neutral path, query, and body input to a typed
// codec. It is not a source of identity or authority.
type RawInput struct {
	Path  []NameValues
	Query []NameValues
	Body  []byte
}

// Request identifies an operation and its untrusted raw input. It does not
// carry principal, actor, workspace, or authority fields.
type Request struct {
	OperationID    string
	IdempotencyKey string
	Input          RawInput
}

// Metadata is the complete immutable descriptor bound to a typed operation.
type Metadata struct {
	OperationID        string
	Method             string
	Path               string
	InputType          string
	OutputType         string
	Features           []string
	Revision           string
	InputSchemaDigest  [32]byte
	OutputSchemaDigest [32]byte
	Requirements       policy.Requirements
	SideEffect         SideEffect
	Idempotency        Idempotency
	OutputClass        OutputClass
	MaxReplayBytes     int
}

// InputCodec must be immutable after construction. SchemaDigest identifies
// the schema used by DecodeCanonical. Equality is a configuration check for
// trusted owner-reviewed codecs, not an attestation of codec behavior.
type InputCodec[I any] interface {
	SchemaDigest() [32]byte
	DecodeCanonical(RawInput) (typed I, canonical []byte, err error)
}

// ResolvedResources describes the authorization resource and complete lock
// set derived from validated input and trusted workspace selection.
type ResolvedResources struct {
	AuthorizationResource policy.Resource
	LockSet               []policy.Resource
}

// ResourceResolver maps a typed input to its affected resources.
type ResourceResolver[I any] interface {
	Resolve(identity.Principal, workspacecontext.Selection, I) (ResolvedResources, error)
}

// ResultKind is the finite transport-neutral operation outcome class.
type ResultKind string

const (
	ResultSucceeded ResultKind = "succeeded"
	ResultCreated   ResultKind = "created"
	ResultAccepted  ResultKind = "accepted"
	ResultNoContent ResultKind = "no_content"
)

// Result preserves typed output together with its finite logical outcome.
type Result[O any] struct {
	Kind  ResultKind
	Value O
}

// CachedResult contains a validated canonical output for a future replay store.
// This package does not persist or disclose it.
type CachedResult struct {
	Kind          ResultKind
	CanonicalJSON []byte
}

// OutputCodec must be immutable after construction. SchemaDigest identifies
// the schema used by EncodeCanonical and ValidateCanonical. Equality is a
// configuration check for trusted owner-reviewed codecs, not an attestation.
type OutputCodec[O any] interface {
	SchemaDigest() [32]byte
	EncodeCanonical(O) ([]byte, error)
	ValidateCanonical([]byte) error
}

// Rows is the callback-facing subset of database rows.
type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

// Row is the callback-facing subset of a database row.
type Row interface{ Scan(...any) error }

// DBTX is the callback-facing SQL surface without transaction lifecycle
// methods. No executor or transaction wrapper is implemented in this package.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (Rows, error)
	QueryRowContext(context.Context, string, ...any) Row
}

// InvocationContext is the typed handler context shape reserved for a future
// transaction-aware executor. Defining it does not enable dispatch.
type InvocationContext struct {
	Principal       identity.Principal
	Selection       workspacecontext.Selection
	Resource        policy.Resource
	LockedResources []policy.Resource
	InvocationID    identity.ID
	DB              DBTX
	Effects         EffectEnqueuer
}

// EffectEnqueuer is the operation-scoped durable-intent interface. Its
// transaction-bound implementation is owned by integrator work.
type EffectEnqueuer interface {
	Enqueue(context.Context, uint32, jobs.Intent) (jobs.Job, error)
}

// Definition binds typed codecs, resolver, handler, and copied metadata.
// Callers can inspect only the metadata snapshot and descriptor digest.
type Definition struct {
	metadata         Metadata
	descriptorDigest [32]byte
	decode           func(RawInput) (any, []byte, error)
	resolve          func(identity.Principal, workspacecontext.Selection, any) (ResolvedResources, error)
	encode           func(any) ([]byte, error)
	validateOutput   func([]byte) error
	handle           func(context.Context, InvocationContext, any) (any, error)
	complete         func(any) (CachedResult, error)
	invoke           func(context.Context, InvocationContext, any) (CachedResult, error)
}

// Metadata returns a defensive copy of the bound operation descriptor.
func (d Definition) Metadata() Metadata {
	return cloneMetadata(d.metadata)
}

// DescriptorDigest returns the SHA-256 digest of the canonical descriptor.
func (d Definition) DescriptorDigest() [32]byte {
	return d.descriptorDigest
}

// Registry is a read-only lookup surface for registered operation definitions.
type Registry interface {
	Lookup(string) (Definition, bool)
}

// FrozenRegistry is an immutable snapshot created before application serving.
type FrozenRegistry struct {
	definitions map[string]Definition
}

// Lookup returns a definition snapshot for the exact registered operation ID.
func (r *FrozenRegistry) Lookup(operationID string) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	d, ok := r.definitions[operationID]
	if !ok {
		return Definition{}, false
	}
	return d, true
}

// Bind validates metadata and creates an immutable typed definition. It does
// not execute the handler or establish authorization.
func Bind[I, O any](metadata Metadata, input InputCodec[I], resolver ResourceResolver[I], output OutputCodec[O], handler func(context.Context, InvocationContext, I) (Result[O], error)) (Definition, error) {
	if nilLike(input) || nilLike(resolver) || nilLike(output) || handler == nil {
		return Definition{}, fmt.Errorf("%w: codec, resolver, and handler are required", ErrInvalidDefinition)
	}
	inputSchemaDigest := input.SchemaDigest()
	outputSchemaDigest := output.SchemaDigest()
	if inputSchemaDigest == ([32]byte{}) || outputSchemaDigest == ([32]byte{}) {
		return Definition{}, ErrSchemaDigestMismatch
	}
	meta, err := validateMetadata(metadata)
	if err != nil {
		return Definition{}, err
	}
	if inputSchemaDigest != meta.InputSchemaDigest || outputSchemaDigest != meta.OutputSchemaDigest {
		return Definition{}, ErrSchemaDigestMismatch
	}

	definition := Definition{metadata: meta}
	definition.descriptorDigest, err = digestMetadata(meta)
	if err != nil {
		return Definition{}, fmt.Errorf("%w: digest metadata: %v", ErrInvalidDefinition, err)
	}
	definition.decode = func(raw RawInput) (any, []byte, error) {
		value, canonical, err := input.DecodeCanonical(cloneRawInput(raw))
		if err != nil {
			return nil, nil, err
		}
		return value, append([]byte(nil), canonical...), nil
	}
	definition.resolve = func(principal identity.Principal, selection workspacecontext.Selection, value any) (ResolvedResources, error) {
		typed, ok := value.(I)
		if !ok {
			return ResolvedResources{}, ErrInvalidDefinition
		}
		resolved, err := resolver.Resolve(principal, selection, typed)
		if err != nil {
			return ResolvedResources{}, err
		}
		resolved.LockSet = append([]policy.Resource(nil), resolved.LockSet...)
		return resolved, nil
	}
	definition.encode = func(value any) ([]byte, error) {
		typed, ok := value.(O)
		if !ok {
			return nil, ErrInvalidDefinition
		}
		encoded, err := output.EncodeCanonical(typed)
		if err != nil {
			return nil, err
		}
		return append([]byte(nil), encoded...), nil
	}
	definition.validateOutput = func(value []byte) error {
		return output.ValidateCanonical(append([]byte(nil), value...))
	}
	definition.handle = func(ctx context.Context, invocation InvocationContext, value any) (any, error) {
		typed, ok := value.(I)
		if !ok {
			return nil, ErrInvalidDefinition
		}
		result, err := handler(ctx, cloneInvocationContext(invocation), typed)
		if err != nil {
			return nil, err
		}
		// Box the entire result so an interface-typed nil output retains O.
		return result, nil
	}
	definition.complete = func(value any) (CachedResult, error) {
		result, ok := value.(Result[O])
		if !ok || !validResultKind(result.Kind) {
			return CachedResult{}, ErrInvalidDefinition
		}
		encoded, err := output.EncodeCanonical(result.Value)
		if err != nil {
			return CachedResult{}, err
		}
		if err := output.ValidateCanonical(encoded); err != nil {
			return CachedResult{}, err
		}
		return CachedResult{Kind: result.Kind, CanonicalJSON: append([]byte(nil), encoded...)}, nil
	}
	definition.invoke = func(ctx context.Context, invocation InvocationContext, value any) (CachedResult, error) {
		result, err := definition.handle(ctx, invocation, value)
		if err != nil {
			return CachedResult{}, err
		}
		return definition.complete(result)
	}
	return definition, nil
}

// NewRegistry validates descriptor uniqueness and route compatibility, then
// freezes the definitions into an immutable lookup snapshot.
func NewRegistry(definitions ...Definition) (*FrozenRegistry, error) {
	if len(definitions) == 0 {
		return nil, fmt.Errorf("%w: registry must contain at least one operation", ErrInvalidDefinition)
	}
	registry := &FrozenRegistry{definitions: make(map[string]Definition, len(definitions))}
	routes := make([]routeBinding, 0, len(definitions))
	for index, candidate := range definitions {
		if candidate.metadata.OperationID == "" || candidate.decode == nil || candidate.resolve == nil || candidate.encode == nil || candidate.validateOutput == nil || candidate.invoke == nil || candidate.handle == nil || candidate.complete == nil {
			return nil, fmt.Errorf("%w: definition %d was not created by Bind", ErrInvalidDefinition, index)
		}
		meta, err := validateMetadata(candidate.metadata)
		if err != nil {
			return nil, fmt.Errorf("definition %d: %w", index, err)
		}
		digest, err := digestMetadata(meta)
		if err != nil || digest != candidate.descriptorDigest {
			return nil, fmt.Errorf("%w: definition %d metadata changed after Bind", ErrInvalidDefinition, index)
		}
		if _, exists := registry.definitions[meta.OperationID]; exists {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateOperation, meta.OperationID)
		}
		newRoute := routeBinding{method: meta.Method, path: meta.Path}
		for _, existing := range routes {
			if routesOverlap(existing, newRoute) {
				return nil, fmt.Errorf("%w: %s %s", ErrRouteConflict, meta.Method, meta.Path)
			}
		}
		routes = append(routes, newRoute)
		candidate.metadata = meta
		registry.definitions[meta.OperationID] = candidate
	}
	return registry, nil
}

var (
	operationIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
	requirementPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.:_-][a-z0-9]+)*$`)
	typeNamePattern    = regexp.MustCompile(`^(?:[A-Z][A-Za-z0-9]*|[a-z][A-Za-z0-9]*(?:\.[A-Z][A-Za-z0-9]*)+)$`)
	pathPattern        = regexp.MustCompile(`^/(?:[A-Za-z0-9_~-][A-Za-z0-9._~-]*|\.[A-Za-z0-9_-][A-Za-z0-9._~-]*)(?:/(?:[A-Za-z0-9_~-][A-Za-z0-9._~-]*|\.[A-Za-z0-9_-][A-Za-z0-9._~-]*))*(?:/\*)?$`)
	reservedRoots      = []string{"/signin", "/signout", "/signup", "/auth", "/verify-email", "/forgot-password", "/reset-password", "/oauth", "/.well-known", "/account", "/workspaces", "/billing", "/api", "/mcp", "/healthz", "/readyz"}
)

func validateMetadata(metadata Metadata) (Metadata, error) {
	if metadata.Features == nil || metadata.Requirements.Permissions == nil || metadata.Requirements.Entitlements == nil {
		return Metadata{}, fmt.Errorf("%w: features, permissions, and entitlements must be explicit arrays", ErrInvalidDefinition)
	}
	if !validString(metadata.OperationID, 3, 120) || !operationIDPattern.MatchString(metadata.OperationID) {
		return Metadata{}, fmt.Errorf("%w: invalid operation ID", ErrInvalidDefinition)
	}
	if !validMethod(metadata.Method) {
		return Metadata{}, fmt.Errorf("%w: method must be a supported canonical uppercase method", ErrInvalidDefinition)
	}
	if !validPathPattern(metadata.Path) {
		return Metadata{}, fmt.Errorf("%w: invalid or noncanonical business path", ErrInvalidDefinition)
	}
	if isReserved(metadata.Path) {
		return Metadata{}, fmt.Errorf("%w: %s", ErrReservedRoute, metadata.Path)
	}
	if !validString(metadata.InputType, 1, 160) || !typeNamePattern.MatchString(metadata.InputType) || !validString(metadata.OutputType, 1, 160) || !typeNamePattern.MatchString(metadata.OutputType) {
		return Metadata{}, fmt.Errorf("%w: invalid typed input or output name", ErrInvalidDefinition)
	}
	if !validString(metadata.Revision, 1, 160) {
		return Metadata{}, fmt.Errorf("%w: invalid operation revision", ErrInvalidDefinition)
	}
	if metadata.InputSchemaDigest == ([32]byte{}) || metadata.OutputSchemaDigest == ([32]byte{}) {
		return Metadata{}, fmt.Errorf("%w: schema digests must be nonzero", ErrInvalidDefinition)
	}
	if len(metadata.Features) > 16 {
		return Metadata{}, fmt.Errorf("%w: too many operation features", ErrInvalidDefinition)
	}
	for _, feature := range metadata.Features {
		if feature != "typed-json" && feature != "server-rendered-ui" {
			return Metadata{}, fmt.Errorf("%w: unsupported feature %q", ErrInvalidDefinition, feature)
		}
	}
	features, err := sortedUnique(metadata.Features)
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: features must be unique", ErrInvalidDefinition)
	}
	metadata.Features = features
	permissions, err := validateNames(metadata.Requirements.Permissions)
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: invalid permissions", ErrInvalidDefinition)
	}
	entitlements, err := validateNames(metadata.Requirements.Entitlements)
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: invalid entitlements", ErrInvalidDefinition)
	}
	metadata.Requirements.Permissions = permissions
	metadata.Requirements.Entitlements = entitlements
	if len(permissions) > 64 || len(entitlements) > 64 {
		return Metadata{}, fmt.Errorf("%w: too many requirements", ErrInvalidDefinition)
	}
	if metadata.Requirements.Assurance != identity.AAL1 && metadata.Requirements.Assurance != identity.AAL2 && metadata.Requirements.Assurance != identity.AAL3 {
		return Metadata{}, fmt.Errorf("%w: invalid assurance requirement", ErrInvalidDefinition)
	}
	if metadata.Requirements.MCPExposure != policy.MCPNever && metadata.Requirements.MCPExposure != policy.MCPEligible && metadata.Requirements.MCPExposure != policy.MCPChallenge {
		return Metadata{}, fmt.Errorf("%w: invalid MCP exposure", ErrInvalidDefinition)
	}
	if metadata.SideEffect != SideEffectRead && metadata.SideEffect != SideEffectWrite && metadata.SideEffect != SideEffectExternal {
		return Metadata{}, fmt.Errorf("%w: invalid side effect", ErrInvalidDefinition)
	}
	if metadata.Idempotency != IdempotencyRequired && metadata.Idempotency != IdempotencyNatural && metadata.Idempotency != IdempotencyForbidden {
		return Metadata{}, fmt.Errorf("%w: invalid idempotency mode", ErrInvalidDefinition)
	}
	if metadata.OutputClass != OutputReplaySafe && metadata.OutputClass != OutputSensitive && metadata.OutputClass != OutputSecret {
		return Metadata{}, fmt.Errorf("%w: invalid output classification", ErrInvalidDefinition)
	}
	if metadata.Idempotency == IdempotencyRequired {
		if metadata.SideEffect == SideEffectRead || metadata.OutputClass != OutputReplaySafe || metadata.MaxReplayBytes < 1 || metadata.MaxReplayBytes > 65536 {
			return Metadata{}, fmt.Errorf("%w: required replay needs a write or external effect, replay-safe output, and a 1..65536 byte bound", ErrInvalidDefinition)
		}
	} else if metadata.MaxReplayBytes != 0 {
		return Metadata{}, fmt.Errorf("%w: replay byte bound is only valid for required idempotency", ErrInvalidDefinition)
	}
	return cloneMetadata(metadata), nil
}

func validateNames(names []string) ([]string, error) {
	if len(names) > 64 {
		return nil, ErrInvalidDefinition
	}
	for _, name := range names {
		if !validString(name, 2, 120) || !requirementPattern.MatchString(name) {
			return nil, ErrInvalidDefinition
		}
	}
	return sortedUnique(names)
}

func sortedUnique(values []string) ([]string, error) {
	if values == nil {
		return nil, nil
	}
	copyOf := append([]string{}, values...)
	sort.Strings(copyOf)
	for index := 1; index < len(copyOf); index++ {
		if copyOf[index-1] == copyOf[index] {
			return nil, ErrInvalidDefinition
		}
	}
	return copyOf, nil
}

func validMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return true
	default:
		return false
	}
}

func validPathPattern(path string) bool {
	if !validString(path, 2, 512) || !pathPattern.MatchString(path) || strings.Contains(path, "//") || strings.HasSuffix(path, "/") || strings.ContainsAny(path, "%?#\\") {
		return false
	}
	base := strings.TrimSuffix(path, "/*")
	decoded, err := url.PathUnescape(base)
	if err != nil || decoded != base || !utf8.ValidString(decoded) {
		return false
	}
	for _, r := range decoded {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func isReserved(path string) bool {
	base := strings.TrimSuffix(path, "/*")
	lower := strings.ToLower(base)
	for _, root := range reservedRoots {
		if lower == root || strings.HasPrefix(lower, root+"/") {
			return true
		}
	}
	return false
}

type routeBinding struct {
	method string
	path   string
}

func routesOverlap(left, right routeBinding) bool {
	if left.method != right.method {
		return false
	}
	leftWild := strings.HasSuffix(left.path, "/*")
	rightWild := strings.HasSuffix(right.path, "/*")
	leftBase := strings.TrimSuffix(left.path, "/*")
	rightBase := strings.TrimSuffix(right.path, "/*")
	if !leftWild && !rightWild {
		return leftBase == rightBase
	}
	if leftWild && rightWild {
		return leftBase == rightBase || strings.HasPrefix(leftBase, rightBase+"/") || strings.HasPrefix(rightBase, leftBase+"/")
	}
	if leftWild {
		return strings.HasPrefix(rightBase, leftBase+"/")
	}
	return strings.HasPrefix(leftBase, rightBase+"/")
}

func validString(value string, min, max int) bool {
	if !utf8.ValidString(value) || len(value) < min || len(value) > max || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func cloneMetadata(metadata Metadata) Metadata {
	metadata.Features = cloneStrings(metadata.Features)
	metadata.Requirements.Permissions = cloneStrings(metadata.Requirements.Permissions)
	metadata.Requirements.Entitlements = cloneStrings(metadata.Requirements.Entitlements)
	return metadata
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}

func cloneRawInput(input RawInput) RawInput {
	return RawInput{Path: cloneNameValues(input.Path), Query: cloneNameValues(input.Query), Body: append([]byte(nil), input.Body...)}
}

func cloneNameValues(values []NameValues) []NameValues {
	copyOf := make([]NameValues, len(values))
	for index, value := range values {
		copyOf[index] = NameValues{Name: value.Name, Values: append([]string(nil), value.Values...)}
	}
	return copyOf
}

func cloneInvocationContext(invocation InvocationContext) InvocationContext {
	invocation.Selection.Permissions = cloneStrings(invocation.Selection.Permissions)
	if invocation.Selection.Membership != nil {
		membership := *invocation.Selection.Membership
		invocation.Selection.Membership = &membership
	}
	invocation.LockedResources = append([]policy.Resource(nil), invocation.LockedResources...)
	return invocation
}

func nilLike(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func validResultKind(kind ResultKind) bool {
	switch kind {
	case ResultSucceeded, ResultCreated, ResultAccepted, ResultNoContent:
		return true
	default:
		return false
	}
}

func digestMetadata(metadata Metadata) ([32]byte, error) {
	canonical := struct {
		Version            string   `json:"version"`
		OperationID        string   `json:"operationId"`
		Method             string   `json:"method"`
		Path               string   `json:"path"`
		InputType          string   `json:"inputType"`
		OutputType         string   `json:"outputType"`
		Features           []string `json:"features"`
		Revision           string   `json:"revision"`
		InputSchemaDigest  string   `json:"inputSchemaDigest"`
		OutputSchemaDigest string   `json:"outputSchemaDigest"`
		Permissions        []string `json:"permissions"`
		Entitlements       []string `json:"entitlements"`
		Assurance          string   `json:"assurance"`
		MCPExposure        string   `json:"mcpExposure"`
		SideEffect         string   `json:"sideEffect"`
		Idempotency        string   `json:"idempotency"`
		OutputClass        string   `json:"outputClass"`
		MaxReplayBytes     int      `json:"maxReplayBytes"`
	}{Version: "amos-operation-descriptor-v1", OperationID: metadata.OperationID, Method: metadata.Method, Path: metadata.Path, InputType: metadata.InputType, OutputType: metadata.OutputType, Features: metadata.Features, Revision: metadata.Revision, InputSchemaDigest: fmt.Sprintf("%x", metadata.InputSchemaDigest), OutputSchemaDigest: fmt.Sprintf("%x", metadata.OutputSchemaDigest), Permissions: metadata.Requirements.Permissions, Entitlements: metadata.Requirements.Entitlements, Assurance: string(metadata.Requirements.Assurance), MCPExposure: string(metadata.Requirements.MCPExposure), SideEffect: string(metadata.SideEffect), Idempotency: string(metadata.Idempotency), OutputClass: string(metadata.OutputClass), MaxReplayBytes: metadata.MaxReplayBytes}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
