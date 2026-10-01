// Package businesscontract contains design-only fixtures for the business
// extension descriptor and uses the current public route guard where possible.
// These tests do not exercise a business registry or startup composition.
package businesscontract

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ajent-social/amos/app"
)

type descriptor struct {
	Contract   string      `json:"contract"`
	Operations []operation `json:"operations"`
}

type operation struct {
	OperationID  string   `json:"operationId"`
	Method       string   `json:"method"`
	Path         string   `json:"path"`
	InputType    string   `json:"inputType"`
	OutputType   string   `json:"outputType"`
	Permissions  []string `json:"permissions"`
	Entitlements []string `json:"entitlements"`
	Assurance    string   `json:"assurance"`
	SideEffect   string   `json:"sideEffect"`
	Idempotency  string   `json:"idempotency"`
	MCP          string   `json:"mcp"`
	Features     []string `json:"features"`
}

var (
	idRE          = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
	typeRE        = regexp.MustCompile(`^(?:[A-Z][A-Za-z0-9]*|[a-z][A-Za-z0-9]*(?:\.[A-Z][A-Za-z0-9]*)+)$`)
	nameRE        = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.:_-][a-z0-9]+)*$`)
	pathRE        = regexp.MustCompile(`^/(?:[A-Za-z0-9_~-][A-Za-z0-9._~-]*|\.[A-Za-z0-9_-][A-Za-z0-9._~-]*)(?:/(?:[A-Za-z0-9_~-][A-Za-z0-9._~-]*|\.[A-Za-z0-9_-][A-Za-z0-9._~-]*))*(?:/\*)?$`)
	methods       = set("GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS")
	assurances    = set("aal1", "aal2", "aal3")
	sideEffects   = set("read", "write", "external")
	idempotencies = set("required", "natural", "forbidden")
	mcpModes      = set("never", "eligible", "challenge")
	features      = set("typed-json", "server-rendered-ui")
)

func set(items ...string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}

// validateFixture is deliberately test-only bounded validation mirroring the
// schema's descriptor constraints. It is not a general JSON Schema engine.
func validateFixture(data []byte) error {
	// Check exact JSON property spelling first. encoding/json matches struct
	// fields case-insensitively, while JSON Schema property names are exact.
	var root map[string]json.RawMessage
	if err := decodeSingleJSON(data, &root); err != nil {
		return err
	}
	if err := exactKeys(root, "contract", "operations"); err != nil {
		return err
	}
	var rawOperations []json.RawMessage
	if err := json.Unmarshal(root["operations"], &rawOperations); err != nil {
		return err
	}
	for _, rawOperation := range rawOperations {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(rawOperation, &object); err != nil {
			return err
		}
		if err := exactKeys(object, "operationId", "method", "path", "inputType", "outputType", "permissions", "entitlements", "assurance", "sideEffect", "idempotency", "mcp", "features"); err != nil {
			return err
		}
	}

	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var d descriptor
	if err := decoder.Decode(&d); err != nil {
		return err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	if d.Contract != "amos-contract-v1" || len(d.Operations) == 0 || len(d.Operations) > 256 {
		return errors.New("invalid contract or operation count")
	}
	ids := map[string]bool{}
	for _, op := range d.Operations {
		if !idRE.MatchString(op.OperationID) || len(op.OperationID) < 3 || len(op.OperationID) > 120 || ids[op.OperationID] {
			return errors.New("invalid or duplicate operationId")
		}
		ids[op.OperationID] = true
		if !methods[op.Method] || !canonicalFixturePath(op.Path) || len(op.Path) > 512 {
			return errors.New("invalid method or path")
		}
		if !typeRE.MatchString(op.InputType) || !typeRE.MatchString(op.OutputType) || len(op.InputType) > 160 || len(op.OutputType) > 160 {
			return errors.New("invalid typed input/output")
		}
		if !assurances[op.Assurance] || !sideEffects[op.SideEffect] || !idempotencies[op.Idempotency] || !mcpModes[op.MCP] || op.Features == nil || len(op.Features) > 16 {
			return errors.New("unsupported assurance, MCP mode, or feature count")
		}
		if op.Permissions == nil || op.Entitlements == nil {
			return errors.New("permissions, entitlements, and features must be arrays")
		}
		if err := checkNames(op.Permissions); err != nil {
			return err
		}
		if err := checkNames(op.Entitlements); err != nil {
			return err
		}
		seenFeatures := map[string]bool{}
		for _, feature := range op.Features {
			if !features[feature] || seenFeatures[feature] {
				return errors.New("unsupported feature")
			}
			seenFeatures[feature] = true
		}
	}
	return nil
}

func decodeSingleJSON(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func exactKeys(object map[string]json.RawMessage, allowed ...string) error {
	if object == nil {
		return errors.New("expected JSON object")
	}
	known := set(allowed...)
	for key := range object {
		if !known[key] {
			return errors.New("unknown or incorrectly cased field")
		}
	}
	return nil
}

func canonicalFixturePath(path string) bool {
	if path == "/*" || !pathRE.MatchString(path) || strings.HasSuffix(path, "/") || strings.Contains(path, "//") {
		return false
	}
	for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func checkNames(names []string) error {
	if len(names) > 64 {
		return errors.New("too many requirements")
	}
	seen := map[string]bool{}
	for _, name := range names {
		if !nameRE.MatchString(name) || len(name) < 2 || len(name) > 120 || seen[name] {
			return errors.New("invalid or duplicate requirement")
		}
		seen[name] = true
	}
	return nil
}

func TestBusinessContractDescriptorFixtures(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "api", "extensions", "business.schema.json")
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("schema JSON: %v", err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("unexpected schema dialect: %v", schema["$schema"])
	}
	if schema["additionalProperties"] != false {
		t.Fatal("top-level schema must reject unknown fields")
	}
	defs := schema["$defs"].(map[string]any)
	operationSchema := defs["operation"].(map[string]any)
	operationProperties := operationSchema["properties"].(map[string]any)
	if operationSchema["additionalProperties"] != false {
		t.Fatal("operation schema must reject unknown fields")
	}
	required := operationSchema["required"].([]any)
	requiredNames := map[string]bool{}
	for _, name := range required {
		requiredNames[name.(string)] = true
	}
	for _, name := range []string{"sideEffect", "idempotency", "permissions", "entitlements", "features"} {
		if !requiredNames[name] {
			t.Fatalf("operation schema omits required policy field %q", name)
		}
	}
	policyBytes, err := os.ReadFile(filepath.Join("..", "..", "api", "policy.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var policySchema map[string]any
	if err := json.Unmarshal(policyBytes, &policySchema); err != nil {
		t.Fatalf("policy schema JSON: %v", err)
	}
	policyProperties := policySchema["properties"].(map[string]any)
	for _, name := range []string{"assurance", "sideEffect", "idempotency", "mcp"} {
		if !reflect.DeepEqual(operationProperties[name], policyProperties[name]) {
			t.Errorf("extension policy property %q differs from frozen policy schema", name)
		}
	}
	assertSharedPatternWithControlExclusion(t, operationProperties["operationId"], policyProperties["operationId"], "operationId")
	policyDefs := policySchema["$defs"].(map[string]any)
	policyRequirement := policyDefs["requirementName"]
	assertSharedPatternWithControlExclusion(t, defs["requirementName"], policyRequirement, "requirementName")
	for _, name := range []string{"permissions", "entitlements"} {
		extensionArray := operationProperties[name].(map[string]any)
		policyArray := policyProperties[name].(map[string]any)
		for _, key := range []string{"type", "uniqueItems", "items"} {
			if !reflect.DeepEqual(extensionArray[key], policyArray[key]) {
				t.Errorf("extension policy array %q differs on %q", name, key)
			}
		}
	}

	positive := `{"contract":"amos-contract-v1","operations":[{"operationId":"catalog.list","method":"GET","path":"/catalog","inputType":"catalog.ListInput","outputType":"catalog.ListOutput","permissions":["catalog.read"],"entitlements":[],"assurance":"aal1","sideEffect":"read","idempotency":"forbidden","mcp":"never","features":["typed-json"]}]}`
	if err := validateFixture([]byte(positive)); err != nil {
		t.Fatalf("positive design fixture rejected: %v", err)
	}

	negatives := map[string]string{
		"unknown field":                     strings.Replace(positive, `"mcp":"never"`, `"mcp":"never","principalConstructor":true`, 1),
		"incorrectly cased field":           strings.Replace(positive, `"operationId"`, `"OperationId"`, 1),
		"missing typed output":              strings.Replace(positive, `"outputType":"catalog.ListOutput",`, ``, 1),
		"missing permissions":               strings.Replace(positive, `"permissions":["catalog.read"],`, ``, 1),
		"null permissions":                  strings.Replace(positive, `"permissions":["catalog.read"]`, `"permissions":null`, 1),
		"null entitlements":                 strings.Replace(positive, `"entitlements":[]`, `"entitlements":null`, 1),
		"missing side effect":               strings.Replace(positive, `"sideEffect":"read",`, ``, 1),
		"missing idempotency":               strings.Replace(positive, `"idempotency":"forbidden",`, ``, 1),
		"unsupported feature":               strings.Replace(positive, `"typed-json"`, `"dynamic-code"`, 1),
		"duplicate feature":                 strings.Replace(positive, `"features":["typed-json"]`, `"features":["typed-json","typed-json"]`, 1),
		"operation ID below schema minimum": strings.Replace(positive, `"catalog.list"`, `"ab"`, 1),
		"permission below schema minimum":   strings.Replace(positive, `"catalog.read"`, `"a"`, 1),
		"unknown side effect":               strings.Replace(positive, `"sideEffect":"read"`, `"sideEffect":"unknown"`, 1),
		"unknown idempotency":               strings.Replace(positive, `"idempotency":"forbidden"`, `"idempotency":"unknown"`, 1),
		"operation ID terminal LF":          strings.Replace(positive, `"catalog.list"`, `"catalog.list\n"`, 1),
		"operation ID terminal CR":          strings.Replace(positive, `"catalog.list"`, `"catalog.list\r"`, 1),
		"operation ID control":              strings.Replace(positive, `"catalog.list"`, `"catalog.list\u0001"`, 1),
		"method terminal LF":                strings.Replace(positive, `"GET"`, `"GET\n"`, 1),
		"method terminal CR":                strings.Replace(positive, `"GET"`, `"GET\r"`, 1),
		"method control":                    strings.Replace(positive, `"GET"`, `"GET\u0001"`, 1),
		"path terminal LF":                  strings.Replace(positive, `"/catalog"`, `"/catalog\n"`, 1),
		"path terminal CR":                  strings.Replace(positive, `"/catalog"`, `"/catalog\r"`, 1),
		"path control":                      strings.Replace(positive, `"/catalog"`, `"/catalog\u0001"`, 1),
		"input type terminal LF":            strings.Replace(positive, `"catalog.ListInput"`, `"catalog.ListInput\n"`, 1),
		"input type terminal CR":            strings.Replace(positive, `"catalog.ListInput"`, `"catalog.ListInput\r"`, 1),
		"input type control":                strings.Replace(positive, `"catalog.ListInput"`, `"catalog.ListInput\u0001"`, 1),
		"output type terminal LF":           strings.Replace(positive, `"catalog.ListOutput"`, `"catalog.ListOutput\n"`, 1),
		"output type terminal CR":           strings.Replace(positive, `"catalog.ListOutput"`, `"catalog.ListOutput\r"`, 1),
		"output type control":               strings.Replace(positive, `"catalog.ListOutput"`, `"catalog.ListOutput\u0001"`, 1),
		"requirement terminal LF":           strings.Replace(positive, `"catalog.read"`, `"catalog.read\n"`, 1),
		"requirement terminal CR":           strings.Replace(positive, `"catalog.read"`, `"catalog.read\r"`, 1),
		"requirement control":               strings.Replace(positive, `"catalog.read"`, `"catalog.read\u0001"`, 1),
		"trailing JSON value":               positive + `{}`,
		"noncanonical path":                 strings.Replace(positive, `"/catalog"`, `"/catalog/../admin"`, 1),
	}
	for name, fixture := range negatives {
		t.Run(name, func(t *testing.T) {
			if err := validateFixture([]byte(fixture)); err == nil {
				t.Fatal("rejected design fixture was accepted")
			}
		})
	}
}

func assertSharedPatternWithControlExclusion(t *testing.T, extension, frozen any, name string) {
	t.Helper()
	extensionObject := extension.(map[string]any)
	frozenObject := frozen.(map[string]any)
	for _, key := range []string{"type", "pattern", "minLength", "maxLength"} {
		if !reflect.DeepEqual(extensionObject[key], frozenObject[key]) {
			t.Errorf("extension %s constraint %q differs from frozen policy schema", name, key)
		}
	}
	if extensionObject["not"] == nil {
		t.Errorf("extension %s must explicitly reject control characters", name)
	}
}

// allowsGeneratedBusinessOutputFixture is a design-only ownership oracle used
// by tests; no production generator consumes this rule.
func allowsGeneratedBusinessOutputFixture(path string) bool {
	return strings.HasPrefix(path, "internal/amosgen/business/") &&
		!strings.Contains(path, "..") && !strings.Contains(path, `\`)
}

func TestBusinessContractOwnershipFixturesAreTestOnly(t *testing.T) {
	fixtures := []struct {
		path string
		want bool
	}{
		{"internal/amosgen/business/catalog/list.gen.go", true},
		{"internal/businessgen/emit.go", false},
		{"templates/business/transport/handler.tmpl", false},
		{"business/operations/catalog.go", false},
		{"business/types/catalog.go", false},
		{"ui/overrides/profile.html", false},
		{"app/app.go", false},
		{"internal/amosgen/business/../app/app.go", false},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.path, func(t *testing.T) {
			if got := allowsGeneratedBusinessOutputFixture(fixture.path); got != fixture.want {
				t.Errorf("generated-output ownership for %q = %t, want %t", fixture.path, got, fixture.want)
			}
		})
	}
}

func TestBusinessContractRejectsReservedCatchAllShadow(t *testing.T) {
	// This calls the existing runtime guard through its public app API. It is
	// route-guard evidence only; no operation registry/startup is under test.
	a, err := app.New(app.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := httpHandler()
	for _, pattern := range []string{"/*", "/signin/*", "/mcp/*"} {
		err := a.RegisterBusinessRoute("GET", pattern, h)
		if err == nil {
			t.Errorf("business shadow %q was accepted", pattern)
			continue
		}
		if pattern != "/*" && !errors.Is(err, app.ErrReservedRoute) {
			t.Errorf("%q error=%v, want reserved-route rejection", pattern, err)
		}
	}
	if err := a.RegisterBusinessRoute("GET", "/orders/*", h); err != nil {
		t.Fatalf("nonreserved prefix: %v", err)
	}
	if err := a.RegisterBusinessRoute("GET", "/orders/current", h); !errors.Is(err, app.ErrRouteConflict) {
		t.Fatalf("same-method prefix collision error=%v", err)
	}
	if err := a.RegisterBusinessRoute("POST", "/orders/current", h); err != nil {
		t.Fatalf("distinct method collision: %v", err)
	}
}

func httpHandler() http.Handler { return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}) }
