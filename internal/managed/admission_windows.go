package managed

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const stateKey = `Software\VxInfiniteTeesBridge\Installer`

type admission struct {
	handles      []windows.Handle
	processLease windows.Handle
}

// Close releases payload/directory pins in reverse order, joining handle errors.
// It deliberately retains processLease until OS process exit; successful Go
// cleanup must not let servicing begin while this process is still alive.
func (a *admission) Close() error {
	var result error
	for i := len(a.handles) - 1; i >= 0; i-- {
		result = errors.Join(result, windows.CloseHandle(a.handles[i]))
	}
	a.handles = nil
	return result
}

// The raw kernel lifetime handle has no Go finalizer and is intentionally kept
// until OS process exit, including the small interval after run() cleanup and
// before main's os.Exit. Closing only the payload pins must not create an
// installer handoff while this process is still alive.
func (a *admission) abandon() error {
	result := a.Close()
	if a.processLease != 0 {
		result = errors.Join(result, windows.CloseHandle(a.processLease))
		a.processLease = 0
	}
	return result
}

// pin opens the exact object, denies deletion, and verifies the resolved handle
// rather than trusting a path-level reparse check before another path open.
func pin(path string, directory bool, access, share, creation uint32) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("managed pin name encoding: %w", err)
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := windows.CreateFile(name, access, share, nil, creation, flags, 0)
	if err != nil {
		return 0, fmt.Errorf("managed pin open: %w", err)
	}
	refuse := func(reason error) (windows.Handle, error) {
		return 0, errors.Join(reason, windows.CloseHandle(handle))
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return refuse(fmt.Errorf("managed pin file information: %w", err))
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return refuse(fmt.Errorf("%w: managed pin reparse point", ErrDenied))
	}
	if (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory {
		return refuse(fmt.Errorf("%w: managed pin object kind", ErrDenied))
	}
	if !directory && info.NumberOfLinks != 1 {
		return refuse(fmt.Errorf("%w: managed pin hardlink count", ErrDenied))
	}
	buffer := make([]uint16, 32768)
	size, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return refuse(fmt.Errorf("managed pin final path: %w", err))
	}
	if size == 0 || size >= uint32(len(buffer)) {
		return refuse(fmt.Errorf("%w: managed pin final-path size", ErrDenied))
	}
	if !strings.EqualFold(strings.TrimPrefix(windows.UTF16ToString(buffer[:size]), `\\?\`), path) {
		return refuse(fmt.Errorf("%w: managed pin final-path mismatch", ErrDenied))
	}
	return handle, nil
}

func pinDirectories(path string, a *admission) error {
	var chain []string
	for current := path; current != filepath.Dir(current); current = filepath.Dir(current) {
		chain = append(chain, current)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		handle, err := pin(chain[i], true, windows.FILE_READ_ATTRIBUTES,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.OPEN_EXISTING)
		if err != nil {
			return err
		}
		a.handles = append(a.handles, handle)
	}
	return nil
}

// Admit checks Windows host admission before any control/source/window resources.
// An outside portable image returns (nil, nil) without being adopted or fenced.
// A managed image must be the exact owned GSPconnect.exe, with no pending
// maintenance, a receipt matching revision/version, and the fixed hashed payload
// inventory. Failed verification/native access returns an error matching ErrDenied.
//
// Managed success returns a lease whose Close releases payload/directory pins.
// The no-sharing native host.lock handle intentionally remains until OS process
// exit, not merely until Close, to prevent an installer handoff while the host
// is alive. Callers must retain the lease through normal guarded cleanup.
// Admit uses a short SID-scoped mutex and never starts repair or Windows Installer.
func Admit(image, revision, version string) (lease io.Closer, result error) {
	var a *admission
	defer func() {
		if result != nil && a != nil {
			result = errors.Join(result, a.abandon())
			lease = nil
		}
		if result != nil && !errors.Is(result, ErrDenied) {
			result = errors.Join(ErrDenied, result)
		}
	}()
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(base, "Programs", "VxInfiniteTeesBridge")
	image = filepath.Clean(image)
	actual, err := filepath.EvalSymlinks(image)
	if err != nil {
		return nil, err
	}
	canonicalRoot, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil && !errors.Is(rootErr, os.ErrNotExist) {
		return nil, rootErr
	}
	if rootErr == nil && !strings.EqualFold(canonicalRoot, root) &&
		(strings.EqualFold(filepath.Dir(image), canonicalRoot) || strings.EqualFold(filepath.Dir(actual), canonicalRoot)) {
		return nil, ErrDenied
	}
	owned, err := managedLocation(image, actual, root)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, nil // An outside portable copy is never adopted or fenced.
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := user.User.Sid.String()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;;GA;;;SY)(A;;GA;;;" + sid + ")")
	if err != nil {
		return nil, err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	mutex, err := windows.CreateMutex(&attributes, false, windows.StringToUTF16Ptr(`Global\VxBridge.ManagedInstall.`+sid))
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, err
	}
	defer func() { result = errors.Join(result, windows.CloseHandle(mutex)) }()
	wait, err := windows.WaitForSingleObject(mutex, 500)
	if err != nil {
		return nil, err
	}
	if wait != windows.WAIT_OBJECT_0 && wait != windows.WAIT_ABANDONED {
		return nil, ErrDenied
	}
	defer func() { result = errors.Join(result, windows.ReleaseMutex(mutex)) }()
	a = new(admission)
	state := filepath.Join(base, "VxInfiniteTeesBridge", "installer")
	if err := pinDirectories(state, a); err != nil {
		return nil, err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, stateKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("%w: no owned MSI receipt", ErrDenied)
	}
	defer func() { result = errors.Join(result, key.Close()) }()
	if _, _, err := key.GetValue("Pending", nil); !errors.Is(err, registry.ErrNotExist) {
		return nil, ErrDenied // Any pending value, corrupt type or read error denies.
	}
	data := make([]byte, ReceiptSize)
	size, kind, err := key.GetValue("Receipt", data)
	if err != nil || kind != registry.BINARY || size != ReceiptSize {
		return nil, ErrDenied
	}
	receipt, err := DecodeReceipt(data)
	if err != nil {
		return nil, err
	}
	if err := checkBuild(receipt, revision, version); err != nil {
		return nil, err
	}
	lifetime, err := pin(filepath.Join(state, "host.lock"), false,
		windows.GENERIC_READ|windows.GENERIC_WRITE, 0, windows.OPEN_ALWAYS)
	if err != nil {
		return nil, fmt.Errorf("%w: managed lifetime lease unavailable: %v", ErrDenied, err)
	}
	a.processLease = lifetime
	if err := pinDirectories(root, a); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != len(payloadNames) {
		return nil, ErrDenied
	}
	for i, name := range payloadNames {
		handle, err := pin(filepath.Join(root, name), false, windows.GENERIC_READ, windows.FILE_SHARE_READ, windows.OPEN_EXISTING)
		if err != nil {
			return nil, err
		}
		file := os.NewFile(uintptr(handle), name)
		hash := sha256.New()
		n, readErr := io.Copy(hash, io.LimitReader(file, (32<<20)+1))
		// Transfer the raw handle to the lifetime lease, not an os.File finalizer.
		duplicate := windows.InvalidHandle
		dupErr := windows.DuplicateHandle(windows.CurrentProcess(), handle, windows.CurrentProcess(),
			&duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS)
		closeErr := file.Close()
		if dupErr == nil {
			a.handles = append(a.handles, duplicate)
		}
		if err := errors.Join(readErr, dupErr, closeErr); err != nil {
			return nil, err
		}
		if n > 32<<20 || hex.EncodeToString(hash.Sum(nil)) != receipt.Hashes[i] {
			return nil, ErrDenied
		}
	}
	return a, nil
}
