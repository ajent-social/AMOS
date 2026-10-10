package mcpcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

var (
	operationIDPattern = regexp.MustCompile("^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$")
	toolNamePattern    = regexp.MustCompile("^[a-z][a-z0-9_.-]*$")
	propertyPattern    = regexp.MustCompile("^[a-z][a-zA-Z0-9_]{0,63}$")
)

type mappingDocument struct {
	Contract   string
	Operations []json.RawMessage
}

type mappingOperation struct {
	OperationID string
	ToolName    string
	Description string
	InputSchema inputSchema
	Policy      mappingPolicy
}

type inputSchema struct {
	Type                 string
	Properties           map[string]fieldSchema
	Required             []string
	AdditionalProperties *bool
}

type fieldSchema struct {
	Type        string
	Description string
	Format      string
	Enum        []any
	MinLength   *int
	MaxLength   *int
	Minimum     *float64
	Maximum     *float64
	MinItems    *int
	MaxItems    *int
	Items       *scalarSchema
}

type scalarSchema struct {
	Type        string
	Description string
	Format      string
	Enum        []any
	MinLength   *int
	MaxLength   *int
	Minimum     *float64
	Maximum     *float64
}

type mappingPolicy struct {
	Exposure           string
	Permissions        []string
	Entitlements       []string
	MinimumAssurance   string
	SideEffect         string
	Idempotency        string
	WorkspaceSelection string
	ResourceSelection  string
	ResourceField      string
}

func TestMappingContract(t *testing.T) {
	t.Run("schema is versioned and bounded", func(t *testing.T) {
		raw, err := os.ReadFile("../../api/extensions/mcp.schema.json")
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
			t.Fatalf("unexpected schema dialect: %v", schema["$schema"])
		}
		if schema["additionalProperties"] != false {
			t.Fatal("mapping root must reject unknown fields")
		}
		props := schema["properties"].(map[string]any)
		operations := props["operations"].(map[string]any)
		if operations["maxItems"] != float64(128) {
			t.Fatalf("operation count is not bounded: %v", operations["maxItems"])
		}
	})

	t.Run("valid exposed and hidden mappings", func(t *testing.T) {
		doc := fixtureDocument()
		doc["operations"] = []any{
			operation("todo.update", "todo_update", "eligible", "request_bound", "/todoId"),
			operation("account.read", "account_read", "eligible", "none", ""),
			operation("admin.disable", "admin_disable", "never", "none", ""),
		}
		got, err := validateMapping(mustJSON(doc))
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"account_read", "todo_update"}
		if !equalStrings(got, want) {
			t.Fatalf("published tools = %v, want %v", got, want)
		}
	})

	t.Run("every exposed operation needs enforceable policy", func(t *testing.T) {
		for name, mutate := range map[string]func(map[string]any){
			"missing entitlement policy": func(op map[string]any) {
				delete(op["policy"].(map[string]any), "entitlements")
			},
			"missing workspace selection": func(op map[string]any) {
				delete(op["policy"].(map[string]any), "workspaceSelection")
			},
			"missing permission": func(op map[string]any) {
				op["policy"].(map[string]any)["permissions"] = []any{}
			},
		} {
			t.Run(name, func(t *testing.T) {
				doc := fixtureDocument()
				op := operation("todo.read", "todo_read", "eligible", "none", "")
				mutate(op)
				doc["operations"] = []any{op}
				_, err := validateMapping(mustJSON(doc))
				if err == nil || !strings.Contains(err.Error(), "todo.read") {
					t.Fatalf("got %v, want operation-specific policy failure", err)
				}
			})
		}
	})

	t.Run("tenant selectors require server-side binding", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(map[string]any)
		}{
			{
				name: "explicit workspace must be required input",
				mutate: func(op map[string]any) {
					op["policy"].(map[string]any)["workspaceSelection"] = "explicit_member"
				},
			},
			{
				name: "resource field must be required input",
				mutate: func(op map[string]any) {
					op["policy"].(map[string]any)["resourceSelection"] = "request_bound"
					op["policy"].(map[string]any)["resourceField"] = "/missingId"
				},
			},
			{
				name: "resource field cannot be omitted",
				mutate: func(op map[string]any) {
					op["policy"].(map[string]any)["resourceSelection"] = "request_bound"
				},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				doc := fixtureDocument()
				op := operation("workspace.read", "workspace_read", "eligible", "none", "")
				tc.mutate(op)
				doc["operations"] = []any{op}
				_, err := validateMapping(mustJSON(doc))
				if err == nil || !strings.Contains(err.Error(), "workspace.read") {
					t.Fatalf("got %v, want operation-specific binding failure", err)
				}
			})
		}
	})

	t.Run("unsupported and unbounded schema shapes fail closed", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(map[string]any)
		}{
			{
				name: "union",
				mutate: func(op map[string]any) {
					op["inputSchema"].(map[string]any)["properties"].(map[string]any)["limit"].(map[string]any)["type"] = []any{"integer", "null"}
				},
			},
			{
				name: "nested object",
				mutate: func(op map[string]any) {
					op["inputSchema"].(map[string]any)["properties"].(map[string]any)["limit"].(map[string]any)["type"] = "object"
				},
			},
			{
				name: "unbounded array",
				mutate: func(op map[string]any) {
					f := op["inputSchema"].(map[string]any)["properties"].(map[string]any)["limit"].(map[string]any)
					f["type"] = "array"
					f["items"] = map[string]any{"type": "string", "description": "value"}
					delete(f, "maxItems")
				},
			},
			{
				name: "unbounded string",
				mutate: func(op map[string]any) {
					f := op["inputSchema"].(map[string]any)["properties"].(map[string]any)["limit"].(map[string]any)
					f["type"] = "string"
					delete(f, "minimum")
					delete(f, "maximum")
				},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				doc := fixtureDocument()
				op := operation("todo.query", "todo_query", "eligible", "none", "")
				tc.mutate(op)
				doc["operations"] = []any{op}
				_, err := validateMapping(mustJSON(doc))
				if err == nil || !strings.Contains(err.Error(), "todo.query") {
					t.Fatalf("got %v, want operation-specific schema failure", err)
				}
			})
		}
	})

	t.Run("duplicate names identify both operations", func(t *testing.T) {
		doc := fixtureDocument()
		doc["operations"] = []any{
			operation("todo.read", "todo", "eligible", "none", ""),
			operation("todo.write", "todo", "eligible", "none", ""),
		}
		_, err := validateMapping(mustJSON(doc))
		if err == nil || !strings.Contains(err.Error(), "todo.read") || !strings.Contains(err.Error(), "todo.write") {
			t.Fatalf("got %v, want both conflicting operation IDs", err)
		}
	})
}

func validateMapping(raw []byte) ([]string, error) {
	var doc mappingDocument
	if err := decodeStrict(raw, &doc); err != nil {
		return nil, fmt.Errorf("mapping: %w", err)
	}
	if doc.Contract != "amos-mcp-mapping-v1" {
		return nil, fmt.Errorf("mapping: unsupported contract %q", doc.Contract)
	}
	if len(doc.Operations) == 0 || len(doc.Operations) > 128 {
		return nil, fmt.Errorf("mapping: operation count must be between 1 and 128")
	}
	seenIDs := make(map[string]string, len(doc.Operations))
	seenNames := make(map[string]string, len(doc.Operations))
	visible := make([]mappingOperation, 0, len(doc.Operations))
	for _, operationRaw := range doc.Operations {
		var identity struct {
			OperationID string
		}
		if err := json.Unmarshal(operationRaw, &identity); err != nil {
			return nil, fmt.Errorf("operation <unknown>: invalid operation record: %w", err)
		}
		var op mappingOperation
		prefix := "operation " + identity.OperationID
		if err := decodeStrict(operationRaw, &op); err != nil {
			return nil, fmt.Errorf("%s: invalid operation record: %w", prefix, err)
		}
		if !operationIDPattern.MatchString(op.OperationID) || utf8.RuneCountInString(op.OperationID) > 120 {
			return nil, fmt.Errorf("%s: invalid operation ID", prefix)
		}
		if !toolNamePattern.MatchString(op.ToolName) || len(op.ToolName) > 64 {
			return nil, fmt.Errorf("%s: invalid tool name", prefix)
		}
		if prior, ok := seenIDs[op.OperationID]; ok {
			return nil, fmt.Errorf("%s: duplicate operation ID conflicts with %s", prefix, prior)
		}
		if prior, ok := seenNames[op.ToolName]; ok {
			return nil, fmt.Errorf("%s: duplicate tool name %q conflicts with operation %s", prefix, op.ToolName, prior)
		}
		seenIDs[op.OperationID] = op.OperationID
		seenNames[op.ToolName] = op.OperationID
		if strings.TrimSpace(op.Description) == "" || utf8.RuneCountInString(op.Description) > 2048 {
			return nil, fmt.Errorf("%s: invalid description", prefix)
		}
		if err := validatePolicy(prefix, op); err != nil {
			return nil, err
		}
		if err := validateInput(prefix, op); err != nil {
			return nil, err
		}
		if op.Policy.Exposure != "never" {
			visible = append(visible, op)
		}
	}
	sort.Slice(visible, func(i, j int) bool { return visible[i].OperationID < visible[j].OperationID })
	names := make([]string, 0, len(visible))
	for _, op := range visible {
		names = append(names, op.ToolName)
	}
	return names, nil
}

func validatePolicy(prefix string, op mappingOperation) error {
	p := op.Policy
	if p.Exposure != "never" && p.Exposure != "eligible" && p.Exposure != "challenge" {
		return fmt.Errorf("%s: invalid exposure", prefix)
	}
	if p.Exposure != "never" && len(p.Permissions) == 0 {
		return fmt.Errorf("%s: exposed operation requires at least one permission", prefix)
	}
	if p.Exposure != "never" && p.Entitlements == nil {
		return fmt.Errorf("%s: exposed operation requires an explicit entitlement list", prefix)
	}
	if err := uniqueNonempty(p.Permissions); err != nil {
		return fmt.Errorf("%s: invalid permissions: %w", prefix, err)
	}
	if err := uniqueNonempty(p.Entitlements); err != nil {
		return fmt.Errorf("%s: invalid entitlements: %w", prefix, err)
	}
	if p.MinimumAssurance != "aal1" && p.MinimumAssurance != "aal2" && p.MinimumAssurance != "aal3" {
		return fmt.Errorf("%s: minimum assurance is required", prefix)
	}
	if p.SideEffect != "read" && p.SideEffect != "write" && p.SideEffect != "external" {
		return fmt.Errorf("%s: side effect class is required", prefix)
	}
	if p.Idempotency != "required" && p.Idempotency != "natural" && p.Idempotency != "forbidden" {
		return fmt.Errorf("%s: idempotency rule is required", prefix)
	}
	if p.WorkspaceSelection != "active_principal" && p.WorkspaceSelection != "explicit_member" {
		return fmt.Errorf("%s: workspace selection is required", prefix)
	}
	if p.ResourceSelection != "none" && p.ResourceSelection != "request_bound" {
		return fmt.Errorf("%s: resource selection is required", prefix)
	}
	switch p.ResourceSelection {
	case "none":
		if p.ResourceField != "" {
			return fmt.Errorf("%s: resourceField is only valid for request_bound selection", prefix)
		}
	case "request_bound":
		if !strings.HasPrefix(p.ResourceField, "/") || strings.Count(p.ResourceField, "/") != 1 {
			return fmt.Errorf("%s: request_bound selection requires one top-level resourceField", prefix)
		}
	}
	return nil
}

func validateInput(prefix string, op mappingOperation) error {
	s := op.InputSchema
	if s.Type != "object" || s.AdditionalProperties == nil || *s.AdditionalProperties {
		return fmt.Errorf("%s: input must be a closed object", prefix)
	}
	if len(s.Properties) > 64 {
		return fmt.Errorf("%s: input has more than 64 properties", prefix)
	}
	required := make(map[string]bool, len(s.Required))
	for _, name := range s.Required {
		if !propertyPattern.MatchString(name) || required[name] {
			return fmt.Errorf("%s: invalid or duplicate required property %q", prefix, name)
		}
		required[name] = true
		if _, ok := s.Properties[name]; !ok {
			return fmt.Errorf("%s: required property %q is not defined", prefix, name)
		}
	}
	for name, field := range s.Properties {
		if !propertyPattern.MatchString(name) {
			return fmt.Errorf("%s: invalid input property %q", prefix, name)
		}
		if err := validateField(field); err != nil {
			return fmt.Errorf("%s: input field %q: %w", prefix, name, err)
		}
	}
	if op.Policy.WorkspaceSelection == "explicit_member" && !required["workspaceId"] {
		return fmt.Errorf("%s: explicit_member workspaceId must be required input", prefix)
	}
	if op.Policy.ResourceSelection == "request_bound" {
		name := strings.TrimPrefix(op.Policy.ResourceField, "/")
		if !required[name] {
			return fmt.Errorf("%s: resourceField %q must be required input", prefix, op.Policy.ResourceField)
		}
	}
	return nil
}

func validateField(f fieldSchema) error {
	if strings.TrimSpace(f.Description) == "" || utf8.RuneCountInString(f.Description) > 512 {
		return fmt.Errorf("description is required and bounded")
	}
	switch f.Type {
	case "string":
		if f.MaxLength == nil || *f.MaxLength < 1 || *f.MaxLength > 4096 {
			return fmt.Errorf("string maxLength must be between 1 and 4096")
		}
		if f.MinLength != nil && (*f.MinLength < 0 || *f.MinLength > *f.MaxLength) {
			return fmt.Errorf("string minLength must be within maxLength")
		}
		if f.Format != "" && f.Format != "email" && f.Format != "date-time" && f.Format != "uuid" {
			return fmt.Errorf("unsupported string format %q", f.Format)
		}
		if err := validateEnum(f.Enum); err != nil {
			return err
		}
	case "boolean":
		if f.Format != "" || f.MinLength != nil || f.MaxLength != nil || f.Minimum != nil || f.Maximum != nil || f.MinItems != nil || f.MaxItems != nil || f.Items != nil {
			return fmt.Errorf("boolean field has incompatible constraints")
		}
		if err := validateEnum(f.Enum); err != nil {
			return err
		}
	case "integer", "number":
		if f.Minimum == nil || f.Maximum == nil || math.IsNaN(*f.Minimum) || math.IsNaN(*f.Maximum) || math.IsInf(*f.Minimum, 0) || math.IsInf(*f.Maximum, 0) || *f.Minimum > *f.Maximum {
			return fmt.Errorf("numeric fields require a finite ordered minimum and maximum")
		}
		if f.Format != "" || f.MinLength != nil || f.MaxLength != nil || f.MinItems != nil || f.MaxItems != nil || f.Items != nil {
			return fmt.Errorf("numeric field has incompatible constraints")
		}
		if err := validateEnum(f.Enum); err != nil {
			return err
		}
	case "array":
		if f.Items == nil || f.MinItems == nil || f.MaxItems == nil || *f.MinItems < 0 || *f.MaxItems < 1 || *f.MaxItems > 100 || *f.MinItems > *f.MaxItems {
			return fmt.Errorf("array requires scalar items and ordered bounds within 100")
		}
		if f.Format != "" || f.Enum != nil || f.MinLength != nil || f.MaxLength != nil || f.Minimum != nil || f.Maximum != nil {
			return fmt.Errorf("array field has incompatible constraints")
		}
		if err := validateScalar(*f.Items); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported type %q", f.Type)
	}
	return nil
}

func validateScalar(f scalarSchema) error {
	if strings.TrimSpace(f.Description) == "" {
		return fmt.Errorf("array item description is required")
	}
	if f.Type != "string" && f.Type != "boolean" && f.Type != "integer" && f.Type != "number" {
		return fmt.Errorf("array items must be scalar")
	}
	if f.Type == "string" && (f.MaxLength == nil || *f.MaxLength < 1 || *f.MaxLength > 4096) {
		return fmt.Errorf("array string item requires bounded maxLength")
	}
	if (f.Type == "integer" || f.Type == "number") && (f.Minimum == nil || f.Maximum == nil || *f.Minimum > *f.Maximum || math.IsInf(*f.Minimum, 0) || math.IsInf(*f.Maximum, 0) || math.IsNaN(*f.Minimum) || math.IsNaN(*f.Maximum)) {
		return fmt.Errorf("array numeric item requires finite ordered bounds")
	}
	return nil
}

func validateEnum(values []any) error {
	if len(values) > 64 {
		return fmt.Errorf("enum exceeds 64 values")
	}
	return nil
}

func uniqueNonempty(values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" || seen[value] {
			return fmt.Errorf("values must be nonempty and unique")
		}
		seen[value] = true
	}
	return nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func fixtureDocument() map[string]any {
	return map[string]any{"contract": "amos-mcp-mapping-v1", "operations": []any{}}
}

func operation(id, name, exposure, resourceSelection, resourceField string) map[string]any {
	input := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit":       map[string]any{"type": "integer", "description": "Maximum rows", "minimum": float64(1), "maximum": float64(100)},
			"todoId":      map[string]any{"type": "string", "description": "Selected todo", "maxLength": 36, "format": "uuid"},
			"workspaceId": map[string]any{"type": "string", "description": "Selected workspace", "maxLength": 36, "format": "uuid"},
		},
		"required":             []any{"limit"},
		"additionalProperties": false,
	}
	if resourceSelection == "request_bound" {
		input["required"] = []any{"limit", "todoId"}
	}
	policy := map[string]any{
		"exposure":           exposure,
		"permissions":        []any{"todos.read"},
		"entitlements":       []any{},
		"minimumAssurance":   "aal1",
		"sideEffect":         "read",
		"idempotency":        "natural",
		"workspaceSelection": "active_principal",
		"resourceSelection":  resourceSelection,
	}
	if resourceField != "" {
		policy["resourceField"] = resourceField
	}
	return map[string]any{
		"operationId": id,
		"toolName":    name,
		"description": "Read a workspace-scoped record.",
		"inputSchema": input,
		"policy":      policy,
	}
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
