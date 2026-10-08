package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

const sdlcSourceRef = "docs/planning/wazi-source.json"
const sdlcStateRef = "docs/planning/sdlc-stage-state.json"

// exportSDLC preserves the independently authored lifecycle graph as a separate
// definition. It never flattens multiple files into a fictional source digest.
func exportSDLC(planBytes, stateBytes []byte) (any, error) {
	var source struct {
		Schema          string            `json:"schema"`
		Contract        string            `json:"contract"`
		RequiredTaskIDs []string          `json:"required_task_ids"`
		Tasks           []json.RawMessage `json:"tasks"`
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
			ID                string          `json:"id"`
			Title             string          `json:"title"`
			Stage             string          `json:"stage"`
			Deps              []string        `json:"deps"`
			Acceptance        string          `json:"acceptance"`
			Status            string          `json:"status"`
			NativeProductTask json.RawMessage `json:"native_product_task"`
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		if len(t.NativeProductTask) > 0 {
			var product nativeTask
			if err := json.Unmarshal(t.NativeProductTask, &product); err != nil {
				return nil, err
			}
			if product.ID != t.ID {
				return nil, fmt.Errorf("product identity mismatch %s", t.ID)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				return nil, err
			}
			for _, key := range []string{"title", "stage", "deps", "acceptance"} {
				if _, ok := fields[key]; ok {
					return nil, fmt.Errorf("duplicate authored product field %s.%s", t.ID, key)
				}
			}
			t.Title, t.Stage, t.Deps, t.Acceptance = product.Title, product.Stage, product.Deps, strings.Join(product.Acceptance, "\n")
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
	if source.Contract == "amos-wazi-authored-plan/1" && len(source.RequiredTaskIDs) == 0 {
		return nil, fmt.Errorf("missing retained task inventory")
	}
	retained := map[string]bool{}
	for _, id := range source.RequiredTaskIDs {
		if retained[id] {
			return nil, fmt.Errorf("duplicate retained ID %s", id)
		}
		retained[id] = true
		if _, ok := state[id]; !ok {
			return nil, fmt.Errorf("missing retained task %s", id)
		}
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
		_ = native
		for _, dep := range t["dependencies"].([]any) {
			d := dep.(map[string]any)
			target := strings.TrimPrefix(d["taskId"].(string), "amos:task:")
			isProduct := false
			for _, task := range plan.Epics[0].Tasks {
				if task.ID == target {
					_, isProduct = task.Other["native_product_task"]
					break
				}
			}
			if isProduct {
				continue
			}
			d["predicate"] = "execution-complete"
			delete(d, "requirementId")
		}
	}
	return out, nil
}
