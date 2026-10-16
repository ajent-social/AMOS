// Package ownership renders a bounded, data-only ownership manifest component.
// It performs no filesystem access or fetching and does not replace the
// initializer's manifest, prove public provenance, or authorize an upgrade.
package ownership

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"sort"
	"strings"
)

type Class string

const (
	Generated        Class = "generated"
	OwnerAuthored    Class = "owner-authored"
	CopiedOnce       Class = "copied-once"
	Overridden       Class = "overridden"
	Format                 = "amos-ownership-component-v1"
	MaxFiles               = 4096
	MaxPins                = 256
	MaxBaselineBytes       = 16 << 20
	MaxBaselines           = MaxFiles + 1
)

var (
	ErrMetadata    = errors.New("ownership: invalid metadata")
	ErrLimit       = errors.New("ownership: resource limit exceeded")
	ErrPath        = errors.New("ownership: invalid public relative path")
	ErrAlias       = errors.New("ownership: conflicting paths or pins")
	ErrClass       = errors.New("ownership: invalid ownership class")
	ErrAuthored    = errors.New("ownership: authored ownership required")
	ErrBaseline    = errors.New("ownership: baseline unavailable or inconsistent")
	versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?(\+incompatible)?$`)
	schemaPattern  = regexp.MustCompile(`^v[1-9][0-9]*$`)
	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// File records a current content digest, never its contents. BaselineSHA256 is
// mandatory for generated and copied-once files, optional for explicit overrides,
// and forbidden for owner-authored files. An override never grants management.
type File struct {
	Path           string `json:"path"`
	Class          Class  `json:"class"`
	SHA256         string `json:"sha256"`
	BaselineSHA256 string `json:"baselineSha256,omitempty"`
}

// Pin identifies a public dependency selected by the caller. Module revisions
// use canonical v-prefixed versions; workflow revisions are full commit SHAs.
// Syntax validation is not upstream provenance or consumer qualification.
type Pin struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
	// ContentSHA256 pins module archive bytes independently of mutable tags.
	// Workflow commits already identify content and must omit this field.
	ContentSHA256 string `json:"contentSha256,omitempty"`
}

// Input is read synchronously and must not be concurrently mutated. Baselines
// maps lowercase SHA-256 digests to actual bytes. All supplied bytes are bounded
// and checked; they are neither retained nor included in rendered output.
type Input struct {
	GeneratorVersion       string
	ConfigSchemaVersion    string
	TemplateBaselineSHA256 string
	Files                  []File
	Modules                []Pin
	Workflows              []Pin
	Baselines              map[string][]byte
}

type manifest struct {
	Format                 string `json:"format"`
	GeneratorVersion       string `json:"generatorVersion"`
	ConfigSchemaVersion    string `json:"configSchemaVersion"`
	TemplateBaselineSHA256 string `json:"templateBaselineSha256"`
	Files                  []File `json:"files"`
	Modules                []Pin  `json:"modules"`
	Workflows              []Pin  `json:"workflows"`
}

// Render validates and copies records, then returns canonical sorted JSON with
// a trailing newline. Paths must already be slash-normalized: repairing a path
// could silently assign ownership to a different file. Callers remain
// responsible for trusted classification, public metadata, and filesystem
// symlink/preimage checks before consuming this data in an upgrade.
func Render(in Input) ([]byte, error) {
	if len(in.Files) > MaxFiles || len(in.Modules) > MaxPins || len(in.Workflows) > MaxPins || len(in.Baselines) > MaxBaselines {
		return nil, ErrLimit
	}
	if !version(in.GeneratorVersion) || len(in.ConfigSchemaVersion) > 32 || !schemaPattern.MatchString(in.ConfigSchemaVersion) {
		return nil, ErrMetadata
	}
	total := 0
	for digest, content := range in.Baselines {
		if len(content) > MaxBaselineBytes-total {
			return nil, ErrLimit
		}
		total += len(content)
		if !validDigest(digest) || Sum(content) != digest {
			return nil, ErrBaseline
		}
	}
	if !hasBaseline(in, in.TemplateBaselineSHA256) {
		return nil, ErrBaseline
	}
	m := manifest{Format: Format, GeneratorVersion: in.GeneratorVersion, ConfigSchemaVersion: in.ConfigSchemaVersion,
		TemplateBaselineSHA256: in.TemplateBaselineSHA256, Files: append([]File{}, in.Files...),
		Modules: append([]Pin{}, in.Modules...), Workflows: append([]Pin{}, in.Workflows...)}
	seen := make(map[string]bool, len(m.Files))
	for _, file := range m.Files {
		if !publicPath(file.Path) {
			return nil, ErrPath
		}
		key := strings.ToLower(file.Path)
		if seen[key] {
			return nil, ErrAlias
		}
		seen[key] = true
		if !validDigest(file.SHA256) {
			return nil, ErrMetadata
		}
		switch file.Class {
		case Generated, CopiedOnce:
			if authoredPath(key) {
				return nil, ErrAuthored
			}
			if file.BaselineSHA256 == "" {
				return nil, ErrBaseline
			}
		case OwnerAuthored:
			if file.BaselineSHA256 != "" {
				return nil, ErrAuthored
			}
		case Overridden:
		default:
			return nil, ErrClass
		}
		if file.BaselineSHA256 != "" && !hasBaseline(in, file.BaselineSHA256) {
			return nil, ErrBaseline
		}
	}
	// A file cannot also be an ancestor directory of another recorded file.
	for key := range seen {
		for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return nil, ErrAlias
			}
		}
	}
	if err := validatePins(m.Modules, false); err != nil {
		return nil, err
	}
	if err := validatePins(m.Workflows, true); err != nil {
		return nil, err
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	sort.Slice(m.Modules, func(i, j int) bool { return m.Modules[i].Name < m.Modules[j].Name })
	sort.Slice(m.Workflows, func(i, j int) bool { return m.Workflows[i].Name < m.Workflows[j].Name })
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, ErrMetadata
	}
	return append(data, '\n'), nil
}

// Sum returns the lowercase SHA-256 digest of bytes.
func Sum(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func hasBaseline(in Input, digest string) bool {
	if !validDigest(digest) {
		return false
	}
	_, ok := in.Baselines[digest]
	return ok
}

func version(v string) bool { return len(v) <= 128 && versionPattern.MatchString(v) }

func validDigest(v string) bool { return len(v) == 64 && digestPattern.MatchString(v) }

func authoredPath(p string) bool {
	for _, root := range []string{"app/business", "api/business", "business/operations", "business/types", "ui/overrides"} {
		if p == root || strings.HasPrefix(p, root+"/") || strings.HasPrefix(p, root+".") {
			return true
		}
	}
	return false
}

func publicPath(p string) bool {
	if len(p) == 0 || len(p) > 512 || path.Clean(p) != p {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		allowedDot := segment == ".github" || segment == ".gitignore" || segment == ".dockerignore" || segment == ".golangci.yml"
		if len(segment) > 128 || (!segmentPattern.MatchString(segment) && !allowedDot) || strings.HasSuffix(segment, ".") {
			return false
		}
		s := strings.ToLower(segment)
		base := strings.SplitN(s, ".", 2)[0]
		if base == "con" || base == "prn" || base == "aux" || base == "nul" || (len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '0' && base[3] <= '9') {
			return false
		}
		switch base {
		case "secrets", "secret", "credentials", "credential", "private", "password", "passwords", "token", "tokens", "id_rsa", "id_ed25519", "tmp", "temp", "logs", "cache", "node_modules", "vendor", "build", "dist", "bin":
			return false
		}
		for _, suffix := range []string{".pem", ".key", ".p12", ".pfx", ".log", ".tmp", ".bak", ".swp", ".db", ".sqlite", ".exe", ".dll"} {
			if strings.HasSuffix(s, suffix) {
				return false
			}
		}
	}
	return true
}

func validatePins(pins []Pin, workflow bool) error {
	seen := make(map[string]bool, len(pins))
	for _, pin := range pins {
		if len(pin.Name) > 512 || len(pin.Revision) > 128 || !publicPinName(pin.Name, workflow) {
			return ErrMetadata
		}
		if seen[strings.ToLower(pin.Name)] {
			return ErrAlias
		}
		seen[strings.ToLower(pin.Name)] = true
		if workflow {
			if !commitPattern.MatchString(pin.Revision) || pin.Revision == strings.Repeat("0", 40) || pin.ContentSHA256 != "" {
				return ErrMetadata
			}
		} else if !version(pin.Revision) || !validDigest(pin.ContentSHA256) {
			return ErrMetadata
		}
	}
	return nil
}

func publicPinName(name string, workflow bool) bool {
	parts := strings.Split(name, "/")
	if workflow {
		return len(parts) == 6 && parts[0] == "github.com" && parts[3] == ".github" && parts[4] == "workflows" &&
			publicPath(parts[1]+"/"+parts[2]+"/"+parts[5]) && (strings.HasSuffix(parts[5], ".yml") || strings.HasSuffix(parts[5], ".yaml"))
	}
	// This component admits explicit public module-host profiles only. Other
	// registries need their own reviewed public provenance/profile admission.
	if len(parts) < 2 {
		return false
	}
	if parts[0] == "github.com" && len(parts) < 3 {
		return false
	}
	switch parts[0] {
	case "github.com", "golang.org", "go.uber.org", "go.opentelemetry.io", "gopkg.in", "go.yaml.in":
		return publicPath(strings.Join(parts[1:], "/"))
	}
	return false
}
