//go:build !windows

package main

import "errors"

func execute(configuration) error {
	return errors.New("private product lifecycle execution is restricted to disposable GitHub-hosted Windows")
}
