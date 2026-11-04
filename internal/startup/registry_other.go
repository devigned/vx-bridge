//go:build !windows

package startup

import "errors"

// CurrentUser returns a Windows-required error and no registry backend on
// non-Windows systems.
func CurrentUser() (Backend, error) { return nil, errors.New("Windows_required") }
