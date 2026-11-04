package control

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
)

func TestOnlyStatusAndGuardedQuitCommands(t *testing.T) {
	for _, command := range []string{"status\n", "quit\n", "force\n", "quit now\n"} {
		t.Run(command, func(t *testing.T) {
			server, client := net.Pipe()
			defer client.Close()
			controller := bridge.NewController()
			done := make(chan error, 1)
			go func() { done <- serveOneWithStatus(server, controller, nil) }()
			client.SetDeadline(time.Now().Add(time.Second))
			if _, err := client.Write([]byte(command)); err != nil {
				t.Fatal(err)
			}

			if command == "status\n" || command == "quit\n" {
				select {
				case request := <-controller.Requests:
					if request.Quit != (command == "quit\n") {
						t.Fatal("control action changed")
					}
					request.Reply <- bridge.Snapshot{Version: "test", Quit: "pending_source_departure"}
				case <-time.After(time.Second):
					t.Fatal("control did not query owner")
				}
				var response bridge.Snapshot
				if err := json.NewDecoder(client).Decode(&response); err != nil || response.Quit != "pending_source_departure" {
					t.Fatal("quit did not preserve guarded state")
				}
			} else {
				line, err := bufio.NewReader(client).ReadString('\n')
				if err != nil || line != "{\"error\":\"unsupported_control_command\"}\n" {
					t.Fatal("arbitrary control command accepted")
				}
				select {
				case <-controller.Requests:
					t.Fatal("invalid command reached owner")
				default:
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestControllerDeadlineDoesNotAuthorizeForceQuit(t *testing.T) {
	controller := bridge.NewController()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := controller.Query(ctx, true); err == nil {
		t.Fatal("blocked controller returned success")
	}
}

func TestDesktopHealthDecoratesActualGuardedSnapshot(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	controller := bridge.NewController()
	done := make(chan error, 1)
	go func() {
		done <- serveOneWithStatus(server, controller, func(state *bridge.Snapshot) {
			state.Desktop = "tray_restore_failed_use_control"
			state.LocalLogs = "local_logs_write_or_rotation_failed_use_status"
			state.Startup = "startup_conflict_not_changed"
		})
	}()
	client.SetDeadline(time.Now().Add(time.Second))
	client.Write([]byte("quit\n"))
	request := <-controller.Requests
	if !request.Quit {
		t.Fatal("desktop decorator bypassed guarded owner request")
	}
	request.Reply <- bridge.Snapshot{Version: "test", Source: "attached", Quit: "pending_source_departure"}
	var state bridge.Snapshot
	if err := json.NewDecoder(client).Decode(&state); err != nil || state.Source != "attached" ||
		state.Quit != "pending_source_departure" || state.Desktop != "tray_restore_failed_use_control" ||
		state.LocalLogs != "local_logs_write_or_rotation_failed_use_status" || state.Startup != "startup_conflict_not_changed" {
		t.Fatal("operational failure missing from IPC or owner state overwritten", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
