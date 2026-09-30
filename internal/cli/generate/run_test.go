package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cliContract = `openapi: 3.1.1
info:
  title: CLI fixture
  version: 1
paths:
  /ping:
    get:
      operationId: fixture.ping
      responses:
        '200':
          description: pong
      x-amos-policy:
        operationId: fixture.ping
        permissions: []
        entitlements: []
        assurance: aal1
        sideEffect: read
        idempotency: forbidden
        mcp: never
`

func TestRunWritesValidatedManifestAtomically(t *testing.T) {
	directory := t.TempDir()
	spec := filepath.Join(directory, "contract.yaml")
	output := filepath.Join(directory, "generated", "manifest.json")
	if err := os.Mkdir(filepath.Dir(output), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, []byte(cliContract), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"--spec", spec, "--manifest", output}, &stdout, &stderr); err != nil {
		t.Fatalf("Run() error = %v, stderr = %q", err, stderr.String())
	}
	manifest, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read generated manifest: %v", err)
	}
	if !strings.Contains(string(manifest), `"fixture.ping"`) || !strings.Contains(stdout.String(), "validated 1 operation") {
		t.Fatalf("manifest/status missing expected operation: status=%q manifest=%s", stdout.String(), manifest)
	}
	first := append([]byte(nil), manifest...)
	stdout.Reset()
	if err := Run([]string{"--spec", spec, "--manifest", output}, &stdout, &stderr); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	manifest, err = os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, manifest) {
		t.Fatal("identical source changed manifest bytes on repeat run")
	}
}

func TestRunRejectsInvalidInputWithoutTouchingExistingOutput(t *testing.T) {
	directory := t.TempDir()
	spec := filepath.Join(directory, "invalid.yaml")
	output := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(spec, []byte(strings.Replace(cliContract, "        mcp: never\n", "", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	const handwritten = "owner data\n"
	if err := os.WriteFile(output, []byte(handwritten), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"--spec", spec, "--manifest", output}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "mcp is missing or unsupported") {
		t.Fatalf("Run() error = %v, want policy validation failure", err)
	}
	actual, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != handwritten {
		t.Fatalf("invalid input changed existing output to %q", actual)
	}
}

func TestRunDoesNotEchoMissingInputPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-source.yaml")
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"--spec", path}, &stdout, &stderr); err == nil || err.Error() != "unable to read OpenAPI source" {
		t.Fatalf("Run() error = %v, want sanitized read error", err)
	}
	if strings.Contains(stdout.String()+stderr.String(), path) {
		t.Fatal("missing-input error leaked the source path")
	}
}

func TestRunRejectsContractWithoutOperationPolicy(t *testing.T) {
	directory := t.TempDir()
	spec := filepath.Join(directory, "missing-policy.yaml")
	withoutPolicy := strings.Replace(cliContract, `      x-amos-policy:
        operationId: fixture.ping
        permissions: []
        entitlements: []
        assurance: aal1
        sideEffect: read
        idempotency: forbidden
        mcp: never
`, "", 1)
	if err := os.WriteFile(spec, []byte(withoutPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"--spec", spec}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "required policy extension is missing") {
		t.Fatalf("Run() error = %v, want missing-policy rejection", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("invalid contract emitted partial output: %q", stdout.String())
	}
}
