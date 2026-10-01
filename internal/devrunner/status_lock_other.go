//go:build !darwin && !linux

package devrunner

import "os"

func openPrivateStateUpdateLock(*os.Root) (*os.File, error) {
	return nil, ErrProjectStateUnavailable
}

func tryLockPrivateStateUpdate(*os.File) (bool, error) {
	return false, ErrProjectStateUnavailable
}

func unlockPrivateStateUpdate(*os.File) error {
	return ErrProjectStateUnavailable
}
