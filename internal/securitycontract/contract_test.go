package securitycontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readCheckedInMatrix(t *testing.T) ([]byte, string) {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "docs", "security", "control-matrix.json"))
	if err != nil {
		t.Fatalf("read checked-in control matrix: %v", err)
	}
	return data, root
}

func decodeMatrixFixture(t *testing.T) (Matrix, string) {
	t.Helper()
	data, root := readCheckedInMatrix(t)
	var matrix Matrix
	if err := json.Unmarshal(data, &matrix); err != nil {
		t.Fatalf("decode checked-in control matrix: %v", err)
	}
	return matrix, root
}

func TestCheckedInMatrixValidates(t *testing.T) {
	data, root := readCheckedInMatrix(t)
	if err := DecodeAndValidate(data, root); err != nil {
		t.Fatalf("validate checked-in matrix: %v", err)
	}
}

func TestThreatCoverageRejectsUnownedP0(t *testing.T) {
	matrix, root := decodeMatrixFixture(t)
	for i := range matrix.Threats {
		if matrix.Threats[i].Severity == "P0" {
			matrix.Threats[i].DeniedPathControl = ""
			break
		}
	}
	if err := ValidateMatrix(matrix, root); err == nil || !strings.Contains(err.Error(), "owned denied-path test control") || !strings.Contains(err.Error(), "P0 threat") {
		t.Fatalf("ValidateMatrix() error = %v, want unowned P0 denial-test failure", err)
	}
}

func TestRejectsPromptOnlyControlEvidence(t *testing.T) {
	matrix, root := decodeMatrixFixture(t)
	matrix.Controls[0].SourceRefs = []SourceRef{{Kind: "prompt_text", Path: "docs/tasks/T15.1.md"}}
	if err := ValidateMatrix(matrix, root); err == nil || !strings.Contains(err.Error(), "prompt text is not evidence") {
		t.Fatalf("ValidateMatrix() error = %v, want prompt-only evidence failure", err)
	}
}

func TestControlMustCiteItsAssignedOwnerTask(t *testing.T) {
	matrix, root := decodeMatrixFixture(t)
	matrix.Controls[0].OwnerTask = "T15.1"
	if err := ValidateMatrix(matrix, root); err == nil || !strings.Contains(err.Error(), "does not cite its assigned owner task contract") {
		t.Fatalf("ValidateMatrix() error = %v, want owner/task evidence mismatch", err)
	}
}

func TestThreatCoverageRequiresReleaseIdentityCompromise(t *testing.T) {
	matrix, root := decodeMatrixFixture(t)
	filtered := matrix.Threats[:0]
	for _, threat := range matrix.Threats {
		if threat.ID != "THR-RELEASE-IDENTITY-COMPROMISE" {
			filtered = append(filtered, threat)
		}
	}
	matrix.Threats = filtered
	if err := ValidateMatrix(matrix, root); err == nil || !strings.Contains(err.Error(), "missing required release-identity boundary") {
		t.Fatalf("ValidateMatrix() error = %v, want missing release-identity boundary", err)
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	data, root := readCheckedInMatrix(t)
	data = append(data[:len(data)-2], []byte(",\n  \"prompt_authority\": \"evidence\"\n}\n")...)
	if err := DecodeAndValidate(data, root); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("DecodeAndValidate() error = %v, want unknown field rejection", err)
	}
}
