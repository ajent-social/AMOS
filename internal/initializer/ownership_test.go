package initializer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInitializerRecordsAuthoredBusinessPreimages(t *testing.T) {
	input := testInput(t)
	result, err := Initialize(context.Background(), input, GeneratorFunc(func(ctx context.Context, c Config, files *Files) error {
		if err := files.WriteAuthoredFile(ctx, "app/business.go", []byte("package app\n"), 0644); err != nil {
			return err
		}
		return files.WriteFile(ctx, "go.mod", []byte("module "+c.ModulePath()+"\n"), 0644)
	}))
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for _, file := range result.Manifest.Files {
		owners[file.Path] = file.Owner
	}
	if owners["app/business.go"] != "authored" || owners["go.mod"] != "managed" {
		t.Fatal("business ownership was not preserved")
	}
}

func TestReadManagedFileRejectsAuthoredAndChangedPreimage(t *testing.T) {
	input := testInput(t)
	_, err := Initialize(context.Background(), input, GeneratorFunc(func(ctx context.Context, c Config, files *Files) error {
		if err := files.WriteAuthoredFile(ctx, "app/business.go", []byte("package app\n"), 0644); err != nil {
			return err
		}
		if data, _, err := files.ReadManagedFile(ctx, "app/business.go"); err == nil || len(data) != 0 {
			t.Fatal("authored preimage was admitted as managed authority")
		}
		if err := files.WriteFile(ctx, ".amos/runtime.json", []byte("private synthetic configuration"), 0600); err != nil {
			return err
		}
		data, exists, err := files.ReadManagedFile(ctx, ".amos/runtime.json")
		if err != nil || !exists || string(data) != "private synthetic configuration" {
			t.Fatal("managed preimage unavailable")
		}
		if err := os.WriteFile(filepath.Join(files.root, ".amos", "runtime.json"), []byte("changed synthetic configuration"), 0600); err != nil {
			return err
		}
		if data, _, err := files.ReadManagedFile(ctx, ".amos/runtime.json"); err == nil || len(data) != 0 {
			t.Fatal("changed managed preimage accepted")
		}
		return ErrGeneration
	}))
	if err == nil {
		t.Fatal("interrupted generation unexpectedly finalized")
	}
}

func TestInitializerRejectsControlCharacterTarget(t *testing.T) {
	for _, target := range []string{"app\nforged", "app\x1b[2J", "app\tother", "app\u0085other"} {
		input := testInput(t)
		input.Target = target
		if _, _, _, err := validateInput(input); err == nil {
			t.Fatal("control-character target accepted")
		}
	}
}
