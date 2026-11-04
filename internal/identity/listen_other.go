//go:build !windows

package identity

import (
	"context"
	"net"
)

// ListenExclusive opens an ordinary TCP4 listener at address on non-Windows
// systems, returning bind errors. It exists for ephemeral-port automated tests
// and does not supply Winsock exclusive-address semantics. The caller owns it.
func ListenExclusive(address string) (*net.TCPListener, error) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp4", address)
	if err != nil {
		return nil, err
	}
	return listener.(*net.TCPListener), nil
}
