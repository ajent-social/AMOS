package atomic

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameNoReplacePreservesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "stage")
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("staged"), 0600); err != nil {
		t.Fatal(err)
	}
	identity, err := DirectoryIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameNoReplace(source, target, identity); err != nil {
		t.Fatalf("first rename: %v", err)
	}
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RenameNoReplace(source, target, identity); err == nil {
		t.Fatal("existing target was replaced")
	}
	got, err := os.ReadFile(filepath.Join(target, "file"))
	if err != nil || string(got) != "staged" {
		t.Fatalf("target bytes=%q err=%v", got, err)
	}
	if _, err := os.Stat(source); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source state: %v", err)
	}
}

func TestRenameNoReplaceRejectsEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "stage")
	target := filepath.Join(dir, "authored-empty-target")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "generated"), []byte("generated"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	identity, err := DirectoryIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameNoReplace(source, target, identity); err == nil {
		t.Fatal("rename replaced an existing empty directory")
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("authored empty destination changed: %v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(source, "generated")); err != nil {
		t.Fatalf("source was moved despite conflict: %v", err)
	}
}

func TestRenameNoReplaceRejectsReplacedParent(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "selected")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	identity, err := DirectoryIdentity(parent)
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(base, "moved")
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(parent, "stage")
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := RenameNoReplace(source, target, identity); err == nil {
		t.Fatal("rename accepted a replaced parent directory")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source moved after parent replacement: %v", err)
	}
}
