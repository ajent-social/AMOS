package application

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ajent-social/amos/internal/initializer"
	"github.com/ajent-social/amos/internal/scaffold/ci"
)

func TestValidationWorkflowGeneratedAndResume(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	generator := Generator{SourceDir: root}
	newInput := func() initializer.Input {
		return initializer.Input{SchemaVersion: 1, AppSlug: "validation-app", ModulePath: "example.com/validation/app", ParentDir: ownerTempDir(t), Target: "validation-app", Modules: []string{"identity", "workspace"}, Mode: "evaluation", BusinessMode: "integrated-go", PublicOrigin: "http://127.0.0.1:8080"}
	}
	assertOutput := func(t *testing.T, input initializer.Input, result initializer.Result) {
		t.Helper()
		for name, want := range map[string][]byte{
			ci.ValidationWorkflowPath:  ci.RenderValidation(),
			ci.ValidationGoVersionPath: []byte("module amos-validation-toolchain\n\ngo 1.27.1\n\ntoolchain go1.27.1\n"),
		} {
			got, e := os.ReadFile(filepath.Join(input.ParentDir, result.Target, filepath.FromSlash(name)))
			if e != nil || !bytes.Equal(got, want) {
				t.Fatalf("actual generated %s mismatch: %v", name, e)
			}
			found := false
			for _, file := range result.Manifest.Files {
				if file.Path == name {
					found = true
					if file.Owner != "managed" || file.Mode != 0644 {
						t.Fatal("CI output lost managed ownership/mode")
					}
				}
			}
			if !found {
				t.Fatalf("%s missing from actual manifest", name)
			}
		}
	}
	t.Run("initialize_and_repeat", func(t *testing.T) {
		input := newInput()
		result, e := initializer.Initialize(context.Background(), input, generator)
		if e != nil || !result.Finalized {
			t.Fatalf("actual application initialize: %v", e)
		}
		assertOutput(t, input, result)
		path := filepath.Join(input.ParentDir, result.Target, ci.ValidationWorkflowPath)
		custom := []byte("owner-authored workflow edit\n")
		if e = os.WriteFile(path, custom, 0644); e != nil {
			t.Fatal(e)
		}
		if _, e = initializer.Initialize(context.Background(), input, generator); !errors.Is(e, initializer.ErrTargetConflict) {
			t.Fatalf("repeat replaced existing project: %v", e)
		}
		got, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(got, custom) {
			t.Fatal("repeat overwrote owner workflow")
		}
	})
	interrupted := initializer.GeneratorFunc(func(ctx context.Context, c initializer.Config, files *initializer.Files) error {
		if e := generator.Generate(ctx, c, files); e != nil {
			return e
		}
		return errors.New("synthetic interruption after actual application generation")
	})
	t.Run("resume", func(t *testing.T) {
		input := newInput()
		partial, e := initializer.Initialize(context.Background(), input, interrupted)
		if !errors.Is(e, initializer.ErrGeneration) || partial.StagingName == "" {
			t.Fatalf("missing resumable output: %v", e)
		}
		result, e := initializer.Resume(context.Background(), input, partial.StagingName, generator)
		if e != nil || !result.Finalized {
			t.Fatalf("actual application resume: %v", e)
		}
		assertOutput(t, input, result)
	})
	for _, name := range []string{ci.ValidationWorkflowPath, ci.ValidationGoVersionPath} {
		t.Run("modified_preimage/"+name, func(t *testing.T) {
			input := newInput()
			partial, e := initializer.Initialize(context.Background(), input, interrupted)
			if !errors.Is(e, initializer.ErrGeneration) || partial.StagingName == "" {
				t.Fatalf("missing interrupted output: %v", e)
			}
			path := filepath.Join(input.ParentDir, partial.StagingName, filepath.FromSlash(name))
			custom := []byte("owner-authored preimage edit\n")
			if e = os.WriteFile(path, custom, 0644); e != nil {
				t.Fatal(e)
			}
			if _, e = initializer.Resume(context.Background(), input, partial.StagingName, generator); !errors.Is(e, initializer.ErrResumeConflict) {
				t.Fatalf("changed CI preimage accepted: %v", e)
			}
			got, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(got, custom) {
				t.Fatal("resume overwrote changed CI preimage")
			}
		})
	}
}
