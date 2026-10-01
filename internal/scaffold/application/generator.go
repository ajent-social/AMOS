// Package application generates the real, integrated-Go local reference app.
package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ajent-social/amos/internal/initializer"
	"github.com/ajent-social/amos/internal/scaffold/local"
	"github.com/google/uuid"
)

const MaxFrameworkFiles = 128
const initializerFileBudget = 128
const generatedFileOverhead = 16
const maxFrameworkFileBytes = 8 << 20
const maxFrameworkBytes = 64 << 20

var ErrFrameworkUnavailable = errors.New("trusted AMOS runtime source is unavailable")

// Generator copies only the static import closure needed by the local runtime.
// SourceDir must be an operator-selected public AMOS checkout.
type Generator struct{ SourceDir string }

var roots = []string{"apphost", "examples/reference/app/business", "examples/reference/ui", "examples/reference/migrations", "migrations"}

func (g Generator) Generate(ctx context.Context, c initializer.Config, files *initializer.Files) error {
	if ctx == nil || files == nil || c.Mode() != "evaluation" || c.BusinessMode() != "integrated-go" {
		return initializer.ErrInvalidInput
	}
	modules := map[string]bool{}
	for _, m := range c.Modules() {
		modules[m] = true
	}
	if !modules["identity"] || !modules["workspace"] {
		return initializer.ErrInvalidInput
	}
	if modules["billing"] {
		return initializer.ErrGeneratorUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	closure, err := selectFramework(g.SourceDir)
	if err != nil {
		return err
	}
	ownerSource := make(map[string][]byte)
	rewrite := strings.NewReplacer("github.com/ajent-social/amos/examples/reference/app/business", c.ModulePath()+"/app/business", "github.com/ajent-social/amos/examples/reference/ui", c.ModulePath()+"/ui", "github.com/ajent-social/amos/examples/reference/migrations", c.ModulePath()+"/migrations")
	for name, data := range closure {
		for _, mapping := range []struct{ from, to string }{{"examples/reference/app/business/", "app/business/"}, {"examples/reference/ui/", "ui/"}, {"examples/reference/migrations/", "migrations/"}} {
			if strings.HasPrefix(name, mapping.from) {
				target := mapping.to + strings.TrimPrefix(name, mapping.from)
				content := data
				if strings.HasSuffix(name, ".go") {
					content = []byte(rewrite.Replace(string(data)))
				}
				ownerSource[target] = content
			}
		}
	}
	if len(closure) > MaxFrameworkFiles || len(closure)+generatedFileOverhead+len(ownerSource) > initializerFileBudget {
		return fmt.Errorf("framework runtime closure exceeds bounded initializer file limit")
	}
	if err := (local.Generator{}).Generate(ctx, c, files); err != nil {
		return err
	}
	persisted, exists, err := files.ReadManagedFile(ctx, ".amos/runtime.json")
	if err != nil {
		return err
	}
	var private string
	if exists {
		private = string(persisted)
	} else {
		ids := make([]string, 3)
		for i := range ids {
			id, e := uuid.NewV7()
			if e != nil {
				return initializer.ErrGeneration
			}
			ids[i] = id.String()
		}
		material, rate := make([]byte, 32), make([]byte, 32)
		if _, err = rand.Read(material); err != nil {
			return initializer.ErrGeneration
		}
		if _, err = rand.Read(rate); err != nil {
			return initializer.ErrGeneration
		}
		private = fmt.Sprintf("{\n  \"schemaVersion\": 1,\n  \"installationId\": %q,\n  \"applicationId\": %q,\n  \"environmentId\": %q,\n  \"origin\": %q,\n  \"materialKey\": %q,\n  \"rateKey\": %q\n}\n", ids[0], ids[1], ids[2], c.PublicOrigin(), hex.EncodeToString(material), hex.EncodeToString(rate))
	}
	ownerFiles := map[string]string{
		"cmd/app/main.go":         rewrite.Replace(serveMain),
		"cmd/app/migrate.go":      rewrite.Replace(migrateMain),
		"cmd/app/runtime_test.go": runtimeTests,
		"go.mod":                  generatedMod(c.ModulePath(), closure["go.mod"]),
		"go.sum":                  string(closure["go.sum"]),
		"LICENSE":                 string(closure["LICENSE"]) + "\n\nThis application includes AMOS components licensed under Apache-2.0. See NOTICE for dependency attributions.\n",
		"NOTICE":                  string(closure["NOTICE"]),
		"THIRD_PARTY_NOTICES.md":  string(closure["THIRD_PARTY_NOTICES.md"]),
	}
	for name, value := range ownerFiles {
		if strings.HasSuffix(name, ".go") {
			data, err := format.Source([]byte(value))
			if err != nil {
				return initializer.ErrGeneration
			}
			value = string(data)
		}
		if err := files.WriteFile(ctx, name, []byte(value), 0644); err != nil {
			return err
		}
	}
	for name, data := range ownerSource {
		if strings.HasSuffix(name, ".go") {
			var err error
			data, err = format.Source(data)
			if err != nil {
				return initializer.ErrGeneration
			}
		}
		if err := files.WriteAuthoredFile(ctx, name, data, 0644); err != nil {
			return err
		}
	}
	if err := files.WriteFile(ctx, ".amos/runtime.json", []byte(private), 0600); err != nil {
		return err
	}
	for name, data := range closure {
		if err := files.WriteFile(ctx, filepath.ToSlash(filepath.Join(".amos/framework", name)), data, 0644); err != nil {
			return err
		}
	}
	return nil
}

// ValidateSource performs bounded source analysis without executing code.
func ValidateSource(source string) error { _, err := selectFramework(source); return err }

func selectFramework(root string) (map[string][]byte, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, ErrFrameworkUnavailable
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrFrameworkUnavailable
	}
	mod, err := readRegular(abs, "go.mod")
	if err != nil || !strings.HasPrefix(string(mod), "module github.com/ajent-social/amos\n") {
		return nil, ErrFrameworkUnavailable
	}
	if strings.Contains(string(mod), "replace ") || strings.Contains(string(mod), "=> /") || strings.Contains(string(mod), "file://") {
		return nil, ErrFrameworkUnavailable
	}
	selected := map[string][]byte{}
	var totalBytes int64
	queue := append([]string(nil), roots...)
	packages := map[string]bool{}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if packages[pkg] {
			continue
		}
		packages[pkg] = true
		dir := filepath.Join(abs, filepath.FromSlash(pkg))
		if !safeDirectory(abs, pkg) {
			return nil, ErrFrameworkUnavailable
		}
		entries, e := os.ReadDir(dir)
		if e != nil {
			return nil, ErrFrameworkUnavailable
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				return nil, ErrFrameworkUnavailable
			}
			n := entry.Name()
			if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			path := filepath.Join(dir, n)
			b, e := os.ReadFile(path)
			if e != nil {
				return nil, ErrFrameworkUnavailable
			}
			rel, _ := filepath.Rel(abs, path)
			rel = filepath.ToSlash(rel)
			if !cleanSelected(rel) || len(selected) >= MaxFrameworkFiles {
				return nil, ErrFrameworkUnavailable
			}
			totalBytes += int64(len(b))
			if len(b) > maxFrameworkFileBytes || totalBytes > maxFrameworkBytes {
				return nil, ErrFrameworkUnavailable
			}
			selected[rel] = b
			f, e := parser.ParseFile(token.NewFileSet(), path, b, parser.ImportsOnly)
			if e != nil {
				return nil, ErrFrameworkUnavailable
			}
			for _, im := range f.Imports {
				p := strings.Trim(im.Path.Value, "\"")
				prefix := "github.com/ajent-social/amos/"
				if strings.HasPrefix(p, prefix) {
					child := strings.TrimPrefix(p, prefix)
					if !safeDirectory(abs, child) {
						return nil, ErrFrameworkUnavailable
					}
					if !packages[child] {
						queue = append(queue, child)
					}
				}
			}
		}
	}
	// Runtime packages in this closure embed the reference migration and UI assets.
	for _, name := range []string{"LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "go.mod", "go.sum"} {
		b, e := readRegular(abs, name)
		if e != nil {
			return nil, ErrFrameworkUnavailable
		}
		selected[name] = b
		totalBytes += int64(len(b))
		if totalBytes > maxFrameworkBytes {
			return nil, ErrFrameworkUnavailable
		}
	}
	// Copy embed inputs declared by selected Go files, bounded to the source tree.
	for name, data := range selected {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		for _, pattern := range embedPatterns(data) {
			matches, e := filepath.Glob(filepath.Join(abs, filepath.FromSlash(filepath.Dir(name)), pattern))
			if e != nil || len(matches) == 0 {
				return nil, ErrFrameworkUnavailable
			}
			for _, match := range matches {
				rel, e := filepath.Rel(abs, match)
				if e != nil {
					return nil, ErrFrameworkUnavailable
				}
				rel = filepath.ToSlash(rel)
				if !cleanSelected(rel) {
					return nil, ErrFrameworkUnavailable
				}
				b, e := readRegular(abs, rel)
				if e != nil {
					return nil, ErrFrameworkUnavailable
				}
				if _, exists := selected[rel]; !exists {
					totalBytes += int64(len(b))
					if len(b) > maxFrameworkFileBytes || totalBytes > maxFrameworkBytes {
						return nil, ErrFrameworkUnavailable
					}
				}
				selected[rel] = b
			}
		}
	}
	if len(selected) > MaxFrameworkFiles {
		return nil, fmt.Errorf("framework runtime closure exceeds file limit (%d)", len(selected))
	}
	keys := make([]string, 0, len(selected))
	for k := range selected {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !cleanSelected(k) {
			return nil, ErrFrameworkUnavailable
		}
	}
	return selected, nil
}

func safeDirectory(root, rel string) bool {
	if !cleanSelected(rel) {
		return false
	}
	current := root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		current = filepath.Join(current, part)
		i, err := os.Lstat(current)
		if err != nil || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

func readRegular(root, name string) ([]byte, error) {
	parts := strings.Split(filepath.ToSlash(name), "/")
	current := root
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fs.ErrPermission
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, fs.ErrPermission
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, fs.ErrNotExist
		}
		if i == len(parts)-1 && info.Size() > maxFrameworkFileBytes {
			return nil, fs.ErrInvalid
		}
	}
	p := filepath.Join(root, filepath.FromSlash(name))
	i, e := os.Lstat(p)
	if e != nil || !i.Mode().IsRegular() {
		return nil, fs.ErrNotExist
	}
	if i.Size() > maxFrameworkFileBytes {
		return nil, fs.ErrInvalid
	}
	return os.ReadFile(p)
}

var safePath = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)

func cleanSelected(p string) bool {
	return safePath.MatchString(p) && !strings.Contains(p, "..") && !strings.HasPrefix(p, ".")
}
func embedPatterns(data []byte) []string {
	var out []string
	f, e := parser.ParseFile(token.NewFileSet(), "source.go", data, parser.ParseComments)
	if e != nil {
		return out
	}
	for _, g := range f.Comments {
		for _, c := range g.List {
			line := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			if strings.HasPrefix(line, "go:embed ") {
				out = append(out, strings.Fields(strings.TrimPrefix(line, "go:embed "))...)
			}
		}
	}
	return out
}
func generatedMod(module string, frameworkMod []byte) string {
	raw := string(frameworkMod)
	var deps []string
	in := false
	for _, line := range strings.Split(raw, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "require (") {
			in = true
			continue
		}
		if in && s == ")" {
			in = false
			continue
		}
		if in && s != "" && !strings.HasPrefix(s, "//") {
			deps = append(deps, s)
		}
		if !in && strings.HasPrefix(s, "require ") {
			deps = append(deps, strings.TrimPrefix(s, "require "))
		}
	}
	sort.Strings(deps)
	var bld strings.Builder
	fmt.Fprintf(&bld, "module %s\n\ngo 1.27.0\n\nrequire (\n\tgithub.com/ajent-social/amos v0.0.0\n", module)
	for _, d := range deps {
		fmt.Fprintf(&bld, "\t%s\n", d)
	}
	bld.WriteString(")\n\nreplace github.com/ajent-social/amos => ./.amos/framework\n")
	return bld.String()
}
