package application

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// The adopted workflow commands must work in the actual emitted application,
// whose module root intentionally contains no Go files. This is local command
// qualification, not hosted Actions or database/provider acceptance.
func TestValidationWorkflowGeneratedConsumerCommands(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	input := initializer.Input{SchemaVersion: 1, AppSlug: "validation-consumer", ModulePath: "example.com/validation/consumer", ParentDir: ownerTempDir(t), Target: "validation-consumer", Modules: []string{"identity", "workspace"}, Mode: "evaluation", BusinessMode: "integrated-go", PublicOrigin: "http://127.0.0.1:8080"}
	result, err := initializer.Initialize(context.Background(), input, Generator{SourceDir: root})
	if err != nil || !result.Finalized {
		t.Fatalf("actual consumer initialize: %v", err)
	}
	target := filepath.Join(input.ParentDir, result.Target)
	rootFiles, err := filepath.Glob(filepath.Join(target, "*.go"))
	if err != nil || len(rootFiles) != 0 {
		t.Fatal("generated consumer must retain its real nested-package layout")
	}
	env := append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOMAXPROCS=2", "GOFLAGS=-p=1")
	run := func(args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = target
		cmd.Env = env
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("generated consumer go %v: %v\n%s", args, e, out)
		}
		return out
	}
	if got := strings.TrimSpace(string(run("env", "GOVERSION"))); got != "go"+ci.ValidationGoVersion {
		t.Fatalf("actual consumer toolchain %q, want go%s", got, ci.ValidationGoVersion)
	}
	// Exact argument vectors in approved upstream snapshot680caa87; no dummy
	// root package, altered package scope, service fixture or swallowed failure.
	run("vet", "--", "./...")
	out := run("test", "-race", "./...")
	if !bytes.Contains(out, []byte(input.ModulePath+"/cmd/app")) {
		t.Fatalf("actual generated tests did not execute: %s", out)
	}
}
