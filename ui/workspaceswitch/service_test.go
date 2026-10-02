package workspaceswitch

import (
	"testing"

	"github.com/google/uuid"
)

func TestGeneratedOrganizationLabelsDifferForUUIDv7CreatedTogether(t *testing.T) {
	first := uuid.MustParse("01890f0e-0000-7000-8000-000000000001")
	second := uuid.MustParse("01890f0e-0000-7000-8000-000000000002")
	if first.String()[:8] != second.String()[:8] {
		t.Fatal("test IDs must share a UUIDv7 time prefix")
	}
	firstName := generatedWorkspaceName(first, "organization")
	secondName := generatedWorkspaceName(second, "organization")
	if firstName == secondName {
		t.Fatalf("distinct organizations have ambiguous labels %q", firstName)
	}
}
