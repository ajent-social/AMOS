// Package businessgen generates typed business registration skeletons. It does
// not install routes, invoke handlers, implement codecs, or grant authority.
package businessgen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"strconv"
	"strings"

	"github.com/ajent-social/amos/internal/codegen"
	"github.com/ajent-social/amos/internal/codegen/gotypes"
	"github.com/ajent-social/amos/internal/initializer"
)

const (
	OutputDirectory = "internal/amosgen/business"
	MaxOutputBytes  = 8 << 20
	maxOperations   = 256
	journalFormat   = "amos-business-generation-v1"
)

var (
	ErrContract = errors.New("business generation: unsupported or invalid contract")
	ErrConflict = errors.New("business generation: ownership or preimage conflict")
	ErrCanceled = errors.New("business generation: canceled or missing context")
)

type outputFile struct {
	name string
	data []byte
}

// Generator owns a validated, immutable source snapshot and fixed output set.
// Use it in the normal initializer transaction for first publication, then
// VerifyRepeat for an identical finalized-tree regeneration. Updating or
// replacing a finalized output belongs to the separately qualified upgrader.
type Generator struct{ files []outputFile }

type fileRecord struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
	Owner  string `json:"owner"`
}

type generationRecord struct {
	Format           string       `json:"format"`
	GeneratorVersion string       `json:"generatorVersion"`
	SourceSHA256     string       `json:"sourceSha256"`
	Files            []fileRecord `json:"files"`
}

// New validates supported OpenAPI and types before constructing any output.
// Generated source imports only public AMOS packages. Runtime schema digests,
// revision, output class and replay bound remain explicit owner bindings; the
// generator does not invent codecs or claim their semantic qualification.
func New(source []byte) (*Generator, error) {
	if len(source) == 0 || len(source) > codegen.MaxContractBytes {
		return nil, ErrContract
	}
	source = bytes.Clone(source)
	contract, err := codegen.Validate(source)
	if err != nil || len(contract.Operations) == 0 || len(contract.Operations) > maxOperations {
		return nil, ErrContract
	}
	models, err := gotypes.Generate(source)
	if err != nil {
		return nil, ErrContract
	}
	bindings, err := renderBindings(contract)
	if err != nil {
		return nil, ErrContract
	}
	files := []outputFile{{"models_gen.go", models}, {"bindings_gen.go", bindings}}
	record := generationRecord{Format: journalFormat, GeneratorVersion: "1", SourceSHA256: digest(source), Files: []fileRecord{}}
	total := 0
	for _, f := range files {
		total += len(f.data)
		record.Files = append(record.Files, fileRecord{Path: OutputDirectory + "/" + f.name, SHA256: digest(f.data), Size: len(f.data), Owner: "managed"})
	}
	journal, err := json.MarshalIndent(record, "", "  ")
	if err != nil || total+len(journal)+1 > MaxOutputBytes {
		return nil, ErrContract
	}
	files = append(files, outputFile{"generation.json", append(journal, '\n')})
	return &Generator{files: files}, nil
}

// Generate participates in the existing initializer journal, without changing
// its format or writing authored paths. All known preimages are checked before
// staging writes. The initializer owns atomic no-replace directory publication.
func (g *Generator) Generate(ctx context.Context, _ initializer.Config, files *initializer.Files) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrCanceled
	}
	if g == nil || len(g.files) != 3 || files == nil {
		return ErrContract
	}
	for _, file := range g.files {
		old, exists, err := files.ReadManagedFile(ctx, OutputDirectory+"/"+file.name)
		if err != nil || (exists && !bytes.Equal(old, file.data)) {
			return ErrConflict
		}
	}
	for _, file := range g.files {
		if err := files.WriteFile(ctx, OutputDirectory+"/"+file.name, file.data, 0644); err != nil {
			return ErrConflict
		}
	}
	return nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// opIdentifier mirrors the already qualified Go types emitter's operation
// naming rule. Tests parse generated adapters by compiling the actual consumer;
// the types emitter independently rejects colliding or unsafe identifiers.
func opIdentifier(id string) string {
	var b strings.Builder
	b.WriteString("Op")
	upper := true
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			if upper && r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			}
			b.WriteRune(r)
			upper = false
		} else {
			upper = true
		}
	}
	return b.String()
}

func stringsLiteral(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Quote(v)
	}
	return "[]string{" + strings.Join(parts, ",") + "}"
}

func renderBindings(contract codegen.Manifest) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`// Code generated by AMOS business bindings v1; DO NOT EDIT.
// Registration only: no routes, dispatch, codec implementation, or authority.
package apigen
import (
 "context"
 "github.com/ajent-social/amos/app/operation"
 "github.com/ajent-social/amos/identity"
 "github.com/ajent-social/amos/policy"
)
// BindingMetadata requires explicit owner-reviewed runtime schema artifacts.
// Digest consistency does not qualify codec semantics or authorize execution.
type BindingMetadata struct {
 Revision string
 InputSchemaDigest [32]byte
 OutputSchemaDigest [32]byte
 Features []string
 OutputClass operation.OutputClass
 MaxReplayBytes int
}
type Binding[I,O any] struct {
 Metadata BindingMetadata
 Input operation.InputCodec[I]
 Resolver operation.ResourceResolver[I]
 Output operation.OutputCodec[O]
 Handler func(context.Context, operation.InvocationContext, I) (operation.Result[O], error)
}
type Bindings struct {
`)
	for _, op := range contract.Operations {
		n := opIdentifier(op.Policy.OperationID)
		fmt.Fprintf(&b, "%s Binding[%sRequest,%sResponse]\n", n, n, n)
	}
	b.WriteString("}\n// BuildRegistry validates every required binding before returning a frozen registry.\n// It never invokes business handlers or installs callable transports.\nfunc BuildRegistry(bindings Bindings) (*operation.FrozenRegistry,error) {\ndefinitions := make([]operation.Definition,0,")
	b.WriteString(strconv.Itoa(len(contract.Operations)))
	b.WriteString(")\n")
	for i, op := range contract.Operations {
		n := opIdentifier(op.Policy.OperationID)
		fmt.Fprintf(&b, "b%d := bindings.%s\n", i, n)
		fmt.Fprintf(&b, "d%d, err := operation.Bind(operation.Metadata{\n", i)
		fmt.Fprintf(&b, "OperationID:%q,Method:%q,Path:%q,InputType:%q,OutputType:%q,\n", op.Policy.OperationID, op.Method, op.Path, n+"Request", n+"Response")
		fmt.Fprintf(&b, "Revision:b%d.Metadata.Revision,InputSchemaDigest:b%d.Metadata.InputSchemaDigest,OutputSchemaDigest:b%d.Metadata.OutputSchemaDigest,Features:b%d.Metadata.Features,OutputClass:b%d.Metadata.OutputClass,MaxReplayBytes:b%d.Metadata.MaxReplayBytes,\n", i, i, i, i, i, i)
		fmt.Fprintf(&b, "Requirements:policy.Requirements{Permissions:%s,Entitlements:%s,Assurance:identity.AssuranceLevel(%q),MCPExposure:policy.MCPExposure(%q)},SideEffect:operation.SideEffect(%q),Idempotency:operation.Idempotency(%q),\n", stringsLiteral(op.Policy.Permissions), stringsLiteral(op.Policy.Entitlements), op.Policy.Assurance, op.Policy.MCP, op.Policy.SideEffect, op.Policy.Idempotency)
		fmt.Fprintf(&b, "},b%d.Input,b%d.Resolver,b%d.Output,b%d.Handler)\nif err != nil { return nil,err };definitions = append(definitions,d%d)\n", i, i, i, i, i)
	}
	b.WriteString("return operation.NewRegistry(definitions...)\n}\n")
	return format.Source(b.Bytes())
}
