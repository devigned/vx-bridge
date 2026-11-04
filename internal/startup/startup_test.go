package startup

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type fakeBackend struct {
	mu                           sync.Mutex
	value                        string
	exists                       bool
	writes, deletes              int
	readErr, writeErr, deleteErr error
}

func (b *fakeBackend) Locked(action func(Store) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return action(b)
}
func (b *fakeBackend) Read() (string, bool, error) { return b.value, b.exists, b.readErr }
func (b *fakeBackend) Write(value string) error {
	if b.writeErr != nil {
		return b.writeErr
	}
	b.value, b.exists = value, true
	b.writes++
	return nil
}
func (b *fakeBackend) Delete() error {
	if b.deleteErr != nil {
		return b.deleteErr
	}
	b.value, b.exists = "", false
	b.deletes++
	return nil
}

func testManager(t *testing.T, backend Backend, port int) *Manager {
	t.Helper()
	manager, err := New(backend, filepath.Join(t.TempDir(), "path with spaces", "GSPconnect.exe"), port)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestDefaultOffQuotedRegistrationAndExplicitReversal(t *testing.T) {
	backend := &fakeBackend{}
	manager := testManager(t, backend, 1000)
	if state, err := manager.State(); err != nil || state != "startup_off" || backend.writes != 0 {
		t.Fatal("initialization/read enabled startup")
	}
	if state, err := manager.Toggle(); err != nil || state != "startup_registered_policy_applies" {
		t.Fatal("explicit enable failed", state, err)
	}
	if backend.value != manager.command || !strings.HasPrefix(backend.value, `"`) ||
		!strings.HasSuffix(backend.value, `" --desktop --target-port 1000`) {
		t.Fatal("executable was not quoted or required arguments were lost")
	}
	// A policy-disabled registration still exists. Reading it never writes the
	// Run value again, nor is there any StartupApproved mutation interface.
	for i := 0; i < 5; i++ {
		if state, err := manager.State(); err != nil || state != "startup_registered_policy_applies" {
			t.Fatal("registration presence conflated with guaranteed execution")
		}
	}
	if backend.writes != 1 {
		t.Fatal("refresh overrode Windows startup policy")
	}
	if state, err := manager.Toggle(); err != nil || state != "startup_off" || backend.deletes != 1 {
		t.Fatal("owned registration did not reverse")
	}
}

func TestOtherCopyOrArgumentsCannotBeOverwrittenOrDeleted(t *testing.T) {
	for _, value := range []string{`"C:\other\GSPconnect.exe" --desktop --target-port 999`, "unknown command"} {
		backend := &fakeBackend{value: value, exists: true}
		manager := testManager(t, backend, 999)
		if state, err := manager.State(); err != nil || state != "startup_conflict_not_changed" {
			t.Fatal("conflict not identified")
		}
		if state, err := manager.Toggle(); err == nil || state != "startup_conflict_not_changed" {
			t.Fatal("conflict was not reported")
		}
		if backend.value != value || backend.writes != 0 || backend.deletes != 0 {
			t.Fatal("other copy's registration was changed")
		}
	}
}

func TestReadWriteAndUnregisterErrorsAreNotSuccess(t *testing.T) {
	fault := errors.New("invented_access_denied")
	for _, operation := range []string{"read", "write", "delete"} {
		t.Run(operation, func(t *testing.T) {
			backend := &fakeBackend{}
			manager := testManager(t, backend, 999)
			switch operation {
			case "read":
				backend.readErr = fault
			case "write":
				backend.writeErr = fault
			case "delete":
				backend.value, backend.exists, backend.deleteErr = manager.command, true, fault
			}
			if state, err := manager.Toggle(); !errors.Is(err, fault) || state != "startup_change_failed_use_status" {
				t.Fatal("registry failure hidden", state, err)
			}
		})
	}
}

func TestStartupArgumentsRejectUnownedImagesAndInjection(t *testing.T) {
	image := filepath.Join(t.TempDir(), "GSPconnect.exe")
	for _, test := range []struct {
		image string
		port  int
	}{
		{"GSPconnect.exe", 999}, {filepath.Join(t.TempDir(), "vxbridge.exe"), 999},
		{image + `" --evil`, 999}, {image, 12321}, {image, 0}, {image, 65536},
		{filepath.Join(t.TempDir(), strings.Repeat("x", 261), "GSPconnect.exe"), 999},
	} {
		if _, err := Command(test.image, test.port); err == nil {
			t.Fatal("unsafe startup arguments accepted", test)
		}
	}
}

func TestSimultaneousCopiesSerializeOwnershipCheckAndMutation(t *testing.T) {
	backend := &fakeBackend{}
	first, second := testManager(t, backend, 999), testManager(t, backend, 1000)
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	results := make(chan error, 2)
	for _, manager := range []*Manager{first, second} {
		go func() {
			defer workers.Done()
			<-start
			_, err := manager.Toggle()
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 || backend.writes != 1 || backend.deletes != 0 {
		t.Fatal("simultaneous copy silently overwrote startup ownership")
	}
}
