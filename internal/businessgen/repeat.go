package businessgen

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/ajent-social/amos/internal/initializer"
	"github.com/ajent-social/amos/internal/initializer/atomic"
)

// VerifyRepeat performs a real finalized-tree repeat, accepting only identical
// output with the initializer's managed ownership and exact recorded preimages.
// It writes nothing, even on success. Changed source, missing/tampered manifests,
// symlinks, extra output files and authored collisions fail closed. This is not
// an updater and does not claim durable exclusion against an owner concurrently
// editing their own tree; callers must serialize generation with owner edits.
func (g *Generator) VerifyRepeat(ctx context.Context, project string) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrCanceled
	}
	if g == nil || len(g.files) != 3 {
		return ErrContract
	}
	if atomic.SecureParent(project) != nil {
		return ErrConflict
	}
	root, err := os.OpenRoot(project)
	if err != nil {
		return ErrConflict
	}
	defer func() { _ = root.Close() }()
	for _, dir := range []string{"internal", "internal/amosgen", OutputDirectory} {
		info, err := root.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrConflict
		}
	}
	manifestBytes, err := readRegular(root, ".amos-manifest.json", 512<<10)
	if err != nil {
		return ErrConflict
	}
	var manifest initializer.Manifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(new(any)) != io.EOF || manifest.SchemaVersion != initializer.SchemaVersion || manifest.TemplateVersion != initializer.TemplateVersion || len(manifest.Files) > initializer.MaxGeneratedFiles {
		return ErrConflict
	}
	owned := make(map[string]initializer.FileRecord)
	seen := make(map[string]bool)
	for _, record := range manifest.Files {
		fold := strings.ToLower(record.Path)
		if seen[fold] {
			return ErrConflict
		}
		seen[fold] = true
		if fold == OutputDirectory || strings.HasPrefix(fold, OutputDirectory+"/") {
			if !strings.HasPrefix(record.Path, OutputDirectory+"/") {
				return ErrConflict
			}
			owned[record.Path] = record
		}
	}
	if len(owned) != len(g.files) {
		return ErrConflict
	}
	for _, expected := range g.files {
		if ctx.Err() != nil {
			return ErrCanceled
		}
		name := OutputDirectory + "/" + expected.name
		record, ok := owned[name]
		if !ok || record.Owner != "managed" || record.Mode != 0644 || record.SHA256 != digest(expected.data) || record.Size != int64(len(expected.data)) {
			return ErrConflict
		}
		current, err := readRegular(root, name, len(expected.data))
		if err != nil || !bytes.Equal(current, expected.data) {
			return ErrConflict
		}
	}
	// Directory inventory is bounded, including unknown entries. A recursive
	// traversal would accept a broader namespace than the fixed output set.
	dir, err := root.Open(OutputDirectory)
	if err != nil {
		return ErrConflict
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(len(g.files) + 1)
	if err != nil && err != io.EOF || len(entries) != len(g.files) {
		return ErrConflict
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return ErrConflict
		}
		if _, ok := owned[OutputDirectory+"/"+entry.Name()]; !ok {
			return ErrConflict
		}
	}
	return nil
}

func readRegular(root *os.Root, name string, max int) ([]byte, error) {
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != 0644 || before.Size() > int64(max) {
		return nil, ErrConflict
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, ErrConflict
	}
	defer func() { _ = f.Close() }()
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, ErrConflict
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil || len(data) > max {
		return nil, ErrConflict
	}
	return data, nil
}
