package jobs

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidateIntentBoundsAndJSON(t *testing.T) {
	installation, _ := uuid.NewV7()
	application, _ := uuid.NewV7()
	base := Intent{InstallationID: installation, ApplicationID: application, Key: "request-1", Kind: "email.send", Payload: []byte(`{"to":"synthetic@example.test"}`), Deadline: time.Now().UTC().Add(time.Hour)}
	if err := ValidateIntent(base, 128); err != nil {
		t.Fatal(err)
	}
	base.Payload = []byte("not-json")
	if err := ValidateIntent(base, 128); err == nil {
		t.Fatal("accepted malformed JSON")
	}
	base.Payload = []byte(`{"to":"synthetic@example.test"}`)
	if err := ValidateIntent(base, 8); err == nil {
		t.Fatal("accepted oversized payload")
	}
}
