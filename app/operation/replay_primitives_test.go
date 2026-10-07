package operation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/policy"
	workspacecontext "github.com/ajent-social/amos/workspace/context"
)

func TestReplayRequestHashKnownAnswers(t *testing.T) {
	t.Parallel()
	var sequence [32]byte
	for i := range sequence {
		sequence[i] = byte(i)
	}
	// Independent Python hashlib vectors: sha256(b'amos-operation-replay-v1\0'
	// + descriptor + b'\0' + canonical). No production helper generated these.
	cases := []struct {
		name        string
		descriptor  [32]byte
		input, want string
	}{
		{"empty", [32]byte{}, "", "ddcc5e2374e68e1ce962ef0a85c54c937cc93ce9c347eb6927ebd763fa8ce0e0"},
		{"path query body", sequence, `{"path":{"id":"item-1"},"query":{"view":"full"},"body":{"name":"alpha"}}`, "ab84e2c55047aa9bc87feeed1de21129e8f842630848d30dc508dc60337278d2"},
		{"embedded zero", sequence, "\x00a\x00", "7bd396a9abaaf4e98275b1b31cbd093cd374c586660ddc19e7e05435779a26a0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.input)
			got := replayRequestHash(tc.descriptor, input)
			if hex.EncodeToString(got[:]) != tc.want {
				t.Fatalf("hash = %x, want %s", got, tc.want)
			}
			if string(input) != tc.input {
				t.Fatal("input mutated")
			}
			changed := tc.descriptor
			changed[0] ^= 1
			if replayRequestHash(changed, input) == got {
				t.Fatal("descriptor omitted")
			}
			if replayRequestHash(tc.descriptor, append(input, 'x')) == got {
				t.Fatal("input omitted")
			}
		})
	}
}

type replayInput struct {
	Path struct {
		ID string `json:"id"`
	} `json:"path"`
	Query struct {
		View string `json:"view"`
	} `json:"query"`
	Body struct {
		Name string `json:"name"`
	} `json:"body"`
}

type replayProbe struct {
	inputDigests, outputDigests, decodes, resolves, encodes, invokes, validates int
	validate                                                                    func([]byte) error
}

type replayInputCodec struct{ probe *replayProbe }

func (c replayInputCodec) SchemaDigest() [32]byte {
	c.probe.inputDigests++
	return sha256.Sum256([]byte("replay-test-input-v1"))
}
func (c replayInputCodec) DecodeCanonical(raw RawInput) (replayInput, []byte, error) {
	c.probe.decodes++
	var input replayInput
	invalid := errors.New("invalid test input")
	if len(raw.Path) != 1 || raw.Path[0].Name != "id" || len(raw.Path[0].Values) != 1 || raw.Path[0].Values[0] == "" ||
		len(raw.Query) != 1 || raw.Query[0].Name != "view" || len(raw.Query[0].Values) != 1 || (raw.Query[0].Values[0] != "full" && raw.Query[0].Values[0] != "short") {
		return input, nil, invalid
	}
	// This deliberately finite fixture schema allows exactly one body field,
	// rejecting duplicate/unknown keys as well as trailing JSON.
	decoder := json.NewDecoder(bytes.NewReader(raw.Body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return input, nil, invalid
	}
	token, err = decoder.Token()
	if err != nil || token != "name" {
		return input, nil, invalid
	}
	if err := decoder.Decode(&input.Body.Name); err != nil || input.Body.Name == "" {
		return input, nil, invalid
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return input, nil, invalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return input, nil, invalid
	}
	input.Path.ID = raw.Path[0].Values[0]
	input.Query.View = raw.Query[0].Values[0]
	canonical, err := json.Marshal(input)
	return input, canonical, err
}

type replayOutputCodec struct{ probe *replayProbe }

func (c replayOutputCodec) SchemaDigest() [32]byte {
	c.probe.outputDigests++
	return sha256.Sum256([]byte("replay-test-output-v1"))
}
func (c replayOutputCodec) EncodeCanonical(value string) ([]byte, error) {
	c.probe.encodes++
	return json.Marshal(value)
}
func (c replayOutputCodec) ValidateCanonical(value []byte) error {
	c.probe.validates++
	if c.probe.validate != nil {
		return c.probe.validate(value)
	}
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil {
		return err
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return err
	}
	if !bytes.Equal(value, canonical) {
		return errors.New("noncanonical test output")
	}
	return nil
}

type replayResolver struct{ probe *replayProbe }

func (r replayResolver) Resolve(identity.Principal, workspacecontext.Selection, replayInput) (ResolvedResources, error) {
	r.probe.resolves++
	return ResolvedResources{}, errors.New("unexpected resolver")
}

func replayDefinition(t *testing.T, maxBytes int) (Definition, *replayProbe) {
	t.Helper()
	probe := &replayProbe{}
	metadata := Metadata{
		OperationID: "items.update", Method: "POST", Path: "/items/*",
		InputType: "items.Input", OutputType: "items.Output", Features: []string{"typed-json"}, Revision: "v1",
		InputSchemaDigest: sha256.Sum256([]byte("replay-test-input-v1")), OutputSchemaDigest: sha256.Sum256([]byte("replay-test-output-v1")),
		Requirements: policy.Requirements{Permissions: []string{"items.write"}, Entitlements: []string{}, Assurance: identity.AAL1, MCPExposure: policy.MCPNever},
		SideEffect:   SideEffectWrite, Idempotency: IdempotencyRequired, OutputClass: OutputReplaySafe, MaxReplayBytes: maxBytes,
	}
	definition, err := Bind(metadata, replayInputCodec{probe}, replayResolver{probe}, replayOutputCodec{probe}, func(context.Context, InvocationContext, replayInput) (Result[string], error) {
		probe.invokes++
		return Result[string]{}, errors.New("unexpected handler")
	})
	if err != nil {
		t.Fatal(err)
	}
	return definition, probe
}

func assertReplayCalls(t *testing.T, p *replayProbe, decodes, validates int) {
	t.Helper()
	if p.inputDigests != 1 || p.outputDigests != 1 || p.decodes != decodes || p.validates != validates || p.resolves != 0 || p.encodes != 0 || p.invokes != 0 {
		t.Fatalf("unexpected codec/resolver/handler calls: %+v", p)
	}
}

func TestReplayRequestHashCanonicalInput(t *testing.T) {
	t.Parallel()
	definition, probe := replayDefinition(t, 32)
	raw := RawInput{Path: []NameValues{{Name: "id", Values: []string{"item-1"}}}, Query: []NameValues{{Name: "view", Values: []string{"full"}}}, Body: []byte(`{"name":"alpha"}`)}
	_, canonical, err := definition.decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != `{"path":{"id":"item-1"},"query":{"view":"full"},"body":{"name":"alpha"}}` {
		t.Fatalf("canonical = %s", canonical)
	}
	first := replayRequestHash(definition.descriptorDigest, canonical)
	equivalent := cloneRawInput(raw)
	equivalent.Body = []byte(" { \"name\" : \"\\u0061lpha\" } ")
	_, same, err := definition.decode(equivalent)
	if err != nil {
		t.Fatal(err)
	}
	if replayRequestHash(definition.descriptorDigest, same) != first {
		t.Fatal("equivalent typed input differs")
	}
	changes := []struct {
		name   string
		change func(*RawInput)
	}{
		{"path", func(r *RawInput) { r.Path[0].Values[0] = "item-2" }},
		{"query", func(r *RawInput) { r.Query[0].Values[0] = "short" }},
		{"body", func(r *RawInput) { r.Body = []byte(`{"name":"beta"}`) }},
	}
	for _, tc := range changes {
		t.Run(tc.name, func(t *testing.T) {
			changed := cloneRawInput(raw)
			tc.change(&changed)
			_, value, err := definition.decode(changed)
			if err != nil {
				t.Fatal(err)
			}
			if replayRequestHash(definition.descriptorDigest, value) == first {
				t.Fatal("validated change omitted from hash")
			}
		})
	}
	invalid := []struct {
		name   string
		change func(*RawInput)
	}{
		{"unknown path", func(r *RawInput) { r.Path[0].Name = "actor" }},
		{"duplicate path", func(r *RawInput) { r.Path = append(r.Path, r.Path[0]) }},
		{"path cardinality", func(r *RawInput) { r.Path[0].Values = append(r.Path[0].Values, "item-2") }},
		{"unknown query", func(r *RawInput) { r.Query[0].Name = "authority" }},
		{"duplicate query", func(r *RawInput) { r.Query = append(r.Query, r.Query[0]) }},
		{"unknown body", func(r *RawInput) { r.Body = []byte(`{"other":"alpha"}`) }},
		{"duplicate body", func(r *RawInput) { r.Body = []byte(`{"name":"alpha","name":"beta"}`) }},
		{"trailing body", func(r *RawInput) { r.Body = []byte(`{"name":"alpha"}{}`) }},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			changed := cloneRawInput(raw)
			tc.change(&changed)
			if _, _, err := definition.decode(changed); err == nil {
				t.Fatal("invalid input admitted")
			}
		})
	}
	assertReplayCalls(t, probe, 2+len(changes)+len(invalid), 0)
}

func TestValidateCachedResultFiniteKindsAndCopies(t *testing.T) {
	t.Parallel()
	for _, kind := range []ResultKind{ResultSucceeded, ResultCreated, ResultAccepted, ResultNoContent} {
		t.Run(string(kind), func(t *testing.T) {
			definition, probe := replayDefinition(t, 4)
			original := []byte(`"ok"`)
			stored := CachedResult{Kind: kind, CanonicalJSON: append([]byte(nil), original...)}
			var retained []byte
			probe.validate = func(value []byte) error { retained = value; value[1] = 'x'; return nil }
			got, err := validateCachedResult(definition, stored, sha256.Sum256(original))
			if err != nil || got.Kind != kind || !bytes.Equal(got.CanonicalJSON, original) {
				t.Fatalf("result = %+v, error = %v", got, err)
			}
			if !bytes.Equal(stored.CanonicalJSON, original) {
				t.Fatal("validator mutated stored bytes")
			}
			retained[2] = 'y'
			if !bytes.Equal(got.CanonicalJSON, original) {
				t.Fatal("validator retained returned buffer")
			}
			got.CanonicalJSON[1] = 'z'
			if !bytes.Equal(stored.CanonicalJSON, original) || retained[1] != 'x' {
				t.Fatal("returned buffer aliases caller or validator")
			}
			assertReplayCalls(t, probe, 0, 1)
		})
	}
	// Test the helper's own copy even without Bind's defensive validator wrapper.
	definition, _ := replayDefinition(t, 4)
	stored := CachedResult{Kind: ResultCreated, CanonicalJSON: []byte(`"ok"`)}
	definition.validateOutput = func(value []byte) error { value[0] = '!'; return nil }
	got, err := validateCachedResult(definition, stored, sha256.Sum256(stored.CanonicalJSON))
	if err != nil || string(got.CanonicalJSON) != `"ok"` || string(stored.CanonicalJSON) != `"ok"` {
		t.Fatal("helper did not isolate validator input")
	}
}

func TestValidateCachedResultRejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		change         func(*Definition, *CachedResult, *[32]byte)
		validatorCalls int
	}{
		{"zero definition", func(d *Definition, _ *CachedResult, _ *[32]byte) { *d = Definition{} }, 0},
		{"missing operation", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.metadata.OperationID = "" }, 0},
		{"zero descriptor", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.descriptorDigest = [32]byte{} }, 0},
		{"missing decode", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.decode = nil }, 0},
		{"missing resolve", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.resolve = nil }, 0},
		{"missing encode", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.encode = nil }, 0},
		{"missing invoke", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.invoke = nil }, 0},
		{"missing validator", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.validateOutput = nil }, 0},
		{"natural", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.metadata.Idempotency = IdempotencyNatural }, 0},
		{"forbidden", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.metadata.Idempotency = IdempotencyForbidden }, 0},
		{"sensitive", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.metadata.OutputClass = OutputSensitive }, 0},
		{"secret", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.metadata.OutputClass = OutputSecret }, 0},
		{"read", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.metadata.SideEffect = SideEffectRead }, 0},
		{"zero bound", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.metadata.MaxReplayBytes = 0 }, 0},
		{"negative bound", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.metadata.MaxReplayBytes = -1 }, 0},
		{"excessive bound", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.metadata.MaxReplayBytes = 65537 }, 0},
		{"invalid kind", func(_ *Definition, s *CachedResult, _ *[32]byte) { s.Kind = "unknown" }, 0},
		{"empty kind", func(_ *Definition, s *CachedResult, _ *[32]byte) { s.Kind = "" }, 0},
		{"empty bytes", func(_ *Definition, s *CachedResult, hash *[32]byte) {
			s.CanonicalJSON = nil
			*hash = sha256.Sum256(nil)
		}, 0},
		{"over bound", func(d *Definition, _ *CachedResult, _ *[32]byte) { d.metadata.MaxReplayBytes = 3 }, 0},
		{"wrong digest", func(_ *Definition, _ *CachedResult, hash *[32]byte) { hash[0] ^= 1 }, 0},
		{"changed bytes", func(_ *Definition, s *CachedResult, _ *[32]byte) { s.CanonicalJSON[1] = 'x' }, 0},
		{"codec rejection", func(_ *Definition, s *CachedResult, hash *[32]byte) {
			s.CanonicalJSON = []byte(`null`)
			*hash = sha256.Sum256(s.CanonicalJSON)
		}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			definition, probe := replayDefinition(t, 4)
			stored := CachedResult{Kind: ResultCreated, CanonicalJSON: []byte(`"ok"`)}
			digest := sha256.Sum256(stored.CanonicalJSON)
			tc.change(&definition, &stored, &digest)
			before := append([]byte(nil), stored.CanonicalJSON...)
			got, err := validateCachedResult(definition, stored, digest)
			if err != errInvalidReplayResult || got.Kind != "" || got.CanonicalJSON != nil {
				t.Fatalf("invalid result = %+v, error = %v", got, err)
			}
			if !bytes.Equal(stored.CanonicalJSON, before) {
				t.Fatal("invalid stored bytes mutated")
			}
			assertReplayCalls(t, probe, 0, tc.validatorCalls)
		})
	}
}

func TestValidateCachedResultSafeErrorAndCanonicalBytes(t *testing.T) {
	t.Parallel()
	definition, probe := replayDefinition(t, 65536)
	secretError := errors.New("synthetic codec detail must stay private")
	probe.validate = func(value []byte) error { value[0] = '!'; return secretError }
	stored := CachedResult{Kind: ResultSucceeded, CanonicalJSON: []byte(`"ok"`)}
	got, err := validateCachedResult(definition, stored, sha256.Sum256(stored.CanonicalJSON))
	if err != errInvalidReplayResult || errors.Is(err, secretError) || strings.Contains(err.Error(), "synthetic") || got.CanonicalJSON != nil || string(stored.CanonicalJSON) != `"ok"` {
		t.Fatal("codec rejection leaked diagnostics or bytes")
	}
	probe.validate = nil
	for _, value := range []string{` "ok"`, `"\u006fk"`, `{"id":1}`, `not JSON`} {
		stored.CanonicalJSON = []byte(value)
		if _, err := validateCachedResult(definition, stored, sha256.Sum256(stored.CanonicalJSON)); err != errInvalidReplayResult {
			t.Fatalf("noncanonical/schema-invalid output admitted: %q", value)
		}
	}
	stored.CanonicalJSON = []byte(`"` + strings.Repeat("x", 65534) + `"`)
	if _, err := validateCachedResult(definition, stored, sha256.Sum256(stored.CanonicalJSON)); err != nil {
		t.Fatalf("maximum legal output rejected: %v", err)
	}
	stored.CanonicalJSON = []byte(`"` + strings.Repeat("x", 65535) + `"`)
	if _, err := validateCachedResult(definition, stored, sha256.Sum256(stored.CanonicalJSON)); err != errInvalidReplayResult {
		t.Fatal("maximum+1 admitted")
	}
	assertReplayCalls(t, probe, 0, 6)
}
