//go:build darwin || linux

package devrunner

import (
	"errors"
	"os"
	"syscall"
)

func openPrivateStateUpdateLock(root *os.Root) (*os.File, error) {
	if root == nil {
		return nil, ErrProjectStateUnavailable
	}
	return root.OpenFile(stateLockName, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
}

func tryLockPrivateStateUpdate(file *os.File) (bool, error) {
	if file == nil {
		return false, ErrProjectStateUnavailable
	}
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func unlockPrivateStateUpdate(file *os.File) error {
	if file == nil {
		return ErrProjectStateUnavailable
	}
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
