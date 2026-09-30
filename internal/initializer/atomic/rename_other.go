//go:build !darwin && !linux

package atomic

import "errors"

var ErrUnsupported = errors.New("atomic no-replace rename is unsupported on this platform")

func RenameNoReplace(string, string, string) error { return ErrUnsupported }
func DirectoryIdentity(string) (string, error)     { return "", ErrUnsupported }
