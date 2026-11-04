package desktop

import (
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/devigned/vx-bridge/internal/identity"
	"golang.org/x/sys/windows"
)

//go:embed icons/*.ico
var icons embed.FS

const (
	trayCallback = 0x8001
	nimAdd       = 0
	nimModify    = 1
	nimDelete    = 2
	nimVersion   = 4
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	shell32         = windows.NewLazySystemDLL("shell32.dll")
	shellNotify     = shell32.NewProc("Shell_NotifyIconW")
	registerMessage = user32.NewProc("RegisterWindowMessageW")
	createOwner     = user32.NewProc("CreateWindowExW")
	destroyOwner    = user32.NewProc("DestroyWindow")
	createPopup     = user32.NewProc("CreatePopupMenu")
	appendMenu      = user32.NewProc("AppendMenuW")
	destroyMenu     = user32.NewProc("DestroyMenu")
	trackPopup      = user32.NewProc("TrackPopupMenuEx")
	endMenu         = user32.NewProc("EndMenu")
	setForeground   = user32.NewProc("SetForegroundWindow")
	postMessage     = user32.NewProc("PostMessageW")
	getCursor       = user32.NewProc("GetCursorPos")
	systemMetrics   = user32.NewProc("GetSystemMetrics")
	createIcon      = user32.NewProc("CreateIconFromResourceEx")
	destroyIcon     = user32.NewProc("DestroyIcon")
	messageBox      = user32.NewProc("MessageBoxW")
	clearLastError  = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetLastError")
	coInitialize    = windows.NewLazySystemDLL("ole32.dll").NewProc("CoInitializeEx")
	coUninitialize  = windows.NewLazySystemDLL("ole32.dll").NewProc("CoUninitialize")
)

// The x64 NOTIFYICONDATAW layout includes its trailing GUID/balloon icon.
type iconData struct {
	// Size is the native structure size passed to Shell_NotifyIconW.
	Size uint32
	// Window is the compatibility HWND receiving notifications.
	Window uintptr
	// ID identifies this icon within Window.
	ID uint32
	// Flags selects which fields Shell_NotifyIconW reads.
	Flags uint32
	// Callback is the application message used for icon input.
	Callback uint32
	// Icon is the owned HICON for the current status mark.
	Icon uintptr
	// Tip is the fixed-capacity, NUL-terminated UTF-16 tooltip.
	Tip [128]uint16
	// State contains native icon-state bits selected by Mask.
	State uint32
	// Mask selects the State bits to update.
	Mask uint32
	// Info is native balloon text; this tray leaves it empty.
	Info [256]uint16
	// Version is the native timeout/version union, set to 4 for NIM_SETVERSION.
	Version uint32
	// Title is native balloon title storage, unused here.
	Title [64]uint16
	// InfoFlag contains native balloon flags, unused here.
	InfoFlag uint32
	// GUID is the native persistent icon identifier, unused for the HWND/ID form.
	GUID windows.GUID
	// Balloon is the native custom balloon HICON slot, unused here.
	Balloon uintptr
}

type tray struct {
	runtime  *Runtime
	thread   uint32
	hwnd     uintptr
	owner    uintptr
	taskbar  uint32
	data     iconData
	handles  map[string]uintptr
	added    bool
	version  bool
	last     time.Time
	icon     string
	tip      string
	warned   bool
	menuOpen bool
	dismiss  func() error
	notify   func(uint32, *iconData) error
}

func nativeTray(r *Runtime) identity.Tray { return &tray{runtime: r, notify: notifyIcon} }

func notifyIcon(command uint32, data *iconData) error {
	ok, _, _ := shellNotify.Call(uintptr(command), uintptr(unsafe.Pointer(data)))
	if ok == 0 {
		return errors.New("Shell_NotifyIconW_failed")
	}
	return nil
}

// Open initializes tray resources for hwnd on the current identity owner thread.
// Initialization failure is recorded as UI degradation, not a shutdown request.
func (t *tray) Open(hwnd uintptr) {
	t.thread, t.hwnd = windows.GetCurrentThreadId(), hwnd
	if err := t.open(); err != nil {
		t.failed("tray_initialization_failed_use_control")
	}
}

func (t *tray) open() error {
	for _, proc := range []*windows.LazyProc{shellNotify, registerMessage, createOwner, destroyOwner,
		createPopup, appendMenu, destroyMenu, trackPopup, endMenu, setForeground, postMessage,
		getCursor, systemMetrics, createIcon, destroyIcon, clearLastError} {
		if err := proc.Find(); err != nil {
			return errors.New("tray_entrypoint_unavailable")
		}
	}
	if t.dismiss == nil {
		t.dismiss = func() error {
			if ok, _, _ := endMenu.Call(); ok == 0 {
				return errors.New("tray_menu_cancel_failed")
			}
			return nil
		}
	}
	event := windows.StringToUTF16Ptr("TaskbarCreated")
	message, _, _ := registerMessage.Call(uintptr(unsafe.Pointer(event)))
	if message == 0 {
		return errors.New("tray_restore_message_registration_failed")
	}
	t.taskbar = uint32(message)
	// This hidden owned standard window is only a popup-menu owner. It cannot
	// replace the visible, unowned exact-title compatibility main window.
	class := windows.StringToUTF16Ptr("STATIC")
	owner, _, _ := createOwner.Call(0x80, uintptr(unsafe.Pointer(class)), 0,
		0x80000000, 0, 0, 0, 0, t.hwnd, 0, 0, 0)
	if owner == 0 {
		return errors.New("tray_menu_owner_creation_failed")
	}
	t.owner = owner
	t.handles = make(map[string]uintptr)
	size, _, _ := systemMetrics.Call(49) // SM_CXSMICON; Shell scales the multi-size originals.
	for _, state := range []string{"waiting", "linked", "attention", "pending"} {
		data, err := icons.ReadFile("icons/" + state + ".ico")
		if err != nil {
			return err
		}
		image, err := iconImage(data, int(size))
		if err != nil {
			return err
		}
		icon, _, _ := createIcon.Call(uintptr(unsafe.Pointer(&image[0])), uintptr(len(image)), 1,
			0x00030000, size, size, 0)
		runtime.KeepAlive(image)
		if icon == 0 {
			return errors.New("tray_embedded_icon_load_failed")
		}
		t.handles[state] = icon
	}
	t.data = iconData{Size: uint32(unsafe.Sizeof(iconData{})), Window: t.hwnd, ID: 1,
		Flags: 1 | 2 | 4 | 0x80, Callback: trayCallback}
	return t.update(true)
}

func iconImage(data []byte, size int) ([]byte, error) {
	if len(data) < 6 || binary.LittleEndian.Uint32(data[:4]) != 0x00010000 {
		return nil, errors.New("invalid_embedded_icon")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count < 1 || count > 16 || len(data) < 6+16*count {
		return nil, errors.New("invalid_embedded_icon_directory")
	}
	selected, best := -1, 1<<30
	for i := 0; i < count; i++ {
		entry := data[6+16*i : 6+16*(i+1)]
		width := int(entry[0])
		if width == 0 {
			width = 256
		}
		distance := width - size
		if distance < 0 {
			distance = 1000 - distance
		}
		if distance < best {
			selected, best = i, distance
		}
	}
	entry := data[6+16*selected : 6+16*(selected+1)]
	length, offset := uint64(binary.LittleEndian.Uint32(entry[8:12])), uint64(binary.LittleEndian.Uint32(entry[12:16]))
	if length < 40 || offset < uint64(6+16*count) || offset+length > uint64(len(data)) {
		return nil, errors.New("invalid_embedded_icon_image")
	}
	return data[offset : offset+length], nil
}

func (t *tray) update(restore bool) error {
	view := t.runtime.View()
	if !restore && t.added && t.version && view.Icon == t.icon && view.Tooltip == t.tip {
		return nil
	}
	t.data.Icon = t.handles[view.Icon]
	if t.data.Icon == 0 {
		return errors.New("tray_icon_not_initialized")
	}
	t.data.Tip = [128]uint16{}
	text, err := windows.UTF16FromString(view.Tooltip)
	if err != nil || len(text) > len(t.data.Tip) {
		return errors.New("tray_tooltip_invalid")
	}
	copy(t.data.Tip[:], text)
	command := uint32(nimModify)
	if restore || !t.added {
		command = nimAdd
	}
	if err := t.notify(command, &t.data); err != nil {
		return err
	}
	t.added = true
	if command == nimAdd || !t.version {
		t.data.Version = 4
		if err := t.notify(nimVersion, &t.data); err != nil {
			return err
		}
		t.version = true
	}
	t.icon, t.tip = view.Icon, view.Tooltip
	t.runtime.UIState("tray_available")
	return nil
}

// Handle reports whether it consumed an identity-window message on the owner
// thread. Close input requests guarded Quit; timers update/dismiss the tray.
// Wrong-thread or native failures are recorded without closing the source.
func (t *tray) Handle(message uint32, wparam, lparam uintptr) bool {
	if windows.GetCurrentThreadId() != t.thread {
		t.failed("tray_wrong_owner_thread_use_control")
		return true
	}
	switch {
	case message == 0x0010: // WM_CLOSE is another guarded request, never DestroyWindow.
		t.runtime.Request(Quit)
		return true
	case message == t.taskbar && t.taskbar != 0:
		t.added, t.version = false, false
		if err := t.update(true); err != nil {
			t.failed("tray_restore_failed_use_control")
		}
		return true
	case message == 0x0113: // Existing identity timer; no I/O or controller waits here.
		if t.runtime.stopping.Load() && t.menuOpen {
			if err := t.dismiss(); err != nil {
				t.failed("tray_menu_dismiss_failed_use_control")
			}
			return true
		}
		if time.Since(t.last) >= 500*time.Millisecond {
			t.last = time.Now()
			if err := t.update(false); err != nil {
				t.failed("tray_update_failed_use_control")
			}
		}
		return true
	case message == trayCallback:
		event := uint32(lparam & 0xffff)
		if event == 0x7b || event == 0x400 || event == 0x401 || event == 0x205 {
			if err := t.showMenu(wparam); err != nil {
				t.failed("tray_menu_failed_use_control")
			}
		}
		return true
	}
	return false
}

func (t *tray) showMenu(anchor uintptr) (result error) {
	menuHandle, _, _ := createPopup.Call()
	if menuHandle == 0 {
		return errors.New("tray_menu_create_failed")
	}
	defer func() {
		if ok, _, _ := destroyMenu.Call(menuHandle); ok == 0 {
			result = errors.Join(result, errors.New("tray_menu_cleanup_failed"))
		}
	}()
	for _, item := range t.runtime.View().Items {
		flags := uintptr(0)
		if item.Separator {
			flags = 0x800
		}
		if item.Disabled {
			flags |= 0x3
		}
		if item.Checked {
			flags |= 0x8
		}
		text, err := windows.UTF16PtrFromString(item.Text)
		if err != nil {
			return errors.New("tray_menu_text_invalid")
		}
		if ok, _, _ := appendMenu.Call(menuHandle, flags, uintptr(item.Action), uintptr(unsafe.Pointer(text))); ok == 0 {
			return errors.New("tray_menu_append_failed")
		}
	}
	point := notificationAnchor(anchor)
	if point.X == -1 && point.Y == -1 {
		if ok, _, _ := getCursor.Call(uintptr(unsafe.Pointer(&point))); ok == 0 {
			return errors.New("tray_menu_position_failed")
		}
	}
	if ok, _, _ := setForeground.Call(t.owner); ok == 0 {
		return errors.New("tray_menu_foreground_failed")
	}
	// TPM_RETURNCMD/TPM_NONOTIFY prevent command dispatch on another thread.
	t.menuOpen = true
	defer func() { t.menuOpen = false }()
	clearLastError.Call(0)
	command, _, err := trackPopup.Call(menuHandle, 0x100|0x80|0x2,
		uintptr(point.X), uintptr(point.Y), t.owner, 0)
	if ok, _, _ := postMessage.Call(t.owner, 0, 0, 0); ok == 0 {
		return errors.New("tray_menu_dismissal_failed")
	}
	if command == 0 {
		if t.runtime.stopping.Load() {
			return nil
		}
		if err != windows.ERROR_SUCCESS {
			return errors.New("tray_menu_tracking_failed")
		}
		return nil // Ordinary Escape/dismissal.
	}
	switch Action(command) {
	case OpenLogs, ToggleStartup, Quit, SelectInfiniteTees, SelectGSPro, SelectStandby:
		t.runtime.Request(Action(command))
	default:
		return errors.New("tray_unknown_menu_command")
	}
	return nil
}

func (t *tray) failed(code string) {
	t.runtime.UIState(code)
	if !t.warned && !t.runtime.stopping.Load() {
		t.warned = true
		// The separate dialog thread never blocks the identity/engine owner.
		// Its owned dialog cannot become the unowned compatibility main window.
		go func() {
			if err := notifyOwned(t.hwnd, "The tray is unavailable or degraded. The bridge has NOT been stopped.\n"+
				"Use the separate vxbridge.exe --status / --quit controls.\n"+
				"Disconnect Labs normally before quitting. Local bounded status is retained."); err != nil {
				t.runtime.UIState("tray_and_notice_unavailable_use_control")
			}
		}()
	}
}

// Close removes the tray icon and releases icon/menu-owner resources on the
// identity owner thread, returning joined native cleanup errors. Call it after
// the source-first guard; it does not destroy the compatibility window itself.
func (t *tray) Close() error {
	if t.thread != 0 && windows.GetCurrentThreadId() != t.thread {
		return errors.New("tray_cleanup_wrong_owner_thread")
	}
	var failures []error
	if t.added {
		if err := t.notify(nimDelete, &t.data); err != nil {
			failures = append(failures, err)
		}
	}
	for _, icon := range t.handles {
		if ok, _, _ := destroyIcon.Call(icon); ok == 0 {
			failures = append(failures, errors.New("tray_icon_cleanup_failed"))
		}
	}
	if t.owner != 0 {
		if ok, _, _ := destroyOwner.Call(t.owner); ok == 0 {
			failures = append(failures, errors.New("tray_menu_owner_cleanup_failed"))
		}
	}
	return errors.Join(failures...)
}

func openLogDirectory(directory string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := coInitialize.Find(); err != nil {
		return errors.New("shell_COM_unavailable")
	}
	if err := coUninitialize.Find(); err != nil {
		return errors.New("shell_COM_cleanup_unavailable")
	}
	result, _, _ := coInitialize.Call(0, 0x2|0x4) // STA, no legacy OLE1 DDE.
	if int32(result) < 0 {
		return errors.New("shell_COM_initialization_failed")
	}
	defer coUninitialize.Call()
	path, err := windows.UTF16PtrFromString(directory)
	if err != nil {
		return err
	}
	verb := windows.StringToUTF16Ptr("open")
	return windows.ShellExecute(0, verb, path, nil, nil, windows.SW_SHOWNORMAL)
}

// Notify displays a blocking informational Windows dialog containing text.
// It locks the calling goroutine to an OS thread until the dialog is dismissed.
// Invalid text or unavailable/failed native dialog calls return an error.
func Notify(text string) error {
	return notifyOwned(0, text)
}

func notifyOwned(owner uintptr, text string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := messageBox.Find(); err != nil {
		return errors.New("desktop_notice_unavailable")
	}
	title := windows.StringToUTF16Ptr("VX Bridge")
	body, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return errors.New("desktop_notice_text_invalid")
	}
	if result, _, _ := messageBox.Call(owner, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0x40); result == 0 {
		return errors.New("desktop_notice_failed")
	}
	return nil
}

// DuplicateMessage formats state into a notice that leaves the existing host
// untouched and directs the user to its tray or distinct control executable.
func DuplicateMessage(state string) string {
	return fmt.Sprintf("The existing VX Bridge was left untouched.\n%s\n"+
		"Use its tray, or the separate vxbridge.exe --status / --quit controls.\n"+
		"Game readiness is not reported. Disconnect Labs normally before quitting.", state)
}
