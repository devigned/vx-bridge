package main

import (
	"context"

	"github.com/devigned/vx-bridge/internal/identity"
)

// nativeWindow adapts Window, the shared native identity implementation, to the
// frozen probe's private window backend without adopting bridge shutdown policy.
type nativeWindow struct {
	// Window owns the native resources on the probe's locked owner thread.
	// The probe backend controls its lifecycle, not the persistent bridge.
	identity.Window
}

func (w *nativeWindow) open() (windowVerification, error) { return w.Open() }
func (w *nativeWindow) loop(ctx context.Context) error    { return w.Loop(ctx) }
func (w *nativeWindow) close() (windowCleanup, error) {
	c, err := w.Close()
	return windowCleanup{c.OwnerThread, c.Destroyed, c.ClassReleased}, err
}

func startCompatibilityWindow(ctx context.Context) *windowSession {
	return startWindow(ctx, &nativeWindow{})
}
