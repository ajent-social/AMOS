//go:build !darwin && !linux

package devrunner

import "os"

func openPrivateLock(*os.Root, string) (*os.File, error) {
	return nil, ErrProjectStateUnavailable
}

func tryLockPrivate(*os.File) (bool, error) {
	return false, ErrProjectStateUnavailable
}

func unlockPrivate(*os.File) error {
	return ErrProjectStateUnavailable
}
