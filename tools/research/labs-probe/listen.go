package main

import (
	"net"

	"github.com/devigned/vx-bridge/internal/identity"
)

func listenExclusive(address string) (*net.TCPListener, error) {
	return identity.ListenExclusive(address)
}
