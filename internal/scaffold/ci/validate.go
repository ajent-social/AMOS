package ci

import (
	"bytes"
	"context"
	_ "embed"

	"github.com/ajent-social/amos/internal/initializer"
)

const (
	ValidationWorkflowPath     = ".github/workflows/amos-validate.yml"
	ValidationGoVersionPath    = ".github/amos-ci/go.mod"
	ValidationGoVersion        = "1.27.1"
	ValidationLintVersion      = "v2.13.2"
	ValidationWorkflowRevision = "680caa87f92395cb034df6a9b2dc95b517134209"
)

// The embedded caller is fixed, not an evaluation of caller-supplied templates.
// Its upstream workflows remain CANDIDATE; this is no release/CI qualification.
//
//go:embed templates/validate.yml.tmpl
var validationWorkflow []byte

// RenderValidation returns an independent copy of the credential-free caller.
func RenderValidation() []byte { return bytes.Clone(validationWorkflow) }

// GenerateValidation journals the workflow and its exact toolchain selection.
// It uses the initializer's managed-preimage checks, including during resume.
func GenerateValidation(ctx context.Context, config initializer.Config, files *initializer.Files) error {
	if ctx == nil || files == nil || config.AppSlug() == "" {
		return initializer.ErrInvalidInput
	}
	if err := files.WriteFile(ctx, ValidationWorkflowPath, RenderValidation(), 0644); err != nil {
		return err
	}
	return files.WriteFile(ctx, ValidationGoVersionPath, []byte("module amos-validation-toolchain\n\ngo "+ValidationGoVersion+"\n\ntoolchain go"+ValidationGoVersion+"\n"), 0644)
}
