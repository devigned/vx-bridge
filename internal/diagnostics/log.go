// Package diagnostics records sanitized bridge/desktop events in bounded
// per-user JSONL files. [Logger] implements [bridge.EventSink] without making the
// engine or UI wait on disk, and [Logger.Health] exposes degraded persistence.
// Logs are diagnostic aids, not a durable audit or a place for raw shot data.
package diagnostics

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
)

const (
	// FileCount is the fixed number of rotating bridge.N.jsonl slots.
	FileCount = 5
	// FileBytes is the maximum size in bytes of each production log slot.
	FileBytes = 2 << 20
	// QueueSize is the maximum number of queued file-worker records.
	QueueSize = 32
)

var socketCode = regexp.MustCompile(`^(source_|source_departed_|target_stream_)(EOF|peer_reset|local_cancel_or_close|deadline|frame_size|framing|unknown_io)(_incomplete_frame)?(_errno_[0-9]{1,20})?$`)

var fixedCodes = func() map[string]bool {
	codes := make(map[string]bool)
	for _, code := range strings.Fields(`
host_listening_no_game_ready_claim quit_requested_disconnect_Labs_normally
source_listener_close_failed_waiting_for_accept_barrier second_source_rejected_without_eviction
source_attached previous_source_epoch_actual_response_write_completed previous_source_epoch_response_write_uncertain
departed_source_socket_cleanup_failed admitted_upstream_write_completed_after_target_retirement
source_200_complete_local_write_not_peer_consumption target_connected_ready_unknown_new_shots_only
target_unavailable target_write_partial_or_uncertain target_response_profile_invalid
gspro_notification_202_observed_not_receipt gspro_notification_203_observed_not_receipt
unsolicited_or_duplicate_200_discarded target_reported_failure_no_success_ACK
target_receipt_deadline_delivery_uncertain delivery_uncertain_never_replay held_shot_target_generation_lost
source_listener_failed platform_fault_waiting_for_source_departure identity_unavailable_pending_source_departure
source_profile_invalid source_quarantined absent_source host_stopping target_socket_close_failed
rejected_source_close_failed stale_dial_socket_close_failed upstream_response_queue_overload_no_source_close
late_or_unmatched_club_omitted club_fragment_eligibility_changed_no_ACK merge_size_or_profile_rejected
same_session_same_ID_merged duplicate_ball_omitted_no_ACK target_receipt_lane_busy_no_queue_no_ACK
shot_target_eligibility_unavailable_or_changed_never_replay shot_no_longer_eligible_never_replay
uncorrelated_target_200_not_exactly_once_or_game_ready
upstream_response_discarded_source_epoch_ended upstream_response_discarded_target_generation_retired
upstream_response_discarded_source_quarantined upstream_response_discarded_source_writer_stopped
upstream_response_discarded_upstream_queue_overload
accepted_200_not_written_source_epoch_ended accepted_200_not_written_target_generation_retired
accepted_200_not_written_source_quarantined accepted_200_not_written_source_writer_stopped
accepted_200_not_written_upstream_queue_overload
source_response_write_failed_delivery_uncertain
platform_fault_channel_closed diagnostics_sink_failed diagnostics_sink_failed_use_status
control_request_failed control_listener_failed quit_request_failed startup_unavailable_use_status
opening_logs open_logs_failed startup_change_pending startup_change_failed desktop_actions_busy
desktop_worker_cleanup_deadline desktop_status_unavailable
diagnostic_report_pending diagnostic_report_saved diagnostic_report_partial
diagnostic_report_cancelled diagnostic_report_failed report_bug_pending report_bug_failed
destination_switch_requested_no_new_shots destination_switch_applied_new_shots_only destination_retired
held_shot_destination_switch_no_ACK target_shot_profile_unsupported_no_ACK
destination_change_already_pending destination_change_blocked_quit destination_change_blocked_source_fault
invalid_destination upstream_response_discarded_destination_switch_pending
destination_change_pending destination_change_failed destination_preference_save_failed
tray_starting tray_available tray_initialization_failed_use_control tray_wrong_owner_thread_use_control
tray_restore_failed_use_control tray_update_failed_use_control tray_menu_failed_use_control
tray_and_notice_unavailable_use_control tray_menu_dismiss_failed_use_control
`) {
		codes[code] = true
	}
	return codes
}()

// Build identifies the producer in every persisted diagnostic record.
// Callers supply non-sensitive build metadata; these strings are not redacted.
type Build struct {
	// Version is the bridge protocol/application version label.
	Version string `json:"version"`
	// Release is the actual release version, separate from the protocol label.
	Release string `json:"release,omitempty"`
	// Revision is the source revision stamped into the build.
	Revision string `json:"revision"`
	// Go is the Go toolchain identity used by the producer.
	Go string `json:"go"`
	// OS and Architecture identify the producer, not an installed copy.
	OS           string `json:"os,omitempty"`
	Architecture string `json:"architecture,omitempty"`
}

// record pairs Event, the sanitized local outcome, with Build, its producer.
type record struct {
	// Event is the sanitized time, outcome code, and local alias for this entry.
	bridge.Event
	// Build identifies the entry's producer using the caller-supplied build metadata.
	Build Build `json:"build"`
}

// Health is a point-in-time persistence status, not a guarantee of durable data.
type Health struct {
	// State is "local_logs", an opening/slow state, or the first failure code.
	State string
	// Lost counts calls rejected by WriteEvent, not all records potentially
	// lost by a later asynchronous disk error.
	Lost uint64
}

// Logger has a single file owner and a finite queue. A blocked disk is never
// awaited by the engine or UI; Close is bounded by the caller's context.
// Construct it with New; the zero value is not usable. WriteEvent, Health, and
// Close are safe for concurrent use. Stop producers before final Close if a
// clean flush result is required.
type Logger struct {
	build   Build
	queue   chan record
	done    chan struct{}
	mu      sync.Mutex
	closed  bool
	failure string
	lost    uint64
	recent  []bridge.Event
	active  atomic.Int64
	opened  atomic.Bool
}

// New starts a logger that asynchronously opens fixed slots in directory,
// creating it if needed. It returns before storage is verified; consult Health
// and Close for opening/write/cleanup failures. An empty directory is rejected
// by the worker rather than selecting a fallback location.
func New(directory string, build Build) *Logger {
	return newLogger(build, func() (io.WriteCloser, error) {
		return openSlots(directory, FileBytes)
	})
}

func newLogger(build Build, open func() (io.WriteCloser, error)) *Logger {
	l := &Logger{build: build, queue: make(chan record, QueueSize), done: make(chan struct{})}
	l.active.Store(time.Now().UnixNano())
	go func() {
		defer close(l.done)
		writer, err := open()
		if err != nil {
			l.fail("local_logs_open_failed_use_status")
			l.active.Store(0)
			return
		}
		l.active.Store(0)
		l.opened.Store(true)
		for ev := range l.queue {
			data, err := json.Marshal(ev)
			if err == nil {
				l.active.Store(time.Now().UnixNano())
				var n int
				n, err = writer.Write(append(data, '\n'))
				if err == nil && n != len(data)+1 {
					err = io.ErrShortWrite
				}
			}
			if err != nil {
				l.fail("local_logs_write_or_rotation_failed_use_status")
				break
			}
			l.active.Store(0)
		}
		l.active.Store(time.Now().UnixNano())
		if err := writer.Close(); err != nil {
			l.fail("local_logs_close_failed_use_status")
		}
		l.active.Store(0)
	}()
	return l
}

func (l *Logger) fail(code string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failure == "" {
		l.failure = code
	}
}

// WriteEvent retains ev in a bounded recent-event slice and queues a record
// without waiting for disk. Unknown reason text is replaced by a short hash and
// its alias cleared. Known reasons must carry only run-local aliases, not IDs.
// A nil error means queued, not persisted. A full queue, known failure, or closed
// logger returns an error and increments Health.Lost; failures remain visible.
func (l *Logger) WriteEvent(ev bridge.Event) error {
	ev = sanitizeEvent(ev)
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.recent) == 64 {
		copy(l.recent, l.recent[1:])
		l.recent = l.recent[:63]
	}

	l.recent = append(l.recent, ev)
	if l.closed && l.failure == "" {
		l.failure = "local_logs_closed_use_status"
	}
	if l.closed || l.failure != "" {
		l.lost++
		return errors.New("local_logs_unavailable_use_status")
	}
	select {
	case l.queue <- record{Event: ev, Build: l.build}:
		return nil
	default:
		l.failure = "local_logs_queue_full_use_status"
		l.lost++
		return errors.New(l.failure)
	}
}

var redactedCode = regexp.MustCompile(`^unrecognized_event_redacted_[0-9a-f]{8}_use_status$`)

func sanitizeEvent(ev bridge.Event) bridge.Event {
	if !fixedCodes[ev.Reason] && !socketCode.MatchString(ev.Reason) && !redactedCode.MatchString(ev.Reason) {
		hash := sha256.Sum256([]byte(ev.Reason))
		ev.Reason, ev.Alias = fmt.Sprintf("unrecognized_event_redacted_%x_use_status", hash[:4]), 0
	}
	return ev
}

// Health returns cached worker state and the count of rejected events without
// disk I/O. Opening or I/O active longer than 2 s is reported as slow unless a
// recorded failure already takes precedence.
func (l *Logger) Health() Health {
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.failure
	if state == "" {
		state = "local_logs"
		if !l.opened.Load() {
			state = "local_logs_opening"
		}
		if start := l.active.Load(); start != 0 && time.Since(time.Unix(0, start)) > 2*time.Second {
			state = "local_logs_slow_use_status"
		}
	}
	return Health{State: state, Lost: l.lost}
}

// Close stops queue admission and waits for the file worker to drain and close.
// Repeated calls may wait on the same worker. Any recorded health failure is
// returned even after the worker exits. If ctx expires, Close records a flush
// failure and returns ctx.Err without forcibly interrupting disk I/O. Closing
// does not sync files to stable storage or shut down the bridge.
func (l *Logger) Close(ctx context.Context) error {
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.queue)
	}
	l.mu.Unlock()
	select {
	case <-l.done:
		if health := l.Health(); health.State != "local_logs" {
			return errors.New(health.State)
		}
		return nil
	case <-ctx.Done():
		l.fail("local_logs_flush_deadline_use_status")
		return ctx.Err()
	}
}

// Fixed slots avoid rename/delete cleanup and its hidden retention failures.
// Only these five names are opened; rotation stops visibly on any slot error.
type slots struct {
	directory string
	maximum   int64
	index     int
	size      int64
	file      *os.File
}

func openSlots(directory string, maximum int64) (*slots, error) {
	if directory == "" || maximum < 1024 {
		return nil, errors.New("invalid_log_directory_or_budget")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	s := &slots{directory: directory, maximum: maximum}
	var newest time.Time
	for i := 0; i < FileCount; i++ {
		info, err := os.Lstat(s.name(i))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > maximum {
			return nil, errors.New("unexpected_log_slot_type_or_size")
		}
		if info.ModTime().After(newest) {
			s.index, s.size, newest = i, info.Size(), info.ModTime()
		}
	}
	file, err := os.OpenFile(s.name(s.index), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	s.file = file
	return s, nil
}

func (s *slots) name(index int) string {
	return filepath.Join(s.directory, "bridge."+string(rune('0'+index))+".jsonl")
}

// Write appends data to the active slot, rotating through fixed names at the
// byte budget. Records over 1024 bytes are rejected. Native write/close/rotation
// failures return the actual count/error; no alternative filenames are created.
func (s *slots) Write(data []byte) (int, error) {
	if len(data) > 1024 || int64(len(data)) > s.maximum {
		return 0, errors.New("log_record_exceeds_budget")
	}
	if s.size+int64(len(data)) > s.maximum {
		if err := s.file.Close(); err != nil {
			return 0, err
		}
		next := (s.index + 1) % FileCount
		if info, err := os.Lstat(s.name(next)); err == nil {
			if !info.Mode().IsRegular() {
				return 0, errors.New("unexpected_rotation_slot_type")
			}
		} else if !os.IsNotExist(err) {
			return 0, err
		}
		file, err := os.OpenFile(s.name(next), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return 0, err
		}
		s.file, s.index, s.size = file, next, 0
	}
	n, err := s.file.Write(data)
	s.size += int64(n)
	return n, err
}

// Close closes the active slot file and returns its close error.
func (s *slots) Close() error { return s.file.Close() }
