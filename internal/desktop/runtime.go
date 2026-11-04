package desktop

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
	"github.com/devigned/vx-bridge/internal/destination"
	"github.com/devigned/vx-bridge/internal/diagnostics"
	"github.com/devigned/vx-bridge/internal/identity"
	"github.com/devigned/vx-bridge/internal/startup"
)

type startupManager interface {
	// State reads registration without changing it.
	State() (string, error)
	// Toggle rechecks ownership, changes registration, and verifies the result.
	Toggle() (string, error)
}

type destinationPreferences interface {
	// Save persists an explicit selection independently of bridge transition.
	Save(destination.ID) error
}

// Runtime separates status polling and guarded Quit from potentially blocking
// desktop actions. Construct it with New; the zero value is not usable.
// Serialize Start and Close with lifecycle setup. After setup, Request, UIState,
// Decorate, View, and BeginStop can be called concurrently by UI/control workers.
// Runtime does not own the controller, logger, native window, or their lifetimes.
type Runtime struct {
	controller  *bridge.Controller
	logs        *diagnostics.Logger
	startup     startupManager
	directory   string
	openLogs    func(string) error
	actions     chan Action
	support     chan Action
	supportBusy atomic.Bool
	saveReport  func() (string, error)
	reportBug   func() error
	quit        atomic.Bool
	stopping    atomic.Bool
	mu          sync.Mutex
	snapshot    bridge.Snapshot
	available   bool
	ui, launch  string
	action      string
	preferences destinationPreferences
	preference  string
	cancel      context.CancelFunc
	done        chan struct{}
}

//go:generate go run icons/generate.go

// New creates an unstarted Runtime with non-nil controller and logs.
// A nil manager marks startup unavailable; nil preferences allow selection but
// report that the choice was not saved. directory is the fixed Open Logs path,
// not a path supplied by a tray request. New performs no startup registration.
func New(controller *bridge.Controller, logs *diagnostics.Logger, manager *startup.Manager, directory string, preferences *destination.Preferences) *Runtime {
	r := &Runtime{controller: controller, logs: logs, directory: directory,
		openLogs: openLogDirectory, actions: make(chan Action, 1),
		support: make(chan Action, 1), reportBug: openBugForm,
		ui: "tray_starting", launch: "startup_unavailable", done: make(chan struct{})}
	if preferences != nil {
		r.preferences = preferences
	}

	if manager != nil {
		r.startup = manager
	}
	return r
}

// Reporting configures the shared exporter before Start. It performs no I/O.
func (r *Runtime) Reporting(build diagnostics.Build) {
	r.saveReport = func() (string, error) { return ReportDialog(build) }
}

// Start launches status/Quit polling and separate action/support workers.
// Call it once after the native identity is verified and the controller can be
// serviced. A blocked shell, registration, or preference operation does not block
// the polling worker. Start does not create the native tray.
func (r *Runtime) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case action := <-r.support:
				r.runSupport(action)
			}
		}
	}()
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			queryCtx, stop := context.WithTimeout(ctx, 200*time.Millisecond)
			requested := r.quit.Swap(false)
			state, err := r.controller.Query(queryCtx, requested)
			stop()
			if requested && err != nil {
				r.setAction("quit_request_failed")
			}
			r.mu.Lock()
			r.available = err == nil
			if err == nil {
				r.snapshot = state
			}
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-r.controller.Done:
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		defer workers.Done()
		r.readStartup()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.readStartup()
			case action := <-r.actions:
				switch action {
				case OpenLogs:
					if r.directory == "" || r.openLogs(r.directory) != nil {
						r.setAction("open_logs_failed")
					} else {
						r.setAction("")
					}
				case ToggleStartup:
					if r.startup == nil {
						r.setAction("startup_change_failed")
						continue
					}
					state, err := r.startup.Toggle()
					r.mu.Lock()
					r.launch = state
					r.mu.Unlock()
					if err != nil {
						r.setAction("startup_change_failed")
					} else {
						r.setAction("")
					}
				case SelectInfiniteTees, SelectGSPro, SelectStandby:
					id, _ := actionDestination(action)
					r.selectDestination(ctx, id)
				}
			}
		}
	}()
	go func() { workers.Wait(); close(r.done) }()
}

func (r *Runtime) readStartup() {
	state := "startup_unavailable"
	if r.startup != nil {
		var err error
		state, err = r.startup.State()
		if err != nil {
			state = "startup_unavailable"
		}
	}
	r.mu.Lock()
	r.launch = state
	r.mu.Unlock()
}

func (r *Runtime) runSupport(action Action) {
	defer r.supportBusy.Store(false)
	if action == ReportBug {
		r.setAction("report_bug_pending")
		if r.reportBug() != nil {
			r.setAction("report_bug_failed")
		} else {
			r.setAction("")
		}
		return
	}
	r.setAction("diagnostic_report_pending")
	if r.saveReport == nil {
		r.setAction("diagnostic_report_failed")
		return
	}
	result, err := r.saveReport()
	switch {
	case err != nil:
		r.setAction("diagnostic_report_failed")
	case result == "cancelled":
		r.setAction("diagnostic_report_cancelled")
	case result == "saved_partial":
		r.setAction("diagnostic_report_partial")
	case result == "saved":
		r.setAction("diagnostic_report_saved")
	default:
		r.setAction("diagnostic_report_failed")
	}
}

// Request queues a supported tray action without waiting for desktop I/O.
// Quit sets a separate atomic request for the poller; repeated requests may
// coalesce. Other actions use a one-entry queue and report busy when it is full.
// Selection success acknowledges a bridge request before saving a preference,
// not completed switching. Request reports progress/failure in decorated status.
func (r *Runtime) Request(action Action) {
	if action == Quit {
		r.quit.Store(true)
		return
	}
	if action == SaveReport || action == ReportBug {
		if !r.supportBusy.CompareAndSwap(false, true) {
			r.setAction("desktop_actions_busy")
			return
		}
		select {
		case r.support <- action:
		default:
			r.supportBusy.Store(false)
			r.setAction("desktop_actions_busy")
		}
		return
	}
	if action == OpenLogs {
		r.setAction("opening_logs")
	} else if _, selected := actionDestination(action); selected {
		r.setAction("destination_change_pending")
	} else {
		r.setAction("startup_change_pending")
	}
	select {
	case r.actions <- action:
	default:
		r.setAction("desktop_actions_busy")
	}
}

func (r *Runtime) selectDestination(ctx context.Context, id destination.ID) {
	request, cancel := context.WithTimeout(ctx, time.Second)
	state, err := r.controller.Select(request, id)
	cancel()
	if err != nil {
		r.mu.Lock()
		r.preference = "destination_preference_unconfirmed"
		r.mu.Unlock()
		r.setAction("destination_change_failed")
		return
	}
	r.mu.Lock()
	r.snapshot, r.available = state, true
	r.mu.Unlock()
	if r.preferences == nil || r.preferences.Save(id) != nil {
		r.mu.Lock()
		r.preference = "destination_preference_save_failed"
		r.mu.Unlock()
		r.setAction("destination_preference_save_failed")
		return
	}
	r.mu.Lock()
	r.preference = ""
	r.mu.Unlock()
	r.setAction("")
}

func (r *Runtime) setAction(code string) {
	r.mu.Lock()
	r.action = code
	r.mu.Unlock()
	if code != "" {
		r.logs.WriteEvent(eventNow(code))
	}
}

// UIState records a native tray health code and emits a sanitized log event
// when the code changes. It never requests bridge shutdown.
func (r *Runtime) UIState(code string) {
	r.mu.Lock()
	changed := r.ui != code
	r.ui = code
	r.mu.Unlock()
	if changed {
		r.logs.WriteEvent(eventNow(code))
	}
}

// Decorate adds cached desktop, startup, action, and preference state plus current
// logger health to a non-nil snapshot. It performs no disk or controller I/O.
// The caller must own state while it is modified.
func (r *Runtime) Decorate(state *bridge.Snapshot) {
	r.mu.Lock()
	state.Desktop, state.Startup, state.DesktopAction = r.ui, r.launch, r.action
	state.DestinationPreference = r.preference
	r.mu.Unlock()
	health := r.logs.Health()
	state.LocalLogs = health.State
	state.LocalLogLost = health.Lost
}

// View returns a menu model from cached/decorated status, explicitly warning
// when the last owner query failed. It does not query the bridge synchronously.
func (r *Runtime) View() View {
	r.mu.Lock()
	state, available := r.snapshot, r.available
	r.mu.Unlock()
	r.Decorate(&state)
	return menu(state, available)
}

// Close cancels desktop workers and waits for their completion or ctx's deadline.
// It returns "desktop_not_started" before Start. Blocking desktop I/O may outlive
// the wait; a timeout is logged and returned rather than treated as completion.
// Close does not close the logger, native tray, or source. The host calls it only
// after the bridge's source-first guard permits cleanup.
func (r *Runtime) Close(ctx context.Context) error {
	if r.cancel == nil {
		return errors.New("desktop_not_started")
	}
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		r.logs.WriteEvent(eventNow("desktop_worker_cleanup_deadline"))
		return ctx.Err()
	}
}

// BeginStop is called only after the engine's source-departure/admission guard.
// The UI timer can then dismiss a native popup on its own thread.
func (r *Runtime) BeginStop() { r.stopping.Store(true) }

// Tray returns a new native adapter for the identity window's owner thread,
// or nil on non-Windows systems. Its resources are managed by that window,
// separately from Runtime's workers.
func (r *Runtime) Tray() identity.Tray { return nativeTray(r) }
