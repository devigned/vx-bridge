package main

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReadOwnershipBeforeDeleteLeaseAvoidsSharingConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-link-bytes")
	if err := os.WriteFile(path, []byte("controlled original bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	deleting, err := windows.CreateFile(name, windows.GENERIC_READ|windows.DELETE, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	read, readErr := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if readErr == nil {
		windows.CloseHandle(read)
	}
	closeErr := windows.CloseHandle(deleting)
	if closeErr != nil || !errors.Is(readErr, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("old DELETE/read sharing conflict not established: read=%v close=%v", readErr, closeErr)
	}
	owned, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	read, err = windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		windows.CloseHandle(owned)
		t.Fatal(err)
	}
	if err := windows.CloseHandle(read); err != nil {
		t.Error(err)
	}
	var before, after windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(owned, &before); err != nil {
		t.Error(err)
	}
	firstHash := hashPrivateHandle(t, owned)
	if err := windows.CloseHandle(owned); err != nil {
		t.Fatal(err)
	}
	deleting, err = windows.CreateFile(name, windows.GENERIC_READ|windows.DELETE, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.CloseHandle(deleting); err != nil {
			t.Error(err)
		}
	}()
	if err := windows.GetFileInformationByHandle(deleting, &after); err != nil {
		t.Fatal(err)
	}
	currentHash := hashPrivateHandle(t, deleting)
	if before.VolumeSerialNumber != after.VolumeSerialNumber || before.FileIndexHigh != after.FileIndexHigh ||
		before.FileIndexLow != after.FileIndexLow || firstHash != currentHash {
		t.Fatal("read/delete handoff changed exact object or bytes")
	}
}

func hashPrivateHandle(t *testing.T, handle windows.Handle) [32]byte {
	t.Helper()
	var data [128]byte
	var n uint32
	if err := windows.ReadFile(handle, data[:], &n, nil); err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data[:n])
}

func TestSameFileIdentityDoesNotProveUnchangedShortcutBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-link-bytes")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	read, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var before, after windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(read, &before); err != nil {
		t.Error(err)
	}
	firstHash := hashPrivateHandle(t, read)
	if err := windows.CloseHandle(read); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	deleting, err := windows.CreateFile(name, windows.GENERIC_READ|windows.DELETE, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.CloseHandle(deleting); err != nil {
			t.Error(err)
		}
	}()
	if err := windows.GetFileInformationByHandle(deleting, &after); err != nil {
		t.Fatal(err)
	}
	if before.VolumeSerialNumber != after.VolumeSerialNumber || before.FileIndexHigh != after.FileIndexHigh ||
		before.FileIndexLow != after.FileIndexLow || firstHash == hashPrivateHandle(t, deleting) {
		t.Fatal("same-ID content-change negative control was not established")
	}
}
