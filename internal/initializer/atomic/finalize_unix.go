//go:build darwin || linux

package atomic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// SecureParent rejects a finalization directory writable by another local user.
// Descriptor-based finalization independently repeats these checks.
func SecureParent(path string) error {
	fd, err := secureParentFD(path)
	if err != nil {
		return err
	}
	return unix.Close(fd)
}

// Every ancestor must resist replacement by a foreign local user. A sticky
// directory is allowed only when owned by root/current UID and all traversed
// entries are also root/current-UID owned. The final parent must be owner-only
// writable. Opening each component relative to the preceding fd never follows
// a substituted intermediate symlink.
func secureParentFD(path string) (int, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return -1, err
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	fail := func(e error) (int, error) { _ = unix.Close(fd); return -1, e }
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(absolute), "/"), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fail(unix.EINVAL)
		}
		var parent unix.Stat_t
		if err = unix.Fstat(fd, &parent); err != nil {
			return fail(err)
		}
		uid := uint32(os.Geteuid())
		if parent.Uid != 0 && parent.Uid != uid {
			return fail(unix.EPERM)
		}
		if parent.Mode&0022 != 0 && parent.Mode&unix.S_ISVTX == 0 {
			return fail(unix.EPERM)
		}
		child, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			return fail(openErr)
		}
		var entry unix.Stat_t
		if err = unix.Fstat(child, &entry); err != nil {
			_ = unix.Close(child)
			return fail(err)
		}
		if entry.Uid != 0 && entry.Uid != uid {
			_ = unix.Close(child)
			return fail(unix.EPERM)
		}
		_ = unix.Close(fd)
		fd = child
	}
	var final unix.Stat_t
	if err = unix.Fstat(fd, &final); err != nil {
		return fail(err)
	}
	if final.Uid != uint32(os.Geteuid()) || final.Mode&0022 != 0 {
		return fail(unix.EPERM)
	}
	return fd, nil
}

func statIdentity(stat unix.Stat_t) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)))
	return hex.EncodeToString(sum[:])
}

// FinalizeNoReplace binds chmod and rename to the validated source inode and
// parent directory. A replaced/symlink source is rejected without following it.
// committed is true even if the post-rename durability sync fails.
func FinalizeNoReplace(oldPath, newPath, expectedParent, expectedSource string) (committed bool, err error) {
	parent := filepath.Dir(oldPath)
	if parent != filepath.Dir(newPath) || expectedSource == "" {
		return false, unix.EINVAL
	}
	dir, err := secureParentFD(parent)
	if err != nil {
		return false, err
	}
	defer func() { _ = unix.Close(dir) }()
	var parentStat unix.Stat_t
	if err = unix.Fstat(dir, &parentStat); err != nil {
		return false, err
	}
	if statIdentity(parentStat) != expectedParent {
		return false, unix.ESTALE
	}
	if parentStat.Uid != uint32(os.Geteuid()) || parentStat.Mode&0022 != 0 {
		return false, unix.EPERM
	}
	source, err := unix.Openat(dir, filepath.Base(oldPath), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	defer func() { _ = unix.Close(source) }()
	changedMode := false
	defer func() {
		if !committed && changedMode {
			_ = unix.Fchmod(source, 0700)
		}
	}()
	var sourceStat unix.Stat_t
	if err = unix.Fstat(source, &sourceStat); err != nil {
		return false, err
	}
	if sourceStat.Uid != uint32(os.Geteuid()) || sourceStat.Mode&0777 != 0700 || statIdentity(sourceStat) != expectedSource {
		return false, unix.ESTALE
	}
	if err = unix.Fchmod(source, 0755); err != nil {
		return false, err
	}
	changedMode = true
	var entry unix.Stat_t
	if err = unix.Fstatat(dir, filepath.Base(oldPath), &entry, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return false, err
	}
	if entry.Mode&unix.S_IFMT != unix.S_IFDIR || statIdentity(entry) != expectedSource {
		return false, unix.ESTALE
	}
	if err = renameAtExclusive(dir, filepath.Base(oldPath), filepath.Base(newPath)); err != nil {
		return false, err
	}
	return true, unix.Fsync(dir)
}
