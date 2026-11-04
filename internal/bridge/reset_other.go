//go:build !windows

package bridge

import (
	"errors"
	"syscall"
)

func peerReset(err error) bool { return errors.Is(err, syscall.ECONNRESET) }
