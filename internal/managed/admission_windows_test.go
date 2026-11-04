package managed

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Only private temporary files: no Admit(), fixed production state/key/pipe or
// application launch. Native execution is separately gated by the review scope.
func privateCanonicalDir(t *testing.T) string {
	t.Helper()
	raw := t.TempDir()
	dir, err := filepath.EvalSymlinks(raw)
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}
		t.Fatal("canonicalizing private temp directory", err)
	}
	// TempDir can retain an 8.3 TMP spelling; pin requires the normalized name.
	t.Logf("private temp spelling normalized=%t", !strings.EqualFold(raw, dir))
	return dir
}

func TestPrivateKernelPinsPreventMutationUntilClosed(t *testing.T) {
	path := filepath.Join(privateCanonicalDir(t), "invented payload.fixture")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	handle, err := pin(path, false, windows.GENERIC_READ, windows.FILE_SHARE_READ, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			if err := windows.CloseHandle(handle); err != nil {
				t.Error(err)
			}
		}
	})
	if err := os.WriteFile(path, []byte("after"), 0o600); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatal("private pin did not deny writes", err)
	}
	renamed := path + ".new"
	if err := os.Rename(path, renamed); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) &&
		!errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatal("private pin did not deny rename", err)
	}
	if err := windows.CloseHandle(handle); err != nil {
		t.Fatal(err)
	}
	closed = true
	if err := os.WriteFile(path, []byte("after"), 0o600); err != nil {
		t.Fatal("unleased positive write control failed", err)
	}
	if err := os.Rename(path, renamed); err != nil {
		t.Fatal("unleased absent-target rename control failed", err)
	}
	lease, err := pin(renamed, false, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal(err)
	}
	secondHandle, second := pin(renamed, false, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, windows.OPEN_EXISTING)
	if second == nil {
		if err := windows.CloseHandle(secondHandle); err != nil {
			t.Error(err)
		}
	}
	if err := windows.CloseHandle(lease); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(second, windows.ERROR_SHARING_VIOLATION) {
		t.Fatal("lifetime handle did not exclude a second mutation admission", second)
	}
}

func TestPrivateAdmissionCleanupRetainsProcessLifetime(t *testing.T) {
	dir := privateCanonicalDir(t)
	path := filepath.Join(dir, "invented lifetime.fixture")
	handle, err := pin(path, false, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, windows.OPEN_ALWAYS)
	if err != nil {
		t.Fatal(err)
	}
	a := &admission{processLease: handle}
	t.Cleanup(func() {
		if err := a.abandon(); err != nil {
			t.Error(err)
		}
	})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	other, err := pin(path, false, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, windows.OPEN_EXISTING)
	if err == nil {
		if err := windows.CloseHandle(other); err != nil {
			t.Error(err)
		}
		t.Fatal("normal cleanup released the process lifetime before OS exit")
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatal("expected actual sharing denial after normal cleanup", err)
	}
	if err := a.abandon(); err != nil {
		t.Fatal(err)
	}
	other, err = pin(path, false, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal("failed-admission close positive control", err)
	}
	if err := windows.CloseHandle(other); err != nil {
		t.Fatal(err)
	}
}

func TestPrivatePinRefusalsStayFailClosed(t *testing.T) {
	dir := privateCanonicalDir(t)
	path := filepath.Join(dir, "invented guarded.fixture")
	if err := os.WriteFile(path, []byte("invented"), 0o600); err != nil {
		t.Fatal(err)
	}
	refused := func(name string, directory bool, reason string) {
		t.Helper()
		handle, err := pin(name, directory, windows.GENERIC_READ, windows.FILE_SHARE_READ, windows.OPEN_EXISTING)
		if err == nil {
			if closeErr := windows.CloseHandle(handle); closeErr != nil {
				t.Error(closeErr)
			}
			t.Fatal("private pin accepted forbidden input", reason)
		}
		if !errors.Is(err, ErrDenied) || !strings.Contains(err.Error(), "managed pin "+reason) {
			t.Fatal("private pin lost its redacted refusal reason", reason, err)
		}
		if strings.Contains(err.Error(), dir) {
			t.Fatal("managed pin diagnostic disclosed a path")
		}
	}
	refused(dir+`\.\`+filepath.Base(path), false, "final-path mismatch")
	refused(path, true, "object kind")
	link := filepath.Join(dir, "invented extra-link.fixture")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	refused(path, false, "hardlink count")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	handle, err := pin(path, false, windows.GENERIC_READ, windows.FILE_SHARE_READ, windows.OPEN_EXISTING)
	if err != nil {
		t.Fatal("canonical single-link positive control", err)
	}
	if err := windows.CloseHandle(handle); err != nil {
		t.Fatal(err)
	}
}
