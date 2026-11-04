package bridge

import (
	"fmt"
	"io"
	"net"
	"time"
)

// EventSink receives only sanitized events, on the bounded diagnostic worker,
// never on the session owner. It must not be used to pass shot payloads.
type EventSink interface {
	// WriteEvent accepts a sanitized event or reports sink failure. Calls are
	// serial on the diagnostic worker; implementations should remain bounded.
	WriteEvent(Event) error
}

// consoleEvents writes sanitized lines to Writer, the caller-owned console sink.
type consoleEvents struct {
	// Writer is the non-nil caller-owned console sink; the adapter never closes it.
	io.Writer
}

// WriteEvent writes ev's time, reason, and local alias, returning the write error.
func (c consoleEvents) WriteEvent(ev Event) error {
	_, err := fmt.Fprintf(c.Writer, "%s %s shot_alias=%d\n", ev.At.Format(time.RFC3339), ev.Reason, ev.Alias)
	return err
}

// RunWithDiagnostics serves the same source-first lifecycle as Run using a
// non-nil typed sink and "local_logs" diagnostic state instead of a console.
// It does not close sink. After guarded teardown it waits at most 100 ms for
// queued events to reach the sink; callers remain responsible for sink cleanup.
// Sink failure never cancels or closes an attached source.
func RunWithDiagnostics(listener net.Listener, config Config, controller *Controller, faults <-chan string, sink EventSink, version string) error {
	return run(listener, config, controller, faults, sink, "local_logs", version)
}
