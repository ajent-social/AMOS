package ownership

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func fixture() Input {
	baseline := []byte("synthetic public template bytes")
	digest := Sum(baseline)
	return Input{
		GeneratorVersion: "v1.2.3", ConfigSchemaVersion: "v1", TemplateBaselineSHA256: digest,
		Baselines: map[string][]byte{digest: baseline},
		Files: []File{
			{Path: "internal/amosgen/business/transport.go", Class: Generated, SHA256: digest, BaselineSHA256: digest},
			{Path: "app/business.go", Class: OwnerAuthored, SHA256: Sum([]byte("authored"))},
			{Path: "README.md", Class: CopiedOnce, SHA256: digest, BaselineSHA256: digest},
			{Path: "ui/overrides/home.html", Class: Overridden, SHA256: Sum([]byte("override")), BaselineSHA256: digest},
		},
		Modules:   []Pin{{Name: "github.com/example/public", Revision: "v1.0.0", ContentSHA256: digest}, {Name: "golang.org/x/crypto", Revision: "v0.40.0", ContentSHA256: digest}},
		Workflows: []Pin{{Name: "github.com/example/public/.github/workflows/validate.yml", Revision: strings.Repeat("a", 40)}, {Name: "github.com/example/public/.github/workflows/lint.yml", Revision: strings.Repeat("b", 40)}},
	}
}

func TestManifestDeterministicAndOwned(t *testing.T) {
	in := fixture()
	beforeFiles := append([]File{}, in.Files...)
	beforeModules := append([]Pin{}, in.Modules...)
	got, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	if in.Files[0] != beforeFiles[0] || in.Modules[0] != beforeModules[0] {
		t.Fatal("input reordered")
	}
	for i := 0; i < len(in.Files); i++ {
		for j := i; j < len(in.Files); j++ {
			in.Files[i], in.Files[j] = in.Files[j], in.Files[i]
			in.Modules[0], in.Modules[1] = in.Modules[1], in.Modules[0]
			in.Workflows[0], in.Workflows[1] = in.Workflows[1], in.Workflows[0]
			again, err := Render(in)
			if err != nil || !bytes.Equal(got, again) {
				t.Fatalf("order-dependent output: %v", err)
			}
		}
	}
	var decoded manifest
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Format != Format || len(decoded.Files) != 4 || decoded.Files[0].Path != "README.md" || decoded.GeneratorVersion != in.GeneratorVersion {
		t.Fatal("missing canonical metadata")
	}
	if bytes.Contains(got, []byte("synthetic public template bytes")) || got[len(got)-1] != '\n' {
		t.Fatal("baseline leak or missing terminator")
	}
	in.Files[0].Path = "changed"
	in.Baselines[in.TemplateBaselineSHA256][0] = 'X'
	if bytes.Contains(got, []byte("changed")) {
		t.Fatal("output retains mutable input")
	}
}

func TestManifestRejectPaths(t *testing.T) {
	for _, p := range []string{"../outside.go", "/outside.go", "a/../../outside.go", "a/../x.go", "./main.go", "a//b.go", "a/", "C:/x.go", `C:\x.go`, `a\b.go`, "%2e%2e/x.go", "a\x00b", "a\nb", "", ".env", "config/.env.production", ".git/config", "secrets/token", "credentials.json", "config/private.key", "server.pem", "tmp/a", "foo.log", "CON", "aux.txt", "com1.txt", "a.", "café.go", "a b.go", strings.Repeat("a", 513)} {
		t.Run(p, func(t *testing.T) {
			in := fixture()
			in.Files[0].Path = p
			got, err := Render(in)
			if !errors.Is(err, ErrPath) || got != nil {
				t.Fatalf("unsafe path accepted: %v", err)
			}
			if strings.Contains(err.Error(), p) && p != "" && len(p) > 4 {
				t.Fatal("error disclosed path")
			}
		})
	}
	for _, p := range []string{".github/workflows/validate.yml", ".gitignore", "internal/amosgen/main.go", "ui/style.css"} {
		in := fixture()
		in.Files[0].Path = p
		if _, err := Render(in); err != nil {
			t.Fatalf("valid path rejected: %v", err)
		}
	}
}

func TestManifestRejectAliases(t *testing.T) {
	for _, pair := range [][2]string{{"a.go", "a.go"}, {"a.go", "A.go"}, {"a", "a/b.go"}, {"A/B.go", "a"}} {
		in := fixture()
		in.Files[0].Path = pair[0]
		in.Files[1].Path = pair[1]
		if _, err := Render(in); !errors.Is(err, ErrAlias) {
			t.Fatalf("alias accepted: %v", err)
		}
	}
}

func TestManifestRejectReclassification(t *testing.T) {
	for _, p := range []string{"app/business", "app/business.go", "app/business/service.go", "api/business.yaml", "api/business/spec.yaml", "ui/overrides/home.html", "APP/BUSINESS/service.go"} {
		for _, class := range []Class{Generated, CopiedOnce} {
			in := fixture()
			in.Files[0].Path = p
			in.Files[0].Class = class
			if _, err := Render(in); !errors.Is(err, ErrAuthored) {
				t.Fatalf("authored path reclassified: %v", err)
			}
		}
	}
	in := fixture()
	in.Files[1].BaselineSHA256 = in.TemplateBaselineSHA256
	if _, err := Render(in); !errors.Is(err, ErrAuthored) {
		t.Fatal("authored baseline accepted")
	}
	in = fixture()
	in.Files[0].Class = "managed"
	if _, err := Render(in); !errors.Is(err, ErrClass) {
		t.Fatal("unknown class accepted")
	}
}

func TestManifestFrozenBusinessAuthoredRoots(t *testing.T) {
	for _, p := range []string{"business/operations/handler.go", "business/types/domain.go", "BUSINESS/OPERATIONS/handler.go", "Business/Types/domain.go"} {
		for _, class := range []Class{Generated, CopiedOnce} {
			t.Run(p+"/"+string(class), func(t *testing.T) {
				in := fixture()
				in.Files[0].Path = p
				in.Files[0].Class = class
				if got, err := Render(in); !errors.Is(err, ErrAuthored) || got != nil {
					t.Fatalf("frozen authored root reclassified: %v", err)
				}
			})
		}
	}
}

func TestManifestBaselineResolution(t *testing.T) {
	tests := map[string]func(*Input){
		"missing template":         func(in *Input) { in.TemplateBaselineSHA256 = strings.Repeat("b", 64) },
		"missing managed baseline": func(in *Input) { in.Files[0].BaselineSHA256 = "" },
		"unretrievable":            func(in *Input) { in.Files[0].BaselineSHA256 = strings.Repeat("b", 64) },
		"mismatched bytes":         func(in *Input) { in.Baselines[in.TemplateBaselineSHA256] = []byte("different") },
		"override missing":         func(in *Input) { in.Files[3].BaselineSHA256 = strings.Repeat("b", 64) },
		"malformed extra baseline": func(in *Input) { in.Baselines["bad"] = nil },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			in := fixture()
			mutate(&in)
			if got, err := Render(in); !errors.Is(err, ErrBaseline) || got != nil {
				t.Fatalf("bad baseline accepted: %v", err)
			}
		})
	}
	in := fixture()
	in.Files[3].BaselineSHA256 = ""
	if _, err := Render(in); err != nil {
		t.Fatalf("explicit override without baseline rejected: %v", err)
	}
	// A legitimate empty baseline resolves by map membership, not length.
	in = fixture()
	digest := Sum(nil)
	in.Baselines[digest] = nil
	in.Files[0].BaselineSHA256 = digest
	if _, err := Render(in); err != nil {
		t.Fatalf("empty baseline rejected: %v", err)
	}
}

func TestManifestMetadataAndPins(t *testing.T) {
	tests := map[string]func(*Input){
		"generator":          func(in *Input) { in.GeneratorVersion = "latest" },
		"schema":             func(in *Input) { in.ConfigSchemaVersion = "v0" },
		"file digest":        func(in *Input) { in.Files[0].SHA256 = "bad" },
		"module revision":    func(in *Input) { in.Modules[0].Revision = "main" },
		"module traversal":   func(in *Input) { in.Modules[0].Name = "github.com/../public" },
		"private host":       func(in *Input) { in.Modules[0].Name = "private.invalid/a/b" },
		"module credentials": func(in *Input) { in.Modules[0].Name = "github.com/user:secret/repo" },
		"workflow branch":    func(in *Input) { in.Workflows[0].Revision = "main" },
		"workflow zero":      func(in *Input) { in.Workflows[0].Revision = strings.Repeat("0", 40) },
		"workflow directory": func(in *Input) { in.Workflows[0].Name = "github.com/example/public/foo/validate.yml" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			in := fixture()
			mutate(&in)
			if got, err := Render(in); !errors.Is(err, ErrMetadata) || got != nil {
				t.Fatalf("bad metadata accepted: %v", err)
			}
		})
	}
	in := fixture()
	in.Modules[1] = in.Modules[0]
	if _, err := Render(in); !errors.Is(err, ErrAlias) {
		t.Fatal("duplicate module accepted")
	}
	in = fixture()
	in.Workflows[1] = in.Workflows[0]
	if _, err := Render(in); !errors.Is(err, ErrAlias) {
		t.Fatal("duplicate workflow accepted")
	}
}

func TestManifestBounds(t *testing.T) {
	for _, mutate := range []func(*Input){
		func(in *Input) { in.Files = make([]File, MaxFiles+1) },
		func(in *Input) { in.Modules = make([]Pin, MaxPins+1) },
		func(in *Input) { in.Workflows = make([]Pin, MaxPins+1) },
		func(in *Input) { in.Baselines = map[string][]byte{Sum(nil): make([]byte, MaxBaselineBytes+1)} },
	} {
		in := fixture()
		mutate(&in)
		if _, err := Render(in); !errors.Is(err, ErrLimit) {
			t.Fatalf("unbounded input accepted: %v", err)
		}
	}
}
