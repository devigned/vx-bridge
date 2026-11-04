// Vxbridge runs the Windows x64 Labs-to-game bridge or queries its local host.
// Distribution builds stage the host as GSPconnect.exe for the Labs-facing
// identity and keep vxbridge.exe distinct for --status and --quit.
// --version prints build identity without starting a host.
//
// Host startup verifies managed installation state, loads the selected game,
// claims private control and exclusive source listeners, and verifies the native
// identity window before entering the bridge state machine. Desktop mode adds
// tray actions, startup registration management, and bounded sanitized logs.
// Quit waits for normal Labs departure; it never force-closes an attached source.
// Build/installer tools are separate commands, not ordinary startup repair paths.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
	"github.com/devigned/vx-bridge/internal/control"
	"github.com/devigned/vx-bridge/internal/desktop"
	"github.com/devigned/vx-bridge/internal/destination"
	"github.com/devigned/vx-bridge/internal/diagnostics"
	"github.com/devigned/vx-bridge/internal/identity"
	"github.com/devigned/vx-bridge/internal/managed"
	"github.com/devigned/vx-bridge/internal/startup"
)

const version = "vxbridge/prototype-multigame-v1"

var buildRevision = "development"
var defaultDesktop = "false"
var releaseVersion = "development"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func buildIdentity() string {
	identity := version + " release=" + releaseVersion + " revision=" + buildRevision + " " + runtime.Version() + " defaultDesktop=" + defaultDesktop
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision", "vcs.modified", "CGO_ENABLED", "GOOS", "GOARCH":
				identity += " " + setting.Key + "=" + setting.Value
			}
		}
	}
	return identity
}

func run(args []string, out, stderr io.Writer) (exit int) {
	flags := flag.NewFlagSet("vxbridge", flag.ContinueOnError)
	flags.SetOutput(stderr)
	status := flags.Bool("status", false, "same-user local host status (run as vxbridge.exe)")
	quit := flags.Bool("quit", false, "request guarded Quit; never forces attached Labs closed")
	showVersion := flags.Bool("version", false, "show build identity without starting a host")
	desktopMode := flags.Bool("desktop", defaultDesktop == "true", "native tray and local logs; no console required")
	port := flags.Int("target-port", 999, "Infinite Tees IPv4 loopback port (host mode only)")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
	if *port < 1 || *port > 65535 || *port == 12321 || (*status && *quit) ||
		(*showVersion && (*status || *quit)) || ((*status || *quit) && *port != 999) {
		fmt.Fprintln(stderr, "vxbridge: invalid or conflicting options; target port must be 1..65535 excluding 12321.")
		return 2
	}
	if *showVersion {
		if _, err := fmt.Fprintln(out, buildIdentity()); err != nil {
			return 1
		}
		return 0
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		fmt.Fprintln(stderr, "vxbridge: Windows x64 only.")
		return 2
	}
	image, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, "vxbridge: cannot verify process image.")
		return 1
	}
	compatibilityImage := strings.EqualFold(filepath.Base(image), "GSPconnect.exe")
	if *status || *quit {
		if compatibilityImage {
			fmt.Fprintln(stderr, "vxbridge: use distinct vxbridge.exe for control; no second GSPconnect identity.")
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		state, err := control.Query(ctx, *quit)
		if err != nil {
			fmt.Fprintf(stderr, "vxbridge: local control failed: %v\n", err)
			return 1
		}
		if err := json.NewEncoder(out).Encode(state); err != nil {
			fmt.Fprintln(stderr, "vxbridge: cannot print control response.")
			return 1
		}
		return 0
	}
	if !compatibilityImage {
		fmt.Fprintln(stderr, "vxbridge: parent must stage reviewed GSPconnect.exe in an application-owned directory; no automatic copy or vendor replacement.")
		return 2
	}
	lease, existing, err := admitManaged(image, managed.Admit, control.Query)
	if err != nil {
		if existing != nil {
			if *desktopMode {
				if notice := desktop.Notify(desktop.DuplicateMessage(desktop.ExistingSummary(*existing))); notice != nil {
					fmt.Fprintln(stderr, "vxbridge: existing-host notice unavailable; no existing host changed.")
				}
			} else if err := json.NewEncoder(out).Encode(existing); err != nil {
				fmt.Fprintln(stderr, "vxbridge: cannot print existing host status.")
			}
			return 1
		}
		fmt.Fprintf(stderr, "vxbridge: managed installation admission refused: %v\n", err)
		if *desktopMode {
			if notice := desktop.Notify("Repair required: run VX Bridge Setup and choose Repair / Reinstall.\n" +
				"No host was started. Installed files or maintenance state could not be verified.\n" +
				"Do not force-close an attached bridge."); notice != nil {
				fmt.Fprintln(stderr, "vxbridge: managed admission notice unavailable.")
			}
		}
		return 1
	}
	if lease != nil {
		defer func() {
			if err := lease.Close(); err != nil {
				fmt.Fprintf(stderr, "vxbridge: managed lifetime lease cleanup failed: %v\n", err)
				exit = 1
			}
		}()
	}
	target := fmt.Sprintf("127.0.0.1:%d", *port)
	directory, err := os.UserConfigDir()
	if err != nil {
		fmt.Fprintln(stderr, "vxbridge: destination preference directory unavailable; host not started.")
		return 1
	}
	selected, preferences, err := hostDestination(directory)
	if err != nil {
		fmt.Fprintln(stderr, "vxbridge: destination preference unreadable or invalid; host not started.")
		if *desktopMode {
			if err := desktop.Notify("The saved game choice is unreadable or invalid. No host was started.\n" +
				"Inspect the per-user VxInfiniteTeesBridge destination.json preference; no vendor settings were changed."); err != nil {
				fmt.Fprintln(stderr, "vxbridge: destination preference notice unavailable.")
			}
		}
		return 1
	}
	if !*desktopMode {
		if _, err := fmt.Fprintf(out, "%s\nVX Bridge prototype; source 127.0.0.1:12321 -> %s %s.\n"+
			"Game ready UNKNOWN. Dropped/uncertain shots are never replayed or success-ACKed.\n"+
			"Ctrl+C/--quit waits for actual source departure. Disconnect Labs normally first.\n"+
			"GSPro OpenAPI and live game switching are not yet qualified.\n"+
			"Console X/Task Manager/OS termination can still crash attached Labs (E14); not qualified safe.\n", buildIdentity(), selected.Name(), selected.Endpoint(target)); err != nil {
			fmt.Fprintln(stderr, "vxbridge: startup diagnostics failed; not starting.")
			return 1
		}
	}
	interrupts := make(chan os.Signal, 8)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	pipe, existing, err := claimHost(control.Listen, control.Query)
	if err != nil {
		if *desktopMode {
			summary := "Another host may be starting, or private control is unavailable."
			if existing != nil {
				summary = desktop.ExistingSummary(*existing)
			}
			if notice := desktop.Notify(desktop.DuplicateMessage(summary)); notice != nil {
				fmt.Fprintln(stderr, "vxbridge: duplicate/control-unavailable notice failed; no existing host changed.")
			}
		} else if existing != nil {
			if err := json.NewEncoder(out).Encode(existing); err != nil {
				fmt.Fprintln(stderr, "vxbridge: cannot print existing host status.")
			}
		} else {
			fmt.Fprintf(stderr, "vxbridge: private control bind failed: %v; no existing host changed.\n", err)
		}
		return 1
	}
	listener, err := identity.ListenExclusive("127.0.0.1:12321")
	if err != nil {
		pipe.Close()
		if *desktopMode {
			if notice := desktop.Notify("The source port is already in use or unavailable. No process was changed.\n" +
				"Close the real connector normally before starting this bridge; never kill an attached receiver."); notice != nil {
				fmt.Fprintln(stderr, "vxbridge: source bind and desktop notice failed; no process changed.")
			}
		} else {
			fmt.Fprintf(stderr, "vxbridge: exclusive source bind failed: %v; no process changed.\n", err)
		}
		return 1
	}
	return host(listener, pipe, target, interrupts, out, stderr, *desktopMode, image, *port, selected, preferences)
}

func hostDestination(directory string) (destination.ID, *destination.Preferences, error) {
	preferences := &destination.Preferences{Path: filepath.Join(directory, "VxInfiniteTeesBridge", "destination.json")}
	selected, err := preferences.Load()
	return selected, preferences, err
}

func admitManaged(image string, admit func(string, string, string) (io.Closer, error),
	query func(context.Context, bool) (bridge.Snapshot, error)) (io.Closer, *bridge.Snapshot, error) {
	lease, err := admit(image, buildRevision, releaseVersion)
	if err == nil {
		return lease, nil, nil
	}
	// A healthy managed host owns the lifetime lease too; a duplicate is not
	// proof that its installation needs repair. Never request Quit here.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	existing, queryErr := query(ctx, false)
	if queryErr == nil {
		return nil, &existing, err
	}
	return nil, nil, err
}

func claimHost(listen func() (net.Listener, error), query func(context.Context, bool) (bridge.Snapshot, error)) (net.Listener, *bridge.Snapshot, error) {
	pipe, err := listen()
	if err == nil {
		return pipe, nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state, queryError := query(ctx, false)
	if queryError == nil {
		return nil, &state, err
	}
	return nil, nil, err
}

func host(listener net.Listener, pipe net.Listener, target string, interrupts <-chan os.Signal, out, stderr io.Writer, desktopMode bool, image string, port int, selected destination.ID, preferences *destination.Preferences) (exit int) {
	faults := make(chan string, 8)
	ctx, cancel := context.WithCancel(context.Background())
	controller := bridge.NewController()
	var app *desktop.Runtime
	var logs *diagnostics.Logger
	var tray identity.Tray
	if desktopMode {
		directory, directoryError := os.UserCacheDir()
		if directoryError == nil {
			directory = filepath.Join(directory, "VxInfiniteTeesBridge", "logs")
		} else {
			directory = ""
		}
		logs = diagnostics.New(directory, diagnostics.Build{Version: version, Revision: buildRevision, Go: runtime.Version()})
		var manager *startup.Manager
		backend, startupError := startup.CurrentUser()
		if startupError == nil {
			manager, startupError = startup.New(backend, image, port)
		}
		if startupError != nil {
			logs.WriteEvent(bridge.Event{At: time.Now().UTC(), Reason: "startup_unavailable_use_status"})
		}
		app = desktop.New(controller, logs, manager, directory, preferences)
		tray = app.Tray()
		defer func() {
			flush, stop := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer stop()
			if err := logs.Close(flush); err != nil {
				exit = 1
				fmt.Fprintln(stderr, "vxbridge: local diagnostics unavailable at stop; bounded in-memory status was used.")
				if notice := desktop.Notify("The bridge has stopped after source-first cleanup (or failed before starting).\n" +
					"Local logs could not be fully persisted. Earlier bounded status was the fallback; logging is not a durable audit."); notice != nil {
					fmt.Fprintln(stderr, "vxbridge: final diagnostics failure notice unavailable.")
				}
			}
		}()
	}
	window := startWindowBackend(ctx, faults, &identity.Window{Tray: tray})
	if err := <-window.ready; err != nil {
		listener.Close()
		pipe.Close()
		cancel()
		cleanup := <-window.done
		fmt.Fprintf(stderr, "vxbridge: native identity failed closed: %v; cleanup: %v\n", err, cleanup)
		if desktopMode {
			if err := desktop.Notify("The required native identity window could not be verified. No host was started.\n" +
				"Launch the reviewed GSPconnect.exe on the signed-in interactive Windows desktop, not SSH or a service."); err != nil {
				fmt.Fprintln(stderr, "vxbridge: native identity failure notice unavailable.")
			}
		}
		return 1
	}
	var decorate func(*bridge.Snapshot)
	if app != nil {
		app.Start()
		decorate = app.Decorate
	}
	controlDone := make(chan error, 1)
	go func() {
		err := control.ServeWithStatus(pipe, controller, faults, decorate)
		if err != nil {
			select {
			case <-controller.Done:
			default:
				faults <- "control_listener_failed"
			}
		}
		controlDone <- err
	}()
	go func() {
		for {
			select {
			case <-interrupts:
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				_, err := controller.Query(ctx, true)
				cancel()
				if err != nil {
					select {
					case faults <- "quit_request_failed":
					default:
					}
				}
			case <-controller.Done:
				return
			}
		}
	}()
	var result error
	config := bridge.Defaults(target)
	config.Destination = selected
	if logs != nil {
		result = bridge.RunWithDiagnostics(listener, config, controller, faults, logs, buildIdentity())
	} else {
		result = bridge.Run(listener, config, controller, faults, out, buildIdentity())
	}
	// Run only returns after Quit/fatal request AND no source may be attached.
	if app != nil {
		app.BeginStop()
	}
	cancel()
	windowResult := <-window.done
	var desktopResult error
	if app != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 500*time.Millisecond)
		desktopResult = app.Close(cleanup)
		stop()
	}
	pipe.Close()
	controlResult := <-controlDone
	if result != nil || windowResult != nil || desktopResult != nil || (controlResult != nil && !errors.Is(controlResult, net.ErrClosed)) {
		fmt.Fprintf(stderr, "vxbridge: stopped with error; session=%v window=%v desktop=%v control=%v\n", result, windowResult, desktopResult, controlResult)
		return 1
	}
	return 0
}
