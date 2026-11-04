package startup

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func TestPrivateHKCURegistrationOnly(t *testing.T) {
	// No use of CurrentUser(), Run, StartupApproved, services or scheduled tasks.
	name := fmt.Sprintf("P02-%d-%d", os.Getpid(), time.Now().UnixNano())
	path := `Software\VxBridgeIsolatedTests-` + name
	backend := registryBackend{path: path, value: "Bridge", mutex: `Local\VxBridgeTest.` + name}
	defer func() {
		if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil {
			t.Error(err)
		}
	}()
	manager, err := New(backend, filepath.Join(t.TempDir(), "GSPconnect.exe"), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := manager.State(); err != nil || state != "startup_off" {
		t.Fatal(state, err)
	}
	if _, err := manager.Toggle(); err != nil {
		t.Fatal(err)
	}
	value, exists, err := backend.Read()
	if err != nil || !exists || value != manager.command {
		t.Fatal("private Run-shaped value was not written exactly", err)
	}
	// Multiple goroutines exercise Win32's thread-owned mutex, not only a
	// process-local Go lock. All concurrent reads remain read-only.
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Go(func() {
			if state, err := manager.State(); err != nil || state != "startup_registered_policy_applies" {
				t.Error("cross-thread ownership/read failure", state, err)
			}
		})
	}
	workers.Wait()
	if _, err := manager.Toggle(); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := backend.Read(); err != nil || exists {
		t.Fatal("private registration did not reverse", err)
	}
}
