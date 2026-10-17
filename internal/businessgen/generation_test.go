package businessgen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ajent-social/amos/internal/initializer"
)

func sourceFixture(t *testing.T) []byte {
	t.Helper()
	source, err := os.ReadFile("../codegen/testdata/typed/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func newFixture(t *testing.T) *Generator {
	t.Helper()
	g, err := New(sourceFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func publish(t *testing.T, g *Generator) string {
	t.Helper()
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	in := initializer.Input{SchemaVersion: initializer.SchemaVersion, AppSlug: "business-demo", ModulePath: "example.test/business-demo", ParentDir: parent, Target: "app", Modules: []string{"identity", "workspace"}, PublicOrigin: "http://127.0.0.1:8080", BusinessMode: "integrated-go", Mode: "evaluation"}
	result, err := initializer.Initialize(context.Background(), in, initializer.GeneratorFunc(func(ctx context.Context, c initializer.Config, files *initializer.Files) error {
		for _, name := range []string{"business/operations/handler.go", "business/types/domain.go", "ui/overrides/home.html"} {
			if err := files.WriteAuthoredFile(ctx, name, []byte("owner canary\n"), 0644); err != nil {
				return err
			}
		}
		return g.Generate(ctx, c, files)
	}))
	if err != nil || !result.Finalized {
		t.Fatalf("publication failed: %v", err)
	}
	return filepath.Join(parent, "app")
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = digest(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGenerationOwnership(t *testing.T) {
	g := newFixture(t)
	root := publish(t, g)
	// These are actual files in a finalized initializer tree, not an in-memory
	// simulation. Authored edits after publication must also remain untouched.
	for _, p := range []string{"business/operations/handler.go", "business/types/domain.go", "ui/overrides/home.html"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte("owner edited canary\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshot(t, root)
	for range 2 {
		if err := g.VerifyRepeat(context.Background(), root); err != nil {
			t.Fatal(err)
		}
	}
	after := snapshot(t, root)
	if len(before) != len(after) {
		t.Fatal("repeat changed file inventory")
	}
	for name, sum := range before {
		if after[name] != sum {
			t.Fatalf("repeat changed file: %s", name)
		}
	}
	changed := bytes.Replace(sourceFixture(t), []byte("widgets:create"), []byte("widgets:update"), 1)
	other, err := New(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.VerifyRepeat(context.Background(), root); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed contract accepted: %v", err)
	}
	for name, sum := range before {
		if snapshot(t, root)[name] != sum {
			t.Fatal("failed repeat wrote files")
		}
	}
	// The ownership record provides exact baseline digests, without source
	// bytes, host paths, credentials or authored file contents.
	data, err := os.ReadFile(filepath.Join(root, OutputDirectory, "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record generationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Format != journalFormat || len(record.Files) != 2 || record.SourceSHA256 != digest(sourceFixture(t)) || bytes.Contains(data, []byte(root)) {
		t.Fatal("bad generation ownership record")
	}
}

func TestGenerationRejectsFinalizedConflicts(t *testing.T) {
	tests := map[string]func(*testing.T, string){
		"unmanaged file": func(t *testing.T, r string) {
			write(t, filepath.Join(r, OutputDirectory, "unknown.go"), []byte("unmanaged"))
		},
		"changed preimage": func(t *testing.T, r string) {
			write(t, filepath.Join(r, OutputDirectory, "models_gen.go"), []byte("authored change"))
		},
		"missing ownership": func(t *testing.T, r string) {
			if err := os.Remove(filepath.Join(r, ".amos-manifest.json")); err != nil {
				t.Fatal(err)
			}
		},
		"tampered journal": func(t *testing.T, r string) {
			write(t, filepath.Join(r, OutputDirectory, "generation.json"), []byte("{}"))
		},
		"authored reclassification": func(t *testing.T, r string) {
			mutateManifest(t, r, func(m *initializer.Manifest) {
				for i := range m.Files {
					if m.Files[i].Path == OutputDirectory+"/models_gen.go" {
						m.Files[i].Owner = "authored"
					}
				}
			})
		},
		"duplicate ownership": func(t *testing.T, r string) {
			mutateManifest(t, r, func(m *initializer.Manifest) { m.Files = append(m.Files, m.Files[0]) })
		},
		"case alias ownership": func(t *testing.T, r string) {
			mutateManifest(t, r, func(m *initializer.Manifest) {
				f := m.Files[0]
				f.Path = strings.ToUpper(f.Path)
				m.Files = append(m.Files, f)
			})
		},
		"changed file mode": func(t *testing.T, r string) {
			if err := os.Chmod(filepath.Join(r, OutputDirectory, "models_gen.go"), 0600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			g := newFixture(t)
			r := publish(t, g)
			change(t, r)
			before := snapshot(t, r)
			if err := g.VerifyRepeat(context.Background(), r); !errors.Is(err, ErrConflict) {
				t.Fatalf("conflict accepted: %v", err)
			}
			after := snapshot(t, r)
			if len(after) != len(before) {
				t.Fatal("conflict mutated inventory")
			}
			for p, sum := range before {
				if after[p] != sum {
					t.Fatal("conflict mutated file")
				}
			}
		})
	}
}

func TestGenerationRejectsSymlinksAndNonregular(t *testing.T) {
	for _, name := range []string{"models_gen.go", "generation.json", "directory"} {
		t.Run(name, func(t *testing.T) {
			g := newFixture(t)
			r := publish(t, g)
			if name == "directory" {
				from := filepath.Join(r, OutputDirectory)
				if err := os.Rename(from, from+"-saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("business-saved", from); err != nil {
					t.Fatal(err)
				}
			} else {
				p := filepath.Join(r, OutputDirectory, name)
				data, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				write(t, p+"-saved", data)
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(name+"-saved", p); err != nil {
					t.Fatal(err)
				}
			}
			if err := g.VerifyRepeat(context.Background(), r); !errors.Is(err, ErrConflict) {
				t.Fatalf("symlink accepted: %v", err)
			}
		})
	}
	g := newFixture(t)
	r := publish(t, g)
	p := filepath.Join(r, OutputDirectory, "models_gen.go")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0700); err != nil {
		t.Fatal(err)
	}
	if err := g.VerifyRepeat(context.Background(), r); !errors.Is(err, ErrConflict) {
		t.Fatal("nonregular output accepted")
	}
}

func TestGenerationRejectsUnmanagedStagingCollision(t *testing.T) {
	g := newFixture(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	in := initializer.Input{SchemaVersion: initializer.SchemaVersion, AppSlug: "business-demo", ModulePath: "example.test/business-demo", ParentDir: parent, Target: "app", Modules: []string{"identity", "workspace"}, PublicOrigin: "http://127.0.0.1:8080", BusinessMode: "integrated-go", Mode: "evaluation"}
	_, err := initializer.Initialize(context.Background(), in, initializer.GeneratorFunc(func(ctx context.Context, c initializer.Config, f *initializer.Files) error {
		if err := f.WriteAuthoredFile(ctx, OutputDirectory+"/models_gen.go", []byte("owner collision"), 0644); err != nil {
			return err
		}
		return g.Generate(ctx, c, f)
	}))
	if err == nil {
		t.Fatal("authored collision was published")
	}
	if _, err := os.Stat(filepath.Join(parent, "app")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed transaction finalized target")
	}
}

func TestGenerationDeterministicBoundedContracts(t *testing.T) {
	source := sourceFixture(t)
	a, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	for i := range a.files {
		if a.files[i].name != b.files[i].name || !bytes.Equal(a.files[i].data, b.files[i].data) {
			t.Fatal("nondeterministic generator")
		}
	}
	for _, input := range [][]byte{nil, []byte("not OpenAPI"), make([]byte, (4<<20)+1), bytes.Replace(source, []byte("additionalProperties: false"), []byte("additionalProperties: true"), 1)} {
		if _, err := New(input); !errors.Is(err, ErrContract) {
			t.Fatal("unsupported contract accepted")
		}
	}
	if err := a.Generate(nil, initializer.Config{}, nil); !errors.Is(err, ErrCanceled) {
		t.Fatal("missing context accepted")
	}
	if err := a.VerifyRepeat(nil, ""); !errors.Is(err, ErrCanceled) {
		t.Fatal("missing repeat context accepted")
	}
}

func write(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
}
func mutateManifest(t *testing.T, r string, change func(*initializer.Manifest)) {
	t.Helper()
	p := filepath.Join(r, ".amos-manifest.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m initializer.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	change(&m)
	data, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	write(t, p, data)
}

// This test always builds and runs a real external generated consumer. It does
// not skip on missing Go/dependencies. Coordinators must run it under their
// required build-resource admission, or report the missing check explicitly.
func TestGeneratedExternalConsumer(t *testing.T) {
	g := newFixture(t)
	root := publish(t, g)
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	module, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	module = bytes.Replace(module, []byte("module github.com/ajent-social/amos"), []byte("module example.test/business-demo"), 1)
	module = append(module, []byte("\nrequire github.com/ajent-social/amos v0.0.0\nreplace github.com/ajent-social/amos => "+strconv.Quote(repo)+"\n")...)
	write(t, filepath.Join(root, "go.mod"), module)
	sums, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "go.sum"), sums)
	// Keep authored implementation in its frozen owner-controlled destination.
	write(t, filepath.Join(root, "business/operations/handler.go"), []byte(consumerImplementation))
	write(t, filepath.Join(root, "business/types/domain.go"), []byte("package types\n"))
	write(t, filepath.Join(root, "consumer_test.go"), []byte(consumerTest))
	cmd := exec.Command("go", "test", "-json", "-mod=readonly", "-count=1", ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("external generated consumer failed: %v\nstdout:\n%s\nstderr:\n%s", err, output, diagnostics.Bytes())
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	passes := 0
	for {
		var event struct{ Action, Test string }
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("invalid consumer result: %v", err)
		}
		if event.Test != "" && event.Action == "pass" {
			passes++
		}
		if event.Action == "skip" || event.Action == "fail" {
			t.Fatalf("consumer reported %s", event.Action)
		}
	}
	if passes != 10 {
		t.Fatalf("expected ten named consumer passes, got %d", passes)
	}
	t.Logf("external consumer: %d named passes, zero failures/skips", passes)
	if err := g.VerifyRepeat(context.Background(), root); err != nil {
		t.Fatalf("authored external consumer broke repeat: %v", err)
	}
}
