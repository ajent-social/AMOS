// Command portableplan exports the AMOS authored plan as a read-only portable
// definition. Narrative task status is retained but never becomes gate evidence.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

const adapterVersion = "amos-portable-plan/0.0.1"
const sourceRef = "docs/planning/plan-data.json"
const stateRef = "docs/planning/execution-state.json"

type nativePlan struct {
	SchemaVersion int    `json:"schema_version"`
	Contract      string `json:"contract"`
	Epics         []struct {
		Tasks []nativeTask `json:"tasks"`
	} `json:"epics"`
}
type nativeTask struct {
	ID         string                     `json:"id"`
	Title      string                     `json:"title"`
	Stage      string                     `json:"stage"`
	Deps       []string                   `json:"deps"`
	Acceptance []string                   `json:"acceptance"`
	Other      map[string]json.RawMessage `json:"-"`
}

func (t *nativeTask) UnmarshalJSON(b []byte) error {
	type fields nativeTask
	if err := json.Unmarshal(b, (*fields)(t)); err != nil {
		return err
	}
	return json.Unmarshal(b, &t.Other)
}

type narrative struct {
	Status        string `json:"status"`
	Certification string `json:"certification"`
}

func digest(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }

func export(planBytes, stateBytes []byte) (any, error) {
	var plan nativePlan
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	if plan.SchemaVersion != 1 || plan.Contract == "" {
		return nil, errors.New("unsupported native plan version or missing contract")
	}
	var state map[string]narrative
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		return nil, fmt.Errorf("narrative state: %w", err)
	}
	if state == nil {
		state = map[string]narrative{}
	}
	planDigest := digest(planBytes)
	revision := planDigest
	known := map[string]bool{}
	graph := map[string][]string{}
	for _, epic := range plan.Epics {
		for _, t := range epic.Tasks {
			if t.ID == "" || known[t.ID] {
				return nil, fmt.Errorf("empty or duplicate task ID %q", t.ID)
			}
			known[t.ID] = true
			graph[t.ID] = t.Deps
		}
	}
	visit := map[string]uint8{}
	var checkGraph func(string) error
	checkGraph = func(id string) error {
		if visit[id] == 1 {
			return fmt.Errorf("native dependency cycle at %s", id)
		}
		if visit[id] == 2 {
			return nil
		}
		visit[id] = 1
		seen := map[string]bool{}
		for _, dep := range graph[id] {
			if !known[dep] {
				return fmt.Errorf("task %s has unknown dependency %s", id, dep)
			}
			if seen[dep] {
				return fmt.Errorf("task %s repeats dependency %s", id, dep)
			}
			seen[dep] = true
			if err := checkGraph(dep); err != nil {
				return err
			}
		}
		visit[id] = 2
		return nil
	}
	for _, epic := range plan.Epics {
		for _, t := range epic.Tasks {
			if err := checkGraph(t.ID); err != nil {
				return nil, err
			}
		}
	}
	for id := range state {
		if !known[id] {
			return nil, fmt.Errorf("narrative state references unknown task %q", id)
		}
	}
	tasks := make([]any, 0, len(known))
	requirements := make([]any, 0, len(known))
	for _, epic := range plan.Epics {
		for _, t := range epic.Tasks {
			if t.Title == "" || t.Stage == "" || len(t.Acceptance) == 0 {
				return nil, fmt.Errorf("task %s lacks title, stage, or acceptance", t.ID)
			}
			id := "amos:task:" + t.ID
			deps := make([]any, 0, len(t.Deps))
			for _, dep := range t.Deps {
				deps = append(deps, map[string]any{"taskId": "amos:task:" + dep, "predicate": "domain-accepted", "requirementId": "amos:acceptance:" + dep})
			}
			meta := map[string]any{"nativeStage": t.Stage, "nativeContract": plan.Contract, "nativeTask": t.Other}
			if n, ok := state[t.ID]; ok {
				meta["narrativeStatus"] = n.Status
				meta["narrativeCertification"] = n.Certification
				meta["narrativeQualification"] = "unverified"
			}
			tasks = append(tasks, map[string]any{"id": id, "title": t.Title, "stage": t.Stage, "authoredStatus": "pending", "acceptance": strings.Join(t.Acceptance, "\n"), "source": map[string]any{"ref": sourceRef, "canonicalId": t.ID}, "dependencies": deps, "metadata": meta})
			requirements = append(requirements, map[string]any{"id": "amos:acceptance:" + t.ID, "taskId": id, "predicate": "domain-accepted", "policyRevision": "amos-native/1", "subject": map[string]any{}, "domain": "amos:task-acceptance"})
		}
	}
	// Sorting is defensive: source order is stable, but callers should not rely on it.
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].(map[string]any)["id"].(string) < tasks[j].(map[string]any)["id"].(string)
	})
	sort.Slice(requirements, func(i, j int) bool {
		return requirements[i].(map[string]any)["id"].(string) < requirements[j].(map[string]any)["id"].(string)
	})
	definition := map[string]any{
		"id": "amos:plan", "revision": revision, "digest": planDigest, "title": "AMOS authored delivery plan",
		"source": map[string]any{"authority": "native", "authorityId": "amos:repository", "ref": sourceRef, "revision": revision, "digest": planDigest, "adapterVersion": adapterVersion},
		"tasks":  tasks, "requirements": requirements, "executionUnits": []any{},
		"metadata": map[string]any{"nativeSchemaVersion": plan.SchemaVersion, "narrativeStateRef": stateRef, "narrativeStateDigest": digest(stateBytes), "narrativeStatusIsQualifiedEvidence": false},
	}
	return map[string]any{"contractVersion": "0.0.1", "definition": definition, "evidence": []any{}, "evaluations": []any{}}, nil
}

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		os.Exit(2)
	}
	plan, err := os.ReadFile(sourceRef)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	state, err := os.ReadFile(stateRef)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := export(plan, state)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
