package identity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wsPopup         = 0x80000000
	wsChild         = 0x40000000
	wsExToolWindow  = 0x00000080
	wsExNoActivate  = 0x08000000
	wsExTransparent = 0x00000020
	wsExLayered     = 0x00080000
	wsExAppWindow   = 0x00040000
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmMouseActivate = 0x0021
	wmNCHitTest     = 0x0084
	wmTimer         = 0x0113
)

var (
	probeUser32          = windows.NewLazySystemDLL("user32.dll")
	registerClass        = probeUser32.NewProc("RegisterClassExW")
	unregisterClass      = probeUser32.NewProc("UnregisterClassW")
	createWindow         = probeUser32.NewProc("CreateWindowExW")
	destroyWindow        = probeUser32.NewProc("DestroyWindow")
	defWindowProc        = probeUser32.NewProc("DefWindowProcW")
	setWindowPos         = probeUser32.NewProc("SetWindowPos")
	setLayeredAttributes = probeUser32.NewProc("SetLayeredWindowAttributes")
	getLayeredAttributes = probeUser32.NewProc("GetLayeredWindowAttributes")
	getWindowLong        = probeUser32.NewProc("GetWindowLongPtrW")
	getWindow            = probeUser32.NewProc("GetWindow")
	getParent            = probeUser32.NewProc("GetParent")
	getAncestor          = probeUser32.NewProc("GetAncestor")
	getWindowText        = probeUser32.NewProc("GetWindowTextW")
	getWindowRect        = probeUser32.NewProc("GetWindowRect")
	setTimer             = probeUser32.NewProc("SetTimer")
	killTimer            = probeUser32.NewProc("KillTimer")
	getMessage           = probeUser32.NewProc("GetMessageW")
	translateMessage     = probeUser32.NewProc("TranslateMessage")
	dispatchMessage      = probeUser32.NewProc("DispatchMessageW")
	postQuitMessage      = probeUser32.NewProc("PostQuitMessage")
	compatibilityWndProc = windows.NewCallback(researchWindowProc)
	windowSequence       atomic.Uint64
	trayWindows          sync.Map
)

// windowClass preserves the Windows x64 WNDCLASSEXW ABI.
type windowClass struct {
	// Size is the structure size; Style contains class style flags.
	Size, Style uint32
	// WndProc is the process-wide native callback thunk.
	WndProc uintptr
	// ClassExtra and WindowExtra are native extra-byte budgets, zero here.
	ClassExtra, WindowExtra int32
	// Instance is the module handle. Icon and Cursor are class resource handles;
	// Background is a brush handle. These optional resources are zero here.
	Instance, Icon, Cursor, Background uintptr
	// MenuName is the unused class menu; ClassName is the unique UTF-16 name.
	MenuName, ClassName *uint16
	// SmallIcon is the unused native small-icon handle.
	SmallIcon uintptr
}

// nativeMessage preserves the Windows x64 MSG ABI.
type nativeMessage struct {
	// Window is the message's target HWND.
	Window uintptr
	// Message is its native message ID.
	Message uint32
	// WParam and LParam carry the message-specific native arguments.
	WParam, LParam uintptr
	// Time is the message's native millisecond timestamp.
	Time uint32
	// X and Y are signed screen coordinates from the message.
	X, Y int32
	// Private is the native reserved trailing field.
	Private uint32
}

// Window owns one compatibility HWND, registered class, and polling timer.
// Its zero value can be opened once on Windows x64. The caller must lock its
// goroutine to one OS thread for Open, Loop, and Close, including failure cleanup.
// It is not safe for concurrent use or repeated setup/cleanup cycles.
type Window struct {
	// Tray is an optional adapter installed only after successful verification.
	// Set it before Open; it shares this window's owner thread.
	Tray      Tray
	module    windows.Handle
	className *uint16
	atom      uintptr
	hwnd      windows.HWND
	thread    uint32
	timer     uintptr
	created   bool
}

// One process-wide callback thunk avoids allocating a permanent callback per run.
func researchWindowProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	if window, ok := trayWindows.Load(hwnd); ok && window.(*Window).Tray.Handle(message, wparam, lparam) {
		return 0
	}
	switch message {
	case wmDestroy:
		postQuitMessage.Call(0)
		return 0
	case wmNCHitTest:
		return ^uintptr(0) // HTTRANSPARENT
	case wmMouseActivate:
		return 4 // MA_NOACTIVATEANDEAT
	}
	result, _, _ := defWindowProc.Call(hwnd, uintptr(message), wparam, lparam)
	return result
}

// Open creates and verifies the exact-title compatibility window on the current
// OS thread, then opens Tray if configured. It returns partial evidence and an
// error on unavailable native APIs or failed setup/property checks.
// Even on failure, the caller must call Close on this thread for partial resources.
// ImageNameMatches is reported but not required by Open; the host checks it.
func (w *Window) Open() (Verification, error) {
	state := Verification{Requested: true}
	for _, proc := range []*windows.LazyProc{
		registerClass, unregisterClass, createWindow, destroyWindow, defWindowProc,
		setWindowPos, setLayeredAttributes, getLayeredAttributes, getWindowLong,
		getWindow, getParent, getAncestor, getWindowText, getWindowRect,
		setTimer, killTimer, getMessage, translateMessage, dispatchMessage, postQuitMessage,
	} {
		if proc.Find() != nil {
			return state, windowFault("required_User32_entrypoint_unavailable")
		}
	}
	w.thread = windows.GetCurrentThreadId()
	if windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, nil, &w.module) != nil {
		return state, windowFault("GetModuleHandleExW_failed")
	}
	name := fmt.Sprintf("VxBridgeResearch.ProbeV2.%d.%d", os.Getpid(), windowSequence.Add(1))
	w.className = windows.StringToUTF16Ptr(name)
	class := windowClass{
		Size:      uint32(unsafe.Sizeof(windowClass{})),
		WndProc:   compatibilityWndProc,
		Instance:  uintptr(w.module),
		ClassName: w.className,
	}
	w.atom, _, _ = registerClass.Call(uintptr(unsafe.Pointer(&class)))
	if w.atom == 0 {
		return state, windowFault("RegisterClassExW_failed")
	}
	title := windows.StringToUTF16Ptr(windowTitle)
	position := int32(-32000)
	hwnd, _, _ := createWindow.Call(
		wsExToolWindow|wsExNoActivate|wsExTransparent|wsExLayered,
		uintptr(unsafe.Pointer(w.className)), uintptr(unsafe.Pointer(title)), wsPopup,
		uintptr(position), uintptr(position), 1, 1, 0, 0, uintptr(w.module), 0,
	)
	if hwnd == 0 {
		return state, windowFault("CreateWindowExW_failed")
	}
	w.hwnd, w.created, state.Created = windows.HWND(hwnd), true, true
	if ok, _, _ := setLayeredAttributes.Call(hwnd, 0, 0, 2); ok == 0 {
		return state, windowFault("SetLayeredWindowAttributes_failed")
	}
	// Unlike the first ShowWindow call, this is not overridden by inherited hidden startup info.
	const showWithoutActivation = 0x0040 | 0x0010 | 0x0004 | 0x0002 | 0x0001
	if ok, _, _ := setWindowPos.Call(hwnd, 0, 0, 0, 0, 0, showWithoutActivation); ok == 0 {
		return state, windowFault("SetWindowPos_failed")
	}
	w.timer, _, _ = setTimer.Call(hwnd, 1, 50, 0)
	if w.timer == 0 {
		return state, windowFault("SetTimer_failed")
	}
	state, err := w.verify()
	if err == nil && w.Tray != nil {
		trayWindows.Store(hwnd, w)
		w.Tray.Open(hwnd)
	}
	return state, err
}

func (w *Window) verify() (Verification, error) {
	state := Verification{Requested: true, Created: true}
	image, err := os.Executable()
	if err != nil {
		return state, windowFault("process_image_check_failed")
	}
	state.ImageNameMatches = strings.EqualFold(filepath.Base(image), "GSPconnect.exe")
	var pid uint32
	tid, err := windows.GetWindowThreadProcessId(w.hwnd, &pid)
	if err != nil {
		return state, windowFault("GetWindowThreadProcessId_failed")
	}
	state.SameProcess = pid == uint32(os.Getpid())
	state.SameOwnerThread = tid == w.thread && tid == windows.GetCurrentThreadId()
	owner, _, _ := getWindow.Call(uintptr(w.hwnd), 4) // GW_OWNER
	parent, _, _ := getParent.Call(uintptr(w.hwnd))
	root, _, _ := getAncestor.Call(uintptr(w.hwnd), 2) // GA_ROOT
	styleIndex, exStyleIndex := int32(-16), int32(-20)
	style, _, _ := getWindowLong.Call(uintptr(w.hwnd), uintptr(styleIndex))
	exStyle, _, _ := getWindowLong.Call(uintptr(w.hwnd), uintptr(exStyleIndex))
	state.Unowned = owner == 0
	state.TopLevel = parent == 0 && root == uintptr(w.hwnd) && style&wsChild == 0 && style&wsPopup != 0
	state.ToolWindow = exStyle&wsExToolWindow != 0
	state.NoActivate = exStyle&wsExNoActivate != 0
	state.ClickThrough = exStyle&wsExTransparent != 0
	state.Layered = exStyle&wsExLayered != 0
	state.NotAppWindow = exStyle&wsExAppWindow == 0
	state.Visible = windows.IsWindowVisible(w.hwnd)
	state.VisibleStyle = style&0x10000000 != 0
	state.DesktopVisible = windows.IsWindowVisible(windows.GetDesktopWindow())
	state.NotForeground = windows.GetForegroundWindow() != w.hwnd
	var text [64]uint16
	n, _, _ := getWindowText.Call(uintptr(w.hwnd), uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
	state.ExactFixedTitle = n == uintptr(len(windowTitle)) && windows.UTF16ToString(text[:]) == windowTitle
	var key, flags uint32
	var alpha byte
	ok, _, _ := getLayeredAttributes.Call(uintptr(w.hwnd), uintptr(unsafe.Pointer(&key)),
		uintptr(unsafe.Pointer(&alpha)), uintptr(unsafe.Pointer(&flags)))
	state.AlphaZero = ok != 0 && flags&2 != 0 && alpha == 0
	var rect struct{ left, top, right, bottom int32 }
	ok, _, _ = getWindowRect.Call(uintptr(w.hwnd), uintptr(unsafe.Pointer(&rect)))
	state.OffscreenOnePixel = ok != 0 && rect.left == -32000 && rect.top == -32000 &&
		rect.right-rect.left == 1 && rect.bottom-rect.top == 1
	if !state.Verified() {
		otherProperties := state
		otherProperties.Visible = true
		if !state.Visible && !state.DesktopVisible && otherProperties.Verified() {
			return state, windowFault("noninteractive_desktop_not_visible")
		}
		return state, windowFault("native_window_properties_not_verified")
	}
	return state, nil
}

// Loop services native messages after Open on the same locked OS thread.
// Cancellation is checked between messages and returns nil; the installed
// timer lets an idle loop observe it. Native retrieval failures, unexpected
// WM_QUIT, or an unhandled WM_CLOSE return errors without releasing resources.
// The host must not cancel an attached source's window merely because I/O fails.
func (w *Window) Loop(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		var message nativeMessage
		result, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if err := checkMessageResult(int32(result)); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		if message.Message == wmClose {
			if w.Tray != nil && w.Tray.Handle(wmClose, message.WParam, message.LParam) {
				continue
			}
			return windowFault("unexpected_WM_CLOSE")
		}
		if message.Message == wmTimer && w.Tray != nil {
			w.Tray.Handle(wmTimer, message.WParam, message.LParam)
		}
		if message.Message != wmTimer {
			translateMessage.Call(uintptr(unsafe.Pointer(&message)))
			dispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
		}
	}
}

// Close releases tray, timer, HWND, and class resources on the creating OS
// thread, returning cleanup evidence and joined native failures. Wrong-thread
// cleanup is refused. It must run after the host's source-departure guard or
// after failed startup before any source is admitted; it does not enforce that
// bridge lifecycle condition itself.
func (w *Window) Close() (Cleanup, error) {
	state := Cleanup{OwnerThread: w.thread == windows.GetCurrentThreadId()}
	if w.thread != 0 && !state.OwnerThread {
		return state, windowFault("cleanup_wrong_owner_thread")
	}
	var failures []error
	if w.Tray != nil {
		trayWindows.Delete(uintptr(w.hwnd))
		failures = append(failures, w.Tray.Close())
	}
	if w.timer != 0 && windows.IsWindow(w.hwnd) {
		if ok, _, _ := killTimer.Call(uintptr(w.hwnd), w.timer); ok == 0 {
			failures = append(failures, windowFault("KillTimer_failed"))
		}
	}
	if w.created {
		if windows.IsWindow(w.hwnd) {
			if ok, _, _ := destroyWindow.Call(uintptr(w.hwnd)); ok == 0 {
				failures = append(failures, windowFault("DestroyWindow_failed"))
			}
		}
		state.Destroyed = !windows.IsWindow(w.hwnd)
		if !state.Destroyed {
			failures = append(failures, windowFault("window_still_exists"))
		}
	}
	if w.atom != 0 {
		if ok, _, _ := unregisterClass.Call(uintptr(unsafe.Pointer(w.className)), uintptr(w.module)); ok == 0 {
			failures = append(failures, windowFault("UnregisterClassW_failed"))
		} else {
			state.ClassReleased = true
		}
	}
	return state, errors.Join(failures...)
}
