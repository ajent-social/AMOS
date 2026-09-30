package audit_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ajent-social/amos/audit"
	"github.com/google/uuid"
)

func TestDecodeEventRejectsSensitiveAndUnknownFieldsWithoutEcho(t *testing.T) {
	event := validEvent(t)
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal("marshal synthetic event")
	}
	payload := strings.TrimSuffix(string(encoded), "}") + `,"token":"synthetic-bearer-value","request_body":"synthetic-private-body"}`
	_, err = audit.DecodeEvent([]byte(payload))
	if !errors.Is(err, audit.ErrInvalidEvent) {
		t.Fatalf("sensitive fields result=%v", err)
	}
	if strings.Contains(err.Error(), "synthetic-bearer-value") || strings.Contains(err.Error(), "synthetic-private-body") {
		t.Fatal("rejection error echoed sensitive input")
	}
}

func TestValidateRejectsArbitraryMetadata(t *testing.T) {
	event := validEvent(t)
	event.Attributes = []audit.Attribute{{Key: "token", Value: "synthetic-secret"}}
	if err := audit.Validate(event); !errors.Is(err, audit.ErrInvalidEvent) {
		t.Fatalf("arbitrary metadata result=%v", err)
	}
}

func TestValidateRequiresRFC4122Variant(t *testing.T) {
	event := validEvent(t)
	event.WorkspaceID = nonRFCVariant(t)
	if err := audit.Validate(event); !errors.Is(err, audit.ErrInvalidEvent) {
		t.Fatalf("non-RFC UUID variant result=%v", err)
	}
}

func validEvent(t *testing.T) audit.Event {
	t.Helper()
	newID := func() uuid.UUID {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatal("generate synthetic UUIDv7")
		}
		return id
	}
	return audit.Event{
		InstallationID: newID(), ApplicationID: newID(), EnvironmentID: newID(), WorkspaceID: newID(),
		Action:       audit.ActionMaterialCreated,
		ResourceType: audit.ResourceMaterial, ResourceID: newID(), Outcome: audit.OutcomeSucceeded,
		CorrelationID: newID(), Attributes: []audit.Attribute{{Key: "category", Value: "secret"}, {Key: "state", Value: "created"}},
	}
}

func nonRFCVariant(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal("generate synthetic UUIDv7")
	}
	id[8] = (id[8] & 0x3f) | 0x40
	if id.Version() != 7 || id.Variant() == uuid.RFC4122 {
		t.Fatal("invalid-variant UUID fixture is malformed")
	}
	return id
}
