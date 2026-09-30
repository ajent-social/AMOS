// Package codegen validates the AMOS OpenAPI contract and produces stable
// generation manifests. Transport source generation is deliberately layered
// on this validated representation by the later transport-generation task.
package codegen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	yaml "go.yaml.in/yaml/v4"
)

const (
	OpenAPIVersion    = "3.1.1"
	PolicyExtension   = "x-amos-policy"
	ManifestVersion   = "amos-generation-manifest-v1"
	MaxContractBytes  = 4 << 20
	MaxYAMLDepth      = 128
	MaxReferenceCount = 4096
)

var (
	operationNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
	requirementPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.:_-][a-z0-9]+)*$`)
)

// OperationPolicy mirrors the required operation metadata in
// api/policy.schema.json. Slice fields are non-nil when present, including
// explicitly empty arrays, so omission remains distinguishable from "none".
type OperationPolicy struct {
	OperationID  string   `json:"operationId"`
	Permissions  []string `json:"permissions"`
	Entitlements []string `json:"entitlements"`
	Assurance    string   `json:"assurance"`
	SideEffect   string   `json:"sideEffect"`
	Idempotency  string   `json:"idempotency"`
	MCP          string   `json:"mcp"`
}

// Operation is the stable subset consumed by code generation.
type Operation struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Policy OperationPolicy `json:"policy"`
}

// Manifest is byte-stable for identical input bytes and parser/generator
// versions. Operations are sorted by operationId before serialization.
type Manifest struct {
	Format      string      `json:"format"`
	InputSHA256 string      `json:"inputSha256"`
	OpenAPI     string      `json:"openapi"`
	Operations  []Operation `json:"operations"`
}

// Validate parses an OpenAPI document with the pinned libopenapi parser,
// checks the supported OpenAPI version and AMOS policy extensions, then
// returns the operations in deterministic order.
func Validate(source []byte) (Manifest, error) {
	if len(source) > MaxContractBytes {
		return Manifest{}, fmt.Errorf("contract exceeds the %d byte limit", MaxContractBytes)
	}
	if len(bytes.TrimSpace(source)) == 0 {
		return Manifest{}, errors.New("contract is empty")
	}
	var root yaml.Node
	if err := yaml.Unmarshal(source, &root); err != nil {
		return Manifest{}, errors.New("parse OpenAPI YAML: invalid document syntax")
	}
	if err := validateYAMLTree(&root); err != nil {
		return Manifest{}, err
	}
	if err := validateSupportedFields(documentRoot(&root)); err != nil {
		return Manifest{}, err
	}
	if err := rejectReferenceCycles(&root); err != nil {
		return Manifest{}, err
	}
	configuration := &datamodel.DocumentConfiguration{
		AllowFileReferences:   false,
		AllowRemoteReferences: false,
	}
	document, err := libopenapi.NewDocumentWithConfiguration(source, configuration)
	if err != nil {
		return Manifest{}, errors.New("parse OpenAPI document: parser rejected the contract")
	}
	if version := document.GetVersion(); version != OpenAPIVersion {
		return Manifest{}, fmt.Errorf("openapi version is unsupported; require %s", OpenAPIVersion)
	}
	model, err := document.BuildV3Model()
	if err != nil {
		return Manifest{}, errors.New("validate OpenAPI 3 model: unresolved reference or invalid model")
	}
	if model == nil || model.Model.Paths == nil || model.Model.Paths.PathItems == nil {
		return Manifest{}, errors.New("OpenAPI document must contain paths and at least one operation")
	}

	manifest := Manifest{Format: ManifestVersion, OpenAPI: OpenAPIVersion}
	seen := make(map[string]string)
	for path, item := range model.Model.Paths.PathItems.FromOldest() {
		if item == nil {
			return Manifest{}, fmt.Errorf("paths[%q]: path item is empty", path)
		}
		if ref := item.GetReference(); ref != "" {
			return Manifest{}, fmt.Errorf("paths[%q]: Path Item references are unsupported; inline the item", path)
		}
		operations := item.GetOperations()
		if operations == nil {
			continue
		}
		for method, operation := range operations.FromOldest() {
			location := fmt.Sprintf("paths[%q].%s", path, method)
			if operation == nil {
				return Manifest{}, fmt.Errorf("%s: operation is empty", location)
			}
			if operation.Callbacks != nil && operation.Callbacks.Len() > 0 {
				return Manifest{}, fmt.Errorf("%s.callbacks: callbacks are unsupported by this generator version", location)
			}
			id := operation.OperationId
			if !validOperationName(id) {
				return Manifest{}, fmt.Errorf("%s.operationId: invalid or missing operation ID", location)
			}
			if prior, exists := seen[id]; exists {
				return Manifest{}, fmt.Errorf("%s.operationId: duplicate operation ID also declared at %s", location, prior)
			}
			seen[id] = location

			if operation.Extensions == nil {
				return Manifest{}, fmt.Errorf("%s.%s: required policy extension is missing", location, PolicyExtension)
			}
			encoded, ok := operation.Extensions.Get(PolicyExtension)
			if !ok || encoded == nil {
				return Manifest{}, fmt.Errorf("%s.%s: required policy extension is missing", location, PolicyExtension)
			}
			policy, err := decodePolicy(encoded)
			if err != nil {
				return Manifest{}, fmt.Errorf("%s.%s: %w", location, PolicyExtension, err)
			}
			if policy.OperationID != id {
				return Manifest{}, fmt.Errorf("%s.%s.operationId: does not match operationId", location, PolicyExtension)
			}
			if err := validatePolicy(policy); err != nil {
				return Manifest{}, fmt.Errorf("%s.%s: %w", location, PolicyExtension, err)
			}
			manifest.Operations = append(manifest.Operations, Operation{Method: strings.ToUpper(method), Path: path, Policy: policy})
		}
	}
	if len(manifest.Operations) == 0 {
		return Manifest{}, errors.New("OpenAPI document must contain at least one operation")
	}
	sort.Slice(manifest.Operations, func(i, j int) bool {
		return manifest.Operations[i].Policy.OperationID < manifest.Operations[j].Policy.OperationID
	})
	manifest.InputSHA256 = sha256Hex(source)
	return manifest, nil
}

// RenderManifest emits canonical indented JSON with a trailing newline.
func RenderManifest(manifest Manifest) ([]byte, error) {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode generation manifest: %w", err)
	}
	return append(encoded, '\n'), nil
}

func decodePolicy(value any) (OperationPolicy, error) {
	var policy OperationPolicy
	var decoded any
	if node, ok := value.(*yaml.Node); ok {
		if err := node.Decode(&decoded); err != nil {
			return policy, errors.New("policy extension must be a valid object")
		}
	} else {
		decoded = value
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return policy, fmt.Errorf("encode policy extension: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return policy, fmt.Errorf("invalid policy object: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return policy, errors.New("policy extension contains trailing data")
	}
	return policy, nil
}

func validatePolicy(policy OperationPolicy) error {
	if !validOperationName(policy.OperationID) {
		return fmt.Errorf("operationId %q is invalid", policy.OperationID)
	}
	if policy.Permissions == nil {
		return errors.New("permissions is required; use [] when none are required")
	}
	if policy.Entitlements == nil {
		return errors.New("entitlements is required; use [] when none are required")
	}
	if !oneOf(policy.Assurance, "aal1", "aal2", "aal3") {
		return errors.New("assurance is missing or unsupported")
	}
	if !oneOf(policy.SideEffect, "read", "write", "external") {
		return errors.New("sideEffect is missing or unsupported")
	}
	if !oneOf(policy.Idempotency, "required", "natural", "forbidden") {
		return errors.New("idempotency is missing or unsupported")
	}
	if !oneOf(policy.MCP, "never", "eligible", "challenge") {
		return errors.New("mcp is missing or unsupported")
	}
	if err := validateNames("permissions", policy.Permissions); err != nil {
		return err
	}
	if err := validateNames("entitlements", policy.Entitlements); err != nil {
		return err
	}
	sort.Strings(policy.Permissions)
	sort.Strings(policy.Entitlements)
	return nil
}

func validateNames(field string, names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if len(name) < 2 || len(name) > 120 || !requirementPattern.MatchString(name) {
			return fmt.Errorf("%s entry has invalid format", field)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%s contains duplicate entries", field)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validOperationName(name string) bool {
	return len(name) >= 3 && len(name) <= 120 && operationNamePattern.MatchString(name)
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}
