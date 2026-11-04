package main

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReadOnlyComponentEnumerationMatchesActualCaller(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	u := userContext{SID: user.User.Sid.String(), PID: os.Getpid()}
	if _, err := componentClients(u, nil); err != nil {
		t.Fatal(err)
	}
}
