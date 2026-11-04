package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func privatePipe(t *testing.T) (windows.Handle, windows.Handle) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(fmt.Sprintf(`\\.\pipe\VXBridge.PrivateDelivery.%d.%d`, os.Getpid(), time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	server, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_NOWAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1, 4096, 4096, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.CloseHandle(server); err != nil {
			t.Error(err)
		}
	})
	client, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.CloseHandle(client); err != nil {
			t.Error(err)
		}
	})
	mode := uint32(windows.PIPE_READMODE_BYTE | windows.PIPE_NOWAIT)
	if err := windows.SetNamedPipeHandleState(client, &mode, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := windows.ConnectNamedPipe(server, nil); err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		t.Fatal(err)
	}
	return server, client
}

func pipeWord(t *testing.T, pipe windows.Handle, write bool) {
	t.Helper()
	data := []byte{1, 0, 0, 0}
	var n uint32
	var err error
	if write {
		err = windows.WriteFile(pipe, data, &n, nil)
	} else {
		err = windows.ReadFile(pipe, data, &n, nil)
	}
	if err != nil || n != 4 || binary.LittleEndian.Uint32(data) != 1 {
		t.Fatalf("private reply/confirmation write=%t bytes=%d error=%v data=%x", write, n, err, data)
	}
}

func TestDisconnectDiscardsUnreadReplyAndConfirmationPreservesIt(t *testing.T) {
	t.Run("old-disconnect-before-read", func(t *testing.T) {
		server, client := privatePipe(t)
		pipeWord(t, server, true)
		if err := windows.DisconnectNamedPipe(server); err != nil {
			t.Fatal(err)
		}
		var data [4]byte
		var n uint32
		err := windows.ReadFile(client, data[:], &n, nil)
		if n != 0 || (!errors.Is(err, windows.ERROR_BROKEN_PIPE) && !errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED) && !errors.Is(err, windows.ERROR_NO_DATA)) {
			t.Fatalf("old unread-reply loss precondition not reproduced: bytes=%d error=%v", n, err)
		}
	})
	t.Run("confirmed-consumption-before-disconnect", func(t *testing.T) {
		server, client := privatePipe(t)
		pipeWord(t, server, true)
		pipeWord(t, client, false)
		pipeWord(t, client, true)
		pipeWord(t, server, false)
		if err := windows.DisconnectNamedPipe(server); err != nil {
			t.Fatal(err)
		}
	})
}
