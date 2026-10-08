package main

import (
	"encoding/json"
	"testing"
)

func TestSDLCExportPreservesSourceAndDoesNotQualifyNarrative(t *testing.T) {
	source := []byte(`{"schema":"amos-local-sdlc-plan-v1","contract":"ordinary","tasks":[{"id":"X","title":"Review","stage":"review","acceptance":"First line\n  second line","status":"COMPLETE","owner":"Reviewer","deps":[]},{"id":"Y","title":"Land","stage":"verify-landed","acceptance":"Verify landing","deps":["X"]}]}`)
	state := []byte(`{"tasks":{"X":{"status":"COMPLETE"}}}`)
	result, err := exportSDLC(source, state)
	if err != nil {
		t.Fatal(err)
	}
	out := result.(map[string]any)
	def := out["definition"].(map[string]any)
	if def["digest"] != digest(source) || def["id"] != "amos:plan" {
		t.Fatal("source identity changed")
	}
	if _, exists := out["snapshot"]; exists {
		t.Fatal("fabricated execution snapshot")
	}
	if len(out["evidence"].([]any)) != 0 || len(out["evaluations"].([]any)) != 0 {
		t.Fatal("fabricated qualified evidence")
	}
	tasks := def["tasks"].([]any)
	first := tasks[0].(map[string]any)
	if first["acceptance"] != "First line\n  second line" || first["authoredStatus"] != "pending" {
		t.Fatal("acceptance/status mapping changed")
	}
	if first["source"].(map[string]any)["ref"] != sdlcSourceRef {
		t.Fatal("wrong source reference")
	}
	dep := tasks[1].(map[string]any)["dependencies"].([]any)[0].(map[string]any)
	if dep["predicate"] != "execution-complete" || dep["taskId"] != "amos:task:X" {
		t.Fatal("native dependency changed")
	}
}
func TestSDLCRejectsInvalidGraphAndJournal(t *testing.T) {
	for _, tc := range []struct{ name, source, state string }{
		{"unknown dependency", `{"schema":"amos-local-sdlc-plan-v1","contract":"x","tasks":[{"id":"X","title":"x","stage":"review","acceptance":"a","deps":["MISSING"]}]}`, `{"tasks":{}}`},
		{"cycle", `{"schema":"amos-local-sdlc-plan-v1","contract":"x","tasks":[{"id":"X","title":"x","stage":"review","acceptance":"a","deps":["X"]}]}`, `{"tasks":{}}`},
		{"missing acceptance", `{"schema":"amos-local-sdlc-plan-v1","contract":"x","tasks":[{"id":"X","title":"x","stage":"review","deps":[]}]}`, `{"tasks":{}}`},
		{"foreign journal", `{"schema":"amos-local-sdlc-plan-v1","contract":"x","tasks":[{"id":"X","title":"x","stage":"review","acceptance":"a","deps":[]}]}`, `{"tasks":{"Y":{"status":"COMPLETE"}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := exportSDLC([]byte(tc.source), []byte(tc.state)); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
}
func TestSDLCDeterministic(t *testing.T) {
	source := []byte(`{"schema":"amos-local-sdlc-plan-v1","contract":"x","tasks":[{"id":"X","title":"x","stage":"author","acceptance":"a","deps":[]}]}`)
	a, e := exportSDLC(source, []byte(`{"tasks":{}}`))
	if e != nil {
		t.Fatal(e)
	}
	b, e := exportSDLC(source, []byte(`{"tasks":{}}`))
	if e != nil {
		t.Fatal(e)
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if string(ab) != string(bb) {
		t.Fatal("nondeterministic")
	}
}
