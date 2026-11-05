// Package bridge owns the source-to-game delivery state machine.
// [Run] accepts one Labs source at a time, uses [protocol] to inspect retained
// JSON frames, and uses [destination] to select a target profile. I/O workers
// report events to a single state owner rather than mutate its session state.
//
// # Delivery and shutdown
//
// Only newly eligible ball shots can be sent. A short hold may join matching
// club data, but unavailable or uncertain deliveries are never replayed.
// Target acceptance, a completed local upstream write, and game readiness are
// distinct outcomes; [Snapshot] never treats a TCP connection or 200 as readiness.
// Quit and fatal faults wait for observed source EOF/reset and the listener's
// admission barrier before local teardown, not for root-context cancellation.
//
// [Controller] serves desktop and local-control requests. [EventSink] receives
// sanitized diagnostics separately from the state owner and raw shot buffers.
package bridge

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/devigned/vx-bridge/internal/destination"
	"github.com/devigned/vx-bridge/internal/protocol"
)

const (
	queueCount = 16
	queueBytes = 2 << 20
	ledgerSize = 256
)

// Config sets a run's target, deadlines, and pacing. Start with Defaults;
// the zero value is invalid. Run copies the configuration, so later changes
// do not reconfigure a running engine.
type Config struct {
	// Target is the Infinite Tees TCP address; other profiles select their own.
	Target string
	// Destination selects a profile; an empty ID defaults to Infinite Tees.
	Destination destination.ID
	// Hold is the club-fragment wait for a ball without club data.
	// Dial bounds each target connection attempt. Write bounds each full
	// frame write. Receipt bounds the wait from dispatch for target acceptance.
	// All four durations must be positive.
	Hold, Dial, Write, Receipt time.Duration
	// RetryMin is the initial target retry delay; RetryMax caps the backoff
	// and must be at least RetryMin. Both must be positive.
	// Pace is the minimum interval between completed upstream replies; a
	// non-positive value adds no delay. Frame is the positive read deadline
	// from an opening brace to a complete source or target JSON object.
	RetryMin, RetryMax, Pace, Frame time.Duration
	// DialContext opens target TCP4 connections and must be non-nil.
	// It must honor context cancellation and return a usable connection on
	// success; ownership of returned connections transfers to the engine.
	DialContext func(context.Context, string, string) (net.Conn, error)
}

// Defaults returns a valid configuration for target with a 600 ms club hold,
// 2 s dial/write deadlines, 5 s receipt/frame deadlines, 2..10 s retry backoff,
// 60 ms upstream pacing, and a net.Dialer. Destination remains empty and
// therefore defaults to Infinite Tees when Run starts.
func Defaults(target string) Config {
	return Config{Target: target, Hold: 600 * time.Millisecond, Dial: 2 * time.Second,
		Write: 2 * time.Second, Receipt: 5 * time.Second, RetryMin: 2 * time.Second,
		RetryMax: 10 * time.Second, Pace: 60 * time.Millisecond, Frame: 5 * time.Second,
		DialContext: (&net.Dialer{}).DialContext}
}

// Counts records cumulative local outcomes for one engine run, including
// multiple source epochs. Counters overlap and are not an exactly-once ledger;
// TargetAccept does not prove display in the game and SourceACK does not prove
// consumption by Labs.
type Counts struct {
	// Balls counts inspected ball envelopes, including duplicates and drops.
	// Status and Heartbeats count those non-shot envelope kinds.
	// Merged counts matching club fragments joined to held balls.
	// Duplicate counts balls omitted by the bounded per-source ID ledger.
	// LateClub counts unmatched or late club-only fragments.
	Balls, Status, Heartbeats, Merged, Duplicate, LateClub uint64
	// Sent counts completed full target writes. TargetAccept counts eligible
	// target 200 receipts after a full write. SourceACK counts completed
	// upstream 200 writes; Relayed counts all completed upstream replies.
	// Dropped counts discarded ball candidates, including uncertain delivery.
	Sent, TargetAccept, SourceACK, Relayed, Dropped uint64
	// Uncertain counts pending shots lost when the target is retired.
	// RejectedSource counts extra source connections closed without eviction.
	// Quarantines counts source epochs stopped from forwarding after a fault.
	// Stale counts events from retired epochs/generations or invalid receipts.
	Uncertain, RejectedSource, Quarantines, Stale uint64
	// ReplyDiscarded counts replies discarded before an admitted write.
	// AcceptedACKAbandoned is the subset carrying a previously accepted 200.
	ReplyDiscarded, AcceptedACKAbandoned uint64
	// ReplyCompletedAfterRetire counts successful upstream writes completing
	// after the source epoch or target generation was retired.
	ReplyCompletedAfterRetire uint64
}

// Event is sanitized diagnostic metadata, never a raw shot or player record.
type Event struct {
	// At is the event's UTC observation time.
	At time.Time `json:"at"`
	// Reason is a local outcome code, not arbitrary source or target text.
	Reason string `json:"reason"`
	// Alias is a run-local ball alias, or zero for an event without a ball.
	// It is not the source's ShotNumber.
	Alias uint64 `json:"shot_alias,omitempty"`
}

// Snapshot is a point-in-time status copy. Engine queries copy Recent so callers
// may retain it without sharing the engine's event slice. Status strings are
// local state/outcome codes, not proof of peer consumption or game readiness.
// Desktop and logging health are added outside the engine by the desktop layer.
type Snapshot struct {
	// Version is the supplied build identity. Source describes source attachment
	// or quarantine; Target describes transport/retry state. Ready remains
	// "unknown". Quit describes the guarded shutdown request's progress.
	// Identity records native identity health. Diagnostics records sink/control
	// degradation without forcing an attached source closed.
	Version, Source, Target, Ready, Quit, Identity, Diagnostics string
	// Release, Revision, GoVersion and Architecture are structured host build
	// fields, independent of the legacy composite Version. TargetEndpoint is
	// the selected loopback endpoint (or "none"), not a discovered peer.
	Release, Revision, GoVersion, Architecture, TargetEndpoint string
	// Desktop is tray health; LocalLogs is file-worker health. Startup is the
	// current-user startup registration state, not proof of policy execution.
	// DesktopAction is the most recent local action's progress/failure code.
	Desktop, LocalLogs, Startup, DesktopAction string
	// LocalLogLost counts events rejected by the local logger, not additional
	// queued records potentially lost in a later asynchronous disk failure.
	LocalLogLost uint64
	// SourceEpoch identifies source admissions; TargetGeneration identifies
	// target connection attempts, including retired attempts.
	SourceEpoch, TargetGeneration uint64
	// LastDeparture is the last observed source EOF/reset class.
	// LastSourceError is the last source read/departure class.
	// LastError is the latest recorded fault or discarded-delivery reason.
	LastDeparture, LastSourceError, LastError string
	// LastEvent is the latest event reason, even when no sink accepts it.
	LastEvent string
	// SourceFlags is the latest inspected source report, not game readiness.
	SourceFlags protocol.Flags
	// Counts contains cumulative run-local outcome counters.
	Counts Counts
	// Recent contains up to 64 sanitized events, oldest first.
	Recent []Event
	// Destination is the active profile. RequestedDestination is the selection
	// awaiting a safe transition, or the active selection when no switch is pending.
	Destination, RequestedDestination destination.ID
	// Switch is "idle" or the pending/blocked transition state.
	Switch string
	// DestinationPreference describes a desktop save/confirmation failure;
	// an empty value adds no such warning.
	DestinationPreference string
}

// Request is the controller's engine-owner message. Prefer Controller.Query
// and Controller.Select, which provide buffered replies and cancellation.
// Direct callers must keep reply receivers available after admission.
type Request struct {
	// Quit requests source-first shutdown; it does not force a disconnect.
	Quit bool
	// Reply must be non-nil and able to receive the resulting Snapshot.
	Reply chan Snapshot
	// Destination requests a selection when non-empty.
	Destination destination.ID
	// Result, if non-nil for a selection, receives its error before Reply.
	// It must be buffered or serviced without waiting for Reply first.
	Result chan error
}

// Controller communicates with one Run invocation. Construct it with
// NewController; the zero value has no usable channels. Query and Select may be
// called concurrently, but callers must not replace or close its channels.
// A Controller must not be reused for a second run.
type Controller struct {
	// Requests delivers requests to the single engine owner.
	Requests chan Request
	// Done is closed by Run when an admitted run exits. Invalid configuration
	// returns before that lifecycle begins and does not close Done.
	Done chan struct{}
}

// NewController creates the request and completion channels for one engine run.
func NewController() *Controller {
	return &Controller{Requests: make(chan Request), Done: make(chan struct{})}
}

// Query returns the owner's Snapshot, optionally requesting guarded Quit first.
// The reply acknowledges the request, not completed shutdown. Cancellation
// returns ctx.Err; admission after the host stops returns "host_stopped".
// Cancellation after admission does not retract the Quit request.
func (c *Controller) Query(ctx context.Context, quit bool) (Snapshot, error) {
	return c.query(ctx, Request{Quit: quit})
}

// Select requests a Valid destination and returns the resulting Snapshot and
// selection error. Invalid IDs, pending Quit, source quarantine, or a different
// already-pending switch are rejected. Success means the request was accepted,
// not that a pending delivery drained or the destination connected. Cancellation
// after admission does not undo the request. Select does not save preferences.
func (c *Controller) Select(ctx context.Context, id destination.ID) (Snapshot, error) {
	if !id.Valid() {
		return Snapshot{}, errors.New("invalid_destination")
	}
	return c.query(ctx, Request{Destination: id, Result: make(chan error, 1)})
}

func (c *Controller) query(ctx context.Context, request Request) (Snapshot, error) {
	reply := make(chan Snapshot, 1)
	request.Reply = reply
	select {
	case c.Requests <- request:
	case <-c.Done:
		return Snapshot{}, errors.New("host_stopped")
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
	select {
	case state := <-reply:
		if request.Result != nil {
			select {
			case err := <-request.Result:
				return state, err
			case <-ctx.Done():
				return state, ctx.Err()
			}
		}
		return state, nil
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}

type event struct {
	kind       string
	epoch, gen uint64
	token      uint64
	frame      []byte
	conn       net.Conn
	err        error
	n          int
	code       int
	changed    bool
	permit     chan bool
}

type upstreamWrite struct {
	frame      []byte
	code       int
	epoch, gen uint64
}

type source struct {
	conn        net.Conn
	epoch       uint64
	done        chan struct{}
	writeStop   chan struct{}
	writes      chan upstreamWrite
	queuedBytes int
	queuedCount int
	quarantined bool
}

type target struct {
	conn    net.Conn
	gen     uint64
	token   atomic.Uint64
	cancel  context.CancelFunc
	profile destination.ID
}

type candidate struct {
	key                     [32]byte
	frame                   []byte
	alias, gen, eligibility uint64
	until                   time.Time
}

type pending struct {
	token, alias uint64
	until        time.Time
	written      bool
	receipt      []byte
}

// engine is the sole owner of mutable session state. Workers exchange events;
// only the availability ticket and target write token are shared atomically.
type engine struct {
	config           Config
	listener         net.Listener
	control          *Controller
	events           chan event
	done             chan struct{}
	logs             chan Event
	logFailure       chan error
	printDone        chan struct{}
	diagnosticMode   string
	sink             EventSink
	state            Snapshot
	src              *source
	dst              *target
	held             *candidate
	wait             *pending
	dialing          bool
	stoppingListener bool
	acceptStopped    bool
	retryAt          time.Time
	backoff          time.Duration
	quitting         bool
	switching        bool
	fatal            error
	nextAlias        uint64
	nextToken        uint64
	nextEligibility  uint64
	available        atomic.Uint64
	seen             map[[32]byte]bool
	ledger           [ledgerSize][32]byte
	ledgerNext       int
	ledgerUsed       int
}

// Run serves source sessions until a Quit or fatal fault can complete guarded
// teardown, returning any fatal lifecycle error. listener and control must be
// non-nil; console must accept diagnostic writes. Invalid Config is rejected
// before listener/control ownership transfers. Otherwise Run closes listener
// and control.Done on exit, and owns all admitted source/target connections.
//
// A nil faults channel disables platform notifications. Callers must send only
// sanitized internal outcome codes, never raw device/user text. Identity loop
// failure requests guarded Quit; other fault strings degrade diagnostics. Quit and
// platform faults never authorize closing an attached source: Run can wait
// indefinitely for actual EOF/reset before crossing the admission barrier.
// Console sink failures are reported in Snapshot without canceling the source;
// console is not closed and its worker is not guaranteed to finish at return.
func Run(listener net.Listener, config Config, control *Controller, faults <-chan string, console io.Writer, version string) error {
	return run(listener, config, control, faults, consoleEvents{console}, "console", version)
}

func run(listener net.Listener, config Config, control *Controller, faults <-chan string, sink EventSink, mode, version string) error {
	config.Destination = config.Destination.Default()
	if config.Hold <= 0 || config.Dial <= 0 || config.Write <= 0 || config.Receipt <= 0 ||
		config.RetryMin <= 0 || config.RetryMax < config.RetryMin || config.Frame <= 0 || config.DialContext == nil ||
		!config.Destination.Valid() {
		return errors.New("invalid_bridge_configuration")
	}
	e := &engine{
		config: config, listener: listener, control: control, events: make(chan event),
		done: make(chan struct{}), logs: make(chan Event, 32), logFailure: make(chan error, 1),
		printDone: make(chan struct{}),
		sink:      sink, diagnosticMode: mode,
		backoff: config.RetryMin, seen: make(map[[32]byte]bool),
		state: Snapshot{Version: version, Source: "listening", Target: "absent_source",
			Ready: "unknown", Quit: "not_requested", Identity: "verified", Diagnostics: mode,
			Destination: config.Destination, RequestedDestination: config.Destination, Switch: "idle"},
	}
	defer close(control.Done)
	defer close(e.done)
	defer e.finishDiagnostics()
	defer listener.Close()
	go e.print()
	go e.accept()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	e.log("host_listening_no_game_ready_claim", 0)
	for {
		select {
		case request := <-control.Requests:
			if request.Destination != "" {
				err := e.selectDestination(request.Destination)
				if request.Result != nil {
					request.Result <- err
				}
			}
			if request.Quit {
				e.quitting = true
				e.state.Quit = "pending_source_departure"
				e.log("quit_requested_disconnect_Labs_normally", 0)
			}
			request.Reply <- e.snapshot()
		case reason, open := <-faults:
			if !open {
				faults = nil
				e.state.Diagnostics = "platform_fault_channel_closed"
				e.log("platform_fault_channel_closed", 0)
				continue
			}
			if reason == "identity_message_loop_failed" {
				e.state.Identity = "fault"
				e.fatal = errors.New(reason)
				e.quitting = true
				e.state.Quit = "pending_source_departure"
				e.quarantine("platform_fault_waiting_for_source_departure")
			} else {
				recovery := "_use_console_or_CtrlC"
				if mode == "local_logs" {
					recovery = "_use_tray_or_local_logs"
				}
				e.state.Diagnostics = reason + recovery
				e.log(reason, 0)
			}
		case <-e.logFailure:
			e.state.Diagnostics = mode + "_failed_use_status"
			e.state.LastError = "diagnostics_sink_failed"
			e.log("diagnostics_sink_failed_use_status", 0)
		case ev := <-e.events:
			e.handle(ev)
		case now := <-ticker.C:
			e.tick(now)
		}
		if e.quitting && e.src == nil {
			if !e.stoppingListener && !e.acceptStopped {
				e.stoppingListener = true
				e.state.Quit = "pending_source_admission_barrier"
				if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
					e.fatal = errors.New("source_listener_close_failed")
					e.log("source_listener_close_failed_waiting_for_accept_barrier", 0)
				}
			}
			if e.acceptStopped {
				e.state.Quit = "complete"
				e.closeTarget("host_stopping")
				return e.fatal
			}
		}
	}
}

func (e *engine) snapshot() Snapshot {
	state := e.state
	state.Recent = append([]Event(nil), state.Recent...)
	return state
}

func (e *engine) log(reason string, alias uint64) {
	ev := Event{time.Now().UTC(), reason, alias}
	if len(e.state.Recent) == 64 {
		copy(e.state.Recent, e.state.Recent[1:])
		e.state.Recent = e.state.Recent[:63]
	}
	e.state.Recent = append(e.state.Recent, ev)
	e.state.LastEvent = reason
	if e.state.Diagnostics == e.diagnosticMode+"_failed_use_status" {
		return
	}
	select {
	case e.logs <- ev:
	default:
		e.state.Diagnostics = e.diagnosticMode + "_overload_use_status"
	}
}

func (e *engine) print() {
	defer close(e.printDone)
	for ev := range e.logs {
		if err := e.sink.WriteEvent(ev); err != nil {
			e.logFailure <- err
			return
		}
	}
}

func (e *engine) finishDiagnostics() {
	close(e.logs)
	if e.diagnosticMode == "local_logs" {
		// Only after the source-first guard: let the nonblocking sink receive
		// the final events before the application closes its file worker.
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-e.printDone:
		case <-timer.C:
		}
	}
}

func (e *engine) emit(ev event) bool {
	select {
	case e.events <- ev:
		return true
	case <-e.done:
		if ev.conn != nil {
			if err := ev.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				fmt.Fprintln(os.Stderr, "vxbridge: late unused socket cleanup failed after host stop.")
			}
		}
		return false
	}
}

func (e *engine) accept() {
	for {
		conn, err := e.listener.Accept()
		if err != nil {
			e.emit(event{kind: "accept_error", err: err})
			return
		}
		if !e.emit(event{kind: "accept", conn: conn}) {
			return
		}
	}
}

func (e *engine) handle(ev event) {
	defer e.advanceSwitch()
	switch ev.kind {
	case "accept_error":
		e.acceptStopped = true
		if e.stoppingListener && errors.Is(ev.err, net.ErrClosed) {
			return
		}
		e.fatal = errors.New("source_listener_failed")
		e.quitting = true
		e.quarantine("source_listener_failed")
	case "accept":
		if e.src != nil {
			e.closeConn(ev.conn, "rejected_source_close_failed")
			e.state.Counts.RejectedSource++
			e.log("second_source_rejected_without_eviction", 0)
			return
		}
		e.state.SourceEpoch++
		e.src = &source{conn: ev.conn, epoch: e.state.SourceEpoch, done: make(chan struct{}),
			writeStop: make(chan struct{}), writes: make(chan upstreamWrite, queueCount)}
		e.state.Source = "attached"
		if e.quitting {
			e.state.Quit = "pending_source_departure"
		}
		e.state.Target = "retrying"
		if e.state.Destination == destination.Standby {
			e.state.Target = "standby"
		}
		e.state.SourceFlags = protocol.Flags{}
		e.seen = make(map[[32]byte]bool)
		e.ledgerNext, e.ledgerUsed = 0, 0
		e.backoff, e.retryAt = e.config.RetryMin, time.Now()
		e.log("source_attached", 0)
		go e.readSource(e.src)
		go e.writeSource(e.src)
		if e.state.Identity == "fault" {
			e.quarantine("identity_unavailable_pending_source_departure")
		}
	case "source_frame", "source_fault", "source_departed", "source_write", "source_write_ready", "source_write_discarded":
		if e.src == nil || ev.epoch != e.src.epoch {
			e.state.Counts.Stale++
			if ev.kind == "source_write" {
				if ev.err == nil {
					e.state.Counts.Relayed++
					e.state.Counts.ReplyCompletedAfterRetire++
					if ev.code == 200 {
						e.state.Counts.SourceACK++
					}
					e.log("previous_source_epoch_actual_response_write_completed", 0)
				} else {
					e.log("previous_source_epoch_response_write_uncertain", 0)
				}
			}
			if ev.kind == "source_write_ready" || ev.kind == "source_write_discarded" {
				e.discardReply(ev, "source_epoch_ended")
			}
			if ev.permit != nil {
				ev.permit <- false
			}
			return
		}
		switch ev.kind {
		case "source_frame":
			if !e.src.quarantined {
				if ev.changed {
					ev.token = 0
				}
				e.offer(ev.frame, ev.token)
			}
		case "source_fault":
			e.state.LastSourceError = errorClass(ev.err)
			e.quarantine("source_" + errorClass(ev.err))
		case "source_departed":
			e.state.LastDeparture = errorClass(ev.err)
			e.state.LastSourceError = errorClass(ev.err)
			e.log("source_departed_"+errorClass(ev.err), 0)
			e.closeTarget("absent_source")
			close(e.src.done)
			if !e.src.quarantined {
				close(e.src.writeStop)
			}
			e.discardQueuedReplies("source_epoch_ended")
			if err := e.src.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				e.fatal = errors.New("departed_source_socket_cleanup_failed")
				e.quitting = true
				e.log("departed_source_socket_cleanup_failed", 0)
			}
			e.src = nil
			e.state.Source = "listening"
		case "source_write":
			e.src.queuedBytes -= ev.n
			e.src.queuedCount--
			if ev.err != nil {
				e.quarantine("source_response_write_failed_delivery_uncertain")
				return
			}
			e.state.Counts.Relayed++
			if e.dst == nil || ev.gen != e.dst.gen {
				e.state.Counts.ReplyCompletedAfterRetire++
				e.log("admitted_upstream_write_completed_after_target_retirement", 0)
			}
			if ev.code == 200 {
				e.state.Counts.SourceACK++
				e.log("source_200_complete_local_write_not_peer_consumption", 0)
			}
		case "source_write_ready":
			if e.src.quarantined {
				e.discardReply(ev, "source_quarantined")
				ev.permit <- false
				return
			}
			retired := e.dst == nil || ev.gen != e.dst.gen
			if retired {
				// Historical target acceptance and an unwritten source ACK
				// remain separate outcomes; retirement never erases acceptance.
				e.discardReply(ev, "target_generation_retired")
				ev.permit <- false
				return
			}
			ev.permit <- true
		case "source_write_discarded":
			e.discardReply(ev, "source_writer_stopped")
		}
	case "dial":
		if e.src == nil || e.src.epoch != ev.epoch || e.dst == nil || ev.gen != e.dst.gen {
			if ev.conn != nil {
				e.closeConn(ev.conn, "stale_dial_socket_close_failed")
			}
			e.state.Counts.Stale++
			return
		}
		e.dialing = false
		if ev.err != nil {
			e.closeTarget("target_unavailable")
			return
		}
		e.dst.conn = ev.conn
		e.openEligibility()
		e.backoff = e.config.RetryMin
		e.log("target_connected_ready_unknown_new_shots_only", 0)
		go e.readTarget(e.src.epoch, e.dst)
	case "target_frame", "target_error", "target_write":
		if e.src == nil || ev.epoch != e.src.epoch || e.dst == nil || ev.gen != e.dst.gen {
			e.state.Counts.Stale++
			return
		}
		switch ev.kind {
		case "target_error":
			e.closeTarget("target_stream_" + errorClass(ev.err))
		case "target_write":
			if e.wait == nil || e.wait.token != ev.token {
				e.state.Counts.Stale++
				return
			}
			if ev.err != nil {
				e.closeTarget("target_write_partial_or_uncertain")
				return
			}
			e.wait.written = true
			e.state.Counts.Sent++
			e.acceptReceipt()
		case "target_frame":
			code, err := e.dst.profile.Response(ev.frame)
			if err != nil {
				e.closeTarget("target_response_profile_invalid")
				return
			}
			if code == 200 {
				if ev.changed || e.wait == nil || ev.token == 0 || e.wait.token != ev.token || e.wait.receipt != nil {
					e.state.Counts.Stale++
					e.closeTarget("unsolicited_or_duplicate_200_discarded")
					return
				}
				e.wait.receipt = ev.frame
				e.acceptReceipt()
			} else {
				if e.dst.profile == destination.GSPro {
					switch code {
					case 202:
						e.log("gspro_notification_202_observed_not_receipt", 0)
					case 203:
						e.log("gspro_notification_203_observed_not_receipt", 0)
					}
				}
				if e.switching {
					e.recordReplyDiscard(code, "destination_switch_pending")
				} else {
					e.relay(ev.frame, code)
				}
				if code >= 500 {
					e.closeTarget("target_reported_failure_no_success_ACK")
				}
			}
		}
	}
}

func (e *engine) tick(now time.Time) {
	defer e.advanceSwitch()
	if e.held != nil && !now.Before(e.held.until) {
		held := e.held
		e.held = nil
		e.forward(held)
	}
	if e.wait != nil && !now.Before(e.wait.until) {
		e.closeTarget("target_receipt_deadline_delivery_uncertain")
	}
	if e.src != nil && !e.src.quarantined && !e.switching && e.state.Destination != destination.Standby &&
		e.dst == nil && !e.dialing && !now.Before(e.retryAt) {
		e.state.TargetGeneration++
		ctx, cancel := context.WithTimeout(context.Background(), e.config.Dial)
		e.dst = &target{gen: e.state.TargetGeneration, cancel: cancel, profile: e.state.Destination.Default()}
		e.dialing = true
		e.state.Target = "connecting"
		epoch, gen := e.src.epoch, e.dst.gen
		address := e.dst.profile.Endpoint(e.config.Target)
		go func() {
			defer cancel()
			conn, err := e.config.DialContext(ctx, "tcp4", address)
			e.emit(event{kind: "dial", epoch: epoch, gen: gen, conn: conn, err: err})
		}()
	}
}

func (e *engine) closeTarget(reason string) {
	e.available.Store(0)
	if e.wait != nil {
		e.state.Counts.Uncertain++
		e.drop("delivery_uncertain_never_replay", e.wait.alias)
		e.wait = nil
	}
	if e.held != nil {
		e.drop("held_shot_target_generation_lost", e.held.alias)
		e.held = nil
	}
	if e.dst != nil {
		e.dst.cancel()
		if e.dst.conn != nil {
			e.closeConn(e.dst.conn, "target_socket_close_failed")
		}
		e.dst = nil
	}
	e.dialing = false
	e.state.Target = reason
	e.retryAt = time.Now().Add(e.backoff)
	e.backoff = min(e.backoff*2, e.config.RetryMax)
	e.log(reason, 0)
}

func (e *engine) closeConn(conn net.Conn, reason string) {
	if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		e.state.LastError = reason
		e.log(reason, 0)
	}
}

func (e *engine) quarantine(reason string) {
	e.state.LastError = reason
	e.log(reason, 0)
	if e.src != nil && !e.src.quarantined {
		e.src.quarantined = true
		close(e.src.writeStop)
		e.discardQueuedReplies("source_quarantined")
		e.state.Counts.Quarantines++
		e.state.Source = "quarantined_may_still_be_attached_disconnect_Labs"
		e.closeTarget("source_quarantined")
	}
}

func (e *engine) drop(reason string, alias uint64) {
	e.state.LastError = reason
	e.state.Counts.Dropped++
	e.log(reason, alias)
}

func (e *engine) offer(frame []byte, arrivalEligibility uint64) {
	msg, err := protocol.Inspect(frame)
	if err != nil {
		e.quarantine("source_profile_invalid")
		return
	}
	e.state.SourceFlags = msg.Flags
	if e.held != nil && !time.Now().Before(e.held.until) {
		previous := e.held
		e.held = nil
		e.forward(previous)
	}
	switch msg.Kind {
	case "status":
		e.state.Counts.Status++
		return
	case "heartbeat":
		e.state.Counts.Heartbeats++
		return
	}
	key := sha256.Sum256([]byte(msg.ID))
	if msg.Kind == "club_only" {
		if e.held == nil || e.held.key != key {
			e.state.Counts.LateClub++
			e.log("late_or_unmatched_club_omitted", 0)
			return
		}
		held := e.held
		e.held = nil
		if arrivalEligibility == 0 || arrivalEligibility != held.eligibility {
			e.drop("club_fragment_eligibility_changed_no_ACK", held.alias)
			return
		}
		merged, err := protocol.Merge(held.frame, frame)
		if err != nil {
			e.drop("merge_size_or_profile_rejected", held.alias)
			return
		}
		held.frame = merged
		e.state.Counts.Merged++
		e.log("same_session_same_ID_merged", held.alias)
		e.forward(held)
		return
	}
	e.state.Counts.Balls++
	if e.seen[key] {
		e.state.Counts.Duplicate++
		e.log("duplicate_ball_omitted_no_ACK", 0)
		return
	}
	if e.ledgerUsed == ledgerSize {
		delete(e.seen, e.ledger[e.ledgerNext])
	} else {
		e.ledgerUsed++
	}
	e.ledger[e.ledgerNext] = key
	e.ledgerNext = (e.ledgerNext + 1) % ledgerSize
	e.seen[key] = true
	e.nextAlias++
	if e.held != nil {
		previous := e.held
		e.held = nil
		e.forward(previous)
	}
	shot := &candidate{key: key, frame: frame, alias: e.nextAlias, eligibility: arrivalEligibility, until: time.Now().Add(e.config.Hold)}
	if e.wait != nil {
		e.drop("target_receipt_lane_busy_no_queue_no_ACK", shot.alias)
		return
	}
	if arrivalEligibility == 0 || arrivalEligibility != e.available.Load() || e.dst == nil || e.dst.conn == nil {
		e.drop("shot_target_eligibility_unavailable_or_changed_never_replay", shot.alias)
		return
	}
	shot.gen = e.dst.gen
	if msg.Flags.ContainsClubData {
		e.forward(shot)
	} else {
		e.held = shot
	}
}

func (e *engine) forward(shot *candidate) {
	if e.switching || e.src == nil || e.src.quarantined || e.dst == nil || e.dst.conn == nil || e.dst.gen != shot.gen ||
		shot.eligibility == 0 || shot.eligibility != e.available.Load() {
		e.drop("shot_no_longer_eligible_never_replay", shot.alias)
		return
	}
	if e.wait != nil {
		e.drop("target_receipt_lane_busy_no_queue_no_ACK", shot.alias)
		return
	}
	frame, err := e.dst.profile.Shot(shot.frame)
	if err != nil {
		e.drop("target_shot_profile_unsupported_no_ACK", shot.alias)
		return
	}
	e.nextToken++
	e.available.Store(0)
	// Register before the writer can run or a fast target can reply.
	e.wait = &pending{token: e.nextToken, alias: shot.alias, until: time.Now().Add(e.config.Receipt)}
	epoch, dst, token := e.src.epoch, e.dst, e.nextToken
	go func() {
		dst.token.Store(token)
		n, err := writeAll(dst.conn, frame, e.config.Write)
		e.emit(event{kind: "target_write", epoch: epoch, gen: dst.gen, token: token, n: n, err: err})
	}()
}

func (e *engine) acceptReceipt() {
	if e.wait == nil || !e.wait.written || e.wait.receipt == nil {
		return
	}
	e.state.Counts.TargetAccept++
	e.log("uncorrelated_target_200_not_exactly_once_or_game_ready", e.wait.alias)
	receipt := e.wait.receipt
	e.wait = nil
	e.dst.token.Store(0)
	e.relay(receipt, 200)
	if e.src != nil && !e.src.quarantined && e.dst != nil {
		e.openEligibility()
	}
}

// A reopened gate gets a new ticket even on the same TCP generation, so bytes
// observed before/during a pending receipt cannot regain eligibility afterward.
func (e *engine) openEligibility() {
	if e.switching || e.state.Destination == destination.Standby {
		return
	}
	e.nextEligibility++
	e.available.Store(e.nextEligibility)
	e.state.Target = "connected_ready_unknown"
}

func (e *engine) selectDestination(id destination.ID) error {
	reason := ""
	switch {
	case !id.Valid():
		reason = "invalid_destination"
	case e.quitting:
		reason = "destination_change_blocked_quit"
	case e.src != nil && e.src.quarantined:
		reason = "destination_change_blocked_source_fault"
	case e.switching && id != e.state.RequestedDestination:
		reason = "destination_change_already_pending"
	}
	if reason != "" {
		e.state.LastError = reason
		e.log(reason, 0)
		return errors.New(reason)
	}
	if e.switching || id == e.state.Destination.Default() {
		return nil
	}
	e.switching = true
	e.state.RequestedDestination = id
	e.state.Switch = "waiting_current_delivery"
	e.available.Store(0)
	if e.held != nil {
		e.drop("held_shot_destination_switch_no_ACK", e.held.alias)
		e.held = nil
	}
	e.log("destination_switch_requested_no_new_shots", 0)
	e.advanceSwitch()
	return nil
}

func (e *engine) advanceSwitch() {
	if !e.switching {
		return
	}
	if e.src != nil && e.src.quarantined {
		e.state.Switch = "blocked_source_fault_disconnect_Labs"
		return
	}
	if e.wait != nil || e.src != nil && e.src.queuedCount != 0 {
		return
	}
	e.closeTarget("destination_retired")
	e.state.Destination = e.state.RequestedDestination
	e.switching = false
	e.state.Switch = "idle"
	e.backoff, e.retryAt = e.config.RetryMin, time.Now()
	switch {
	case e.state.Destination == destination.Standby:
		e.state.Target = "standby"
	case e.src == nil:
		e.state.Target = "absent_source"
	default:
		e.state.Target = "retrying"
	}
	e.log("destination_switch_applied_new_shots_only", 0)
}

func (e *engine) relay(frame []byte, code int) {
	if e.src == nil || e.src.quarantined {
		return
	}
	if e.src.queuedCount >= queueCount || e.src.queuedBytes+len(frame) > queueBytes {
		e.recordReplyDiscard(code, "upstream_queue_overload")
		e.quarantine("upstream_response_queue_overload_no_source_close")
		return
	}
	e.src.queuedBytes += len(frame)
	e.src.queuedCount++
	var gen uint64
	if e.dst != nil {
		gen = e.dst.gen
	}
	// Only acceptReceipt calls this with 200, after both actual acceptance
	// and a successful full target write; other replies are live context.
	e.src.writes <- upstreamWrite{frame: frame, code: code, epoch: e.src.epoch, gen: gen}
}

func (e *engine) discardReply(ev event, reason string) {
	if e.src != nil && e.src.epoch == ev.epoch {
		e.src.queuedBytes -= ev.n
		e.src.queuedCount--
	}

	e.recordReplyDiscard(ev.code, reason)
}

func (e *engine) recordReplyDiscard(code int, reason string) {
	e.state.Counts.ReplyDiscarded++
	outcome := "upstream_response_discarded_" + reason
	if code == 200 {
		e.state.Counts.AcceptedACKAbandoned++
		outcome = "accepted_200_not_written_" + reason
	}
	e.state.LastError = outcome
	e.log(outcome, 0)
}

func (e *engine) discardQueuedReplies(reason string) {
	for {
		select {
		case work := <-e.src.writes:
			e.discardReply(event{epoch: work.epoch, gen: work.gen, code: work.code, n: len(work.frame)}, reason)
		default:
			return
		}
	}
}
