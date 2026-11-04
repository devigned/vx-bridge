//go:build !windows

package identity

import "context"

// Window is the non-Windows identity stub. Tray is retained for API shape only
// and is never opened by this implementation.
type Window struct {
	// Tray retains the optional adapter for API shape only; the stub never opens it.
	Tray Tray
}

// Open returns requested-but-uncreated evidence and a Windows-required error.
func (*Window) Open() (Verification, error) {
	return Verification{Requested: true}, windowFault("Windows_required")
}

// Loop returns a Windows-required error without servicing native messages.
func (*Window) Loop(context.Context) error { return windowFault("Windows_required") }

// Close returns empty evidence and no error because the stub owns no resources.
func (*Window) Close() (Cleanup, error) { return Cleanup{}, nil }
