package desktop

import (
	"errors"
	"runtime"
	"time"
	"unsafe"

	"github.com/devigned/vx-bridge/internal/diagnostics"
	"golang.org/x/sys/windows"
)

var (
	commonDialogs = windows.NewLazySystemDLL("comdlg32.dll")
	getSaveName   = commonDialogs.NewProc("GetSaveFileNameW")
	dialogError   = commonDialogs.NewProc("CommDlgExtendedError")
)

type openFilename struct {
	Size                  uint32
	Owner, Instance       uintptr
	Filter, Custom        *uint16
	MaxCustom, Index      uint32
	File                  *uint16
	MaxFile               uint32
	FileTitle             *uint16
	MaxTitle              uint32
	Directory, Title      *uint16
	Flags                 uint32
	Offset, Extension     uint16
	DefaultExtension      *uint16
	Data, Hook            uintptr
	Template              *uint16
	Reserved              uintptr
	ReservedWord, FlagsEx uint32
}

func reportDialog(build diagnostics.Build) (result string, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var owner uintptr
	defer func() {
		if result != "cancelled" {
			err = errors.Join(err, notifyOwned(owner, reportNotice(result, err)))
		}
		if owner != 0 {
			if ok, _, _ := destroyOwner.Call(owner); ok == 0 {
				err = errors.Join(err, errors.New("diagnostic_dialog_cleanup_failed"))
			}
		}
	}()
	for _, proc := range []*windows.LazyProc{getSaveName, dialogError, createOwner, destroyOwner} {
		if err := proc.Find(); err != nil {
			return "", errors.New("diagnostic_dialog_unavailable")
		}
	}
	class := windows.StringToUTF16Ptr("STATIC")
	title := windows.StringToUTF16Ptr("Save diagnostic report")
	owner, _, _ = createOwner.Call(0x80, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		0x80000000, 0, 0, 0, 0, 0, 0, 0, 0)
	if owner == 0 {
		return "", errors.New("diagnostic_dialog_owner_failed")
	}
	name := make([]uint16, 32768)
	copy(name, windows.StringToUTF16("vx-bridge-diagnostics-"+time.Now().UTC().Format("20060102-150405")+".zip"))
	filter := []uint16{'Z', 'I', 'P', ' ', 'r', 'e', 'p', 'o', 'r', 't', ' ', '(', '*', '.', 'z', 'i', 'p', ')', 0,
		'*', '.', 'z', 'i', 'p', 0, 0}
	extension := windows.StringToUTF16Ptr("zip")
	dialog := openFilename{Size: uint32(unsafe.Sizeof(openFilename{})), Owner: owner,
		Filter: &filter[0], Index: 1, File: &name[0], MaxFile: uint32(len(name)),
		Title: title, DefaultExtension: extension,
		Flags: 0x8 | 0x800 | 0x80000 | 0x800000 | 0x2000000} // No cwd change; existing local parent; Explorer; sizing; no recent list.
	ok, _, _ := getSaveName.Call(uintptr(unsafe.Pointer(&dialog)))
	runtime.KeepAlive(dialog)
	if ok == 0 {
		code, _, _ := dialogError.Call()
		if code == 0 {
			return "cancelled", nil
		}
		return "", errors.New("diagnostic_save_dialog_failed")
	}
	return saveReport(windows.UTF16ToString(name), diagnostics.NewCollector(build))
}

func openBugForm() error {
	return openLogDirectory(diagnostics.BugURL)
}
