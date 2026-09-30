package apigen

import (
	"encoding/json"
	"testing"
)

func TestGeneratedObjectValidation(t *testing.T) {
	var input ModelWidgetInput
	if err := json.Unmarshal([]byte(`{"name":"one","priority":1,"labels":[],"note":null}`), &input); err != nil {
		t.Fatalf("valid input was rejected: %v", err)
	}
	if !input.Note.Present || !input.Note.Null {
		t.Fatal("optional nullable field did not preserve explicit null")
	}
	for _, invalid := range []string{
		`{"priority":1,"labels":[]}`,
		`{"name":"one","priority":null,"labels":[]}`,
		`{"name":"one","priority":1,"labels":[],"extra":true}`,
	} {
		if err := json.Unmarshal([]byte(invalid), &input); err == nil {
			t.Errorf("invalid object was accepted: %s", invalid)
		}
	}
}
