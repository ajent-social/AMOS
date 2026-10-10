package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExecRunnerDoesNotForwardUnallowlistedEnvironment(t *testing.T) {
	const key = "AMOS_TEST_SENTINEL_SECRET"
	t.Setenv(key, "sentinel-value-must-not-reach-child")

	output, err := (ExecRunner{}).Run(context.Background(), t.TempDir(), "env")
	if err != nil {
		t.Fatalf("ExecRunner env: %v", err)
	}
	if strings.Contains(output, key) || strings.Contains(output, "sentinel-value-must-not-reach-child") {
		t.Fatal("ExecRunner forwarded an unallowlisted parent environment variable")
	}
	if path, ok := os.LookupEnv("PATH"); ok && !strings.Contains(output, "PATH="+path) {
		t.Fatal("ExecRunner did not preserve PATH for child tool lookup")
	}
}

func validOptions() Options {
	return Options{
		Profile:      "aws_managed",
		BuilderImage: "registry.example/go@sha256:" + strings.Repeat("a", 64),
		RuntimeImage: "registry.example/runtime@sha256:" + strings.Repeat("b", 64),
		Package:      "./cmd/app",
		Tag:          "amos:test",
	}
}

func TestBuildPlan(t *testing.T) {
	p, err := NewPlan(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.platform, "linux/amd64"; got != want {
		t.Fatalf("platform = %q, want %q", got, want)
	}
	if got, want := p.BuildCommand(), []string{"podman", "build", "--pull=always", "--format=oci", "--platform", "linux/amd64", "--file", ".amos/Containerfile", "--tag", "amos:test", "."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("build command = %#v, want %#v", got, want)
	}
	if got, want := p.SBOMCommand(".amos/.sbom-test/sbom.cdx.json"), []string{"syft", "amos:test", "--output", "cyclonedx-json=.amos/.sbom-test/sbom.cdx.json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SBOM command = %#v, want %#v", got, want)
	}
	if got, want := p.ScanCommands(), [][]string{
		{"grype", "amos:test", "--fail-on", "high"},
		{"trivy", "image", "--scanners", "secret", "--severity", "HIGH,CRITICAL", "--exit-code", "1", "amos:test"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scan commands = %#v, want %#v", got, want)
	}

	containerfile := p.Containerfile()
	for _, required := range []string{
		"FROM --platform=linux/amd64 registry.example/go@sha256:" + strings.Repeat("a", 64) + " AS build",
		"FROM --platform=linux/amd64 registry.example/runtime@sha256:" + strings.Repeat("b", 64) + " AS runtime",
		"go build -trimpath -buildvcs=false", "USER 65532:65532", "ENTRYPOINT [\"/amos\"]",
	} {
		if !strings.Contains(containerfile, required) {
			t.Errorf("Containerfile missing %q", required)
		}
	}
	for _, forbidden := range []string{"ARG ", "ENV ", "RUN --mount=type=secret", "password", "token"} {
		if strings.Contains(strings.ToLower(containerfile), strings.ToLower(forbidden)) {
			t.Errorf("Containerfile unexpectedly contains %q", forbidden)
		}
	}
}

func TestPlatformForProfile(t *testing.T) {
	for profile, want := range map[string]string{"aws_managed": "linux/amd64", "aws_vm": "linux/arm64"} {
		got, err := PlatformForProfile(profile)
		if err != nil || got != want {
			t.Errorf("PlatformForProfile(%q) = %q, %v; want %q", profile, got, err, want)
		}
	}
	if _, err := PlatformForProfile("unknown"); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestBuildPlanRejectsUnpinnedOrUnsafeInputs(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Options)
	}{
		{"tagged builder", func(o *Options) { o.BuilderImage = "golang:1.25" }},
		{"tagged runtime", func(o *Options) { o.RuntimeImage = "runtime:latest" }},
		{"unknown profile", func(o *Options) { o.Profile = "linux/amd64" }},
		{"injected package", func(o *Options) { o.Package = "./cmd/app; echo secret" }},
		{"path traversal", func(o *Options) { o.Package = "./cmd/../private" }},
		{"remote image tag", func(o *Options) { o.Tag = "registry.example/amos:latest@sha256:" + strings.Repeat("a", 64) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := validOptions()
			tc.edit(&o)
			if _, err := NewPlan(o); err == nil {
				t.Fatal("NewPlan accepted invalid input")
			}
		})
	}
}

type fixtureRunner struct {
	calls    []string
	fail     string
	hostArch string
}

func (r *fixtureRunner) Run(_ context.Context, dir string, name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if name == r.fail {
		return "", errors.New("fixture scanner finding")
	}
	if name == "uname" {
		if r.hostArch != "" {
			return r.hostArch + "\n", nil
		}
		return "x86_64\n", nil
	}
	if strings.Contains(call, "--format {{.Os}}/{{.Architecture}}") {
		return "linux/amd64\n", nil
	}
	if strings.Contains(call, "--format {{.Config.User}}") {
		return "65532:65532\n", nil
	}
	if strings.Contains(call, "image history") {
		return "fixture history\n", nil
	}
	if name == "syft" {
		for _, arg := range args {
			if strings.HasPrefix(arg, "cyclonedx-json=") {
				path := filepath.Join(dir, strings.TrimPrefix(arg, "cyclonedx-json="))
				if err := os.WriteFile(path, []byte("fixture sbom"), 0o600); err != nil {
					return "", err
				}
			}
		}
	}
	return "", nil
}

func TestBuildAndScanFailsClosedOnScannerFinding(t *testing.T) {
	dir := t.TempDir()
	runner := &fixtureRunner{fail: "trivy"}
	plan, err := NewPlan(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	err = plan.BuildAndScan(context.Background(), dir, runner)
	if err == nil || !strings.Contains(err.Error(), "trivy scan failed") {
		t.Fatalf("BuildAndScan error = %v, want scanner failure", err)
	}
	containerfile, readErr := os.ReadFile(filepath.Join(dir, ".amos", "Containerfile"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(containerfile) != plan.Containerfile() {
		t.Fatal("Podman Containerfile differs from validated plan output")
	}
	if len(runner.calls) != 8 || !strings.HasPrefix(runner.calls[len(runner.calls)-1], "trivy image ") {
		t.Fatalf("commands did not stop at failing scanner: %#v", runner.calls)
	}
}

func TestBuildAndScanRejectsUnqualifiedCrossArchitectureRunner(t *testing.T) {
	dir := t.TempDir()
	o := validOptions()
	o.Profile = "aws_vm"
	plan, err := NewPlan(o)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fixtureRunner{hostArch: "x86_64"}
	if err := plan.BuildAndScan(context.Background(), dir, runner); err == nil || !strings.Contains(err.Error(), "cross-architecture emulation is not enabled") {
		t.Fatalf("BuildAndScan error = %v, want native-runner failure", err)
	}
	if len(runner.calls) != 1 || runner.calls[0] != "uname -m" {
		t.Fatalf("build started before native architecture was checked: %#v", runner.calls)
	}
}

func TestBuildAndScanRejectsMismatchedContainerfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".amos", "Containerfile")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("FROM unreviewed:latest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fixtureRunner{}
	plan, err := NewPlan(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.BuildAndScan(context.Background(), dir, runner); err == nil {
		t.Fatal("mismatched Containerfile was accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("commands ran before Containerfile identity check: %#v", runner.calls)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "FROM unreviewed:latest\n" {
		t.Fatalf("existing Containerfile changed: %q, %v", got, err)
	}
}

func TestBuildAndScanRejectsContainerfileSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("preserve me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(dir, ".amos")); err != nil {
		t.Fatal(err)
	}
	runner := &fixtureRunner{}
	plan, err := NewPlan(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.BuildAndScan(context.Background(), dir, runner); err == nil {
		t.Fatal("symlinked artifact directory was accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("commands ran before path validation: %#v", runner.calls)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "preserve me" {
		t.Fatalf("outside file changed: %q, %v", got, err)
	}
}

func TestBuildAndScanRejectsOutputSymlinks(t *testing.T) {
	for _, output := range []string{"image-history.txt", "sbom.cdx.json"} {
		t.Run(output, func(t *testing.T) {
			dir := t.TempDir()
			artifactDir := filepath.Join(dir, ".amos")
			if err := os.MkdirAll(artifactDir, 0o755); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("preserve me"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(artifactDir, output)); err != nil {
				t.Fatal(err)
			}
			runner := &fixtureRunner{}
			plan, err := NewPlan(validOptions())
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.BuildAndScan(context.Background(), dir, runner); err == nil {
				t.Fatal("symlinked artifact output was accepted")
			}
			if len(runner.calls) != 0 {
				t.Fatalf("commands ran before output-path validation: %#v", runner.calls)
			}
			if got, err := os.ReadFile(outside); err != nil || string(got) != "preserve me" {
				t.Fatalf("outside target changed: %q, %v", got, err)
			}
		})
	}
}

func TestArtifactTemplatePolicy(t *testing.T) {
	path := filepath.Join("..", "scaffold", "ci", "templates", "artifact.yml.tmpl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, required := range []string{
		"git init", "git -c protocol.version=2 fetch --depth=1 origin \"$GITHUB_SHA\"", "git checkout --detach FETCH_HEAD",
		"plan.BuildAndScan", "AMOS_ARTIFACT_PROFILE", "AMOS_BUILDER_IMAGE", "AMOS_RUNTIME_IMAGE",
		"podman", "syft", "grype", "trivy", "AMSL", "Docker Buildx",
	} {
		if !strings.Contains(s, required) {
			t.Errorf("artifact job missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"docker/build-push-action", "podman push", "--quiet", "--ignore-unfixed",
		"pull_request_target:", "write-all", "--secret", "--build-arg",
	} {
		if strings.Contains(strings.ToLower(s), strings.ToLower(forbidden)) {
			t.Errorf("artifact job unexpectedly contains %q", forbidden)
		}
	}
}
