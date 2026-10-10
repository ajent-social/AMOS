package artifact

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func validOptions() Options {
	return Options{
		Platform:     "linux/amd64",
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
	if p.Platform != "linux/amd64" {
		t.Fatalf("platform = %q", p.Platform)
	}
	if got, want := p.BuildCommand("Containerfile"), []string{"podman", "build", "--pull=always", "--format=oci", "--platform", "linux/amd64", "--file", "Containerfile", "--tag", "amos:test", "."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("build command = %#v, want %#v", got, want)
	}
	if got, want := p.SBOMCommand("sbom.json"), []string{"syft", "amos:test", "--output", "cyclonedx-json=sbom.json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SBOM command = %#v, want %#v", got, want)
	}
	commands := p.ScanCommands()
	if len(commands) != 2 || !reflect.DeepEqual(commands[0], []string{"grype", "amos:test", "--fail-on", "high"}) ||
		!reflect.DeepEqual(commands[1], []string{"trivy", "image", "--scanners", "secret", "--severity", "HIGH,CRITICAL", "--exit-code", "1", "amos:test"}) {
		t.Fatalf("scan commands = %#v", commands)
	}

	containerfile := p.Containerfile()
	for _, required := range []string{" AS build", " AS runtime", "go build -trimpath -buildvcs=false", "USER 65532:65532", "ENTRYPOINT [\"/amos\"]"} {
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

func TestBuildPlanRejectsUnpinnedOrUnsafeInputs(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Options)
	}{
		{"tagged builder", func(o *Options) { o.BuilderImage = "golang:1.25" }},
		{"tagged runtime", func(o *Options) { o.RuntimeImage = "runtime:latest" }},
		{"unsupported platform", func(o *Options) { o.Platform = "linux/amd64,linux/arm64" }},
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

func TestArtifactTemplatePolicy(t *testing.T) {
	path := filepath.Join("..", "scaffold", "ci", "templates", "artifact.yml.tmpl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, required := range []string{
		"permissions:", "contents: read", "podman build", "--platform", "podman image history",
		"syft", "grype", "--fail-on high", "trivy image --scanners secret",
		"--exit-code 1", "image inspect", ".Config.User", "65532:65532",
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
