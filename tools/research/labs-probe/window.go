package main

import (
	"context"
	"errors"
	"net"
	"runtime"
	"time"

	"github.com/devigned/vx-bridge/internal/identity"
)

const (
	probeIdentity = "vx-labs-probe/research-only-v3-source-first"
)

type windowVerification = identity.Verification

type windowCleanup struct {
	ownerThread, destroyed, classReleased bool
}

type windowBackend interface {
	open() (windowVerification, error)
	loop(context.Context) error
	close() (windowCleanup, error)
}

type windowStartup struct {
	state    windowVerification
	canceled bool
}

type windowSession struct {
	ready <-chan windowStartup
	done  <-chan windowVerification
}

type windowFault string

// Error returns the probe's native-window failure classification.
func (e windowFault) Error() string { return string(e) }

func startWindow(ctx context.Context, backend windowBackend) *windowSession {
	ready := make(chan windowStartup, 1)
	done := make(chan windowVerification, 1)
	go func() {
		runtime.LockOSThread()
		// Returning while locked retires this thread and its Win32 message queue.
		state := windowVerification{Requested: true}
		if ctx.Err() != nil {
			ready <- windowStartup{state: state, canceled: true}
			done <- state
			return
		}
		state, err := backend.open()
		if err != nil {
			state.SetupFailure = err.Error()
		} else if !state.Verified() {
			state.SetupFailure = "window_properties_not_verified"
		}
		ready <- windowStartup{state: state}
		if state.SetupFailure == "" {
			if err := backend.loop(ctx); err != nil {
				state.LoopFailure = err.Error()
			} else if ctx.Err() == nil {
				state.LoopFailure = "message_loop_exited_without_cancellation"
			}
		}
		cleanup, err := backend.close()
		state.CleanupOwnerThread = cleanup.ownerThread
		state.Destroyed = cleanup.destroyed
		state.ClassReleased = cleanup.classReleased
		if err != nil {
			state.CleanupFailure = err.Error()
		} else if state.Created && (!cleanup.ownerThread || !cleanup.destroyed || !cleanup.classReleased) {
			state.CleanupFailure = "window_cleanup_not_verified"
		}
		done <- state
	}()
	return &windowSession{ready: ready, done: done}
}

// The caller must already own the exclusive listener before starting the window.
func runDiagnostic(listener *net.TCPListener, config limits, report *runReport,
	start func(context.Context) *windowSession) (result outcome) {
	defer func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = closeFailed
		}
	}()
	ctx, cancel := context.WithDeadline(config.context, config.deadline)
	defer cancel()
	parentContext := config.context
	report.NetworkDeadlineUTC = config.deadline.UTC().Format(time.RFC3339Nano)
	session := start(ctx)
	startup := <-session.ready
	report.Window = startup.state
	if startup.canceled || startup.state.SetupFailure != "" {
		report.Window = <-session.done
		result = contextOutcome(ctx, false)
		return windowOutcome(report.Window, result)
	}
	if report.printWindow() != nil {
		cancel()
		report.Window = <-session.done
		return windowOutcome(report.Window, reportFailed)
	}
	config.context = ctx
	networkDone := make(chan outcome, 1)
	go func() { networkDone <- receiveOne(listener, config, report) }()
	select {
	case result = <-networkDone:
		cancel()
		report.Window = <-session.done
	case final := <-session.done:
		cancel()
		result = <-networkDone
		report.Window = final
	}
	if result == positive {
		if parentContext.Err() != nil {
			result = contextOutcome(parentContext, report.Connected)
		} else if !time.Now().Before(config.deadline) {
			result = sessionTimeout
		}
	}
	return windowOutcome(report.Window, result)
}

func windowOutcome(state windowVerification, result outcome) outcome {
	switch {
	case state.CleanupFailure != "":
		return windowCleanupFailed
	case state.SetupFailure != "":
		return windowSetupFailed
	case state.LoopFailure != "":
		return windowLoopFailed
	case result == positive && !state.Verified():
		return windowSetupFailed
	case result == positive && (!state.CleanupOwnerThread || !state.Destroyed || !state.ClassReleased):
		return windowCleanupFailed
	default:
		return result
	}
}

func contextOutcome(ctx context.Context, connected bool) outcome {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if connected {
			return sessionTimeout
		}
		return noConnection
	}
	return interrupted
}

func checkMessageResult(result int32) error {
	if result == -1 {
		return windowFault("GetMessageW_failed")
	}
	if result == 0 {
		return windowFault("unexpected_WM_QUIT")
	}
	return nil
}
