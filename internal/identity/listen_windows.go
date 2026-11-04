package identity

import (
	"context"
	"net"
	"syscall"
)

// ListenExclusive opens a TCP4 listener at address with Winsock
// SO_EXCLUSIVEADDRUSE set before binding. Option/bind failures return no listener;
// it neither evicts an existing owner nor selects a fallback port.
// The caller owns the listener and must close it under its lifecycle guard.
func ListenExclusive(address string) (*net.TCPListener, error) {
	config := net.ListenConfig{
		Control: func(_, _ string, raw syscall.RawConn) error {
			var optionErr error
			err := raw.Control(func(socket uintptr) {
				// Winsock SO_EXCLUSIVEADDRUSE is the bitwise complement of SO_REUSEADDR.
				const exclusiveAddressUse = ^syscall.SO_REUSEADDR
				optionErr = syscall.SetsockoptInt(syscall.Handle(socket), syscall.SOL_SOCKET, exclusiveAddressUse, 1)
			})
			if err != nil {
				return err
			}
			return optionErr
		},
	}
	listener, err := config.Listen(context.Background(), "tcp4", address)
	if err != nil {
		return nil, err
	}
	return listener.(*net.TCPListener), nil
}
