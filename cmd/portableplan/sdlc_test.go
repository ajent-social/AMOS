package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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

func TestDeliveryToProductRetainsDomainAcceptance(t *testing.T) {
	source := []byte(`{"schema":"amos-local-sdlc-plan-v1","contract":"x","tasks":[{"id":"T1.2","native_product_task":{"id":"T1.2","title":"Contract","stage":"S0","deps":[],"acceptance":["Reviewed contract"]}},{"id":"T-SDLC-2-8.1","title":"Preflight","stage":"preflight","acceptance":"Current reviewed dependencies","deps":["T1.2"]},{"id":"NEXT","title":"Review","stage":"review","acceptance":"Review","deps":["T-SDLC-2-8.1"]}]}`)
	out, err := exportSDLC(source, []byte(`{"tasks":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	def := out.(map[string]any)["definition"].(map[string]any)
	for _, row := range def["tasks"].([]any) {
		task := row.(map[string]any)
		if task["id"] == "amos:task:T-SDLC-2-8.1" {
			dep := task["dependencies"].([]any)[0].(map[string]any)
			if dep["predicate"] != "domain-accepted" || dep["requirementId"] != "amos:acceptance:T1.2" {
				t.Fatal("delivery prerequisite weakened", dep)
			}
		}
		if task["id"] == "amos:task:NEXT" {
			dep := task["dependencies"].([]any)[0].(map[string]any)
			if dep["predicate"] != "execution-complete" {
				t.Fatal("lifecycle approval invented", dep)
			}
		}
	}
}
func TestRejectMissingRetainedTaskAndDuplicateProductFields(t *testing.T) {
	for _, raw := range []string{
		`{"schema":"amos-local-sdlc-plan-v1","contract":"amos-wazi-authored-plan/1","required_task_ids":["X","MISSING"],"tasks":[{"id":"X","title":"x","stage":"author","deps":[],"acceptance":"a"}]}`,
		`{"schema":"amos-local-sdlc-plan-v1","contract":"x","tasks":[{"id":"X","title":"conflicting","native_product_task":{"id":"X","title":"canonical","stage":"S0","deps":[],"acceptance":["a"]}}]}`,
	} {
		if _, err := exportSDLC([]byte(raw), []byte(`{"tasks":{}}`)); err == nil {
			t.Fatal("invalid preservation accepted")
		}
	}
}

func TestCLIHelper(t *testing.T) {
	if os.Getenv("AMOS_PLAN_CLI_TEST") != "1" {
		return
	}
	args := []string{"portableplan"}
	if mode := os.Getenv("AMOS_PLAN_CLI_MODE"); mode != "" {
		args = append(args, mode)
	}
	os.Args = args
	main()
	os.Exit(0)
}
func TestCurrentAndHistoricalCLIIdentities(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "planning")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"wazi-source.json":      `{"schema":"amos-local-sdlc-plan-v1","contract":"x","tasks":[{"id":"A","title":"Current","stage":"author","deps":[],"acceptance":"a"},{"id":"B","title":"Review","stage":"review","deps":["A"],"acceptance":"b"}]}`,
		"sdlc-stage-state.json": `{"tasks":{}}`,
		"plan-data.json":        `{"schema_version":1,"contract":"legacy","epics":[{"tasks":[{"id":"OLD","title":"Old","stage":"S0","deps":[],"acceptance":["old"]}]}]}`,
		"execution-state.json":  `{}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		mode, id string
		count    int
	}{
		{"", "amos:plan", 2}, {"--sdlc", "amos:plan", 2}, {"--historical-product", "amos:historical-product-plan", 1},
	} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCLIHelper$")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "AMOS_PLAN_CLI_TEST=1", "AMOS_PLAN_CLI_MODE="+tc.mode)
		b, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Definition struct {
				ID    string
				Tasks []json.RawMessage
			}
		}
		if err = json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		if out.Definition.ID != tc.id || len(out.Definition.Tasks) != tc.count {
			t.Fatalf("mode %s: got %s/%d", tc.mode, out.Definition.ID, len(out.Definition.Tasks))
		}
	}
}
