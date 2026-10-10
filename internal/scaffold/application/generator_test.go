package application

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/initializer"
)

func ownerTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGeneratorRejectsUnsupportedProfilesAndMissingSource(t *testing.T) {
	if _, err := selectFramework(filepath.Join(ownerTempDir(t), "absent")); err == nil {
		t.Fatal("missing apphost source accepted")
	}
	parent := ownerTempDir(t)
	input := initializer.Input{SchemaVersion: 1, AppSlug: "sample-app", ModulePath: "example.com/sample/app", ParentDir: parent, Target: "sample-app", Modules: []string{"identity", "workspace"}, Mode: "production-target", BusinessMode: "integrated-go", PublicOrigin: "https://sample.example", DeploymentProfile: "managed", CloudflareRequired: true}
	_, err := initializer.Initialize(context.Background(), input, Generator{SourceDir: filepath.Join(parent, "absent")})
	if err == nil {
		t.Fatal("unsupported mode accepted")
	}
	if _, err := os.Lstat(filepath.Join(parent, "sample-app")); !os.IsNotExist(err) {
		t.Fatal("unsupported mode created the target")
	}
}

func TestGeneratorCopiesPrivateConfigurationAndReviewedRuntime(t *testing.T) {
	root := os.Getenv("AMOS_RUNTIME_SOURCE")
	if root == "" {
		var err error
		root, err = filepath.Abs("../../..")
		if err != nil {
			t.Fatal("AMOS runtime source is unavailable")
		}
	}
	if _, err := os.Stat(filepath.Join(root, "apphost", "local.go")); err != nil {
		t.Fatal("AMOS runtime source is unavailable")
	}
	parent := ownerTempDir(t)
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	input := initializer.Input{SchemaVersion: 1, AppSlug: "sample-app", ModulePath: "example.com/sample/app", ParentDir: parent, Target: "sample-app", Modules: []string{"identity", "workspace"}, Mode: "evaluation", BusinessMode: "integrated-go", PublicOrigin: "http://127.0.0.1:8080"}
	result, err := initializer.Initialize(context.Background(), input, Generator{SourceDir: root})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	private := filepath.Join(parent, result.Target, ".amos", "runtime.json")
	info, err := os.Stat(private)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("runtime config mode %o", info.Mode().Perm())
	}
	for _, name := range []string{"go.mod", "cmd/app/main.go", "cmd/app/migrate.go", ".amos/framework/apphost/local.go", ".amos/framework/LICENSE", "scripts/dev", "scripts/migrate"} {
		if _, err := os.Stat(filepath.Join(parent, result.Target, name)); err != nil {
			t.Errorf("missing generated path %s: %v", name, err)
		}
	}
	mod, err := os.ReadFile(filepath.Join(parent, result.Target, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(mod) == "" {
		t.Fatal("empty go.mod")
	}
	if strings.Contains(string(mod), "replace github.com/ajent-social/amos => /") || strings.Contains(string(mod), "replace github.com/ajent-social/amos => \\") {
		t.Fatal("absolute path found in generated module")
	}
	config, err := os.ReadFile(private)
	if err != nil {
		t.Fatal(err)
	}
	for _, secretName := range []string{"materialKey", "rateKey"} {
		if !containsJSONField(config, secretName) {
			t.Errorf("private config missing %s", secretName)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	checks := exec.CommandContext(ctx, "go", "test", "./cmd/app")
	checks.Dir = filepath.Join(parent, result.Target)
	if output, err := checks.CombinedOutput(); err != nil {
		t.Fatalf("generated private runtime tests failed: %v\n%s", err, output)
	}
	command := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(parent, "generated-app"), "./cmd/app")
	command.Dir = filepath.Join(parent, result.Target)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated application does not compile: %v\n%s", err, output)
	}
}

func containsJSONField(data []byte, field string) bool {
	return len(data) > 0 && strings.Contains(string(data), `"`+field+`"`)
}

func TestGeneratorResumePreservesPrivateRuntimeIdentity(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal("framework source unavailable")
	}
	parent := ownerTempDir(t)
	input := initializer.Input{SchemaVersion: 1, AppSlug: "resume-app", ModulePath: "example.test/resume", ParentDir: parent, Target: "resume-app", Modules: []string{"identity", "workspace"}, Mode: "evaluation", BusinessMode: "integrated-go", PublicOrigin: "http://127.0.0.1:4188"}
	generator := Generator{SourceDir: root}
	result, err := initializer.Initialize(context.Background(), input, initializer.GeneratorFunc(func(ctx context.Context, c initializer.Config, f *initializer.Files) error {
		if err := generator.Generate(ctx, c, f); err != nil {
			return err
		}
		return initializer.ErrGeneration
	}))
	if err == nil || result.StagingName == "" {
		t.Fatalf("interruption did not preserve an owned stage: %v", err)
	}
	original, err := os.ReadFile(filepath.Join(parent, result.StagingName, ".amos", "runtime.json"))
	if err != nil {
		t.Fatal("private staging configuration unavailable")
	}
	resumed, err := initializer.Resume(context.Background(), input, result.StagingName, generator)
	if err != nil || !resumed.Finalized {
		t.Fatal("generated application resume failed")
	}
	final, err := os.ReadFile(filepath.Join(parent, input.Target, ".amos", "runtime.json"))
	if err != nil || string(original) != string(final) {
		t.Fatal("resume changed private keys or realm identity")
	}
}
