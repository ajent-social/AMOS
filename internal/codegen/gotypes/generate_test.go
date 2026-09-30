package gotypes

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateTypedContractAndCompileConsumer(t *testing.T) {
	source, err := os.ReadFile("../testdata/typed/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := Generate(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, second) {
		t.Fatal("generation is not deterministic")
	}
	text := string(generated)
	for _, want := range []string{
		GeneratedHeader,
		"type ModelWidgetInput struct",
		"Priority ModelWidgetInputPriority",
		"OptionalNullable[ModelWidgetInputNoteValue] `json:\"note\"`",
		"type OpWidgetsCreateHandler interface",
		"type OpWidgetsCreateResponse = ModelWidget",
		"type OperationMetadata struct",
		"SideEffect: \"write\"",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("generated source missing %q", want)
		}
	}
	if strings.Contains(text, "value any") || strings.Contains(text, "interface{}") || strings.Contains(text, "type ModelWidgetInput struct {\n\tPriority any") {
		t.Fatal("generated schema types contain an untyped fallback")
	}

	consumer, err := os.ReadFile("../testdata/typed/consumer.go")
	if err != nil {
		t.Fatal(err)
	}
	consumerTest, err := os.ReadFile("../testdata/typed/consumer_test.go")
	if err != nil {
		t.Fatal(err)
	}
	compileFixture(t, generated, consumer, consumerTest, true)

	// A schema amendment that changes priority to string must break existing
	// business code at compile time instead of silently weakening the field.
	amended := bytes.Replace(source, []byte("        priority:\n          type: integer\n          format: int64"), []byte("        priority:\n          type: string"), 1)
	if bytes.Equal(amended, source) {
		t.Fatal("schema amendment fixture did not apply")
	}
	amendedOutput, err := Generate(amended)
	if err != nil {
		t.Fatalf("amended schema should remain valid: %v", err)
	}
	compileFixture(t, amendedOutput, consumer, consumerTest, false)
}

func compileFixture(t *testing.T, generated, consumer, consumerTest []byte, wantSuccess bool) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string][]byte{"generated.go": generated, "consumer.go": consumer, "consumer_test.go": consumerTest} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GO111MODULE=off")
	output, err := cmd.CombinedOutput()
	if wantSuccess && err != nil {
		t.Fatalf("generated consumer did not compile: %v\n%s", err, output)
	}
	if !wantSuccess {
		if err == nil {
			t.Fatal("typed schema amendment did not cause consumer compile failure")
		}
		if !bytes.Contains(output, []byte("mismatched types")) {
			t.Fatalf("compile failed for an unexpected reason: %v\n%s", err, output)
		}
	}
}

func TestGenerateRejectsUnsupportedSchemaShapes(t *testing.T) {
	source, err := os.ReadFile("../testdata/typed/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ name, from, to string }{
		{"union", "type: [string, 'null']", "oneOf:\n            - type: string\n            - type: integer"},
		{"open-object", "additionalProperties: false", "additionalProperties: true"},
		{"external-ref", "#/components/schemas/WidgetInput", "https://example.invalid/schema.yaml"},
		{"injected-property", "        name:\n          type: string", "        'name; package injected':\n          type: string"},
		{"normalized-collision", "        note:\n          type: [string, 'null']", "        note:\n          type: [string, 'null']\n        'note-value':\n          type: integer"},
		{"object-format", "      type: object\n      additionalProperties: false", "      type: object\n      format: unknown\n      additionalProperties: false"},
		{"array-format", "        labels:\n          type: array", "        labels:\n          type: array\n          format: unknown"},
	} {
		t.Run(change.name, func(t *testing.T) {
			mutated := bytes.Replace(source, []byte(change.from), []byte(change.to), 1)
			if bytes.Equal(mutated, source) {
				t.Fatal("mutation was not applied")
			}
			if _, err := Generate(mutated); err == nil {
				t.Fatal("unsupported schema shape was accepted")
			}
		})
	}
	mutated := bytes.Replace(source, []byte("required: [name, priority, labels]"), []byte("required: [name, priority, labels, foo_bar]"), 1)
	mutated = bytes.Replace(mutated, []byte("        note:\n"), []byte("        'foo-bar':\n          type: string\n        note:\n"), 1)
	if bytes.Equal(mutated, source) {
		t.Fatal("required-name mutation was not applied")
	}
	if _, err := Generate(mutated); err == nil {
		t.Fatal("required name that only matches after Go identifier normalization was accepted")
	}
}
