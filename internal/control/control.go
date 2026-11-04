// Package control exposes same-user Windows pipe status and guarded Quit for
// a running bridge. [Listen] supplies the private transport; [ServeWithStatus]
// serializes requests through [bridge.Controller] and optionally decorates
// replies with desktop health. [Query] does not start a host or select a game.
// Non-Windows builds retain the API but cannot open its pipe transport.
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
)

const responseLimit = 64 << 10

// ServeWithStatus accepts serial status/quit requests until listener.Accept
// fails, returning that error. It closes each accepted connection, not listener.
// Use Listen or an equally authenticated listener; this function adds no caller
// authentication. Requests have a 2 s connection deadline and a 1 s owner-query
// deadline. Request failures send a best-effort code to fault and serving
// continues. decorate, if non-nil, must be nonblocking and may add local health
// to the status copy outside the engine owner.
func ServeWithStatus(listener net.Listener, host *bridge.Controller, fault chan<- string, decorate func(*bridge.Snapshot)) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		if err := serveOneWithStatus(conn, host, decorate); err != nil {
			select {
			case fault <- "control_request_failed":
			default:
			}
		}
	}
}

func serveOneWithStatus(conn net.Conn, host *bridge.Controller, decorate func(*bridge.Snapshot)) error {
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	request, err := bufio.NewReader(io.LimitReader(conn, 32)).ReadString('\n')
	if err != nil {
		return err
	}
	if request != "status\n" && request != "quit\n" {
		_, err := io.WriteString(conn, "{\"error\":\"unsupported_control_command\"}\n")
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state, err := host.Query(ctx, request == "quit\n")
	if err != nil {
		return err
	}
	if decorate != nil {
		decorate(&state)
	}
	return json.NewEncoder(conn).Encode(state)
}

// Query reads status from the current user's host, optionally requesting guarded
// Quit. ctx must have a deadline; it bounds connection I/O, while cancellation
// is passed to pipe dialing. After dialing, cancellation alone does not interrupt
// I/O earlier than that deadline. A successful Quit reply is not completed
// shutdown. Query decodes from at most 64 KiB of response data; dial/I/O,
// decoding, and missing-version errors are returned. A decode error may
// accompany a partially filled Snapshot.
// Query always closes its connection and returns a Windows-required error
// on non-Windows systems.
func Query(ctx context.Context, quit bool) (bridge.Snapshot, error) {
	conn, err := dial(ctx)
	if err != nil {
		return bridge.Snapshot{}, err
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		return bridge.Snapshot{}, errors.New("control_deadline_required")
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return bridge.Snapshot{}, err
	}
	command := "status\n"
	if quit {
		command = "quit\n"
	}
	if _, err := io.WriteString(conn, command); err != nil {
		return bridge.Snapshot{}, err
	}
	var state bridge.Snapshot
	if err := json.NewDecoder(io.LimitReader(conn, responseLimit)).Decode(&state); err != nil {
		return state, err
	}
	if state.Version == "" {
		return state, errors.New("invalid_control_response")
	}
	return state, nil
}
