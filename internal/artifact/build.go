package artifact

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type Options struct {
	Platform     string
	BuilderImage string
	RuntimeImage string
	Package      string
	Tag          string
}

type Plan struct {
	Platform     string
	BuilderImage string
	RuntimeImage string
	Package      string
	Tag          string
}

var (
	digestImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*@sha256:[0-9a-f]{64}$`)
	goPackage   = regexp.MustCompile(`^\./[A-Za-z0-9._/-]+$`)
	imageTag    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*$`)
)

func NewPlan(o Options) (Plan, error) {
	if o.Platform != "linux/amd64" && o.Platform != "linux/arm64" {
		return Plan{}, fmt.Errorf("unsupported single OCI platform %q", o.Platform)
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
	return Plan(o), nil
}

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
`, p.Platform, p.BuilderImage, p.Package, p.Platform, p.RuntimeImage)
}

func (p Plan) BuildCommand(containerfile string) []string {
	return []string{"podman", "build", "--pull=always", "--format=oci", "--platform", p.Platform, "--file", containerfile, "--tag", p.Tag, "."}
}

func (p Plan) SBOMCommand(path string) []string {
	return []string{"syft", p.Tag, "--output", "cyclonedx-json=" + path}
}

func (p Plan) ScanCommands() [][]string {
	return [][]string{
		{"grype", p.Tag, "--fail-on", "high"},
		{"trivy", "image", "--scanners", "secret", "--severity", "HIGH,CRITICAL", "--exit-code", "1", p.Tag},
	}
}
