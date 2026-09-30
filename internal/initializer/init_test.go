package initializer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testInput(t *testing.T) Input {
	t.Helper()
	return Input{
		SchemaVersion: SchemaVersion,
		AppSlug:       "todo-demo",
		ModulePath:    "example.test/todo-demo",
		ParentDir:     t.TempDir(),
		Target:        "todo-demo",
		Modules:       []string{"billing", "workspace", "identity"},
		PublicOrigin:  "http://127.0.0.1:8080",
		BusinessMode:  "integrated-go",
		Mode:          "evaluation",
	}
}

func sampleGenerator(ctx context.Context, config Config, files *Files) error {
	if config.AppSlug() == "" || config.ModulePath() == "" {
		return ErrInvalidInput
	}
	if err := files.WriteFile(ctx, "go.mod", []byte("module "+config.ModulePath()+"\n"), 0644); err != nil {
		return err
	}
	return files.WriteFile(ctx, "cmd/app/main.go", []byte("package main\nfunc main() {}\n"), 0644)
}

func TestInitCreatesFilesManifestAndNormalizedConfig(t *testing.T) {
	input := testInput(t)
	var gotModules []string
	generator := GeneratorFunc(func(ctx context.Context, config Config, files *Files) error {
		gotModules = config.Modules()
		gotModules[0] = "mutated"
		return sampleGenerator(ctx, config, files)
	})
	result, err := Initialize(context.Background(), input, generator)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Finalized || result.StagingName != "" || result.Target != input.Target {
		t.Fatalf("unexpected result: %#v", result)
	}
	if want := []ModuleRecord{{ID: "billing", Version: "1.0"}, {ID: "identity", Version: "1.0"}, {ID: "workspace", Version: "1.0"}}; !reflect.DeepEqual(result.Manifest.Modules, want) {
		t.Fatalf("modules=%v, want %v", result.Manifest.Modules, want)
	}
	target := filepath.Join(input.ParentDir, input.Target)
	mainFile, err := os.ReadFile(filepath.Join(target, "cmd", "app", "main.go"))
	if err != nil || string(mainFile) != "package main\nfunc main() {}\n" {
		t.Fatalf("generated file=%q err=%v", mainFile, err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(target, ".amos-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.TemplateVersion != TemplateVersion || len(manifest.Files) != 2 || manifest.Files[0].Path != "cmd/app/main.go" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	if _, err := os.Lstat(filepath.Join(target, ".amos-resume.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resume journal remained public: %v", err)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0755 {
		t.Fatalf("target mode=%o", info.Mode().Perm())
	}
}

func TestInitRequiresGeneratorAndPreservesAuthoredTarget(t *testing.T) {
	input := testInput(t)
	if _, err := Initialize(context.Background(), input, nil); !errors.Is(err, ErrGeneratorUnavailable) {
		t.Fatalf("nil generator error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(input.ParentDir, input.Target)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nil generator created target: %v", err)
	}
	target := filepath.Join(input.ParentDir, input.Target)
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	authored := []byte("owner authored\n")
	if err := os.WriteFile(filepath.Join(target, "README.md"), authored, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(context.Background(), input, GeneratorFunc(sampleGenerator)); !errors.Is(err, ErrTargetConflict) {
		t.Fatalf("existing target error=%v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "README.md"))
	if err != nil || !bytes.Equal(got, authored) {
		t.Fatalf("authored bytes changed: %q err=%v", got, err)
	}
}

func TestInitRejectsInvalidInputsBeforeWriting(t *testing.T) {
	for _, mutate := range []func(*Input){
		func(in *Input) { in.AppSlug = "../bad" },
		func(in *Input) { in.ModulePath = "https://user:secret@example.test/x" },
		func(in *Input) { in.Target = "../escape" },
		func(in *Input) { in.Target = "/absolute" },
		func(in *Input) { in.Modules = []string{"unknown-capability"} },
		func(in *Input) { in.Modules = []string{"workspace"} },
		func(in *Input) { in.Modules = []string{"identity", "identity"} },
		func(in *Input) { in.SchemaVersion = 999 },
		func(in *Input) { in.PublicOrigin = "http://192.0.2.1:8080" },
	} {
		input := testInput(t)
		mutate(&input)
		if _, err := Initialize(context.Background(), input, GeneratorFunc(sampleGenerator)); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid input accepted (%+v): %v", input, err)
		}
		entries, err := os.ReadDir(input.ParentDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("invalid input wrote into parent: %v", entries)
		}
	}
}

func TestInitRejectsSymlinkTargetParent(t *testing.T) {
	input := testInput(t)
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(input.ParentDir, "linked")); err != nil {
		t.Fatal(err)
	}
	input.Target = "linked/app"
	if _, err := Initialize(context.Background(), input, GeneratorFunc(sampleGenerator)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("symlink target parent error=%v", err)
	}
	entries, _ := os.ReadDir(external)
	if len(entries) != 0 {
		t.Fatalf("wrote through symlink: %v", entries)
	}
}

func TestInitPartialOutputCanResumeAndModifiedPreimageIsPreserved(t *testing.T) {
	input := testInput(t)
	first := GeneratorFunc(func(ctx context.Context, config Config, files *Files) error {
		if err := files.WriteFile(ctx, "go.mod", []byte("module "+config.ModulePath()+"\n"), 0644); err != nil {
			return err
		}
		return errors.New("synthetic generator interruption")
	})
	partial, err := Initialize(context.Background(), input, first)
	if !errors.Is(err, ErrGeneration) || partial.StagingName == "" {
		t.Fatalf("expected resumable interruption, got %#v %v", partial, err)
	}
	resumed, err := Resume(context.Background(), input, partial.StagingName, GeneratorFunc(sampleGenerator))
	if err != nil || !resumed.Finalized {
		t.Fatalf("resume result=%#v err=%v", resumed, err)
	}

	input2 := testInput(t)
	partial, err = Initialize(context.Background(), input2, first)
	if !errors.Is(err, ErrGeneration) {
		t.Fatalf("partial generation error=%v", err)
	}
	stage := filepath.Join(input2.ParentDir, partial.StagingName)
	if err := os.WriteFile(filepath.Join(stage, ".amos-resume.json.tmp"), []byte("interrupted metadata write"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(stage, "go.mod")
	if err := os.WriteFile(file, []byte("authored edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resume(context.Background(), input2, partial.StagingName, GeneratorFunc(sampleGenerator)); !errors.Is(err, ErrResumeConflict) {
		t.Fatalf("changed preimage accepted: %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != "authored edit\n" {
		t.Fatalf("resume changed authored bytes: %q err=%v", got, err)
	}
}

func TestInitCancellationBeforeAndAfterAtomicFinalize(t *testing.T) {
	input := testInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	beforeOps := transactionOps{beforeRename: func(context.Context) error { cancel(); return context.Canceled }}
	before, err := initialize(ctx, input, GeneratorFunc(sampleGenerator), beforeOps)
	if !errors.Is(err, ErrCanceled) || before.StagingName == "" {
		t.Fatalf("pre-finalization result=%#v err=%v", before, err)
	}
	if _, err := os.Stat(filepath.Join(input.ParentDir, input.Target)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target appeared before finalize: %v", err)
	}
	resumed, err := Resume(context.Background(), input, before.StagingName, GeneratorFunc(sampleGenerator))
	if err != nil || !resumed.Finalized {
		t.Fatalf("validated-stage resume=%#v err=%v", resumed, err)
	}

	input2 := testInput(t)
	ctx2, cancel2 := context.WithCancel(context.Background())
	afterOps := transactionOps{afterRename: func(context.Context) error { cancel2(); return context.Canceled }}
	after, err := initialize(ctx2, input2, GeneratorFunc(sampleGenerator), afterOps)
	if !errors.Is(err, ErrCanceledAfterFinalization) || !after.Finalized {
		t.Fatalf("post-finalization result=%#v err=%v", after, err)
	}
	if _, err := os.Stat(filepath.Join(input2.ParentDir, input2.Target, "go.mod")); err != nil {
		t.Fatalf("finalized output missing after cancellation: %v", err)
	}
}

func TestInitFinalizationRecheckPreservesConcurrentTarget(t *testing.T) {
	input := testInput(t)
	authored := []byte("concurrent owner data\n")
	ops := transactionOps{beforeRename: func(context.Context) error {
		target := filepath.Join(input.ParentDir, input.Target)
		if err := os.Mkdir(target, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(target, "README.md"), authored, 0644)
	}}
	result, err := initialize(context.Background(), input, GeneratorFunc(sampleGenerator), ops)
	if !errors.Is(err, ErrTargetConflict) || result.Finalized {
		t.Fatalf("race result=%#v err=%v", result, err)
	}
	got, err := os.ReadFile(filepath.Join(input.ParentDir, input.Target, "README.md"))
	if err != nil || !bytes.Equal(got, authored) {
		t.Fatalf("concurrent authored bytes changed: %q err=%v", got, err)
	}
}

func TestInitDecodeInputRejectsUnknownSecretField(t *testing.T) {
	_, err := DecodeInput(strings.NewReader(`{"schemaVersion":1,"credentialValue":"must-not-be-accepted"}`))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown secret field error=%v", err)
	}
}

func TestInitRecoversInterruptedPendingFileWrite(t *testing.T) {
	input := testInput(t)
	partial, err := Initialize(context.Background(), input, GeneratorFunc(func(ctx context.Context, config Config, files *Files) error {
		if err := files.WriteFile(ctx, "go.mod", []byte("module "+config.ModulePath()+"\n"), 0644); err != nil {
			return err
		}
		return errors.New("interrupted after first file")
	}))
	if !errors.Is(err, ErrGeneration) {
		t.Fatalf("partial generation error=%v", err)
	}
	stage := filepath.Join(input.ParentDir, partial.StagingName)
	j, err := readJournal(stage)
	if err != nil {
		t.Fatal(err)
	}
	record := FileRecord{Path: "cmd/app/main.go", SHA256: digest([]byte("package main\nfunc main() {}\n")), Mode: 0644, Size: int64(len("package main\nfunc main() {}\n")), Owner: "managed"}
	temp := "cmd/app/.amos-write-" + j.StageID + "-" + digest([]byte(record.Path))[:12]
	j.Pending = &pendingWrite{Record: record, TempPath: temp}
	if err := writeJournal(stage, j); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stage, "cmd", "app"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, filepath.FromSlash(temp)), []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Resume(context.Background(), input, partial.StagingName, GeneratorFunc(sampleGenerator))
	if err != nil || !result.Finalized {
		t.Fatalf("resume result=%#v err=%v", result, err)
	}
	got, err := os.ReadFile(filepath.Join(input.ParentDir, input.Target, "cmd", "app", "main.go"))
	if err != nil || string(got) != "package main\nfunc main() {}\n" {
		t.Fatalf("recovered file=%q err=%v", got, err)
	}
}

func TestInitAcceptsExplicitProductionProfilesOnly(t *testing.T) {
	for _, profile := range []string{"managed", "small-vm"} {
		input := testInput(t)
		input.Mode = "production-target"
		input.PublicOrigin = "https://app.example.test"
		input.DeploymentProfile = profile
		input.CloudflareRequired = true
		input.Target = profile
		result, err := Initialize(context.Background(), input, GeneratorFunc(sampleGenerator))
		if err != nil {
			t.Fatalf("profile %s: %v", profile, err)
		}
		if result.Manifest.Mode != "production-target" || result.Manifest.PublicOrigin != input.PublicOrigin {
			t.Fatalf("profile %s manifest=%#v", profile, result.Manifest)
		}
	}
	input := testInput(t)
	input.Mode = "production-target"
	input.PublicOrigin = "https://app.example.test"
	input.DeploymentProfile = "managed"
	input.CloudflareRequired = false
	if _, err := Initialize(context.Background(), input, GeneratorFunc(sampleGenerator)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing Cloudflare requirement accepted: %v", err)
	}
}

func TestInitResumeRecordIsBoundToItsTargetParent(t *testing.T) {
	input := testInput(t)
	partial, err := Initialize(context.Background(), input, GeneratorFunc(func(ctx context.Context, config Config, files *Files) error {
		if err := files.WriteFile(ctx, "go.mod", []byte("module "+config.ModulePath()+"\n"), 0644); err != nil {
			return err
		}
		return errors.New("interrupted")
	}))
	if !errors.Is(err, ErrGeneration) {
		t.Fatalf("partial generation error=%v", err)
	}
	parent2 := t.TempDir()
	source := filepath.Join(input.ParentDir, partial.StagingName)
	destination := filepath.Join(parent2, partial.StagingName)
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	err = filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		to := filepath.Join(destination, rel)
		if info.IsDir() {
			return os.Mkdir(to, info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(to, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	moved := input
	moved.ParentDir = parent2
	if _, err := Resume(context.Background(), moved, partial.StagingName, GeneratorFunc(sampleGenerator)); !errors.Is(err, ErrResumeConflict) {
		t.Fatalf("copied resume record accepted under another parent: %v", err)
	}
}
