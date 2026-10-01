//go:build !darwin

package devrunner

import (
	"context"
	"errors"
)

var errProcessProbeUnsupported = errors.New("exact local process identity is unsupported on this platform")

func defaultProcessProbe(context.Context, int, string, string) (ProcessIdentity, error) {
	return ProcessIdentity{}, errProcessProbeUnsupported
}
