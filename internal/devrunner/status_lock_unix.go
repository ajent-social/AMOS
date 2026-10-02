//go:build darwin || linux

package devrunner

import (
	"errors"
	"os"
	"syscall"
)

func openPrivateLock(root *os.Root, name string) (*os.File, error) {
	if root == nil || !validPrivateLockName(name) {
		return nil, ErrProjectStateUnavailable
	}
	return root.OpenFile(name, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
}

func tryLockPrivate(file *os.File) (bool, error) {
	if file == nil {
		return false, ErrProjectStateUnavailable
	}
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func unlockPrivate(file *os.File) error {
	if file == nil {
		return ErrProjectStateUnavailable
	}
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
