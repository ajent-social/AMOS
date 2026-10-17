package ci

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/initializer"

	yaml "go.yaml.in/yaml/v4"
)

type validationStep struct {
	Shell string            `yaml:"shell"`
	Env   map[string]string `yaml:"env"`
	Run   string            `yaml:"run"`
}
type validationJob struct {
	Name    string           `yaml:"name"`
	If      string           `yaml:"if"`
	Uses    string           `yaml:"uses"`
	With    map[string]any   `yaml:"with"`
	Needs   []string         `yaml:"needs"`
	Timeout int              `yaml:"timeout-minutes"`
	Steps   []validationStep `yaml:"steps"`
}
type validationDocument struct {
	On          map[string]any    `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Concurrency struct {
		Group  string `yaml:"group"`
		Cancel bool   `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	Jobs map[string]validationJob `yaml:"jobs"`
}

func decodeValidation(t *testing.T) validationDocument {
	t.Helper()
	var doc validationDocument
	if err := yaml.Unmarshal(RenderValidation(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestValidationWorkflowTrustAndPins(t *testing.T) {
	doc := decodeValidation(t)
	if len(doc.On) != 2 {
		t.Fatal("unexpected trigger")
	}
	for _, event := range []string{"pull_request", "merge_group"} {
		if _, ok := doc.On[event]; !ok {
			t.Fatalf("missing %s trigger", event)
		}
	}
	if len(doc.Permissions) != 1 || doc.Permissions["contents"] != "read" {
		t.Fatal("credential-free token boundary changed")
	}
	if !doc.Concurrency.Cancel || !strings.Contains(doc.Concurrency.Group, "github.event_name") || !strings.Contains(doc.Concurrency.Group, "github.event.pull_request.number || github.ref") {
		t.Fatal("unrelated runs can share concurrency group")
	}
	if len(doc.Jobs) != 4 {
		t.Fatal("unexpected job")
	}
	for _, name := range []string{"go-validation", "go-lint"} {
		job := doc.Jobs[name]
		if job.Uses != "ajent-social/workflows/.github/workflows/"+name+".yml@"+ValidationWorkflowRevision {
			t.Fatal("unqualified or mutable workflow pin")
		}
		if job.With["go-version-file"] != ValidationGoVersionPath || job.With["working-directory"] != "." || job.With["timeout-minutes"] != 20 {
			t.Fatal("unbounded or caller-selected execution input")
		}
	}
	if doc.Jobs["go-validation"].With["enable-postgres"] != false || doc.Jobs["go-validation"].With["race"] != true || doc.Jobs["go-validation"].With["packages"] != "./..." {
		t.Fatal("validation coverage changed")
	}
	if doc.Jobs["go-lint"].With["golangci-lint-version"] != ValidationLintVersion || doc.Jobs["go-lint"].With["args"] != "--timeout=15m" {
		t.Fatal("lint version or owner configuration policy changed")
	}
	raw := RenderValidation()
	for _, forbidden := range []string{"secrets:", "secrets.", "write", "pull_request_target:", "workflow_dispatch:", "push:", "environment:", "continue-on-error:", "--no-config"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("unsafe caller field %q", forbidden)
		}
	}
	copyBytes := RenderValidation()
	copyBytes[0] = '!'
	if bytes.Equal(copyBytes, RenderValidation()) {
		t.Fatal("renderer exposes mutable embedded bytes")
	}
	if err := GenerateValidation(context.Background(), initializer.Config{}, nil); err == nil {
		t.Fatal("invalid emission accepted")
	}
}

func TestValidationWorkflowAggregateResults(t *testing.T) {
	doc := decodeValidation(t)
	for _, id := range []string{"aggregate", "merge-queue"} {
		job := doc.Jobs[id]
		wantName, wantIf := "AMOS / Validate", "${{ always() }}"
		if id == "merge-queue" {
			wantName, wantIf = "AMOS / Merge Queue", "${{ always() && github.event_name == 'merge_group' }}"
		}
		if job.Name != wantName || job.If != wantIf || job.Timeout != 1 || strings.Join(job.Needs, ",") != "go-validation,go-lint" || len(job.Steps) != 1 {
			t.Fatal("aggregate can be skipped or lose child dependency")
		}
		step := job.Steps[0]
		if step.Shell != "bash" || step.Env["VALIDATION_RESULT"] != "${{ needs.go-validation.result }}" || step.Env["LINT_RESULT"] != "${{ needs.go-lint.result }}" {
			t.Fatal("aggregate does not inspect actual child results")
		}
		for _, validation := range []string{"success", "failure", "cancelled", "skipped", "", "unknown"} {
			for _, lint := range []string{"success", "failure", "cancelled", "skipped", "", "unknown"} {
				t.Run(id+"/"+validation+"/"+lint, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, "bash", "-c", step.Run)
					cmd.Env = append(os.Environ(), "VALIDATION_RESULT="+validation, "LINT_RESULT="+lint)
					out, err := cmd.CombinedOutput()
					if ctx.Err() != nil {
						t.Fatal(ctx.Err())
					}
					if (err == nil) != (validation == "success" && lint == "success") {
						t.Fatalf("aggregate result=%v: %s", err, out)
					}
				})
			}
		}
	}
}
