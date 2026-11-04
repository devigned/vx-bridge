package desktop

import (
	"errors"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestNativeTrayABIEmbeddedIconsAndOwnerThreadRestoration(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if unsafe.Sizeof(iconData{}) != 976 {
		t.Fatalf("incorrect Windows x64 NOTIFYICONDATAW layout: %d", unsafe.Sizeof(iconData{}))
	}
	class := windows.StringToUTF16Ptr("STATIC")
	title := windows.StringToUTF16Ptr("P02 isolated tray test")
	hwnd, _, _ := createOwner.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		0x80000000, 0, 0, 1, 1, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatal("private native test window creation failed")
	}
	defer destroyOwner.Call(hwnd)
	r := testRuntime(t)
	var commands []uint32
	tray := &tray{runtime: r, notify: func(command uint32, data *iconData) error {
		if data.Window != hwnd {
			t.Error("tray replaced its callback/identity window")
		}
		commands = append(commands, command)
		return nil
	}, warned: true} // Never show a real modal or icon on the SSH/session-0 desktop.
	tray.Open(hwnd)
	if !tray.added || !tray.version || len(tray.handles) != 4 {
		t.Fatal("native tray setup failed", r.ui)
	}
	var pid uint32
	tid, err := windows.GetWindowThreadProcessId(windows.HWND(tray.owner), &pid)
	if err != nil || tid != windows.GetCurrentThreadId() || pid != windows.GetCurrentProcessId() ||
		windows.IsWindowVisible(windows.HWND(tray.owner)) {
		t.Fatal("menu owner is visible or on the wrong process/thread")
	}
	owner, _, _ := user32.NewProc("GetWindow").Call(tray.owner, 4)
	if owner != hwnd {
		t.Fatal("menu window can become an unowned compatibility main window")
	}
	commands = nil
	if !tray.Handle(tray.taskbar, 0, 0) || len(commands) != 2 || commands[0] != nimAdd || commands[1] != nimVersion {
		t.Fatal("TaskbarCreated did not restore icon and v4 registration")
	}
	for i := 0; i < 10; i++ {
		tray.Handle(0x0010, 0, 0)
	}
	if !r.quit.Load() || !windows.IsWindow(windows.HWND(hwnd)) {
		t.Fatal("repeated WM_CLOSE bypassed guarded Quit")
	}
	tray.notify = func(uint32, *iconData) error { return errors.New("injected_shell_failure") }
	tray.Handle(tray.taskbar, 0, 0)
	if r.ui != "tray_restore_failed_use_control" || !windows.IsWindow(windows.HWND(hwnd)) {
		t.Fatal("restore failure was hidden or destroyed identity")
	}
	tray.notify = func(uint32, *iconData) error { return nil }
	tray.Handle(tray.taskbar, 0, 0)
	if r.ui != "tray_available" {
		t.Fatal("restoration did not recover its explicit degraded state")
	}
	dismissed := 0
	tray.menuOpen = true
	tray.dismiss = func() error { dismissed++; return nil }
	tray.Handle(0x0113, 0, 0)
	if dismissed != 0 {
		t.Fatal("popup dismissed before engine's guarded completion")
	}
	r.BeginStop()
	tray.Handle(0x0113, 0, 0)
	if dismissed != 1 {
		t.Fatal("guarded shutdown cannot escape a nested native popup loop")
	}
	tray.menuOpen = false
	if err := tray.Close(); err != nil {
		t.Fatal(err)
	}
	if !windows.IsWindow(windows.HWND(hwnd)) || windows.IsWindow(windows.HWND(tray.owner)) {
		t.Fatal("tray cleanup destroyed identity or leaked menu owner")
	}
}

func TestInvalidIconAndWrongThreadDispatchAreExplicit(t *testing.T) {
	if _, err := iconImage([]byte("not-an-icon"), 16); err == nil {
		t.Fatal("invalid icon accepted")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r := testRuntime(t)
	tray := &tray{runtime: r, thread: windows.GetCurrentThreadId(), warned: true}
	done := make(chan error, 1)
	go func() {
		tray.Handle(0x0010, 0, 0)
		done <- tray.Close()
	}()
	if err := <-done; err == nil || r.ui != "tray_wrong_owner_thread_use_control" || r.quit.Load() {
		t.Fatal("wrong-owner native work was accepted or failure hidden")
	}
}
