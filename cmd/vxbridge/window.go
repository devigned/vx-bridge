package main

import (
	"context"
	"errors"
	"runtime"

	"github.com/devigned/vx-bridge/internal/identity"
)

type windowSession struct {
	ready chan error
	done  chan error
}

type windowBackend interface {
	// Open creates and verifies native identity resources on the owner thread.
	Open() (identity.Verification, error)
	// Loop services that thread until cancellation or a native-loop failure.
	Loop(context.Context) error
	// Close releases resources on the same thread and reports cleanup evidence.
	Close() (identity.Cleanup, error)
}

func startWindowBackend(ctx context.Context, faults chan<- string, window windowBackend) windowSession {
	session := windowSession{make(chan error, 1), make(chan error, 1)}
	go func() {
		runtime.LockOSThread()
		state, setup := window.Open()
		if setup == nil && (!state.Verified() || !state.ImageNameMatches) {
			setup = errors.New("identity_not_verified")
		}
		session.ready <- setup
		var loop error
		if setup == nil {
			loop = window.Loop(ctx)
			if loop == nil && ctx.Err() == nil {
				loop = errors.New("identity_loop_exited_unexpectedly")
			}
			if loop != nil {
				select {
				case faults <- "identity_message_loop_failed":
				case <-ctx.Done():
				}
				// Keep the owner thread and remaining window resources alive.
				// The engine, not an I/O/UI error, decides when departure permits Quit.
				<-ctx.Done()
			}
		}
		cleanup, err := window.Close()
		if state.Created && (!cleanup.OwnerThread || !cleanup.Destroyed || !cleanup.ClassReleased) {
			err = errors.Join(err, errors.New("identity_cleanup_not_verified"))
		}
		session.done <- errors.Join(setup, loop, err)
	}()
	return session
}
