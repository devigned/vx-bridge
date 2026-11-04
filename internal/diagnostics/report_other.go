//go:build !windows

package diagnostics

import "errors"

func reportFolders() (string, string) { return "", "" }
func windowsFacts() (*Windows, error) { return nil, errors.New("Windows_required") }
