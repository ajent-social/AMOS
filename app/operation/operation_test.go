package operation_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ajent-social/amos/app/operation"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/policy"
	workspacecontext "github.com/ajent-social/amos/workspace/context"
)

type testInput struct {
	Name string `json:"name"`
}

type testOutput struct {
	ID string `json:"id"`
}

type testInputCodec struct {
	digest [32]byte
	calls  int
}

func (c *testInputCodec) SchemaDigest() [32]byte {
	c.calls++
	return c.digest
}
func (c *testInputCodec) DecodeCanonical(input operation.RawInput) (testInput, []byte, error) {
	var value testInput
	if err := json.Unmarshal(input.Body, &value); err != nil {
		return testInput{}, nil, err
	}
	canonical, err := json.Marshal(value)
	return value, canonical, err
}

type testOutputCodec struct {
	digest [32]byte
	calls  int
}

func (c *testOutputCodec) SchemaDigest() [32]byte {
	c.calls++
	return c.digest
}
func (c *testOutputCodec) EncodeCanonical(value testOutput) ([]byte, error) {
	return json.Marshal(value)
}
func (c *testOutputCodec) ValidateCanonical(value []byte) error {
	if !json.Valid(value) {
		return errors.New("invalid JSON")
	}
	return nil
}

type testResolver struct{}

func (testResolver) Resolve(identity.Principal, workspacecontext.Selection, testInput) (operation.ResolvedResources, error) {
	return operation.ResolvedResources{}, nil
}

func schemaDigests() ([32]byte, [32]byte) {
	return sha256.Sum256([]byte("input-schema")), sha256.Sum256([]byte("output-schema"))
}

func validMetadata() operation.Metadata {
	inputDigest, outputDigest := schemaDigests()
	return operation.Metadata{
		OperationID:        "todos.create",
		Method:             "POST",
		Path:               "/todos",
		InputType:          "todos.CreateInput",
		OutputType:         "todos.CreateOutput",
		Features:           []string{"typed-json"},
		Revision:           "v1",
		InputSchemaDigest:  inputDigest,
		OutputSchemaDigest: outputDigest,
		Requirements: policy.Requirements{
			Permissions:  []string{"todos.write"},
			Entitlements: []string{},
			Assurance:    identity.AAL1,
			MCPExposure:  policy.MCPNever,
		},
		SideEffect:     operation.SideEffectWrite,
		Idempotency:    operation.IdempotencyRequired,
		OutputClass:    operation.OutputReplaySafe,
		MaxReplayBytes: 1024,
	}
}

func bind(t *testing.T, metadata operation.Metadata) (operation.Definition, error) {
	t.Helper()
	inputDigest, outputDigest := schemaDigests()
	return bindWithCodecDigests(t, metadata, inputDigest, outputDigest)
}

func bindWithCodecDigests(t *testing.T, metadata operation.Metadata, inputDigest, outputDigest [32]byte) (operation.Definition, error) {
	t.Helper()
	return operation.Bind(metadata, &testInputCodec{digest: inputDigest}, testResolver{}, &testOutputCodec{digest: outputDigest}, func(context.Context, operation.InvocationContext, testInput) (operation.Result[testOutput], error) {
		return operation.Result[testOutput]{Kind: operation.ResultCreated, Value: testOutput{ID: "test-id"}}, nil
	})
}

func TestBindAndRegistryFreezeDescriptorSnapshot(t *testing.T) {
	metadata := validMetadata()
	metadata.Features = []string{"server-rendered-ui", "typed-json"}
	metadata.Requirements.Permissions = []string{"todos.write", "workspace.read"}
	metadata.Requirements.Entitlements = []string{"todos.enabled"}
	definition, err := bind(t, metadata)
	if err != nil {
		t.Fatal(err)
	}

	metadata.Features[0] = "mutated-feature"
	metadata.Requirements.Permissions[0] = "mutated.permission"
	metadata.Requirements.Entitlements[0] = "mutated.entitlement"
	registry, err := operation.NewRegistry(definition)
	if err != nil {
		t.Fatal(err)
	}
	registered, ok := registry.Lookup("todos.create")
	if !ok {
		t.Fatal("registered operation not found")
	}
	got := registered.Metadata()
	if strings.Join(got.Features, ",") != "server-rendered-ui,typed-json" {
		t.Fatalf("features were not copied and canonicalized: %v", got.Features)
	}
	if strings.Join(got.Requirements.Permissions, ",") != "todos.write,workspace.read" {
		t.Fatalf("permissions were not copied and canonicalized: %v", got.Requirements.Permissions)
	}
	if strings.Join(got.Requirements.Entitlements, ",") != "todos.enabled" {
		t.Fatalf("metadata changed after Bind: %v", got.Requirements.Entitlements)
	}
	got.Features[0] = "caller-mutation"
	got.Requirements.Permissions[0] = "caller.mutation"
	second, ok := registry.Lookup("todos.create")
	if !ok {
		t.Fatal("registered operation disappeared")
	}
	secondMetadata := second.Metadata()
	if secondMetadata.Features[0] != "server-rendered-ui" || secondMetadata.Requirements.Permissions[0] != "todos.write" {
		t.Fatalf("lookup exposed mutable registry metadata: %+v", secondMetadata)
	}
	if second.DescriptorDigest() == ([32]byte{}) {
		t.Fatal("descriptor digest is empty")
	}
}

func TestDescriptorDigestCanonicalizesSetMetadata(t *testing.T) {
	first := validMetadata()
	first.Features = []string{"server-rendered-ui", "typed-json"}
	first.Requirements.Permissions = []string{"workspace.read", "todos.write"}
	first.Requirements.Entitlements = []string{"todos.enabled", "workspace.enabled"}
	firstDefinition, err := bind(t, first)
	if err != nil {
		t.Fatal(err)
	}

	second := validMetadata()
	second.Features = []string{"typed-json", "server-rendered-ui"}
	second.Requirements.Permissions = []string{"todos.write", "workspace.read"}
	second.Requirements.Entitlements = []string{"workspace.enabled", "todos.enabled"}
	secondDefinition, err := bind(t, second)
	if err != nil {
		t.Fatal(err)
	}
	if firstDefinition.DescriptorDigest() != secondDefinition.DescriptorDigest() {
		t.Fatal("set ordering changed canonical descriptor digest")
	}
}

func TestDescriptorDigestBindsEveryMetadataField(t *testing.T) {
	baseDefinition, err := bind(t, validMetadata())
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		change func(*operation.Metadata)
	}{
		{name: "operation ID", change: func(m *operation.Metadata) { m.OperationID = "todos.update" }},
		{name: "method", change: func(m *operation.Metadata) { m.Method = "PUT" }},
		{name: "path", change: func(m *operation.Metadata) { m.Path = "/todos/item" }},
		{name: "input type", change: func(m *operation.Metadata) { m.InputType = "todos.UpdateInput" }},
		{name: "output type", change: func(m *operation.Metadata) { m.OutputType = "todos.UpdateOutput" }},
		{name: "features", change: func(m *operation.Metadata) { m.Features = []string{"server-rendered-ui"} }},
		{name: "revision", change: func(m *operation.Metadata) { m.Revision = "v2" }},
		{name: "input schema digest", change: func(m *operation.Metadata) { m.InputSchemaDigest = sha256.Sum256([]byte("new-input-schema")) }},
		{name: "output schema digest", change: func(m *operation.Metadata) { m.OutputSchemaDigest = sha256.Sum256([]byte("new-output-schema")) }},
		{name: "permissions", change: func(m *operation.Metadata) { m.Requirements.Permissions = []string{"todos.update"} }},
		{name: "entitlements", change: func(m *operation.Metadata) { m.Requirements.Entitlements = []string{"todos.updated"} }},
		{name: "assurance", change: func(m *operation.Metadata) { m.Requirements.Assurance = identity.AAL2 }},
		{name: "MCP exposure", change: func(m *operation.Metadata) { m.Requirements.MCPExposure = policy.MCPEligible }},
		{name: "side effect", change: func(m *operation.Metadata) { m.SideEffect = operation.SideEffectExternal }},
		{name: "idempotency", change: func(m *operation.Metadata) { m.Idempotency = operation.IdempotencyNatural; m.MaxReplayBytes = 0 }},
		{name: "output classification", change: func(m *operation.Metadata) {
			m.Idempotency = operation.IdempotencyForbidden
			m.MaxReplayBytes = 0
			m.OutputClass = operation.OutputSensitive
		}},
		{name: "replay bound", change: func(m *operation.Metadata) { m.MaxReplayBytes++ }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			metadata := validMetadata()
			mutation.change(&metadata)
			definition, err := bindWithCodecDigests(t, metadata, metadata.InputSchemaDigest, metadata.OutputSchemaDigest)
			if err != nil {
				t.Fatalf("Bind() error = %v", err)
			}
			if definition.DescriptorDigest() == baseDefinition.DescriptorDigest() {
				t.Fatal("metadata change did not change descriptor digest")
			}
		})
	}
}

func TestExplicitEmptyMetadataCollectionsSurviveRegistryFreeze(t *testing.T) {
	metadata := validMetadata()
	metadata.Features = []string{}
	metadata.Requirements.Permissions = []string{}
	metadata.Requirements.Entitlements = []string{}
	definition, err := bind(t, metadata)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := operation.NewRegistry(definition)
	if err != nil {
		t.Fatalf("NewRegistry() with explicit empty arrays: %v", err)
	}
	registered, ok := registry.Lookup(metadata.OperationID)
	if !ok {
		t.Fatal("registered operation not found")
	}
	got := registered.Metadata()
	if got.Features == nil || got.Requirements.Permissions == nil || got.Requirements.Entitlements == nil {
		t.Fatalf("explicit empty arrays became nil: features=%v permissions=%v entitlements=%v", got.Features, got.Requirements.Permissions, got.Requirements.Entitlements)
	}
}

func TestFrozenRegistryConcurrentLookups(t *testing.T) {
	definition, err := bind(t, validMetadata())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := operation.NewRegistry(definition)
	if err != nil {
		t.Fatal(err)
	}

	const readers = 12
	var wait sync.WaitGroup
	wait.Add(readers)
	for range readers {
		go func() {
			defer wait.Done()
			for range 100 {
				found, ok := registry.Lookup("todos.create")
				if !ok || found.Metadata().OperationID != "todos.create" {
					t.Errorf("concurrent lookup returned an invalid definition")
					return
				}
			}
		}()
	}
	wait.Wait()
}

func TestBindRejectsSchemaDigestMismatch(t *testing.T) {
	metadata := validMetadata()
	metadata.InputSchemaDigest = sha256.Sum256([]byte("wrong-input-schema"))
	if _, err := bind(t, metadata); !errors.Is(err, operation.ErrSchemaDigestMismatch) {
		t.Fatalf("input schema mismatch error = %v", err)
	}

	metadata = validMetadata()
	metadata.OutputSchemaDigest = sha256.Sum256([]byte("wrong-output-schema"))
	if _, err := bind(t, metadata); !errors.Is(err, operation.ErrSchemaDigestMismatch) {
		t.Fatalf("output schema mismatch error = %v", err)
	}
}

func TestBindSnapshotsCodecSchemaDigestsOnce(t *testing.T) {
	metadata := validMetadata()
	inputDigest, outputDigest := schemaDigests()
	input := &testInputCodec{digest: inputDigest}
	output := &testOutputCodec{digest: outputDigest}
	handler := func(context.Context, operation.InvocationContext, testInput) (operation.Result[testOutput], error) {
		return operation.Result[testOutput]{Kind: operation.ResultCreated}, nil
	}
	definition, err := operation.Bind(metadata, input, testResolver{}, output, handler)
	if err != nil {
		t.Fatal(err)
	}
	if input.calls != 1 || output.calls != 1 {
		t.Fatalf("schema digest methods called %d/%d times, want once each", input.calls, output.calls)
	}
	if _, err := operation.NewRegistry(definition); err != nil {
		t.Fatal(err)
	}
	if input.calls != 1 || output.calls != 1 {
		t.Fatalf("registry re-read codec digests: %d/%d calls", input.calls, output.calls)
	}
}

func TestBindRejectsZeroSchemaDigestsAfterSnapshot(t *testing.T) {
	metadata := validMetadata()
	_, outputDigest := schemaDigests()
	input := &testInputCodec{}
	output := &testOutputCodec{digest: outputDigest}
	handler := func(context.Context, operation.InvocationContext, testInput) (operation.Result[testOutput], error) {
		return operation.Result[testOutput]{Kind: operation.ResultCreated}, nil
	}
	if _, err := operation.Bind(metadata, input, testResolver{}, output, handler); !errors.Is(err, operation.ErrSchemaDigestMismatch) {
		t.Fatalf("zero codec digest error = %v", err)
	}
	if input.calls != 1 || output.calls != 1 {
		t.Fatalf("zero codec digests were not snapshotted once: %d/%d", input.calls, output.calls)
	}

	metadata = validMetadata()
	metadata.InputSchemaDigest = [32]byte{}
	inputDigest, outputDigest := schemaDigests()
	input = &testInputCodec{digest: inputDigest}
	output = &testOutputCodec{digest: outputDigest}
	if _, err := operation.Bind(metadata, input, testResolver{}, output, handler); !errors.Is(err, operation.ErrInvalidDefinition) {
		t.Fatalf("zero metadata digest error = %v", err)
	}
	if input.calls != 1 || output.calls != 1 {
		t.Fatalf("zero metadata digests were not snapshotted once: %d/%d", input.calls, output.calls)
	}
}

func TestBindRejectsInvalidMetadata(t *testing.T) {
	cases := []struct {
		name   string
		change func(*operation.Metadata)
		want   error
	}{
		{name: "lowercase method", change: func(m *operation.Metadata) { m.Method = "post" }, want: operation.ErrInvalidDefinition},
		{name: "unknown method", change: func(m *operation.Metadata) { m.Method = "TRACE" }, want: operation.ErrInvalidDefinition},
		{name: "invalid operation ID", change: func(m *operation.Metadata) { m.OperationID = "Todos.Create" }, want: operation.ErrInvalidDefinition},
		{name: "control in requirement", change: func(m *operation.Metadata) { m.Requirements.Permissions = []string{"todos.write\n"} }, want: operation.ErrInvalidDefinition},
		{name: "duplicate requirement", change: func(m *operation.Metadata) { m.Requirements.Permissions = []string{"todos.write", "todos.write"} }, want: operation.ErrInvalidDefinition},
		{name: "unsupported feature", change: func(m *operation.Metadata) { m.Features = []string{"dynamic-code"} }, want: operation.ErrInvalidDefinition},
		{name: "duplicate features", change: func(m *operation.Metadata) { m.Features = []string{"typed-json", "typed-json"} }, want: operation.ErrInvalidDefinition},
		{name: "missing feature array", change: func(m *operation.Metadata) { m.Features = nil }, want: operation.ErrInvalidDefinition},
		{name: "missing permission array", change: func(m *operation.Metadata) { m.Requirements.Permissions = nil }, want: operation.ErrInvalidDefinition},
		{name: "missing entitlement array", change: func(m *operation.Metadata) { m.Requirements.Entitlements = nil }, want: operation.ErrInvalidDefinition},
		{name: "invalid assurance", change: func(m *operation.Metadata) { m.Requirements.Assurance = "aal0" }, want: operation.ErrInvalidDefinition},
		{name: "invalid exposure", change: func(m *operation.Metadata) { m.Requirements.MCPExposure = "always" }, want: operation.ErrInvalidDefinition},
		{name: "reserved exact path", change: func(m *operation.Metadata) { m.Path = "/API" }, want: operation.ErrReservedRoute},
		{name: "reserved child path", change: func(m *operation.Metadata) { m.Path = "/api/v1/todos" }, want: operation.ErrReservedRoute},
		{name: "reserved prefix boundary", change: func(m *operation.Metadata) { m.Path = "/apiary/todos" }, want: nil},
		{name: "encoded path", change: func(m *operation.Metadata) { m.Path = "/todos%2fprivate" }, want: operation.ErrInvalidDefinition},
		{name: "path query", change: func(m *operation.Metadata) { m.Path = "/todos?admin=true" }, want: operation.ErrInvalidDefinition},
		{name: "root wildcard", change: func(m *operation.Metadata) { m.Path = "/*" }, want: operation.ErrInvalidDefinition},
		{name: "required sensitive output", change: func(m *operation.Metadata) { m.OutputClass = operation.OutputSensitive }, want: operation.ErrInvalidDefinition},
		{name: "required replay has zero bound", change: func(m *operation.Metadata) { m.MaxReplayBytes = 0 }, want: operation.ErrInvalidDefinition},
		{name: "required replay over bound", change: func(m *operation.Metadata) { m.MaxReplayBytes = 65537 }, want: operation.ErrInvalidDefinition},
		{name: "read requires replay", change: func(m *operation.Metadata) { m.SideEffect = operation.SideEffectRead }, want: operation.ErrInvalidDefinition},
		{name: "unexpected replay bound", change: func(m *operation.Metadata) { m.Idempotency = operation.IdempotencyNatural }, want: operation.ErrInvalidDefinition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metadata := validMetadata()
			tc.change(&metadata)
			_, err := bind(t, metadata)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Bind() error = %v, want success", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Bind() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewRegistryRejectsDuplicateIDsAndOverlappingRoutes(t *testing.T) {
	firstMetadata := validMetadata()
	firstMetadata.Path = "/todos/items"
	first, err := bind(t, firstMetadata)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := validMetadata()
	duplicate.Path = "/todos/duplicate"
	duplicateDefinition, err := bind(t, duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operation.NewRegistry(first, duplicateDefinition); !errors.Is(err, operation.ErrDuplicateOperation) {
		t.Fatalf("duplicate operation error = %v", err)
	}

	second := validMetadata()
	second.OperationID = "todos.list"
	second.Idempotency = operation.IdempotencyForbidden
	second.OutputClass = operation.OutputSensitive
	second.MaxReplayBytes = 0
	second.Path = "/todos/*"
	secondDefinition, err := bind(t, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operation.NewRegistry(first, secondDefinition); !errors.Is(err, operation.ErrRouteConflict) {
		t.Fatalf("overlapping route error = %v", err)
	}
	second.Path = "/todos/items"
	secondDefinition, err = bind(t, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operation.NewRegistry(first, secondDefinition); !errors.Is(err, operation.ErrRouteConflict) {
		t.Fatalf("exact route error = %v", err)
	}

	second.Method = "GET"
	secondDefinition, err = bind(t, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operation.NewRegistry(first, secondDefinition); err != nil {
		t.Fatalf("different methods should not conflict: %v", err)
	}
}

func TestBindRejectsTypedNilDependencies(t *testing.T) {
	metadata := validMetadata()
	inputDigest, outputDigest := schemaDigests()
	var nilInput *testInputCodec
	var nilOutput *testOutputCodec
	var nilResolver *typedNilResolver
	handler := func(context.Context, operation.InvocationContext, testInput) (operation.Result[testOutput], error) {
		return operation.Result[testOutput]{}, nil
	}
	if _, err := operation.Bind(metadata, nilInput, testResolver{}, &testOutputCodec{digest: outputDigest}, handler); !errors.Is(err, operation.ErrInvalidDefinition) {
		t.Fatalf("typed nil input codec error = %v", err)
	}
	if _, err := operation.Bind(metadata, &testInputCodec{digest: inputDigest}, testResolver{}, nilOutput, handler); !errors.Is(err, operation.ErrInvalidDefinition) {
		t.Fatalf("typed nil output codec error = %v", err)
	}
	if _, err := operation.Bind(metadata, &testInputCodec{digest: inputDigest}, nilResolver, &testOutputCodec{digest: outputDigest}, handler); !errors.Is(err, operation.ErrInvalidDefinition) {
		t.Fatalf("typed nil resolver error = %v", err)
	}
	if _, err := operation.Bind(metadata, &testInputCodec{digest: inputDigest}, testResolver{}, &testOutputCodec{digest: outputDigest}, nil); !errors.Is(err, operation.ErrInvalidDefinition) {
		t.Fatalf("nil handler error = %v", err)
	}
}

type typedNilResolver struct{}

func (*typedNilResolver) Resolve(identity.Principal, workspacecontext.Selection, testInput) (operation.ResolvedResources, error) {
	return operation.ResolvedResources{}, nil
}
