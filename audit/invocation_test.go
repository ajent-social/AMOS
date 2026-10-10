package audit_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ajent-social/amos/audit"
)

func TestInvocationEventFiniteVocabulary(t *testing.T) {
	base := validEvent(t)
	base.Action = audit.ActionOperationInvoked
	base.ResourceType = audit.ResourceInvocation
	base.OperationID = "continuity.case.change"
	base.Attributes = []audit.Attribute{}
	for _, outcome := range []audit.Outcome{audit.OutcomeSucceeded, audit.OutcomeDenied, audit.OutcomeUnavailable} {
		t.Run(string(outcome), func(t *testing.T) {
			event := base
			event.Outcome = outcome
			raw, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			got, err := audit.DecodeEvent(raw)
			if err != nil || got.OperationID != event.OperationID || got.ResourceID != event.ResourceID || got.Outcome != outcome {
				t.Fatal("finite invocation round trip failed")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*audit.Event)
	}{
		{"missing operation", func(e *audit.Event) { e.OperationID = "" }},
		{"short operation", func(e *audit.Event) { e.OperationID = "ab" }},
		{"long operation", func(e *audit.Event) { e.OperationID = strings.Repeat("a", 121) }},
		{"operation whitespace", func(e *audit.Event) { e.OperationID = "continuity.case.change " }},
		{"operation control", func(e *audit.Event) { e.OperationID = "continuity\ncase" }},
		{"operation unicode", func(e *audit.Event) { e.OperationID = "continuity.cásé" }},
		{"operation uppercase", func(e *audit.Event) { e.OperationID = "Continuity.case" }},
		{"operation consecutive separators", func(e *audit.Event) { e.OperationID = "continuity..case" }},
		{"wrong resource", func(e *audit.Event) { e.ResourceType = audit.ResourceSession }},
		{"wrong action", func(e *audit.Event) { e.Action = audit.ActionAccessDenied }},
		{"legacy resource with operation", func(e *audit.Event) { e.Action = audit.ActionMaterialCreated; e.ResourceType = audit.ResourceMaterial }},
		{"legacy action invocation resource", func(e *audit.Event) { e.Action = audit.ActionMaterialCreated; e.OperationID = "" }},
		{"otherwise allowed attributes", func(e *audit.Event) { e.Attributes = []audit.Attribute{{Key: "factor", Value: "password"}} }},
		{"invented conflict outcome", func(e *audit.Event) { e.Outcome = "conflict" }},
		{"non RFC resource", func(e *audit.Event) { e.ResourceID = nonRFCVariant(t) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := base
			tc.change(&e)
			if !errors.Is(audit.Validate(e), audit.ErrInvalidEvent) {
				t.Fatal("invalid invocation accepted")
			}
		})
	}
}

func TestLegacyEventOmitsOperationField(t *testing.T) {
	e := validEvent(t)
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "operation_id") {
		t.Fatal("legacy serialization acquired an empty operation field")
	}
	if _, err = audit.DecodeEvent(raw); err != nil {
		t.Fatal("legacy event no longer decodes")
	}
}
