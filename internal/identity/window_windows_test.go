package identity

import (
	"context"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestNativePropertiesAndResourceRelease(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		if unsafe.Sizeof(windowClass{}) != 80 || unsafe.Sizeof(nativeMessage{}) != 48 {
			t.Error("incorrect Windows x64 ABI layout")
			return
		}
		w := &Window{}
		state, err := w.Open()
		if err != nil && err.Error() != "noninteractive_desktop_not_visible" {
			t.Errorf("setup: %v", err)
		}
		if err == nil && !state.Verified() {
			t.Error("unverified native window")
		}
		var pid uint32
		tid, ownerErr := windows.GetWindowThreadProcessId(w.hwnd, &pid)
		if ownerErr != nil || pid != windows.GetCurrentProcessId() || tid != w.thread {
			t.Error("window is not owned by actual process and thread")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := w.Loop(ctx); err != nil {
			t.Error(err)
		}
		cleanup, err := w.Close()
		if err != nil || !cleanup.OwnerThread || !cleanup.Destroyed || !cleanup.ClassReleased || windows.IsWindow(w.hwnd) {
			t.Errorf("cleanup: %+v %v", cleanup, err)
		}
		info := windowClass{Size: uint32(unsafe.Sizeof(windowClass{}))}
		proc := probeUser32.NewProc("GetClassInfoExW")
		ok, _, _ := proc.Call(uintptr(w.module), uintptr(unsafe.Pointer(w.className)), uintptr(unsafe.Pointer(&info)))
		if ok != 0 {
			t.Error("class remains registered")
		}
	}()
	<-done
}
