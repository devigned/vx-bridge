package main

import (
	"context"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

func assertNativeCleanup(t *testing.T, backend *nativeWindow, state windowVerification) {
	t.Helper()
	if !state.CleanupOwnerThread || !state.Destroyed || !state.ClassReleased {
		t.Fatalf("native resources leaked: %+v", state)
	}
}

func TestNativeWindowPropertiesAndOwnerCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend := &nativeWindow{}
	session := startWindow(ctx, backend)
	startup := <-session.ready
	if startup.state.SetupFailure == "noninteractive_desktop_not_visible" && !startup.state.DesktopVisible {
		final := awaitWindow(t, session)
		assertNativeCleanup(t, backend, final)
		t.Log("SSH desktop is noninteractive: WS_VISIBLE set, IsWindowVisible false; production fails closed before accept")
		return
	}
	if startup.state.SetupFailure != "" || !startup.state.Verified() {
		cancel()
		final := awaitWindow(t, session)
		t.Fatalf("native setup failed: %+v cleanup=%+v", startup.state, final)
	}
	cancel()
	final := awaitWindow(t, session)
	if final.LoopFailure != "" || final.CleanupFailure != "" {
		t.Fatalf("native lifecycle failed: %+v", final)
	}
	assertNativeCleanup(t, backend, final)
}

// Session 0 can exercise the real pump/teardown without claiming visible-desktop
// compatibility. The production startWindow path still rejects invisible windows.
func nativeTestLoop(listener *net.TCPListener, config limits, report *runReport) outcome {
	ctx, cancel := context.WithCancel(config.context)
	defer cancel()
	ready := make(chan windowVerification, 1)
	done := make(chan windowVerification, 1)
	backend := &nativeWindow{}
	go func() {
		runtime.LockOSThread()
		state, err := backend.open()
		if err != nil && err.Error() != "noninteractive_desktop_not_visible" {
			state.SetupFailure = err.Error()
		}
		ready <- state
		if state.SetupFailure == "" {
			if err := backend.loop(ctx); err != nil {
				state.LoopFailure = err.Error()
			}
		}
		cleanup, err := backend.close()
		state.CleanupOwnerThread, state.Destroyed, state.ClassReleased =
			cleanup.ownerThread, cleanup.destroyed, cleanup.classReleased
		if err != nil {
			state.CleanupFailure = err.Error()
		}
		done <- state
	}()
	startup := <-ready
	if startup.SetupFailure != "" {
		cancel()
		report.Window = <-done
		listener.Close()
		return windowSetupFailed
	}
	report.Window = startup
	result := receiveOne(listener, config, report)
	cancel()
	report.Window = <-done
	if report.Window.LoopFailure != "" || report.Window.CleanupFailure != "" {
		return windowOutcome(report.Window, result)
	}
	// This returns the invented peer's network result, not a production-window qualification.
	return result
}

func TestNativeWindowTeardownAfterNetworkEOFAndShots(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  outcome
	}{
		{"EOF", "", earlyEOF},
		{"malformed", `{"`, invalidFrame},
		{"two_shots", envelope("1", true, true, false) + envelope("2", true, false, false), positive},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, report, _, _ := exchangeUsing(t, test.input, testLimits(), nativeTestLoop)
			if result != test.want {
				t.Fatalf("result=%v window=%+v", result, report.Window)
			}
			if !report.Window.CleanupOwnerThread || !report.Window.Destroyed || !report.Window.ClassReleased {
				t.Fatalf("native pump did not release resources: %+v", report.Window)
			}
		})
	}
}

func TestNativeWindowPersistsPastGoalUntilSourceEOF(t *testing.T) {
	report := exercisePersistentGoal(t, nativeTestLoop, nil)
	if !report.Window.CleanupOwnerThread || !report.Window.Destroyed || !report.Window.ClassReleased {
		t.Fatalf("source-first EOF did not release native resources: %+v", report.Window)
	}
}

func TestNativeWindowTeardownAfterAcceptDeadline(t *testing.T) {
	listener, err := listenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	config := testLimits()
	config.deadline = time.Now().Add(300 * time.Millisecond)
	report := memoryReport(t, io.Discard)
	result := nativeTestLoop(listener, config, report)
	if result != noConnection || !report.Window.CleanupOwnerThread || !report.Window.Destroyed || !report.Window.ClassReleased {
		t.Fatalf("result=%v window=%+v", result, report.Window)
	}
}

func TestNativeDiagnosticFailsClosedWhenDesktopIsInvisible(t *testing.T) {
	listener, err := listenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	backend := &nativeWindow{}
	config := testLimits()
	deadline := &windowReadyDeadline{Context: context.Background(), done: make(chan struct{})}
	defer deadline.expire()
	config.context = deadline
	config.deadline = time.Now().Add(maxLifetime)
	if guard, ok := t.Deadline(); ok {
		config.deadline = guard
	}
	report := memoryReport(t, io.Discard)
	result := runDiagnostic(listener, config, report,
		func(ctx context.Context) *windowSession {
			session := startWindow(ctx, backend)
			startup := <-session.ready
			ready := make(chan windowStartup, 1)
			ready <- startup
			if startup.state.SetupFailure == "" {
				deadline.expire()
			}
			return &windowSession{ready: ready, done: session.done}
		})
	if report.Window.SetupFailure == "noninteractive_desktop_not_visible" {
		if result != windowSetupFailed || report.Connected || report.Window.Visible || !report.Window.VisibleStyle {
			t.Fatalf("invisible desktop was not rejected: %v %+v", result, report.Window)
		}
	} else if result != noConnection || !report.Window.Verified() {
		t.Fatalf("unexpected native result: %v %+v", result, report.Window)
	}
	assertNativeCleanup(t, backend, report.Window)
}

// The network deadline is induced only after the real native setup result exists.
type windowReadyDeadline struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func (c *windowReadyDeadline) Done() <-chan struct{} { return c.done }
func (c *windowReadyDeadline) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}
func (c *windowReadyDeadline) expire() { c.once.Do(func() { close(c.done) }) }

type failedNativeMessageWindow struct{ nativeWindow }

func (w *failedNativeMessageWindow) open() (windowVerification, error) {
	state, err := w.nativeWindow.open()
	if err != nil && err.Error() == "noninteractive_desktop_not_visible" {
		// Isolate GetMessage error handling from the separately tested visibility gate.
		state.Visible = true
		return state, nil
	}
	return state, err
}

func (*failedNativeMessageWindow) loop(context.Context) error { return checkMessageResult(-1) }

func TestNativeResourcesCleanedAfterMessageLoopFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend := &failedNativeMessageWindow{}
	session := startWindow(ctx, backend)
	<-session.ready
	final := awaitWindow(t, session)
	if final.SetupFailure != "" || final.LoopFailure != "GetMessageW_failed" {
		t.Fatalf("GetMessage failure was not explicit: %+v", final)
	}
	assertNativeCleanup(t, &backend.nativeWindow, final)
}
