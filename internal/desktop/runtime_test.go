package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
	"github.com/devigned/vx-bridge/internal/destination"
	"github.com/devigned/vx-bridge/internal/diagnostics"
	"github.com/devigned/vx-bridge/internal/identity"
)

func testRuntime(t *testing.T) *Runtime {
	t.Helper()
	logger := diagnostics.New(t.TempDir(), diagnostics.Build{Version: "test"})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		logger.Close(ctx)
	})
	return New(bridge.NewController(), logger, nil, "known-log-directory", nil)
}

type blockedPreferences struct {
	entered, release chan struct{}
}

func (p blockedPreferences) Save(destination.ID) error {
	close(p.entered)
	<-p.release
	return errors.New("synthetic_PRIVATE_storage_failure")
}

func TestBlockedPreferenceSaveCannotBlockStatusOrQuitAndFailureIsVisible(t *testing.T) {
	r := testRuntime(t)
	preferences := blockedPreferences{entered: make(chan struct{}), release: make(chan struct{})}
	r.preferences = preferences
	quitSeen, ownerDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(ownerDone)
		state := bridge.Snapshot{Version: "test", Source: "attached", Destination: destination.InfiniteTees}
		for {
			select {
			case request := <-r.controller.Requests:
				if request.Destination != "" {
					state.Destination = request.Destination
					request.Result <- nil
				}
				if request.Quit {
					close(quitSeen)
				}
				request.Reply <- state
			case <-r.controller.Done:
				return
			}
		}
	}()
	r.Start()
	r.Request(SelectGSPro)
	select {
	case <-preferences.entered:
	case <-time.After(time.Second):
		t.Fatal("selection did not reach preference worker")
	}
	r.Request(Quit)
	select {
	case <-quitSeen:
	case <-time.After(time.Second):
		t.Fatal("preference storage blocked guarded Quit")
	}
	close(preferences.release)
	until := time.Now().Add(time.Second)
	for {
		state := bridge.Snapshot{}
		r.Decorate(&state)
		if state.DestinationPreference == "destination_preference_save_failed" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("preference failure was silently treated as remembered")
		}
		time.Sleep(time.Millisecond)
	}
	text := fmt.Sprint(r.View().Items)
	if !strings.Contains(text, "not saved") || strings.Contains(text, "PRIVATE") {
		t.Fatal("preference failure hidden or raw error disclosed")
	}
	close(r.controller.Done)
	<-ownerDone
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestUnconfirmedSelectionNeverLooksRememberedAfterAnotherAction(t *testing.T) {
	r := testRuntime(t)
	close(r.controller.Done)
	r.selectDestination(context.Background(), destination.GSPro)
	r.setAction("")
	state := bridge.Snapshot{}
	r.Decorate(&state)
	if state.DestinationPreference != "destination_preference_unconfirmed" ||
		!strings.Contains(fmt.Sprint(r.View().Items), "not confirmed/saved") {
		t.Fatal("an unconfirmed selection silently looked persisted")
	}
}

func TestDesktopQuitAndDiagnosticFailureKeepAttachedSourceAlive(t *testing.T) {
	// A file used as a directory injects a real storage-open failure. There is
	// no stdout writer at all, nor any runtime port/control/Run registration.
	r := testRuntime(t)
	bad := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(bad, nil, 0600); err != nil {
		t.Fatal(err)
	}
	r.logs = diagnostics.New(filepath.Join(bad, "logs"), diagnostics.Build{Version: "test"})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		r.logs.Close(ctx)
	})
	listener, err := identity.ListenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	config := bridge.Defaults("127.0.0.1:1")
	config.DialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("invented_target_unavailable")
	}
	done := make(chan error, 1)
	go func() {
		done <- bridge.RunWithDiagnostics(listener, config, r.controller, nil, r.logs, "test-no-console")
	}()
	source, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	r.openLogs = func(string) error { return errors.New("invented_shell_error") }
	r.Start()
	until := time.Now().Add(time.Second)
	for {
		query, stop := context.WithTimeout(context.Background(), time.Second)
		state, err := r.controller.Query(query, false)
		stop()
		if err != nil {
			t.Fatal(err)
		}
		if state.Source == "attached" && strings.Contains(state.Diagnostics, "failed") {
			break
		}
		if time.Now().After(until) {
			t.Fatal("sink failure did not reach actual engine status")
		}
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 20; i++ {
		r.Request(Quit)
	}
	time.Sleep(300 * time.Millisecond)
	query, stop := context.WithTimeout(context.Background(), time.Second)
	state, err := r.controller.Query(query, false)
	stop()
	r.Decorate(&state)
	if err != nil || state.Quit != "pending_source_departure" || state.Source != "attached" ||
		!strings.Contains(state.LocalLogs, "logs") {
		t.Fatal("desktop Quit/storage failure lost guarded state", state, err)
	}
	source.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, err := source.Read(make([]byte, 1)); errors.Is(err, io.EOF) {
		t.Fatal("UI/log failure closed attached Labs")
	}
	select {
	case <-done:
		t.Fatal("desktop or diagnostic failure ended attached host")
	default:
	}
	source.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("normal source departure did not complete guarded Quit")
	}
	cleanup, finish := context.WithTimeout(context.Background(), time.Second)
	defer finish()
	if err := r.Close(cleanup); err != nil {
		t.Fatal(err)
	}
}

func TestBlockedDesktopActionCannotBlockStatusOrQuit(t *testing.T) {
	r := testRuntime(t)
	block, entered := make(chan struct{}), make(chan struct{})
	r.openLogs = func(directory string) error {
		if directory != "known-log-directory" {
			t.Error("Open Logs accepted an arbitrary path")
		}
		close(entered)
		<-block
		return nil
	}
	var quits atomic.Uint64
	ownerDone := make(chan struct{})
	go func() {
		defer close(ownerDone)
		for {
			select {
			case request := <-r.controller.Requests:
				if request.Quit {
					quits.Add(1)
				}
				request.Reply <- bridge.Snapshot{Version: "test", Source: "attached", Quit: "pending_source_departure"}
			case <-r.controller.Done:
				return
			}
		}
	}()
	r.Start()
	r.Request(OpenLogs)
	<-entered
	r.Request(Quit)
	until := time.Now().Add(time.Second)
	for quits.Load() == 0 {
		if time.Now().After(until) {
			t.Fatal("Shell action blocked guarded Quit")
		}
		time.Sleep(time.Millisecond)
	}
	close(block)
	close(r.controller.Done)
	<-ownerDone
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
