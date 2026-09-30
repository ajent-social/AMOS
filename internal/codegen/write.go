package codegen

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WriteManifest atomically writes a manifest. Existing files are replaced only
// when they are regular files carrying this generator's manifest marker.
func WriteManifest(path string, content []byte) error {
	if path == "" {
		return errors.New("manifest output path is required")
	}
	if err := validateManifestBytes(content); err != nil {
		return fmt.Errorf("refusing to write invalid generated manifest: %w", err)
	}
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil {
		return errors.New("unable to inspect manifest directory")
	}
	if !info.IsDir() {
		return errors.New("manifest parent is not a directory")
	}

	if current, err := os.Lstat(path); err == nil {
		if !current.Mode().IsRegular() {
			return errors.New("refusing to replace non-regular manifest target")
		}
		existing, err := os.ReadFile(path)
		if err != nil {
			return errors.New("unable to read existing manifest target")
		}
		if err := validateManifestBytes(existing); err != nil {
			return errors.New("refusing to overwrite a file not owned by the generator")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("unable to inspect manifest target")
	}

	temporary, err := os.CreateTemp(parent, ".amos-manifest-*.tmp")
	if err != nil {
		return errors.New("unable to create temporary manifest")
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }() // The temp name is absent after a successful rename.
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return errors.New("unable to set manifest permissions")
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return errors.New("unable to write temporary manifest")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return errors.New("unable to sync temporary manifest")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("unable to close temporary manifest")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.New("unable to atomically replace generated manifest")
	}
	return nil
}

func validateManifestBytes(content []byte) error {
	var header struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(content, &header); err != nil {
		return err
	}
	if header.Format != ManifestVersion {
		return fmt.Errorf("format marker %q is not %q", header.Format, ManifestVersion)
	}
	return nil
}
