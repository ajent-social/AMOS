package artifact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Options is the complete, non-secret input to one OCI build.
type Options struct {
	Profile      string
	BuilderImage string
	RuntimeImage string
	Package      string
	Tag          string
}

// Plan is immutable outside this package. Construct it with NewPlan so exported
// fields cannot be populated to bypass the contract validation.
type Plan struct {
	profile      string
	platform     string
	builderImage string
	runtimeImage string
	pkg          string
	tag          string
}

var (
	digestImage = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]*@sha256:[0-9a-f]{64}$`)
	goPackage   = regexp.MustCompile(`^\./[A-Za-z0-9._/-]+$`)
	imageTag    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*$`)
)

// PlatformForProfile returns the platform selected by the accepted T8.1
// reference profile. The frozen profiles use x86 for Fargate and ARM64 for VM.
func PlatformForProfile(profile string) (string, error) {
	switch profile {
	case "aws_managed":
		return "linux/amd64", nil
	case "aws_vm":
		return "linux/arm64", nil
	default:
		return "", fmt.Errorf("unsupported deployment profile %q", profile)
	}
}

func NewPlan(o Options) (Plan, error) {
	platform, err := PlatformForProfile(o.Profile)
	if err != nil {
		return Plan{}, err
	}
	if !digestImage.MatchString(o.BuilderImage) {
		return Plan{}, errors.New("builder image must be pinned by a lowercase sha256 digest")
	}
	if !digestImage.MatchString(o.RuntimeImage) {
		return Plan{}, errors.New("runtime image must be pinned by a lowercase sha256 digest")
	}
	if !goPackage.MatchString(o.Package) || strings.Contains(o.Package, "..") {
		return Plan{}, fmt.Errorf("invalid Go package path %q", o.Package)
	}
	if !imageTag.MatchString(o.Tag) || strings.Contains(o.Tag, "@") {
		return Plan{}, fmt.Errorf("invalid local image tag %q", o.Tag)
	}
	return Plan{
		profile: o.Profile, platform: platform, builderImage: o.BuilderImage,
		runtimeImage: o.RuntimeImage, pkg: o.Package, tag: o.Tag,
	}, nil
}

func (p Plan) validate() error {
	want, err := NewPlan(Options{
		Profile: p.profile, BuilderImage: p.builderImage, RuntimeImage: p.runtimeImage,
		Package: p.pkg, Tag: p.tag,
	})
	if err != nil {
		return err
	}
	if p != want {
		return errors.New("artifact plan fields do not match the validated profile inputs")
	}
	return nil
}

// Containerfile emits a two-stage Go build with pinned bases and a nonroot
// runtime. It has no build args, secret mounts, credential env vars, or labels.
func (p Plan) Containerfile() string {
	return fmt.Sprintf(`FROM --platform=%s %s AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /out/amos %s

FROM --platform=%s %s AS runtime
COPY --from=build --chown=65532:65532 /out/amos /amos
USER 65532:65532
ENTRYPOINT ["/amos"]
`, p.platform, p.builderImage, p.pkg, p.platform, p.runtimeImage)
}

func (p Plan) BuildCommand() []string {
	return []string{"podman", "build", "--pull=always", "--format=oci", "--platform", p.platform, "--file", ".amos/Containerfile", "--tag", p.tag, "."}
}

func (p Plan) SBOMCommand(outputPath string) []string {
	return []string{"syft", p.tag, "--output", "cyclonedx-json=" + outputPath}
}

func (p Plan) ScanCommands() [][]string {
	return [][]string{
		{"grype", p.tag, "--fail-on", "high"},
		{"trivy", "image", "--scanners", "secret", "--severity", "HIGH,CRITICAL", "--exit-code", "1", p.tag},
	}
}

// Runner executes a command in dir and returns its standard output. Implementations
// must return a non-nil error for any nonzero exit status.
type Runner interface {
	Run(ctx context.Context, dir, name string, args ...string) (string, error)
}

// ExecRunner runs local tools without forwarding arbitrary parent environment
// variables. Build tools receive only path and temporary/runtime directory settings.
type ExecRunner struct{}

var runnerEnvironmentAllowlist = []string{
	"PATH", "TMPDIR", "TMP", "TEMP", "HOME", "XDG_RUNTIME_DIR",
}

func runnerEnvironment() []string {
	env := make([]string, 0, len(runnerEnvironmentAllowlist))
	for _, key := range runnerEnvironmentAllowlist {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func (ExecRunner) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = runnerEnvironment()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s command failed", name)
	}
	return string(out), nil
}

// BuildAndScan writes the validated rendered Containerfile to the exact path
// consumed by Podman, then builds, verifies, records history, emits an SBOM,
// and runs blocking vulnerability and secret scans.
func (p Plan) BuildAndScan(ctx context.Context, dir string, runner Runner) error {
	if err := p.validate(); err != nil {
		return fmt.Errorf("invalid artifact plan: %w", err)
	}
	if runner == nil {
		return errors.New("artifact command runner is required")
	}
	containerfile := filepath.Join(dir, ".amos", "Containerfile")
	if err := ensureContainerfile(containerfile, p.Containerfile()); err != nil {
		return err
	}
	historyPath := filepath.Join(dir, ".amos", "image-history.txt")
	sbomPath := filepath.Join(dir, ".amos", "sbom.cdx.json")
	if err := requireAbsentFile(historyPath); err != nil {
		return err
	}
	if err := requireAbsentFile(sbomPath); err != nil {
		return err
	}
	hostArch, err := runner.Run(ctx, dir, "uname", "-m")
	if err != nil {
		return errors.New("build runner architecture check failed")
	}
	expectedArch := "x86_64"
	if p.platform == "linux/arm64" {
		expectedArch = "aarch64"
	}
	gotArch := strings.TrimSpace(hostArch)
	matchesArch := gotArch == expectedArch || (expectedArch == "aarch64" && gotArch == "arm64")
	if !matchesArch {
		return fmt.Errorf("build runner architecture %q does not match profile platform %q; cross-architecture emulation is not enabled", gotArch, p.platform)
	}
	run := func(command []string) (string, error) {
		return runner.Run(ctx, dir, command[0], command[1:]...)
	}
	if _, err := run(p.BuildCommand()); err != nil {
		return err
	}
	platform, err := runner.Run(ctx, dir, "podman", "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", p.tag)
	if err != nil {
		return errors.New("podman platform inspection failed")
	}
	if strings.TrimSpace(platform) != p.platform {
		return fmt.Errorf("built image platform %q does not match profile platform %q", strings.TrimSpace(platform), p.platform)
	}
	user, err := runner.Run(ctx, dir, "podman", "image", "inspect", "--format", "{{.Config.User}}", p.tag)
	if err != nil {
		return errors.New("podman runtime-user inspection failed")
	}
	if strings.TrimSpace(user) != "65532:65532" {
		return fmt.Errorf("built image user %q is not the required nonroot user", strings.TrimSpace(user))
	}
	history, err := runner.Run(ctx, dir, "podman", "image", "history", "--no-trunc", p.tag)
	if err != nil {
		return errors.New("podman image history failed")
	}
	if err := createNewFile(historyPath, []byte(history), 0o600); err != nil {
		return errors.New("create image history failed")
	}
	sbomDir, err := os.MkdirTemp(filepath.Join(dir, ".amos"), ".sbom-")
	if err != nil {
		return errors.New("create private SBOM output directory failed")
	}
	defer func() { _ = os.RemoveAll(sbomDir) }()
	tempSBOMPath := filepath.Join(sbomDir, "sbom.cdx.json")
	relativeSBOMPath, err := filepath.Rel(dir, tempSBOMPath)
	if err != nil {
		return errors.New("resolve temporary SBOM path failed")
	}
	if _, err := run(p.SBOMCommand(filepath.ToSlash(relativeSBOMPath))); err != nil {
		return err
	}
	if err := installFileNoReplace(tempSBOMPath, sbomPath); err != nil {
		return errors.New("install SBOM without replacement failed")
	}
	for _, command := range p.ScanCommands() {
		if _, err := run(command); err != nil {
			return fmt.Errorf("%s scan failed", command[0])
		}
	}
	return nil
}

func ensureContainerfile(path, contents string) error {
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(parent, 0o755); err != nil {
			return errors.New("create artifact metadata directory failed")
		}
	} else if err != nil {
		return errors.New("inspect artifact metadata directory failed")
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("artifact metadata path is not a real directory")
	}

	info, err = os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("existing .amos/Containerfile is not a regular file")
		}
		existing, err := os.ReadFile(path)
		if err != nil {
			return errors.New("read .amos/Containerfile failed")
		}
		if string(existing) != contents {
			return errors.New("existing .amos/Containerfile does not match validated build plan")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return errors.New("inspect .amos/Containerfile failed")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return errors.New("create .amos/Containerfile failed")
	}
	if _, err := f.WriteString(contents); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return errors.New("write .amos/Containerfile failed")
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return errors.New("close .amos/Containerfile failed")
	}
	return nil
}

func requireAbsentFile(path string) error {
	_, err := os.Lstat(path)
	if err == nil {
		return fmt.Errorf("refusing to overwrite existing artifact output %q", filepath.Base(path))
	}
	if !errors.Is(err, os.ErrNotExist) {
		return errors.New("inspect artifact output path failed")
	}
	return nil
}

func createNewFile(path string, contents []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(contents); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func installFileNoReplace(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("temporary SBOM is not a regular file")
	}
	if err := os.Link(source, destination); err != nil {
		return err
	}
	return os.Remove(source)
}
