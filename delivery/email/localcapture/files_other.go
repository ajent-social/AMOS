//go:build !darwin && !linux

package localcapture

import "os"

func privateDirectory(os.FileInfo) bool              { return false }
func openPrivate(*os.Root, string) (*os.File, error) { return nil, ErrUnavailable }
