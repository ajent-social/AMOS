package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const samplePlan = `{"schema_version":1,"contract":"amos-contract-v1","epics":[{"tasks":[{"id":"T1","title":"One","stage":"S1","deps":[],"acceptance":["First line","Second line"]},{"id":"T2","title":"Two","stage":"S2","deps":["T1"],"acceptance":["Done"]}]}]}`

func TestNarrativeCannotQualifyGate(t *testing.T) {
	v, err := export([]byte(samplePlan), []byte(`{"T1":{"status":"ACCEPTED","certification":"REVIEWED"}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Definition struct {
			Tasks []struct {
				ID             string `json:"id"`
				AuthoredStatus string `json:"authoredStatus"`
				Acceptance     string `json:"acceptance"`
				Dependencies   []struct {
					Predicate     string `json:"predicate"`
					RequirementID string `json:"requirementId"`
				} `json:"dependencies"`
				Metadata map[string]any `json:"metadata"`
			} `json:"tasks"`
			Requirements []json.RawMessage `json:"requirements"`
		} `json:"definition"`
		Evidence    []json.RawMessage `json:"evidence"`
		Evaluations []json.RawMessage `json:"evaluations"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Evidence) != 0 || len(got.Evaluations) != 0 {
		t.Fatal("narrative created qualified evidence")
	}
	if len(got.Definition.Tasks) != 2 || len(got.Definition.Requirements) != 2 {
		t.Fatal("lost tasks or acceptance requirements")
	}
	var requirement struct {
		Subject struct {
			Artifact string `json:"artifact"`
		} `json:"subject"`
	}
	if err := json.Unmarshal(got.Definition.Requirements[0], &requirement); err != nil {
		t.Fatal(err)
	}
	if requirement.Subject.Artifact != "amos:task:T1" {
		t.Fatalf("wrong logical acceptance subject: %+v", requirement.Subject)
	}
	first, second := got.Definition.Tasks[0], got.Definition.Tasks[1]
	if first.AuthoredStatus != "pending" || first.Acceptance != "First line\nSecond line" || first.Metadata["narrativeStatus"] != "ACCEPTED" {
		t.Fatalf("lossy or promoted narrative: %+v", first)
	}
	if len(second.Dependencies) != 1 || second.Dependencies[0].Predicate != "domain-accepted" || second.Dependencies[0].RequirementID != "amos:acceptance:T1" {
		t.Fatalf("wrong dependency: %+v", second.Dependencies)
	}
}

func TestRejectDanglingNativeReferences(t *testing.T) {
	for _, tc := range []struct{ plan, state string }{
		{strings.Replace(samplePlan, `"deps":["T1"]`, `"deps":["missing"]`, 1), `{}`},
		{strings.Replace(samplePlan, `"deps":["T1"]`, `"deps":["T2"]`, 1), `{}`},
		{strings.Replace(samplePlan, `"deps":[]`, `"deps":["T2"]`, 1), `{}`},
		{strings.Replace(samplePlan, `"deps":["T1"]`, `"deps":["T1","T1"]`, 1), `{}`},
		{samplePlan, `{"missing":{"status":"ACCEPTED"}}`},
	} {
		if _, err := export([]byte(tc.plan), []byte(tc.state)); err == nil {
			t.Fatal("accepted dangling native reference")
		}
	}
}
