// Package desktop turns bridge status into a native Windows tray and dispatches
// explicit user actions outside the bridge and identity owner threads.
// [Runtime] polls [bridge.Controller], decorates snapshots with [diagnostics]
// health and [startup] registration state, and saves [destination] preferences
// after accepted selection requests. Transport connectivity is never presented
// as game readiness. Non-Windows builds support model tests, not a native tray.
package desktop

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
	"github.com/devigned/vx-bridge/internal/destination"
)

// Action identifies an explicit tray command, not a bridge protocol code.
type Action uint32

const (
	// OpenLogs asks the desktop worker to open the configured local log directory.
	OpenLogs Action = 1001 + iota
	// ToggleStartup changes only the owned current-user startup registration.
	ToggleStartup
	// Quit requests guarded shutdown, waiting for normal Labs departure.
	Quit
	// SelectInfiniteTees requests the Infinite Tees profile and saves the choice.
	SelectInfiniteTees
	// SelectGSPro requests the GSPro OpenAPI profile and saves the choice.
	SelectGSPro
	// SelectStandby requests no target connection and saves that choice.
	SelectStandby
	// SaveReport opens a native save dialog and collects a local diagnostic ZIP.
	SaveReport
	// ReportBug opens the empty GitHub bug form, never uploading report data.
	ReportBug
)

// Item describes one tray menu row. Status-only rows are Disabled; separators
// do not dispatch an Action.
type Item struct {
	// Text is the displayed label, including Windows ampersand accelerators.
	Text string
	// Action is the command ID for an actionable row; zero is not a command.
	Action Action
	// Checked displays the current selection or registration checkmark.
	Checked bool
	// Disabled prevents the row from dispatching a command.
	Disabled bool
	// Separator draws a divider instead of a labeled row.
	Separator bool
}

// View is a rendered tray model with a newly built menu slice.
type View struct {
	// Icon is "waiting", "linked", "attention", or "pending".
	// Tooltip is the notification area's short status text.
	Icon, Tooltip string
	// Items is the ordered menu, including status rows and explicit actions.
	Items []Item
}

// ExistingSummary returns a short duplicate-host notice from state without
// changing the host or claiming readiness from a connected transport.
func ExistingSummary(state bridge.Snapshot) string {
	labs := "Labs waiting or may still be attached"
	if state.Source == "attached" {
		labs = "Labs connected"
	}
	target := state.Destination.Name() + " waiting/unavailable"
	if state.Target == "connected_ready_unknown" {
		target = state.Destination.Name() + " transport connected"
	}
	if state.Destination == destination.Standby {
		target = "Standby; shots discarded"
	}
	return labs + "; " + target + "."
}

func statusItem(text string) Item { return Item{Text: text, Disabled: true} }

func menu(snapshot bridge.Snapshot, available bool) View {
	profile := snapshot.Destination.Default()
	labs, target, icon := "Waiting for Labs connection", "Waiting for Labs", "waiting"
	if snapshot.Source == "attached" {
		labs = "Connected"
	}
	if strings.HasPrefix(snapshot.Source, "quarantined") {
		labs, icon = "Fault; may still be connected - Disconnect in Labs", "attention"
	}
	switch snapshot.Target {
	case "connected_ready_unknown":
		target = "Connected (transport only)"
		if icon != "attention" {
			icon = "linked"
		}
	case "connecting", "retrying":
		target = "Connecting; no shots queued"
	case "standby":
		target = "No game selected; shots discarded"
	case "", "absent_source":
	default:
		target, icon = "Unavailable; retrying - shots are not replayed", "attention"
	}
	if profile == destination.Standby {
		target = "No game selected; shots discarded"
	}
	if snapshot.Identity == "fault" {
		labs, icon = "Identity fault; Disconnect in Labs normally", "attention"
	}
	if !available {
		labs, target, icon = "Status unavailable - use vxbridge --status", "Status unavailable", "attention"
	}
	pending := snapshot.Quit != "" && snapshot.Quit != "not_requested" && snapshot.Quit != "complete"
	if pending {
		icon = "pending"
	}
	switching := snapshot.Switch != "" && snapshot.Switch != "idle"
	if switching {
		icon = "pending"
	}
	items := []Item{
		statusItem("VX Bridge (prototype)"),
		statusItem("ProTee Labs: " + labs),
		statusItem(profile.Name() + ": " + target),
		statusItem("Game readiness: not reported"),
	}
	if switching {
		items = append(items, statusItem("Switching to "+snapshot.RequestedDestination.Name()+"; new shots discarded"))
		if snapshot.Switch == "blocked_source_fault_disconnect_Labs" {
			items = append(items, statusItem("Switch blocked by source fault; Disconnect in Labs normally"))
		} else {
			items = append(items, statusItem("Waiting for current receipt and upstream writes"))
		}
	}
	if profile == destination.GSPro {
		items = append(items, statusItem("GSPro live behavior: not yet qualified"))
	}
	if snapshot.DestinationPreference == "destination_preference_save_failed" {
		items = append(items, statusItem("Game choice not saved; select again after switching to retry"))
	} else if snapshot.DestinationPreference == "destination_preference_unconfirmed" {
		items = append(items, statusItem("Game choice not confirmed/saved; inspect status and select again"))
	}
	disabled := !available || pending || switching || strings.HasPrefix(snapshot.Source, "quarantined") || snapshot.Identity == "fault"
	items = append(items, Item{Separator: true},
		Item{Text: "Use &Infinite Tees", Action: SelectInfiniteTees, Checked: profile == destination.InfiniteTees, Disabled: disabled},
		Item{Text: "Use &GSPro OpenAPI", Action: SelectGSPro, Checked: profile == destination.GSPro, Disabled: disabled},
		Item{Text: "&Standby (discard shots)", Action: SelectStandby, Checked: profile == destination.Standby, Disabled: disabled},
		Item{Separator: true},
		statusItem(fmt.Sprintf("Forwarded (target 200): %d", snapshot.Counts.TargetAccept)),
		statusItem(fmt.Sprintf("Missed/discarded: %d | Uncertain: %d", snapshot.Counts.Dropped, snapshot.Counts.Uncertain)))
	last := "No shot or connection event yet"
	if len(snapshot.Recent) > 0 {
		ev := snapshot.Recent[len(snapshot.Recent)-1]
		last = ev.At.Local().Format("15:04:05") + " - " + eventText(ev.Reason)
	}
	items = append(items, statusItem("Last: "+last))
	if snapshot.LastError != "" {
		at := ""
		for i := len(snapshot.Recent) - 1; i >= 0; i-- {
			if snapshot.Recent[i].Reason == snapshot.LastError {
				at = snapshot.Recent[i].At.Local().Format("15:04:05") + " - "
				break
			}
		}
		items = append(items, statusItem("Last error: "+at+eventText(snapshot.LastError)))
	}
	logText := "Local logs active"
	if snapshot.LocalLogs != "local_logs" {
		logText, icon = "Local logs unavailable/slow; bounded status retained", "attention"
	}
	if snapshot.Diagnostics != "local_logs" && snapshot.Diagnostics != "" {
		logText, icon = "Diagnostics degraded; inspect vxbridge --status", "attention"
		if strings.HasPrefix(snapshot.Diagnostics, "control_listener_failed") {
			logText = "Local control unavailable; use tray Quit / Open Logs"
		}
	}
	items = append(items, statusItem(logText))
	if snapshot.Desktop != "tray_available" {
		icon = "attention"
		items = append(items, statusItem("Tray degraded; use vxbridge --status / --quit"))
	}
	if snapshot.DesktopAction != "" {
		items = append(items, statusItem(actionText(snapshot.DesktopAction)))
	}
	registered := snapshot.Startup == "startup_registered_policy_applies"
	switch snapshot.Startup {
	case "startup_off":
		items = append(items, statusItem("Windows startup: not registered"))
	case "startup_registered_policy_applies":
		items = append(items, statusItem("Windows startup: registered; Windows policy applies"))
	case "startup_conflict_not_changed":
		items = append(items, statusItem("Windows startup: another copy/arguments registered; not changed"))
	default:
		items = append(items, statusItem("Windows startup: unavailable; registration not changed"))
	}
	items = append(items, Item{Separator: true},
		Item{Text: "Save &diagnostic report...", Action: SaveReport},
		Item{Text: "&Report a bug", Action: ReportBug},
		Item{Text: "Open &Logs", Action: OpenLogs},
		Item{Text: "Auto-launch on &Windows Startup", Action: ToggleStartup, Checked: registered,
			Disabled: snapshot.Startup != "startup_off" && !registered},
		statusItem("Starts when you sign in to Windows."),
		Item{Separator: true})
	quitText := "&Quit"
	if pending {
		items = append(items, statusItem("Waiting for Labs to disconnect"),
			statusItem("Use normal Labs Game Options Disconnect; Quit will wait"))
		quitText = "&Quit (waiting for Labs Disconnect)"
	}
	items = append(items, Item{Text: quitText, Action: Quit})
	tooltip := "VX Bridge: " + profile.Name() + "; " + strings.ToLower(labs) + "; readiness not reported"
	if pending {
		tooltip = "VX Bridge: waiting for normal Labs Disconnect; Quit never forces"
	} else if switching {
		tooltip = "VX Bridge: switching to " + snapshot.RequestedDestination.Name() + "; do not swing"
	} else if icon == "attention" {
		tooltip = "VX Bridge: attention needed - open menu; game readiness not reported"
	}
	return View{Icon: icon, Tooltip: tooltip, Items: items}
}

func eventText(code string) string {
	switch code {
	case "host_listening_no_game_ready_claim":
		return "Waiting for Labs; game readiness not reported"
	case "source_attached":
		return "Labs connected"
	case "target_connected_ready_unknown_new_shots_only":
		return "Selected game connected; only new shots eligible"
	case "uncorrelated_target_200_not_exactly_once_or_game_ready":
		return "Target accepted a shot; not a game-ready signal"
	case "source_200_complete_local_write_not_peer_consumption":
		return "Actual target acceptance relayed to Labs"
	case "same_session_same_ID_merged":
		return "Split ball and club data combined"
	case "quit_requested_disconnect_Labs_normally":
		return "Quit requested; Disconnect in Labs normally"
	case "delivery_uncertain_never_replay", "target_write_partial_or_uncertain", "target_receipt_deadline_delivery_uncertain":
		return "Shot delivery uncertain; never replayed - check game before swinging"
	case "target_receipt_lane_busy_no_queue_no_ACK":
		return "Shot missed: previous target receipt pending; not queued"
	case "shot_target_eligibility_unavailable_or_changed_never_replay", "shot_no_longer_eligible_never_replay", "held_shot_target_generation_lost":
		return "Shot missed: target unavailable or eligibility changed; not replayed"
	case "duplicate_ball_omitted_no_ACK":
		return "Duplicate shot omitted"
	case "late_or_unmatched_club_omitted":
		return "Late/unmatched club data omitted"
	case "target_unavailable":
		return "Selected game unavailable; retrying without replay"
	case "target_reported_failure_no_success_ACK":
		return "Selected game reported failure; no success sent to Labs"
	case "destination_switch_requested_no_new_shots":
		return "Game change requested; new shots discarded while switching"
	case "destination_switch_applied_new_shots_only":
		return "Destination changed; previous shots never replayed"
	case "held_shot_destination_switch_no_ACK":
		return "Unsent shot discarded for game change; no success sent to Labs"
	case "target_shot_profile_unsupported_no_ACK":
		return "Shot missed: missing or unsupported game data; no success sent to Labs"
	case "destination_change_already_pending":
		return "A game change is already pending; wait before selecting again"
	case "destination_change_blocked_quit":
		return "Game change blocked while Quit is pending"
	case "destination_change_blocked_source_fault":
		return "Game change blocked by source fault; Disconnect in Labs normally"
	case "absent_source":
		return "Waiting for Labs"
	case "source_profile_invalid", "source_quarantined":
		return "Source protocol fault; Disconnect in Labs normally"
	case "diagnostics_sink_failed", "diagnostics_sink_failed_use_status":
		return "Local diagnostics failed; inspect bounded status"
	}
	if strings.HasPrefix(code, "source_departed_") {
		return "Labs transport departed; listening for a new connection"
	}
	if strings.HasPrefix(code, "target_stream_") {
		return "Selected game connection lost; retrying without replay"
	}
	if code == "upstream_response_discarded_destination_switch_pending" {
		return "Old game status omitted while switching"
	}
	// Unknown faults stay explicitly visible without dumping arbitrary values.
	hash := sha256.Sum256([]byte(code))
	return fmt.Sprintf("Unrecognized event %x; inspect vxbridge --status", hash[:4])
}

func actionText(code string) string {
	switch code {
	case "opening_logs":
		return "Opening the local log folder..."
	case "diagnostic_report_pending":
		return "Saving a local diagnostic report; nothing is sent"
	case "diagnostic_report_saved":
		return "Diagnostic report saved locally; nothing was sent"
	case "diagnostic_report_partial":
		return "Partial diagnostic report saved; review summary.txt; nothing was sent"
	case "diagnostic_report_cancelled":
		return "Diagnostic report cancelled; nothing was sent"
	case "diagnostic_report_failed":
		return "Cannot save diagnostic report; report the bug without a ZIP"
	case "report_bug_pending":
		return "Opening the bug form; attach a reviewed report only if you choose"
	case "report_bug_failed":
		return "Cannot open the bug form; use the reporting link in the user guide"
	case "open_logs_failed":
		return "Cannot open local logs; inspect vxbridge --status"
	case "startup_change_pending":
		return "Updating startup registration..."
	case "startup_change_failed":
		return "Startup change failed; registration not confirmed - inspect status"
	case "quit_request_failed":
		return "Quit request failed; use vxbridge --quit (never force close)"
	case "desktop_status_unavailable":
		return "Status query unavailable; use vxbridge --status"
	case "desktop_actions_busy":
		return "Another desktop action is pending"
	case "destination_change_pending":
		return "Requesting game change; do not swing"
	case "destination_change_failed":
		return "Cannot confirm game selection; inspect status before swinging"
	case "destination_preference_save_failed":
		return "Game choice requested but not saved; select again after switching to retry"
	default:
		return "Desktop action failed; inspect vxbridge --status"
	}
}

func actionDestination(action Action) (destination.ID, bool) {
	switch action {
	case SelectInfiniteTees:
		return destination.InfiniteTees, true
	case SelectGSPro:
		return destination.GSPro, true
	case SelectStandby:
		return destination.Standby, true
	default:
		return "", false
	}
}

func eventNow(code string) bridge.Event { return bridge.Event{At: time.Now().UTC(), Reason: code} }

// screenPoint is a signed native screen position; X and Y are pixel coordinates.
type screenPoint struct {
	// X is the signed horizontal screen coordinate in pixels.
	// Y is the signed vertical screen coordinate in pixels.
	X, Y int32
}

// NOTIFYICON_VERSION_4 supplies the icon's anchor even for keyboard activation.
func notificationAnchor(packed uintptr) screenPoint {
	return screenPoint{int32(int16(uint16(packed))), int32(int16(uint16(packed >> 16)))}
}
