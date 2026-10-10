// Package buildtrustcontract validates the public, design-only release evidence
// shape and a caller-supplied trusted-builder policy. It does not verify
// signatures, attestations, workflows, or release artifacts at runtime.
package buildtrustcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const SchemaPath = "api/schemas/release-evidence.schema.json"

// Policy contains the consumer's independently pinned identity expectations.
// It must come from reviewed configuration, never from the evidence document.
type Policy struct {
	Repository       string
	AllowedRefs      []string
	WorkflowIdentity string
	BuilderID        string
	Issuer           string
	Audience         string
	VerifierVersion  string
}

type evidence struct {
	SchemaVersion int `json:"schema_version"`
	Artifact      struct {
		Digest string `json:"digest"`
	} `json:"artifact"`
	Source struct {
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
		Ref        string `json:"ref"`
	} `json:"source"`
	Builder struct {
		ID               string `json:"id"`
		WorkflowIdentity string `json:"workflow_identity"`
		WorkflowRevision string `json:"workflow_revision"`
	} `json:"builder"`
	Attestation struct {
		SignerIdentity string `json:"signer_identity"`
		Issuer         string `json:"issuer"`
		Audience       string `json:"audience"`
	} `json:"attestation"`
	Verification struct {
		Tool    string `json:"tool"`
		Version string `json:"version"`
	} `json:"verification"`
}

// Validate rejects malformed evidence and evidence that does not match the
// caller's trusted repository, ref, workflow, builder, issuer, audience, and
// verifier policy. Matching strings is not cryptographic verification.
func Validate(data []byte, repositoryRoot string, policy Policy) error {
	if strings.TrimSpace(repositoryRoot) == "" {
		return fmt.Errorf("repository root is required")
	}
	if policy.Repository == "" || len(policy.AllowedRefs) == 0 || policy.WorkflowIdentity == "" || policy.BuilderID == "" || policy.Issuer == "" || policy.Audience == "" || policy.VerifierVersion == "" {
		return fmt.Errorf("trusted-builder policy is incomplete")
	}
	schemaBytes, err := os.ReadFile(filepath.Join(repositoryRoot, SchemaPath))
	if err != nil {
		return fmt.Errorf("read release evidence schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	if err := compiler.AddResource("release-evidence.schema.json", bytes.NewReader(schemaBytes)); err != nil {
		return fmt.Errorf("load release evidence schema: %w", err)
	}
	schema, err := compiler.Compile("release-evidence.schema.json")
	if err != nil {
		return fmt.Errorf("compile release evidence schema: %w", err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("decode release evidence: %w", err)
	}
	if err := schema.Validate(document); err != nil {
		return fmt.Errorf("release evidence schema: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var record evidence
	if err := decoder.Decode(&record); err != nil {
		return fmt.Errorf("decode release evidence: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("release evidence must contain one JSON value")
	}
	if record.Source.Repository != policy.Repository {
		return fmt.Errorf("unapproved source repository")
	}
	if !contains(policy.AllowedRefs, record.Source.Ref) {
		return fmt.Errorf("unapproved source ref")
	}
	if record.Builder.WorkflowIdentity != policy.WorkflowIdentity {
		return fmt.Errorf("unapproved builder workflow identity")
	}
	if record.Builder.ID != policy.BuilderID {
		return fmt.Errorf("unapproved builder id")
	}
	if record.Attestation.SignerIdentity != policy.WorkflowIdentity {
		return fmt.Errorf("unapproved signing identity")
	}
	if record.Attestation.Issuer != policy.Issuer {
		return fmt.Errorf("unapproved signing issuer")
	}
	if record.Attestation.Audience != policy.Audience {
		return fmt.Errorf("unapproved signing audience")
	}
	if record.Verification.Tool != "gh-attestation" || record.Verification.Version != policy.VerifierVersion {
		return fmt.Errorf("unapproved verification tool or version")
	}
	return nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
