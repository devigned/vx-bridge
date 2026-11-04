package bridge

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

func peerReset(err error) bool {
	return errors.Is(err, syscall.Errno(windows.WSAECONNRESET))
}
