package desktop

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
	"github.com/devigned/vx-bridge/internal/destination"
	"github.com/devigned/vx-bridge/internal/protocol"
)

func TestMenuUsesTransportFactsNeverReadinessFlags(t *testing.T) {
	state := bridge.Snapshot{Source: "attached", Target: "connected_ready_unknown", Ready: "unknown",
		Quit: "not_requested", Desktop: "tray_available", LocalLogs: "local_logs", Diagnostics: "local_logs",
		Startup: "startup_off", Counts: bridge.Counts{TargetAccept: 4, Dropped: 2, Uncertain: 1},
		SourceFlags: protocol.Flags{LaunchMonitorIsReady: true, LaunchMonitorBallDetected: true},
		Recent:      []bridge.Event{{At: time.Now(), Reason: "source_attached"}}}
	view := menu(state, true)
	text := fmt.Sprint(view.Items)
	for _, required := range []string{"VX Bridge (prototype)", "Auto-launch on &Windows Startup",
		"Starts when you sign in to Windows.",
		"ProTee Labs: Connected", "Infinite Tees: Connected (transport only)",
		"Game readiness: not reported", "Forwarded (target 200): 4", "Missed/discarded: 2 | Uncertain: 1"} {
		if !strings.Contains(text, required) {
			t.Fatal("missing actual state", required)
		}
	}
	if view.Icon != "linked" || strings.Contains(text, "ready to") || strings.Contains(text, "READY") {
		t.Fatal("transport was presented as swing readiness")
	}
	for _, item := range view.Items[:4] {
		if !item.Disabled {
			t.Fatal("status header is an unexpected action")
		}
	}
}

func TestGameSelectorShowsAppliedPendingAndUnsavedStates(t *testing.T) {
	state := bridge.Snapshot{Source: "attached", Target: "connected_ready_unknown", Ready: "unknown",
		Quit: "not_requested", Desktop: "tray_available", LocalLogs: "local_logs",
		Diagnostics: "local_logs", Startup: "startup_off", Destination: destination.GSPro, Switch: "idle"}
	view := menu(state, true)
	text := fmt.Sprint(view.Items)
	for _, required := range []string{"GSPro OpenAPI: Connected (transport only)", "not yet qualified",
		"Use &Infinite Tees", "Use &GSPro OpenAPI", "Standby (discard shots)", "Game readiness: not reported"} {
		if !strings.Contains(text, required) {
			t.Fatal("selector hides prototype/destination truth", required)
		}
	}
	for _, item := range view.Items {
		if id, selected := actionDestination(item.Action); selected {
			if item.Disabled || item.Checked != (id == destination.GSPro) {
				t.Fatal("selector checkmark does not describe applied destination")
			}
		}
	}
	state.RequestedDestination, state.Switch = destination.InfiniteTees, "waiting_current_delivery"
	state.DestinationPreference = "destination_preference_save_failed"
	view = menu(state, true)
	text = fmt.Sprint(view.Items)
	for _, required := range []string{"Switching to Infinite Tees", "new shots discarded", "not saved", "Waiting for current receipt"} {
		if !strings.Contains(text, required) {
			t.Fatal("pending switch/persistence failure hidden", required)
		}
	}
	for _, item := range view.Items {
		if _, selected := actionDestination(item.Action); selected && !item.Disabled {
			t.Fatal("conflicting destination choice enabled during a switch")
		}
		if item.Action == Quit && item.Disabled {
			t.Fatal("switch disabled guarded Quit")
		}
	}
	state.Destination, state.Switch, state.Target = destination.Standby, "idle", "absent_source"
	if !strings.Contains(fmt.Sprint(menu(state, true).Items), "No game selected; shots discarded") {
		t.Fatal("standby looked connected or silently buffered shots")
	}
}

func TestPendingQuitStorageFailureAndUnknownFaultRemainVisible(t *testing.T) {
	state := bridge.Snapshot{Source: "attached", Target: "target_unavailable",
		Quit: "pending_source_departure", LocalLogs: "local_logs_write_or_rotation_failed_use_status",
		LastError: "invented_fault_with_PRIVATE_PLAYER", Recent: []bridge.Event{{At: time.Now(), Reason: "invented"}}}
	text := fmt.Sprint(menu(state, true).Items)
	for _, required := range []string{"Waiting for Labs to disconnect", "normal Labs", "Local logs unavailable",
		"Unrecognized event", "not replayed"} {
		if !strings.Contains(text, required) {
			t.Fatal("fault/pending state hidden", required)
		}
	}
	if strings.Contains(text, "PRIVATE_PLAYER") || strings.Contains(text, "invented_fault") {
		t.Fatal("unknown faults dumped raw values")
	}
	for _, item := range menu(state, false).Items {
		if item.Action == Quit && item.Disabled {
			t.Fatal("repeated safe Quit was disabled rather than remaining a guarded request")
		}
	}
}

func TestNotificationAnchorPreservesKeyboardAndNegativeMonitorCoordinates(t *testing.T) {
	for _, point := range []screenPoint{{1200, 900}, {-800, 700}, {-1, -1}} {
		packed := uintptr(uint16(int16(point.X))) | uintptr(uint16(int16(point.Y)))<<16
		if got := notificationAnchor(packed); got != point {
			t.Fatalf("notification-area anchor changed: %v != %v", got, point)
		}
	}
}
