//go:build darwin || linux

package localcapture

import (
	"os"
	"syscall"
)

func owned(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Getuid())
}
func privateDirectory(info os.FileInfo) bool {
	return info.IsDir() && info.Mode().Perm() == 0700 && owned(info)
}
func openPrivate(root *os.Root, name string) (*os.File, error) {
	f, e := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return nil, e
	}
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || !owned(st) {
		_ = f.Close()
		return nil, ErrUnavailable
	}
	return f, nil
}
