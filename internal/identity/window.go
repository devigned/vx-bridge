// Package identity supplies the Labs-facing native window and exclusive source
// listener used before the bridge starts. [Window] preserves the exact
// "ProTee United VX" identity without becoming an ordinary foreground window;
// verification is local evidence, not permission to replace vendor software.
//
// The host keeps Open, Loop, and Close on one locked OS thread and authorizes
// cancellation/cleanup only after the bridge's source-first shutdown guard.
// Tray failures are diagnostic, not permission to destroy an attached receiver.
// Non-Windows window methods are stubs, and the portable listener lacks Winsock
// exclusive-address semantics.
package identity

const windowTitle = "ProTee United VX"

// Verification records native identity properties. Window returns setup/loop
// errors separately; failure strings and cleanup fields are available for
// diagnostic callers to combine observations, not automatically filled by Open.
type Verification struct {
	// Requested reports that window setup was attempted.
	Requested bool `json:"requested"`
	// Created reports that native window creation succeeded.
	Created bool `json:"created"`
	// SameProcess reports that the HWND belongs to the current process.
	SameProcess bool `json:"same_process"`
	// SameOwnerThread reports that verification ran on the creating OS thread.
	SameOwnerThread bool `json:"same_owner_thread"`
	// Unowned reports that the top-level window has no owner HWND.
	Unowned bool `json:"unowned"`
	// TopLevel reports a parentless popup that is its own root, not a child.
	TopLevel bool `json:"top_level"`
	// ExactFixedTitle reports the exact Labs-facing title.
	ExactFixedTitle bool `json:"exact_fixed_title"`
	// Visible is the native IsWindowVisible result for the compatibility HWND.
	Visible bool `json:"visible"`
	// VisibleStyle reports that the window has WS_VISIBLE.
	VisibleStyle bool `json:"visible_style"`
	// DesktopVisible records desktop-window visibility for failure diagnosis;
	// it is not included in Verified's property conjunction.
	DesktopVisible bool `json:"desktop_visible"`
	// ToolWindow reports WS_EX_TOOLWINDOW.
	ToolWindow bool `json:"tool_window"`
	// NoActivate reports WS_EX_NOACTIVATE.
	NoActivate bool `json:"no_activate"`
	// ClickThrough reports WS_EX_TRANSPARENT.
	ClickThrough bool `json:"click_through"`
	// Layered reports WS_EX_LAYERED.
	Layered bool `json:"layered"`
	// AlphaZero reports layered alpha transparency with alpha set to zero.
	AlphaZero bool `json:"alpha_zero"`
	// OffscreenOnePixel reports a 1x1 rectangle at (-32000, -32000).
	OffscreenOnePixel bool `json:"offscreen_one_pixel"`
	// NotAppWindow reports the absence of WS_EX_APPWINDOW.
	NotAppWindow bool `json:"not_app_window"`
	// NotForeground reports that the HWND is not the foreground window.
	NotForeground bool `json:"not_foreground"`
	// ImageNameMatches reports a GSPconnect.exe basename, checked by the host
	// separately from Verified.
	ImageNameMatches bool `json:"image_name_matches"`
	// CleanupOwnerThread can record Cleanup.OwnerThread for a combined report.
	CleanupOwnerThread bool `json:"cleanup_owner_thread"`
	// Destroyed can record Cleanup.Destroyed for a combined report.
	Destroyed bool `json:"destroyed"`
	// ClassReleased can record Cleanup.ClassReleased for a combined report.
	ClassReleased bool `json:"class_released"`
	// SetupFailure can retain a sanitized setup failure classification.
	SetupFailure string `json:"setup_failure,omitempty"`
	// LoopFailure can retain a sanitized message-loop failure classification.
	LoopFailure string `json:"loop_failure,omitempty"`
	// CleanupFailure can retain a sanitized cleanup failure classification.
	CleanupFailure string `json:"cleanup_failure,omitempty"`
}

// Verified reports whether all required created-window properties hold.
// It does not include Requested, DesktopVisible, ImageNameMatches, failure
// strings, or cleanup results; the host separately requires ImageNameMatches.
func (v Verification) Verified() bool {
	return v.Created && v.SameProcess && v.SameOwnerThread && v.Unowned && v.TopLevel &&
		v.ExactFixedTitle && v.Visible && v.VisibleStyle && v.ToolWindow && v.NoActivate && v.ClickThrough &&
		v.Layered && v.AlphaZero && v.OffscreenOnePixel && v.NotAppWindow && v.NotForeground
}

// Cleanup records resource-release evidence. OwnerThread reports the creating
// OS thread, Destroyed reports the created HWND no longer exists, and
// ClassReleased reports successful class unregistration. A zero flag is not
// proof of cleanup, including when that resource was never created.
type Cleanup struct {
	// OwnerThread reports that cleanup ran on the creating OS thread.
	// Destroyed reports that the created HWND no longer exists.
	// ClassReleased reports successful class unregistration.
	OwnerThread, Destroyed, ClassReleased bool
}

// Tray runs on this window's owning OS thread. Its failures are diagnostic,
// never permission to destroy the compatibility window or close Labs.
type Tray interface {
	// Open initializes the tray for the compatibility HWND on its owner thread.
	// Implementations expose failures diagnostically rather than return them.
	Open(uintptr)
	// Handle reports whether it consumed a native message on that thread.
	// A close message must remain a guarded Quit request, not forced teardown.
	Handle(uint32, uintptr, uintptr) bool
	// Close releases only tray resources on the owner thread after the host
	// authorizes source-first cleanup, returning native cleanup failures.
	Close() error
}

type windowFault string

// Error returns the native failure classification without OS/vendor payloads.
func (e windowFault) Error() string { return string(e) }

func checkMessageResult(result int32) error {
	if result == -1 {
		return windowFault("GetMessageW_failed")
	}
	if result == 0 {
		return windowFault("unexpected_WM_QUIT")
	}
	return nil
}
