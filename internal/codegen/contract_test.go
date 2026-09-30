package codegen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validContract = `openapi: 3.1.1
info:
  title: Synthetic AMOS contract
  version: 1.0.0
paths:
  /widgets:
    get:
      operationId: widgets.list
      responses:
        '200':
          description: synthetic response
      x-amos-policy:
        operationId: widgets.list
        permissions: []
        entitlements: []
        assurance: aal1
        sideEffect: read
        idempotency: forbidden
        mcp: never
    post:
      operationId: widgets.create
      responses:
        '201':
          description: synthetic response
      x-amos-policy:
        operationId: widgets.create
        permissions:
          - widgets:create
        entitlements: []
        assurance: aal2
        sideEffect: write
        idempotency: required
        mcp: challenge
`

func TestValidateProducesDeterministicManifest(t *testing.T) {
	manifest, err := Validate([]byte(validContract))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got, want := len(manifest.Operations), 2; got != want {
		t.Fatalf("operations = %d, want %d", got, want)
	}
	if got, want := manifest.Operations[0].Policy.OperationID, "widgets.create"; got != want {
		t.Fatalf("first operation = %q, want sorted operation %q", got, want)
	}
	first, err := RenderManifest(manifest)
	if err != nil {
		t.Fatalf("RenderManifest() error = %v", err)
	}
	secondManifest, err := Validate([]byte(validContract))
	if err != nil {
		t.Fatalf("second Validate() error = %v", err)
	}
	second, err := RenderManifest(secondManifest)
	if err != nil {
		t.Fatalf("second RenderManifest() error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("identical input produced different manifests:\n%s\n%s", first, second)
	}
	if !strings.HasSuffix(string(first), "\n") {
		t.Fatal("manifest does not end with a newline")
	}
}

func TestValidateRejectsUnsupportedContracts(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing policy",
			body: strings.Replace(validContract, "      x-amos-policy:\n        operationId: widgets.list\n        permissions: []\n        entitlements: []\n        assurance: aal1\n        sideEffect: read\n        idempotency: forbidden\n        mcp: never\n", "", 1),
			want: "required policy extension is missing",
		},
		{
			name: "unknown policy field",
			body: strings.Replace(validContract, "        mcp: never\n", "        mcp: never\n        secretDefault: enabled\n", 1),
			want: "unknown field",
		},
		{
			name: "missing required empty arrays",
			body: strings.Replace(validContract, "        permissions: []\n", "", 1),
			want: "permissions is required",
		},
		{
			name: "operation policy id mismatch",
			body: strings.Replace(validContract, "        operationId: widgets.list\n", "        operationId: widgets.other\n", 1),
			want: "does not match operationId",
		},
		{
			name: "duplicate operation IDs",
			body: strings.Replace(validContract, "operationId: widgets.create", "operationId: widgets.list", 2),
			want: "duplicate",
		},
		{
			name: "unsupported OpenAPI version",
			body: strings.Replace(validContract, "openapi: 3.1.1", "openapi: 3.1.0", 1),
			want: "openapi version is unsupported",
		},
		{
			name: "unresolved internal reference",
			body: strings.Replace(validContract, "description: synthetic response", "content:\n            application/json:\n              schema:\n                $ref: '#/components/schemas/Missing'", 1),
			want: "reference target does not exist",
		},
		{
			name: "remote reference",
			body: strings.Replace(validContract, "description: synthetic response", "content:\n            application/json:\n              schema:\n                $ref: 'https://schemas.invalid/secret.yaml'", 1),
			want: "only internal JSON Pointer references are supported",
		},
		{
			name: "local file reference",
			body: strings.Replace(validContract, "description: synthetic response", "content:\n            application/json:\n              schema:\n                $ref: '../private.yaml#/Secret'", 1),
			want: "only internal JSON Pointer references are supported",
		},
		{
			name: "cyclic component references",
			body: validContract + `components:
  schemas:
    A:
      properties:
        b:
          $ref: '#/components/schemas/B'
    B:
      properties:
        a:
          $ref: '#/components/schemas/A'
`,
			want: "cyclic OpenAPI references are unsupported",
		},
		{
			name: "duplicate YAML fields",
			body: strings.Replace(validContract, "      operationId: widgets.list\n", "      operationId: widgets.list\n      operationId: widgets.copy\n", 1),
			want: "duplicate YAML key",
		},
		{
			name: "unsupported operation field",
			body: strings.Replace(validContract, "      operationId: widgets.list\n", "      operationId: widgets.list\n      secretDefault: enabled\n", 1),
			want: "unsupported operation field",
		},
		{
			name: "unsupported webhooks",
			body: validContract + "webhooks: {}\n",
			want: "webhooks are unsupported",
		},
		{
			name: "path item reference",
			body: strings.Replace(validContract, "  /widgets:\n    get:\n", "  /widgets:\n    $ref: '#/components/pathItems/Shared'\n    get:\n", 1),
			want: "Path Item references are unsupported",
		},
		{
			name: "callback operation",
			body: strings.Replace(validContract, "      operationId: widgets.list\n", "      operationId: widgets.list\n      callbacks: {}\n", 1),
			want: "callbacks are unsupported",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Validate([]byte(test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateBoundsInputAndNesting(t *testing.T) {
	t.Run("size", func(t *testing.T) {
		if _, err := Validate(bytes.Repeat([]byte(" "), MaxContractBytes+1)); err == nil || !strings.Contains(err.Error(), "byte limit") {
			t.Fatalf("oversized input error = %v", err)
		}
	})
	t.Run("depth", func(t *testing.T) {
		body := "openapi: 3.1.1\ninfo:\n  title: depth\n  version: 1\npaths: {}\n"
		body += "x-deep: " + strings.Repeat("[", MaxYAMLDepth+4) + "value" + strings.Repeat("]", MaxYAMLDepth+4) + "\n"
		if _, err := Validate([]byte(body)); err == nil || !strings.Contains(err.Error(), "nesting exceeds") {
			t.Fatalf("deep input error = %v", err)
		}
	})
}

func TestValidateErrorsDoNotEchoContractValues(t *testing.T) {
	const sensitiveValue = "private-diagnostic-value"
	malformed := "openapi: [\nsecret: " + sensitiveValue + "\n"
	if _, err := Validate([]byte(malformed)); err == nil || strings.Contains(err.Error(), sensitiveValue) {
		t.Fatalf("syntax error leaked source material or passed: %v", err)
	}
	invalidPolicy := strings.Replace(validContract, "        sideEffect: read\n", "        sideEffect: "+sensitiveValue+"\n", 1)
	if _, err := Validate([]byte(invalidPolicy)); err == nil || strings.Contains(err.Error(), sensitiveValue) {
		t.Fatalf("policy error leaked source value or passed: %v", err)
	}
}

func TestWriteManifestPreservesUnownedFilesAndReplacesOwnOutput(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(path, []byte("handwritten content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := Validate([]byte(validContract))
	if err != nil {
		t.Fatal(err)
	}
	first, err := RenderManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(path, first); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("WriteManifest() error = %v, want ownership refusal", err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != "handwritten content\n" {
		t.Fatalf("handwritten target changed to %q", unchanged)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(path, first); err != nil {
		t.Fatalf("first generated write: %v", err)
	}
	if err := WriteManifest(path, first); err != nil {
		t.Fatalf("idempotent generated write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, first) {
		t.Fatal("generated manifest bytes changed on repeat write")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory has %d entries after writes, want only manifest", len(entries))
	}
}
