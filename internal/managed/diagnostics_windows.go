package managed

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func diagnosticFile(path string, create bool) (*os.File, io.Closer, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) || strings.Contains(path[2:], ":") {
		return nil, nil, errors.New("diagnostic_local_path_required")
	}
	drive := windows.GetDriveType(windows.StringToUTF16Ptr(filepath.VolumeName(path) + `\`))
	if drive != windows.DRIVE_FIXED && drive != windows.DRIVE_REMOVABLE {
		return nil, nil, errors.New("diagnostic_local_drive_required")
	}
	a := new(admission)
	if err := pinDirectoryChain(filepath.Dir(path), a, windows.FILE_SHARE_READ); err != nil {
		return nil, nil, errors.Join(err, a.Close())
	}
	access, share, creation := uint32(windows.GENERIC_READ),
		uint32(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE), uint32(windows.OPEN_EXISTING)
	if create {
		access, share, creation = windows.GENERIC_WRITE, 0, windows.CREATE_NEW
	}
	handle, err := pin(path, false, access, share, creation)
	if err != nil {
		return nil, nil, errors.Join(err, a.Close())
	}
	return os.NewFile(uintptr(handle), path), a, nil
}

func diagnosticReceipt() (Receipt, string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, stateKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return Receipt{}, "receipt_missing", err
	}
	if err != nil {
		return Receipt{}, "receipt_unavailable", err
	}
	data := make([]byte, ReceiptSize)
	size, kind, readErr := key.GetValue("Receipt", data)
	_, _, pendingErr := key.GetValue("Pending", nil)
	closeErr := key.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return Receipt{}, "receipt_unavailable", err
	}
	if kind != registry.BINARY || size != ReceiptSize {
		return Receipt{}, "receipt_invalid", ErrDenied
	}
	r, err := DecodeReceipt(data)
	if err != nil {
		return Receipt{}, "receipt_invalid", err
	}
	if pendingErr == nil {
		return r, "maintenance_pending", nil
	}
	if !errors.Is(pendingErr, registry.ErrNotExist) {
		return r, "pending_unavailable", pendingErr
	}
	return r, "receipt_present_no_pending", nil
}
