//go:build !windows

package control

import (
	"context"
	"errors"
	"net"
)

// Listen returns a Windows-required error and no listener on non-Windows systems.
func Listen() (net.Listener, error) { return nil, errors.New("Windows_required") }
func dial(context.Context) (net.Conn, error) {
	return nil, errors.New("Windows_required")
}
