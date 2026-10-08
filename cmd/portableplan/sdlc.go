package main

import (
	"encoding/json"
	"fmt"
)

const sdlcSourceRef = "docs/planning/wazi-source.json"
const sdlcStateRef = "docs/planning/sdlc-stage-state.json"

// exportSDLC preserves the independently authored lifecycle graph as a separate
// definition. It never flattens multiple files into a fictional source digest.
func exportSDLC(planBytes, stateBytes []byte) (any, error) {
	var source struct {
		Schema   string            `json:"schema"`
		Contract string            `json:"contract"`
		Tasks    []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(planBytes, &source); err != nil {
		return nil, err
	}
	if source.Schema != "amos-local-sdlc-plan-v1" || source.Contract == "" || len(source.Tasks) == 0 {
		return nil, fmt.Errorf("unsupported or empty SDLC source")
	}
	var journal struct {
		Tasks map[string]json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(stateBytes, &journal); err != nil {
		return nil, err
	}
	plan := nativePlan{SchemaVersion: 1, Contract: source.Contract}
	plan.Epics = append(plan.Epics, struct {
		Tasks []nativeTask `json:"tasks"`
	}{})
	state := make(map[string]narrative)
	for _, raw := range source.Tasks {
		var t struct {
			ID         string   `json:"id"`
			Title      string   `json:"title"`
			Stage      string   `json:"stage"`
			Deps       []string `json:"deps"`
			Acceptance string   `json:"acceptance"`
			Status     string   `json:"status"`
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		if t.Acceptance == "" {
			return nil, fmt.Errorf("task %s lacks acceptance", t.ID)
		}
		var original map[string]json.RawMessage
		if err := json.Unmarshal(raw, &original); err != nil {
			return nil, err
		}
		plan.Epics[0].Tasks = append(plan.Epics[0].Tasks, nativeTask{ID: t.ID, Title: t.Title, Stage: t.Stage, Deps: t.Deps, Acceptance: []string{t.Acceptance}, Other: original})
		state[t.ID] = narrative{Status: t.Status}
	}
	for id, raw := range journal.Tasks {
		if _, ok := state[id]; !ok {
			return nil, fmt.Errorf("journal references unknown task %s", id)
		}
		var n narrative
		if err := json.Unmarshal(raw, &n); err != nil {
			return nil, err
		}
		state[id] = n
	}
	out, err := exportNative(plan, state, planBytes, stateBytes, sdlcSourceRef, sdlcStateRef, "amos:plan", "amos-portable-sdlc/0.0.1")
	if err != nil {
		return nil, err
	}
	// Native lifecycle dependencies state ordering, not qualified domain acceptance.
	// Preserve them as execution-complete; never infer approval from stage labels.
	def := out.(map[string]any)["definition"].(map[string]any)
	def["title"] = "AMOS complete SDLC and production delivery plan"
	requirements := []any{}
	for _, req := range def["requirements"].([]any) {
		r := req.(map[string]any)
		id := r["taskId"].(string)
		for _, row := range def["tasks"].([]any) {
			t := row.(map[string]any)
			if t["id"] == id {
				native := t["metadata"].(map[string]any)["nativeTask"].(map[string]json.RawMessage)
				if _, ok := native["native_product_task"]; ok {
					requirements = append(requirements, r)
				}
			}
		}
	}
	def["requirements"] = requirements
	for _, row := range def["tasks"].([]any) {
		t := row.(map[string]any)
		native := t["metadata"].(map[string]any)["nativeTask"].(map[string]json.RawMessage)
		if _, ok := native["native_product_task"]; ok {
			continue
		}
		for _, dep := range t["dependencies"].([]any) {
			d := dep.(map[string]any)
			d["predicate"] = "execution-complete"
			delete(d, "requirementId")
		}
	}
	return out, nil
}
