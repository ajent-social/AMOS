//go:build linux

package atomic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"golang.org/x/sys/unix"
	"path/filepath"
)

// RenameNoReplace atomically moves oldPath to newPath only when the destination
// does not exist. It never falls back to a check followed by ordinary rename.
func RenameNoReplace(oldPath, newPath, expectedParent string) error {
	oldParent, newParent := filepath.Dir(oldPath), filepath.Dir(newPath)
	oldBase, newBase := filepath.Base(oldPath), filepath.Base(newPath)
	if oldParent != newParent || oldBase == "." || oldBase == ".." || oldBase == "" || newBase == "." || newBase == ".." || newBase == "" {
		return unix.EINVAL
	}
	fd, err := unix.Open(oldParent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	statErr := unix.Fstat(fd, &stat)
	if statErr == nil {
		identity := fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
		sum := sha256.Sum256([]byte(identity))
		if hex.EncodeToString(sum[:]) != expectedParent {
			statErr = unix.ESTALE
		}
	}
	if statErr == nil {
		statErr = unix.Renameat2(fd, filepath.Base(oldPath), fd, filepath.Base(newPath), unix.RENAME_NOREPLACE)
	}
	closeErr := unix.Close(fd)
	if statErr != nil {
		return statErr
	}
	if closeErr != nil {
		return nil
	} // rename already committed; closing the directory handle cannot undo it.
	return nil
}

// DirectoryIdentity returns an opaque stable identity for a directory inode.
func DirectoryIdentity(path string) (string, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return "", err
	}
	return opaqueIdentity(fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)), nil
}

func opaqueIdentity(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

func renameAtExclusive(dir int, oldName, newName string) error {
	return unix.Renameat2(dir, oldName, dir, newName, unix.RENAME_NOREPLACE)
}
