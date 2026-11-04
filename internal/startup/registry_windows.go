package startup

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

type registryBackend struct {
	path, value, mutex string
}

// CurrentUser returns the HKCU Run-value backend and SID-scoped ownership mutex
// for this process's user. It reads token identity, not registration, and does
// not enable startup. Token lookup failure is returned.
func CurrentUser() (Backend, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return registryBackend{runKey, "VxInfiniteTeesBridge", `Global\VxBridge.Startup.` + user.User.Sid.String()}, nil
}

// Locked calls action on the mutex-owning OS thread after a wait of at most
// 500 ms. It joins mutex release/close errors with action's result.
func (b registryBackend) Locked(action func(Store) error) (result error) {
	runtime.LockOSThread() // Win32 mutex ownership is per OS thread, not goroutine.
	defer runtime.UnlockOSThread()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;;GA;;;SY)(A;;GA;;;" + sid + ")")
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	mutex, err := windows.CreateMutex(&attributes, false, windows.StringToUTF16Ptr(b.mutex))
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return err
	}
	defer func() { result = errors.Join(result, windows.CloseHandle(mutex)) }()
	wait, err := windows.WaitForSingleObject(mutex, 500)
	if err != nil {
		return err
	}
	if wait != windows.WAIT_OBJECT_0 && wait != windows.WAIT_ABANDONED {
		return errors.New("startup_ownership_lock_unavailable")
	}
	defer func() { result = errors.Join(result, windows.ReleaseMutex(mutex)) }()
	return action(b)
}

// Read returns the owned REG_SZ command, treating a missing key/value as absent.
// Unexpected types and registry/handle-close failures are returned.
func (b registryBackend) Read() (value string, exists bool, result error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, b.path, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer func() { result = errors.Join(result, key.Close()) }()
	value, kind, err := key.GetStringValue(b.value)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if kind != registry.SZ {
		return "", false, errors.New("unexpected_startup_registry_type")
	}
	return value, true, nil
}

// Write sets the owned HKCU value as REG_SZ, returning write and close errors.
func (b registryBackend) Write(value string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, b.path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	err = key.SetStringValue(b.value, value)
	return errors.Join(err, key.Close())
}

// Delete removes only the owned value, returning registry and key-close errors.
func (b registryBackend) Delete() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, b.path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	err = key.DeleteValue(b.value)
	return errors.Join(err, key.Close())
}
