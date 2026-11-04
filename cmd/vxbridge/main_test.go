package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
	"github.com/devigned/vx-bridge/internal/destination"
	"github.com/devigned/vx-bridge/internal/identity"
)

func TestNormalStartupRetainsSavedGameAndCompatibilityStorageName(t *testing.T) {
	directory := t.TempDir()
	selected, preferences, err := hostDestination(directory)
	if err != nil || selected != destination.InfiniteTees ||
		preferences.Path != filepath.Join(directory, "VxInfiniteTeesBridge", "destination.json") {
		t.Fatal("branding changed default choice or persistent storage identity", selected, err)
	}
	for _, id := range []destination.ID{destination.GSPro, destination.Standby, destination.InfiniteTees} {
		if err := preferences.Save(id); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(preferences.Path)
		if err != nil {
			t.Fatal(err)
		}
		selected, loaded, err := hostDestination(directory)
		if err != nil || selected != id || loaded.Path != preferences.Path {
			t.Fatal("ordinary startup overrode the saved destination", selected, err)
		}
		after, err := os.ReadFile(preferences.Path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("ordinary startup rewrote the game choice", err)
		}
	}
	if err := preferences.Save(destination.GSPro); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(preferences.Path); err != nil {
		t.Fatal(err)
	}
	if fresh, _, err := hostDestination(directory); err != nil || fresh != destination.InfiniteTees {
		t.Fatal("removing app-managed preferences did not restore fresh defaults", fresh, err)
	}
	if err := os.WriteFile(preferences.Path, []byte(`{"destination":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if selected, _, err := hostDestination(directory); err == nil || selected != "" {
		t.Fatal("invalid saved choice silently fell back to IT")
	}
}

func TestManagedAdmissionDistinguishesHealthyDuplicateFromRepair(t *testing.T) {
	denied := errors.New("synthetic_local_admission_denied")
	queries := 0
	admit := func(image, revision, version string) (io.Closer, error) {
		if image != "synthetic-owned-host" || revision != buildRevision || version != releaseVersion {
			t.Fatal("managed admission lost its exact image/build identity")
		}
		return nil, denied
	}
	query := func(ctx context.Context, quit bool) (bridge.Snapshot, error) {
		queries++
		if quit {
			t.Fatal("duplicate startup requested Quit")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("existing-host lookup was unbounded")
		}
		return bridge.Snapshot{Version: "synthetic", Source: "attached", Destination: destination.GSPro}, nil
	}
	lease, existing, err := admitManaged("synthetic-owned-host", admit, query)
	if lease != nil || existing == nil || existing.Destination != destination.GSPro ||
		!errors.Is(err, denied) || queries != 1 {
		t.Fatal("healthy duplicate was mislabeled as repair or replaced", existing, err)
	}
	_, existing, err = admitManaged("synthetic-owned-host", admit,
		func(context.Context, bool) (bridge.Snapshot, error) {
			return bridge.Snapshot{}, errors.New("synthetic_no_host")
		})
	if existing != nil || !errors.Is(err, denied) {
		t.Fatal("unreachable host silently bypassed managed refusal")
	}
	success := func(string, string, string) (io.Closer, error) { return nil, nil }
	_, existing, err = admitManaged("synthetic-portable-host", success,
		func(context.Context, bool) (bridge.Snapshot, error) {
			t.Fatal("successful portable admission unexpectedly queried control")
			return bridge.Snapshot{}, nil
		})
	if existing != nil || err != nil {
		t.Fatal("portable startup acquired an installer dependency")
	}
}

func TestVersionAndInvalidArgumentsDoNotStartHost(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"--version"}, &out, io.Discard); code != 0 || !strings.Contains(out.String(), version) {
		t.Fatal("missing explicit build version")
	}

	for _, args := range [][]string{
		{"--target-port=12321"}, {"--target-port=0"}, {"--target-port=65536"},
		{"--status", "--quit"}, {"--unknown"}, {"unexpected"}, {"--version", "--quit"},
		{"--diagnostics=report.zip", "--status"}, {"--diagnostics=report.zip", "--version"},
		{"--diagnostics=report.zip", "--quit"}, {"--diagnostics=report.zip", "--diagnostics-dialog"},
		{"--diagnostics-dialog", "--target-port=999"}, {"--diagnostics-dialog", "--desktop"},
		{"--diagnostics=report.zip", "--desktop=false"},
		{"--diagnostics="}, {"--diagnostics-dialog=false"},
	} {
		if code := run(args, io.Discard, io.Discard); code != 2 {
			t.Fatalf("unsafe arguments accepted: %v", args)
		}
	}
}

func TestReleaseAndDevelopmentVersionIdentity(t *testing.T) {
	originalRelease, originalRevision, originalDesktop := releaseVersion, buildRevision, defaultDesktop
	t.Cleanup(func() {
		releaseVersion, buildRevision, defaultDesktop = originalRelease, originalRevision, originalDesktop
	})
	if releaseVersion != "development" || buildRevision != "development" || defaultDesktop != "false" {
		t.Fatal("ordinary test build must retain non-release defaults")
	}
	for _, release := range []string{"development", "v1.0.1"} {
		for _, desktop := range []string{"false", "true"} {
			releaseVersion, buildRevision, defaultDesktop = release, strings.Repeat("a", 40), desktop
			build := diagnosticBuild()
			if build.Release != release || build.Revision != buildRevision || build.Version != version {
				t.Fatal("structured diagnostics lost release/source identity", build)
			}
			var out bytes.Buffer
			if code := run([]string{"--version"}, &out, io.Discard); code != 0 {
				t.Fatal("version query failed")
			}
			for _, expected := range []string{version, "release=" + release,
				"revision=" + buildRevision, "defaultDesktop=" + desktop} {
				if !strings.Contains(out.String(), expected) {
					t.Fatalf("identity lacks %q: %s", expected, out.String())
				}
			}
		}
	}
}

type fakeIdentity struct{ closed chan struct{} }

func (w fakeIdentity) Open() (identity.Verification, error) {
	return identity.Verification{
		Created: true, SameProcess: true, SameOwnerThread: true, Unowned: true,
		TopLevel: true, ExactFixedTitle: true, Visible: true, VisibleStyle: true,
		ToolWindow: true, NoActivate: true, ClickThrough: true, Layered: true,
		AlphaZero: true, OffscreenOnePixel: true, NotAppWindow: true,
		NotForeground: true, ImageNameMatches: true,
	}, nil
}
func (w fakeIdentity) Loop(context.Context) error { return errors.New("injected_window_fault") }
func (w fakeIdentity) Close() (identity.Cleanup, error) {
	close(w.closed)
	return identity.Cleanup{OwnerThread: true, Destroyed: true, ClassReleased: true}, nil
}

func TestIdentityFailureWaitsForEnginePermissionBeforeDestroying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	faults := make(chan string, 1)
	backend := fakeIdentity{closed: make(chan struct{})}
	session := startWindowBackend(ctx, faults, backend)
	if err := <-session.ready; err != nil {
		t.Fatal(err)
	}
	if fault := <-faults; fault != "identity_message_loop_failed" {
		t.Fatal("window failure not surfaced")
	}
	select {
	case <-backend.closed:
		t.Fatal("window failure destroyed identity beneath possibly attached source")
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-session.done:
		if err == nil {
			t.Fatal("message-loop failure hidden")
		}
	case <-time.After(time.Second):
		t.Fatal("identity did not clean up after guarded engine completion")
	}
}

func TestDuplicateLaunchOnlyQueriesExistingHostNeverQuitsOrReplaces(t *testing.T) {
	queries := 0
	listener, existing, err := claimHost(func() (net.Listener, error) {
		return nil, errors.New("invented_instance_conflict")
	}, func(ctx context.Context, quit bool) (bridge.Snapshot, error) {
		queries++
		if quit {
			t.Fatal("duplicate launch requested shutdown")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("duplicate lookup is not bounded")
		}
		return bridge.Snapshot{Version: "test", Source: "attached"}, nil
	})
	if err == nil || listener != nil || existing == nil || existing.Source != "attached" || queries != 1 {
		t.Fatal("duplicate did not preserve/report existing host")
	}
	_, existing, err = claimHost(func() (net.Listener, error) {
		return nil, errors.New("invented_control_fault")
	}, func(context.Context, bool) (bridge.Snapshot, error) {
		return bridge.Snapshot{}, errors.New("invented_query_fault")
	})
	if err == nil || existing != nil {
		t.Fatal("inaccessible host treated as permission to replace it")
	}
}
