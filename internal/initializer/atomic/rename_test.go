package atomic

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameNoReplacePreservesExistingDestination(t *testing.T) {
	dir := realTempDir(t)
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
	dir := realTempDir(t)
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
	base := realTempDir(t)
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

func TestFinalizeRejectsReplacedSourceAndDoesNotChmodSymlinkTarget(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(fmt.Sprintf("symlink_%v", symlink), func(t *testing.T) {
			dir := realTempDir(t)
			stage := filepath.Join(dir, "stage")
			target := filepath.Join(dir, "target")
			if err := os.Mkdir(stage, 0700); err != nil {
				t.Fatal(err)
			}
			parent, err := DirectoryIdentity(dir)
			if err != nil {
				t.Fatal(err)
			}
			source, err := DirectoryIdentity(stage)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Rename(stage, filepath.Join(dir, "original")); err != nil {
				t.Fatal(err)
			}
			outside := realTempDir(t)
			if symlink {
				err = os.Symlink(outside, stage)
			} else {
				err = os.Mkdir(stage, 0750)
			}
			if err != nil {
				t.Fatal(err)
			}
			committed, err := FinalizeNoReplace(stage, target, parent, source)
			if err == nil || committed {
				t.Fatalf("source substitution committed=%v err=%v", committed, err)
			}
			if info, e := os.Stat(outside); e != nil || info.Mode().Perm() != 0700 {
				t.Fatalf("outside permissions changed: %v %v", info, e)
			}
			if !symlink {
				if info, e := os.Stat(stage); e != nil || info.Mode().Perm() != 0750 {
					t.Fatalf("replacement directory permissions changed: %v %v", info, e)
				}
			}
			if _, err = os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("published replaced source: %v", err)
			}
		})
	}
}

func TestFinalizeRejectsGroupWritableParent(t *testing.T) {
	dir := realTempDir(t)
	if err := os.Chmod(dir, 0770); err != nil {
		t.Fatal(err)
	}
	if err := SecureParent(dir); err == nil {
		t.Fatal("accepted group-writable parent")
	}
}

func TestFinalizeRejectsWritableAncestorAndSymlinkAncestor(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(fmt.Sprintf("symlink_%v", symlink), func(t *testing.T) {
			ancestor := realTempDir(t)
			parent := filepath.Join(ancestor, "parent")
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			stage := filepath.Join(parent, "stage")
			if err := os.Mkdir(stage, 0700); err != nil {
				t.Fatal(err)
			}
			pi, err := DirectoryIdentity(parent)
			if err != nil {
				t.Fatal(err)
			}
			si, err := DirectoryIdentity(stage)
			if err != nil {
				t.Fatal(err)
			}
			old := ancestor + "-owned-move"
			t.Cleanup(func() {
				if err := os.Rename(old, ancestor); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Error(err)
				}
			})
			if symlink {
				if err = os.Rename(ancestor, old); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(old, ancestor); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := os.Remove(ancestor); err != nil {
						t.Error(err)
					}
				}()
			} else {
				if err = os.Chmod(ancestor, 0770); err != nil {
					t.Fatal(err)
				}
			}
			committed, err := FinalizeNoReplace(stage, filepath.Join(parent, "target"), pi, si)
			if err == nil || committed {
				t.Fatalf("unsafe ancestor accepted committed=%v err=%v", committed, err)
			}
		})
	}
}

func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFinalizeOriginalSourceCommits(t *testing.T) {
	dir := realTempDir(t)
	stage := filepath.Join(dir, "stage")
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	parent, err := DirectoryIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	source, err := DirectoryIdentity(stage)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := FinalizeNoReplace(stage, target, parent, source)
	if err != nil || !committed {
		t.Fatalf("finalize committed=%v err=%v", committed, err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("final target permissions %v %v", info, err)
	}
}
