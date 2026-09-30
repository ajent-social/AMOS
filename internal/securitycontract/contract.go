// Package securitycontract validates the public, design-only AMOS threat matrix.
// It does not implement or evaluate runtime authorization decisions.
package securitycontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	MatrixStatusNotImplemented = "NOT_IMPLEMENTED"
	TestStatusNotImplemented   = "NOT_IMPLEMENTED"
)

var (
	idPattern   = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+$`)
	taskPattern = regexp.MustCompile(`^T[0-9]+\.[0-9]+$`)
	testPattern = regexp.MustCompile(`^Test[A-Z][A-Za-z0-9_]*$`)

	// These boundaries are release-blocking invariants from the frozen security
	// contract. In particular, release identity compromise must not disappear
	// merely because its matrix row was removed.
	requiredThreatIDs  = []string{"THR-RELEASE-IDENTITY-COMPROMISE"}
	allowedSourceKinds = map[string]struct{}{
		"frozen_contract": {},
		"rfc":             {},
		"task_contract":   {},
	}
)

// Matrix is a public-safe inventory of security boundaries and planned controls.
type Matrix struct {
	SchemaVersion int         `json:"schema_version"`
	Status        string      `json:"status"`
	Zones         []TrustZone `json:"trust_zones"`
	Principals    []Principal `json:"principals"`
	Assets        []Asset     `json:"critical_assets"`
	Threats       []Threat    `json:"threats"`
	Controls      []Control   `json:"controls"`
}

type TrustZone struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Principal struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	TrustZoneID   string `json:"trust_zone_id"`
	Authority     string `json:"authority"`
	ExplicitlyNot string `json:"explicitly_not"`
}

type Asset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	OwnerZoneID string `json:"owner_zone_id"`
	Impact      string `json:"impact"`
}

type Crossing struct {
	FromZoneID string `json:"from_zone_id"`
	ToZoneID   string `json:"to_zone_id"`
}

type Threat struct {
	ID                string      `json:"id"`
	Severity          string      `json:"severity"`
	ActorID           string      `json:"actor_id"`
	Crossing          Crossing    `json:"crossing"`
	AssetIDs          []string    `json:"asset_ids"`
	Attack            string      `json:"attack"`
	ExpectedDecision  string      `json:"expected_decision"`
	ControlIDs        []string    `json:"control_ids"`
	DeniedPathControl string      `json:"denied_path_control_id"`
	SourceRefs        []SourceRef `json:"source_refs"`
}

type Control struct {
	ID                   string      `json:"id"`
	Requirement          string      `json:"requirement"`
	ImplementationStatus string      `json:"implementation_status"`
	OwnerTask            string      `json:"owner_task"`
	Stage                string      `json:"stage"`
	DeniedPathTest       PlannedTest `json:"denied_path_test"`
	SourceRefs           []SourceRef `json:"source_refs"`
	ResidualRisk         string      `json:"residual_risk"`
}

type PlannedTest struct {
	OwnerTask string `json:"owner_task"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Assertion string `json:"assertion"`
}

type SourceRef struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// DecodeAndValidate strictly parses matrix bytes, rejects unknown JSON fields,
// then validates completeness and the repository-relative evidence links.
func DecodeAndValidate(data []byte, repositoryRoot string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var matrix Matrix
	if err := decoder.Decode(&matrix); err != nil {
		return fmt.Errorf("decode security matrix: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode security matrix: more than one JSON value")
		}
		return fmt.Errorf("decode security matrix trailer: %w", err)
	}
	return ValidateMatrix(matrix, repositoryRoot)
}

// ValidateMatrix checks matrix structure and ensures controls cite frozen
// contracts/task sources rather than relying on unreviewed prompt text.
func ValidateMatrix(matrix Matrix, repositoryRoot string) error {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if repositoryRoot == "" {
		return fmt.Errorf("security matrix validation requires a repository root")
	}
	if matrix.SchemaVersion != 1 {
		add("schema_version must be 1")
	}
	if matrix.Status != MatrixStatusNotImplemented {
		add("matrix status must remain %s until controls are implemented", MatrixStatusNotImplemented)
	}

	zones := make(map[string]TrustZone, len(matrix.Zones))
	for _, zone := range matrix.Zones {
		if !validID(zone.ID) {
			add("trust zone has invalid id %q", zone.ID)
		}
		if _, exists := zones[zone.ID]; exists {
			add("duplicate trust zone id %q", zone.ID)
		}
		if strings.TrimSpace(zone.Name) == "" || strings.TrimSpace(zone.Description) == "" {
			add("trust zone %q is missing a name or description", zone.ID)
		}
		zones[zone.ID] = zone
	}

	principals := make(map[string]Principal, len(matrix.Principals))
	for _, principal := range matrix.Principals {
		if !validID(principal.ID) {
			add("principal has invalid id %q", principal.ID)
		}
		if _, exists := principals[principal.ID]; exists {
			add("duplicate principal id %q", principal.ID)
		}
		if _, exists := zones[principal.TrustZoneID]; !exists {
			add("principal %q references unknown trust zone %q", principal.ID, principal.TrustZoneID)
		}
		if strings.TrimSpace(principal.Name) == "" || strings.TrimSpace(principal.Authority) == "" || strings.TrimSpace(principal.ExplicitlyNot) == "" {
			add("principal %q must state its authority and explicit authority limit", principal.ID)
		}
		principals[principal.ID] = principal
	}

	assets := make(map[string]Asset, len(matrix.Assets))
	for _, asset := range matrix.Assets {
		if !validID(asset.ID) {
			add("asset has invalid id %q", asset.ID)
		}
		if _, exists := assets[asset.ID]; exists {
			add("duplicate asset id %q", asset.ID)
		}
		if _, exists := zones[asset.OwnerZoneID]; !exists {
			add("asset %q references unknown owner zone %q", asset.ID, asset.OwnerZoneID)
		}
		if strings.TrimSpace(asset.Name) == "" || strings.TrimSpace(asset.Impact) == "" {
			add("asset %q must state its name and security impact", asset.ID)
		}
		assets[asset.ID] = asset
	}

	controls := make(map[string]Control, len(matrix.Controls))
	for _, control := range matrix.Controls {
		if !validID(control.ID) {
			add("control has invalid id %q", control.ID)
		}
		if _, exists := controls[control.ID]; exists {
			add("duplicate control id %q", control.ID)
		}
		if strings.TrimSpace(control.Requirement) == "" || strings.TrimSpace(control.ResidualRisk) == "" {
			add("control %q must state its requirement and residual risk", control.ID)
		}
		if control.ImplementationStatus != MatrixStatusNotImplemented {
			add("control %q implementation_status must be %s", control.ID, MatrixStatusNotImplemented)
		}
		if !validTask(control.OwnerTask) {
			add("control %q has invalid or missing owner_task %q", control.ID, control.OwnerTask)
		} else if err := requireRepoFile(repositoryRoot, taskPath(control.OwnerTask)); err != nil {
			add("control %q owner task: %v", control.ID, err)
		}
		if !validStage(control.Stage) {
			add("control %q has invalid stage %q", control.ID, control.Stage)
		}
		if control.DeniedPathTest.OwnerTask != control.OwnerTask {
			add("control %q test owner %q does not match control owner %q", control.ID, control.DeniedPathTest.OwnerTask, control.OwnerTask)
		}
		if !validTask(control.DeniedPathTest.OwnerTask) {
			add("control %q has invalid denied-path test owner %q", control.ID, control.DeniedPathTest.OwnerTask)
		}
		if !validTestPath(control.DeniedPathTest.Path) || !testPattern.MatchString(control.DeniedPathTest.Name) {
			add("control %q must identify an exact planned test path and Test name", control.ID)
		}
		if control.DeniedPathTest.Status != TestStatusNotImplemented {
			add("control %q planned test status must be %s", control.ID, TestStatusNotImplemented)
		}
		if strings.TrimSpace(control.DeniedPathTest.Assertion) == "" {
			add("control %q planned test needs a falsifiable denied-path assertion", control.ID)
		}
		validateSources(control.ID, control.SourceRefs, repositoryRoot, add)
		if validTask(control.OwnerTask) && !hasTaskContractSource(control.SourceRefs, control.OwnerTask) {
			add("control %q does not cite its assigned owner task contract %q", control.ID, control.OwnerTask)
		}
		controls[control.ID] = control
	}

	threats := make(map[string]Threat, len(matrix.Threats))
	usedAssets := make(map[string]bool, len(assets))
	usedControls := make(map[string]bool, len(controls))
	for _, threat := range matrix.Threats {
		if !validID(threat.ID) {
			add("threat has invalid id %q", threat.ID)
		}
		if _, exists := threats[threat.ID]; exists {
			add("duplicate threat id %q", threat.ID)
		}
		if threat.Severity != "P0" && threat.Severity != "P1" && threat.Severity != "P2" {
			add("threat %q has invalid severity %q", threat.ID, threat.Severity)
		}
		actor, actorExists := principals[threat.ActorID]
		if !actorExists {
			add("threat %q references unknown actor %q", threat.ID, threat.ActorID)
		}
		fromZone, fromExists := zones[threat.Crossing.FromZoneID]
		_, toExists := zones[threat.Crossing.ToZoneID]
		if !fromExists || !toExists || threat.Crossing.FromZoneID == threat.Crossing.ToZoneID {
			add("threat %q must identify two distinct known trust zones", threat.ID)
		}
		if actorExists && fromExists && actor.TrustZoneID != fromZone.ID {
			add("threat %q actor %q is outside its stated source trust zone", threat.ID, threat.ActorID)
		}
		if len(threat.AssetIDs) == 0 {
			add("threat %q has no crossing asset", threat.ID)
		}
		for _, assetID := range threat.AssetIDs {
			if _, exists := assets[assetID]; !exists {
				add("threat %q references unknown asset %q", threat.ID, assetID)
			}
			usedAssets[assetID] = true
		}
		if strings.TrimSpace(threat.Attack) == "" || strings.TrimSpace(threat.ExpectedDecision) == "" {
			add("threat %q must describe an attack and expected policy decision", threat.ID)
		}
		if len(threat.ControlIDs) == 0 {
			add("threat %q has no control owner", threat.ID)
		}
		for _, controlID := range threat.ControlIDs {
			if _, exists := controls[controlID]; !exists {
				add("threat %q references unknown control %q", threat.ID, controlID)
			} else {
				usedControls[controlID] = true
			}
		}
		denialControl, denialControlExists := controls[threat.DeniedPathControl]
		if !denialControlExists || !contains(threat.ControlIDs, threat.DeniedPathControl) {
			add("threat %q has no owned denied-path test control", threat.ID)
		} else if denialControl.OwnerTask == "" || denialControl.DeniedPathTest.Name == "" {
			add("threat %q denied-path test control %q is unowned", threat.ID, threat.DeniedPathControl)
		} else if !hasTaskContractSource(threat.SourceRefs, denialControl.OwnerTask) {
			add("threat %q does not cite its denied-path test owner contract %q", threat.ID, denialControl.OwnerTask)
		}
		if threat.Severity == "P0" && (!actorExists || len(threat.AssetIDs) == 0 || len(threat.ControlIDs) == 0 || !denialControlExists) {
			add("P0 threat %q must have an actor, critical asset, control owner, and denied-path test owner", threat.ID)
		}
		validateSources(threat.ID, threat.SourceRefs, repositoryRoot, add)
		threats[threat.ID] = threat
	}

	for _, asset := range matrix.Assets {
		if !usedAssets[asset.ID] {
			add("critical asset %q has no mapped threat crossing", asset.ID)
		}
	}
	for _, control := range matrix.Controls {
		if !usedControls[control.ID] {
			add("control %q is not connected to a threat", control.ID)
		}
	}
	for _, requiredID := range requiredThreatIDs {
		if _, exists := threats[requiredID]; !exists {
			add("missing required release-identity boundary threat %q", requiredID)
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("security matrix invalid:\n - %s", strings.Join(problems, "\n - "))
}

func validateSources(id string, sources []SourceRef, repositoryRoot string, add func(string, ...any)) {
	if len(sources) == 0 {
		add("%q has no architectural/task evidence; prompt-only support is not accepted", id)
		return
	}
	hasArchitecture := false
	hasTask := false
	for _, source := range sources {
		if _, exists := allowedSourceKinds[source.Kind]; !exists {
			add("%q has unsupported evidence kind %q (prompt text is not evidence)", id, source.Kind)
			continue
		}
		if err := requireRepoFile(repositoryRoot, source.Path); err != nil {
			add("%q evidence: %v", id, err)
		}
		if source.Kind == "frozen_contract" || source.Kind == "rfc" {
			hasArchitecture = true
		}
		if source.Kind == "task_contract" {
			hasTask = true
		}
	}
	if !hasArchitecture || !hasTask {
		add("%q needs both frozen-architecture evidence and an implementation task contract", id)
	}
}

func requireRepoFile(repositoryRoot, path string) error {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, `\`) {
		return fmt.Errorf("evidence path must be a non-empty repository-relative path")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean != filepath.FromSlash(path) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("evidence path %q escapes or aliases repository root", path)
	}
	info, err := os.Lstat(filepath.Join(repositoryRoot, clean))
	if err != nil {
		return fmt.Errorf("evidence path %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("evidence path %q is not a regular file", path)
	}
	return nil
}

func validID(value string) bool { return idPattern.MatchString(value) }

func validTask(value string) bool { return taskPattern.MatchString(value) }

func validStage(value string) bool {
	return value == "S0" || value == "S1" || value == "S2" || value == "S3" || value == "S4" || value == "S5"
}

func validTestPath(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, `\`) || !strings.HasSuffix(value, "_test.go") {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	return clean == filepath.FromSlash(value) && clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func taskPath(taskID string) string { return "docs/tasks/" + taskID + ".md" }

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func hasTaskContractSource(sources []SourceRef, taskID string) bool {
	for _, source := range sources {
		if source.Kind == "task_contract" && source.Path == taskPath(taskID) {
			return true
		}
	}
	return false
}
