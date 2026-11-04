package main

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func verifiedWindow() windowVerification {
	return windowVerification{
		Requested: true, Created: true, SameProcess: true, SameOwnerThread: true,
		Unowned: true, TopLevel: true, ExactFixedTitle: true, Visible: true, VisibleStyle: true,
		ToolWindow: true, NoActivate: true, ClickThrough: true, Layered: true,
		AlphaZero: true, OffscreenOnePixel: true, NotAppWindow: true, NotForeground: true,
	}
}

type fakeWindow struct {
	state        windowVerification
	openErr      error
	loopErr      error
	closeErr     error
	earlyReturn  bool
	badCleanup   bool
	opened       bool
	closed       bool
	loopStarted  chan struct{}
	loopFailure  <-chan struct{}
	closedSignal chan struct{}
}

func (w *fakeWindow) open() (windowVerification, error) {
	w.opened = true
	return w.state, w.openErr
}

func (w *fakeWindow) loop(ctx context.Context) error {
	if w.loopErr != nil || w.earlyReturn {
		return w.loopErr
	}
	if w.loopStarted != nil {
		close(w.loopStarted)
	}
	select {
	case <-ctx.Done():
		return nil
	case <-w.loopFailure:
		return windowFault("injected_message_loop_failure")
	}
}

func (w *fakeWindow) close() (windowCleanup, error) {
	w.closed = true
	if w.closedSignal != nil {
		close(w.closedSignal)
	}
	return windowCleanup{!w.badCleanup, !w.badCleanup, !w.badCleanup}, w.closeErr
}

func awaitWindow(t *testing.T, session *windowSession) windowVerification {
	t.Helper()
	select {
	case state := <-session.done:
		return state
	case <-time.After(3 * time.Second):
		t.Fatal("window owner/message loop leaked")
		return windowVerification{}
	}
}

func TestWindowLifecycleFailuresAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name        string
		backend     *fakeWindow
		preCanceled bool
		want        outcome
	}{
		{"cancel", &fakeWindow{state: verifiedWindow()}, false, earlyEOF},
		{"cancel_before_open", &fakeWindow{state: verifiedWindow()}, true, earlyEOF},
		{"partial_setup_failure", &fakeWindow{state: verifiedWindow(), openErr: windowFault("injected_setup")}, false, windowSetupFailed},
		{"unverified_window", &fakeWindow{state: windowVerification{Requested: true}}, false, windowSetupFailed},
		{"get_message_error", &fakeWindow{state: verifiedWindow(), loopErr: checkMessageResult(-1)}, false, windowLoopFailed},
		{"unexpected_quit", &fakeWindow{state: verifiedWindow(), loopErr: checkMessageResult(0)}, false, windowLoopFailed},
		{"unexpected_loop_exit", &fakeWindow{state: verifiedWindow(), earlyReturn: true}, false, windowLoopFailed},
		{"cleanup_failure", &fakeWindow{state: verifiedWindow(), closeErr: windowFault("injected_cleanup")}, false, windowCleanupFailed},
		{"cleanup_unverified", &fakeWindow{state: verifiedWindow(), badCleanup: true}, false, windowCleanupFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.preCanceled {
				cancel()
			}
			session := startWindow(ctx, test.backend)
			<-session.ready
			// Immediate failure/early-return cases finish without cancellation.
			if test.backend.loopErr == nil && !test.backend.earlyReturn {
				cancel()
			}
			state := awaitWindow(t, session)
			if got := windowOutcome(state, earlyEOF); got != test.want {
				t.Fatalf("outcome=%v state=%+v", got, state)
			}
			if test.preCanceled {
				if test.backend.opened || test.backend.closed {
					t.Fatal("pre-cancellation created resources")
				}
			} else if !test.backend.closed {
				t.Fatal("owner cleanup was skipped")
			}
		})
	}
	if checkMessageResult(1) != nil {
		t.Fatal("ordinary message rejected")
	}
}

func TestDiagnosticWindowLifetimeFollowsNetwork(t *testing.T) {
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
			backend := &fakeWindow{state: verifiedWindow()}
			result, report, _, _ := exchangeUsing(t, test.input, testLimits(),
				func(listener *net.TCPListener, config limits, report *runReport) outcome {
					return runDiagnostic(listener, config, report, func(ctx context.Context) *windowSession {
						return startWindow(ctx, backend)
					})
				})
			if result != test.want || !backend.closed || !report.Window.Destroyed || !report.Window.ClassReleased {
				t.Fatalf("result=%v closed=%t window=%+v", result, backend.closed, report.Window)
			}
		})
	}
}

func TestDiagnosticWindowFailuresCannotReportSuccess(t *testing.T) {
	for _, test := range []struct {
		name    string
		backend *fakeWindow
		want    outcome
	}{
		{"setup", &fakeWindow{state: verifiedWindow(), openErr: windowFault("injected_setup")}, windowSetupFailed},
		{"loop", &fakeWindow{state: verifiedWindow(), loopErr: checkMessageResult(-1)}, windowLoopFailed},
		{"cleanup", &fakeWindow{state: verifiedWindow(), closeErr: windowFault("injected_cleanup")}, windowCleanupFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := listenExclusive("127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			config := testLimits()
			config.deadline = time.Now().Add(40 * time.Millisecond)
			report := memoryReport(t, io.Discard)
			config.deadline = time.Now().Add(150 * time.Millisecond)
			result := runDiagnostic(listener, config, report, func(ctx context.Context) *windowSession {
				return startWindow(ctx, test.backend)
			})
			if result != test.want || !test.backend.closed || report.Connected {
				t.Fatalf("result=%v report=%+v", result, report)
			}
			rebound, err := listenExclusive(address)
			if err != nil {
				t.Fatalf("listener leaked: %v", err)
			}
			rebound.Close()
		})
	}
	backend := &fakeWindow{state: verifiedWindow(), closeErr: windowFault("injected_cleanup")}
	result, report, _, _ := exchangeUsing(t,
		envelope("1", true, true, false)+envelope("2", true, false, false), testLimits(),
		func(listener *net.TCPListener, config limits, report *runReport) outcome {
			return runDiagnostic(listener, config, report, func(ctx context.Context) *windowSession {
				return startWindow(ctx, backend)
			})
		})
	if report.Counts.DistinctAcked != 2 || result != windowCleanupFailed {
		t.Fatalf("window failure did not override two-shot outcome: %+v %v", report.Counts, result)
	}
}

func TestDiagnosticDeadlineAndInterruptCloseWindow(t *testing.T) {
	for _, test := range []struct {
		name      string
		expired   bool
		interrupt bool
		want      outcome
	}{
		{name: "deadline", want: noConnection},
		{name: "deadline_before_factory_cancel", expired: true, want: noConnection},
		{name: "interrupt_after_window_ready", interrupt: true, want: interrupted},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := listenExclusive("127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			address := listener.Addr().String()
			report := memoryReport(t, io.Discard)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			config := testLimits()
			config.context = ctx
			config.deadline = time.Now().Add(40 * time.Millisecond)
			backend := &fakeWindow{state: verifiedWindow()}
			if test.expired {
				// Reproduce setup consuming the deadline before the factory's cancel.
				config.deadline = time.Now().Add(-time.Second)
			}
			if test.interrupt {
				// Only explicit cancellation decides this case; the runner bounds hangs.
				config.deadline = time.Now().Add(maxLifetime)
				if deadline, ok := t.Deadline(); ok {
					config.deadline = deadline
				}
				backend.loopStarted = make(chan struct{})
			}
			result := runDiagnostic(listener, config, report, func(ctx context.Context) *windowSession {
				if test.expired && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
					t.Fatal("expired setup must precede factory cancellation")
				}
				session := startWindow(ctx, backend)
				if test.interrupt {
					select {
					case <-backend.loopStarted:
					case <-time.After(3 * time.Second):
						t.Fatal("test window loop did not start")
					}
					cancel()
				} else if test.expired {
					cancel()
				}
				return session
			})
			if result != test.want || report.Connected {
				t.Fatalf("result=%v wanted=%v window=%+v", result, test.want, report.Window)
			}
			if test.interrupt && (!backend.opened || !report.Window.Verified()) {
				t.Fatal("interrupt did not exercise an opened, verified window")
			}
			if backend.opened && (!backend.closed || !report.Window.CleanupOwnerThread ||
				!report.Window.Destroyed || !report.Window.ClassReleased) {
				t.Fatalf("window cleanup incomplete: %+v", report.Window)
			}
			if test.expired && (backend.opened || backend.closed || report.Window.Created) {
				t.Fatal("expired setup created window resources")
			}
			rebound, err := listenExclusive(address)
			if err != nil {
				t.Fatalf("listener leaked: %v", err)
			}
			if err := rebound.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWindowSetupConsoleFailureCleansUp(t *testing.T) {
	listener, err := listenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeWindow{state: verifiedWindow()}
	result := runDiagnostic(listener, testLimits(), memoryReport(t, brokenConsole{}),
		func(ctx context.Context) *windowSession { return startWindow(ctx, backend) })
	if result != reportFailed || !backend.closed {
		t.Fatalf("result=%v closed=%t", result, backend.closed)
	}
}

func TestPositiveRequiresVerifiedWindowAndCleanup(t *testing.T) {
	state := verifiedWindow()
	state.CleanupOwnerThread, state.Destroyed, state.ClassReleased = true, true, true
	if windowOutcome(state, positive) != positive {
		t.Fatal("verified complete result rejected")
	}
	state.Visible = false
	if windowOutcome(state, positive) != windowSetupFailed {
		t.Fatal("unverified visibility allowed a positive result")
	}
	state.Visible = true
	state.ClassReleased = false
	if windowOutcome(state, positive) != windowCleanupFailed {
		t.Fatal("unverified cleanup allowed a positive result")
	}
}

func TestCanceledDeadlineKeepsDistinctOutcome(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) ||
		contextOutcome(ctx, false) != noConnection || contextOutcome(ctx, true) != sessionTimeout {
		t.Fatal("deadline lost distinct connected/unconnected outcome")
	}
}

type acceptNotice struct {
	accepted chan struct{}
}

func (w acceptNotice) Write(data []byte) (int, error) {
	if strings.Contains(string(data), "client accepted;") {
		close(w.accepted)
	}
	return len(data), nil
}

func TestDiagnosticInterruptClosesConnectedReaderAndWindow(t *testing.T) {
	listener, err := listenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config := testLimits()
	config.context = ctx
	accepted := make(chan struct{})
	report := memoryReport(t, acceptNotice{accepted})
	backend := &fakeWindow{state: verifiedWindow()}
	done := make(chan outcome, 1)
	go func() {
		done <- runDiagnostic(listener, config, report,
			func(ctx context.Context) *windowSession { return startWindow(ctx, backend) })
	}()
	peer, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	select {
	case <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("test peer was not accepted")
	}
	cancel()
	select {
	case result := <-done:
		if result != interrupted || !backend.closed || !report.Window.Destroyed || !report.Connected {
			t.Fatalf("cancel did not release connected UI/network: %v %+v", result, report.Window)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connected cancellation leaked reader or window")
	}
}

func TestDiagnosticConnectedDeadlineIsNotNoConnection(t *testing.T) {
	listener, err := listenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	config := testLimits()
	config.deadline = time.Now().Add(150 * time.Millisecond)
	report := memoryReport(t, io.Discard)
	backend := &fakeWindow{state: verifiedWindow()}
	done := make(chan outcome, 1)
	go func() {
		done <- runDiagnostic(listener, config, report,
			func(ctx context.Context) *windowSession { return startWindow(ctx, backend) })
	}()
	peer, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if result := <-done; result != sessionTimeout || !report.Connected || !backend.closed {
		t.Fatalf("connected timeout lost its distinct outcome: %v connected=%t", result, report.Connected)
	}
}
