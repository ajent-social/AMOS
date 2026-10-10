package buildtrustcontract

import (
	"encoding/json"
	"strings"
	"testing"
)

const repository = "https://github.com/ajent-social/AMOS"
const workflow = "https://github.com/ajent-social/AMOS/.github/workflows/release.yml@refs/tags/v1.0.0"
const issuer = "https://token.actions.githubusercontent.com"

type fixture struct {
	SchemaVersion int `json:"schema_version"`
	Artifact      struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
	} `json:"artifact"`
	Source struct {
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
		Ref        string `json:"ref"`
	} `json:"source"`
	Builder struct {
		ID                string `json:"id"`
		WorkflowIdentity  string `json:"workflow_identity"`
		WorkflowRevision  string `json:"workflow_revision"`
		RunID             string `json:"run_id"`
		RunnerEnvironment string `json:"runner_environment"`
	} `json:"builder"`
	Toolchain struct {
		GoVersion           string              `json:"go_version"`
		AttestActionVersion string              `json:"attest_action_version"`
		Inputs              []map[string]string `json:"inputs"`
	} `json:"toolchain"`
	Policy struct {
		ID             string   `json:"id"`
		Digest         string   `json:"digest"`
		RequiredChecks []string `json:"required_checks"`
	} `json:"policy"`
	Attestation struct {
		Format                   string `json:"format"`
		PredicateType            string `json:"predicate_type"`
		PredicateDigest          string `json:"predicate_digest"`
		BundleVersion            string `json:"bundle_version"`
		SignerIdentity           string `json:"signer_identity"`
		Issuer                   string `json:"issuer"`
		Audience                 string `json:"audience"`
		TransparencyLogInclusion bool   `json:"transparency_log_inclusion"`
	} `json:"attestation"`
	Verification struct {
		Tool              string   `json:"tool"`
		Version           string   `json:"version"`
		TrustedRoot       string   `json:"trusted_root"`
		TrustedRootDigest string   `json:"trusted_root_digest"`
		Mode              string   `json:"mode"`
		Checks            []string `json:"checks"`
	} `json:"verification"`
}

func TestReleaseEvidenceContract(t *testing.T) {
	policy := Policy{Repository: repository, AllowedRefs: []string{"refs/tags/v1.0.0"}, WorkflowIdentity: workflow, BuilderID: "https://github.com/actions/runner/github-hosted", Issuer: issuer, Audience: "sigstore", VerifierVersion: "v2.102.0"}
	record := validFixture(t)
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(encoded, "../..", policy); err != nil {
		t.Fatalf("valid trusted evidence rejected: %v", err)
	}

	t.Run("missing trusted builder binding", func(t *testing.T) {
		bad := record
		bad.Builder.WorkflowIdentity = ""
		assertRejected(t, bad, "../..", policy)
	})
	t.Run("missing source commit binding", func(t *testing.T) {
		bad := record
		bad.Source.Commit = ""
		assertRejected(t, bad, "../..", policy)
	})
	t.Run("missing artifact digest binding", func(t *testing.T) {
		bad := record
		bad.Artifact.Digest = ""
		assertRejected(t, bad, "../..", policy)
	})
	t.Run("signed artifact from unapproved builder ref", func(t *testing.T) {
		bad := record
		bad.Source.Ref = "refs/heads/untrusted"
		data, err := json.Marshal(bad)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate(data, "../..", policy); err == nil || !strings.Contains(err.Error(), "unapproved source ref") {
			t.Fatalf("unapproved signed builder/ref accepted or wrong failure: %v", err)
		}
	})
	t.Run("signed artifact from unapproved builder", func(t *testing.T) {
		bad := record
		bad.Builder.ID = "https://example.invalid/untrusted-builder"
		data, err := json.Marshal(bad)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate(data, "../..", policy); err == nil || !strings.Contains(err.Error(), "unapproved builder id") {
			t.Fatalf("unapproved builder accepted or wrong failure: %v", err)
		}
	})
	t.Run("signed artifact from unapproved workflow", func(t *testing.T) {
		bad := record
		bad.Builder.WorkflowIdentity = "ajent-social/AMOS/.github/workflows/untrusted.yml"
		data, err := json.Marshal(bad)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate(data, "../..", policy); err == nil || !strings.Contains(err.Error(), "unapproved builder workflow identity") {
			t.Fatalf("unapproved workflow accepted or wrong failure: %v", err)
		}
	})
	t.Run("wrong signing audience", func(t *testing.T) {
		bad := record
		bad.Attestation.Audience = "https://example.invalid/replay"
		data, err := json.Marshal(bad)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate(data, "../..", policy); err == nil || !strings.Contains(err.Error(), "unapproved signing audience") {
			t.Fatalf("replayed audience accepted or wrong failure: %v", err)
		}
	})
}

func assertRejected(t *testing.T, value fixture, root string, policy Policy) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(data, root, policy); err == nil {
		t.Fatal("invalid release evidence was accepted")
	}
}

func validFixture(t *testing.T) fixture {
	t.Helper()
	var f fixture
	f.SchemaVersion = 1
	f.Artifact.Name, f.Artifact.Digest = "amos-linux-amd64", strings.Repeat("a", 64)
	f.Source.Repository, f.Source.Commit, f.Source.Ref = repository, strings.Repeat("b", 40), "refs/tags/v1.0.0"
	f.Builder.ID = "https://github.com/actions/runner/github-hosted"
	f.Builder.WorkflowIdentity, f.Builder.WorkflowRevision = workflow, strings.Repeat("c", 40)
	f.Builder.RunID, f.Builder.RunnerEnvironment = "123456789", "github-hosted"
	f.Toolchain.GoVersion, f.Toolchain.AttestActionVersion = "go1.27.0", "v4.2.2"
	f.Toolchain.Inputs = []map[string]string{{"name": "go", "version": "go1.27.0", "digest": strings.Repeat("d", 64)}}
	f.Policy.ID, f.Policy.Digest = "amos-release-v1", strings.Repeat("e", 64)
	f.Policy.RequiredChecks = []string{"test", "vet", "lint"}
	f.Attestation.Format, f.Attestation.PredicateType = "in-toto-statement-v1", "https://slsa.dev/provenance/v1"
	f.Attestation.PredicateDigest, f.Attestation.BundleVersion = strings.Repeat("f", 64), "0.3"
	f.Attestation.SignerIdentity, f.Attestation.Issuer, f.Attestation.Audience = workflow, issuer, "sigstore"
	f.Attestation.TransparencyLogInclusion = true
	f.Verification.Tool, f.Verification.Version = "gh-attestation", "v2.102.0"
	f.Verification.TrustedRoot, f.Verification.TrustedRootDigest = "sigstore-public-good", strings.Repeat("1", 64)
	f.Verification.Mode = "online"
	f.Verification.Checks = []string{"artifact_digest", "attestation_signature", "certificate_identity", "certificate_issuer", "provenance_predicate", "source_binding", "builder_policy", "workflow_revision", "policy_digest", "transparency_log_inclusion", "toolchain_inputs"}
	return f
}
