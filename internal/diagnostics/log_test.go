package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
)

func waitHealth(t *testing.T, logger *Logger, check func(Health) bool) {
	t.Helper()
	until := time.Now().Add(time.Second)
	for !check(logger.Health()) {
		if time.Now().After(until) {
			t.Fatalf("health did not settle: %+v", logger.Health())
		}
		time.Sleep(time.Millisecond)
	}
}

func closeLogger(t *testing.T, logger *Logger) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return logger.Close(ctx)
}

func TestFixedSlotsAreBoundedAcrossRotationAndRestart(t *testing.T) {
	directory := t.TempDir()
	for run := 0; run < 3; run++ {
		sink, err := openSlots(directory, 1024)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 80; i++ {
			if _, err := sink.Write(bytes.Repeat([]byte("x"), 300)); err != nil {
				t.Fatal(err)
			}
		}
		if err := sink.Close(); err != nil {
			t.Fatal(err)
		}
		files, err := os.ReadDir(directory)
		if err != nil || len(files) != FileCount {
			t.Fatalf("unbounded file count: %d %v", len(files), err)
		}
		var total int64
		for _, file := range files {
			info, err := file.Info()
			if err != nil || info.Size() > 1024 {
				t.Fatal("slot exceeded exact configured byte budget")
			}
			total += info.Size()
		}
		if total > FileCount*1024 {
			t.Fatal("total log byte budget exceeded")
		}
	}
}

func TestRotationFailureIsReturnedRatherThanCreatingMoreFiles(t *testing.T) {
	directory := t.TempDir()
	sink, err := openSlots(directory, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	if err := os.Mkdir(sink.name(1), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Write(bytes.Repeat([]byte("x"), 900)); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Write(bytes.Repeat([]byte("x"), 900)); err == nil {
		t.Fatal("rotation failure hidden")
	}
	files, _ := os.ReadDir(directory)
	if len(files) != 2 {
		t.Fatal("rotation failure created unbounded alternative names")
	}
}

func TestStructuredEventsNeverPersistRawValues(t *testing.T) {
	directory := t.TempDir()
	logger := New(directory, Build{Version: "test", Revision: "development", Go: "go-test"})
	waitHealth(t, logger, func(h Health) bool { return h.State == "local_logs" })
	now := time.Now().UTC()
	for _, ev := range []bridge.Event{
		{At: now, Reason: "source_attached"},
		{At: now, Reason: `{"DeviceID":"PRIVATE-DEVICE","ShotNumber":987654321,"Speed":184.345,"Player":"PRIVATE-PLAYER"}`, Alias: 987654321},
		{At: now, Reason: "PRIVATE_DEVICE_987654321", Alias: 987654321},
	} {
		if err := logger.WriteEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := closeLogger(t, logger); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "bridge.0.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE-DEVICE", "PRIVATE-PLAYER", "PRIVATE_DEVICE", "987654321", "184.345", "DeviceID", "ShotNumber"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("raw value leaked: %s", secret)
		}
	}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var event record
		if err := json.Unmarshal(line, &event); err != nil || event.Build.Version != "test" || event.At.IsZero() {
			t.Fatal("not a structured, versioned event")
		}
	}
}

func TestMultiGameEventsRemainActionableWithoutBroadeningRedaction(t *testing.T) {
	directory := t.TempDir()
	logger := New(directory, Build{Version: "synthetic"})
	waitHealth(t, logger, func(h Health) bool { return h.State == "local_logs" })
	codes := []string{
		"destination_switch_requested_no_new_shots", "destination_switch_applied_new_shots_only", "destination_retired",
		"held_shot_destination_switch_no_ACK", "target_shot_profile_unsupported_no_ACK",
		"destination_change_already_pending", "destination_change_blocked_quit", "destination_change_blocked_source_fault",
		"invalid_destination", "upstream_response_discarded_destination_switch_pending",
		"destination_change_pending", "destination_change_failed", "destination_preference_save_failed",
	}
	for _, code := range codes {
		if err := logger.WriteEvent(bridge.Event{At: time.Now().UTC(), Reason: code}); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.WriteEvent(bridge.Event{Reason: "destination_change_PRIVATE_DEVICE", Alias: 987654321}); err != nil {
		t.Fatal(err)
	}
	if err := closeLogger(t, logger); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "bridge.0.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range codes {
		if !bytes.Contains(data, []byte(`"reason":"`+code+`"`)) {
			t.Fatal("reviewed multi-game event was hidden as unknown", code)
		}
	}
	if bytes.Contains(data, []byte("PRIVATE_DEVICE")) || bytes.Contains(data, []byte("987654321")) ||
		!bytes.Contains(data, []byte("unrecognized_event_redacted_")) {
		t.Fatal("destination event prefix bypassed privacy redaction")
	}
}

type faultWriter struct {
	block <-chan struct{}
	err   error
	short bool
}

func (w faultWriter) Write(data []byte) (int, error) {
	if w.block != nil {
		<-w.block
	}
	if w.short {
		return len(data) - 1, nil
	}
	if w.err != nil {
		return 0, w.err
	}
	return len(data), nil
}
func (faultWriter) Close() error { return nil }

func TestPermissionDiskFullAndShortWritesAreVisible(t *testing.T) {
	for _, writer := range []faultWriter{{err: os.ErrPermission}, {err: errors.New("invented_disk_full")}, {short: true}} {
		logger := newLogger(Build{}, func() (io.WriteCloser, error) { return writer, nil })
		waitHealth(t, logger, func(h Health) bool { return h.State == "local_logs" })
		if err := logger.WriteEvent(bridge.Event{Reason: "source_attached"}); err != nil {
			t.Fatal(err)
		}
		waitHealth(t, logger, func(h Health) bool { return strings.Contains(h.State, "failed") })
		if err := closeLogger(t, logger); err == nil {
			t.Fatal("failed persistence returned healthy close")
		}
		if len(logger.recent) != 1 {
			t.Fatal("bounded fallback lost the sanitized event")
		}
	}
}

func TestBlockedDiskCannotBlockProducerOrBoundedClose(t *testing.T) {
	release := make(chan struct{})
	logger := newLogger(Build{}, func() (io.WriteCloser, error) { return faultWriter{block: release}, nil })
	waitHealth(t, logger, func(h Health) bool { return h.State == "local_logs" })
	start := time.Now()
	for i := 0; i < 200; i++ {
		logger.WriteEvent(bridge.Event{Reason: "source_attached"})
	}
	if time.Since(start) > 200*time.Millisecond || len(logger.recent) != 64 || len(logger.queue) > QueueSize {
		t.Fatal("blocked storage stalled producer or exceeded memory bounds")
	}
	if !strings.Contains(logger.Health().State, "queue_full") || logger.Health().Lost == 0 {
		t.Fatal("blocked disk overload is not visible")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := logger.Close(ctx); err == nil {
		t.Fatal("blocked disk pretended to flush")
	}
	close(release)
	if err := closeLogger(t, logger); err == nil {
		t.Fatal("overload failure was erased")
	}
}

func TestInvalidDirectoryFailsExplicitly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	logger := New(filepath.Join(path, "logs"), Build{})
	waitHealth(t, logger, func(h Health) bool { return strings.Contains(h.State, "open_failed") })
	if err := closeLogger(t, logger); err == nil {
		t.Fatal("invalid log directory returned success")
	}
}

func TestDefaultTwoMiBBoundaryAndDeclaredBudgets(t *testing.T) {
	if FileCount != 5 || FileBytes != 2<<20 || QueueSize != 32 {
		t.Fatal("documented desktop resource budgets changed")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "bridge.0.jsonl")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), FileBytes-1), 0600); err != nil {
		t.Fatal(err)
	}
	sink, err := openSlots(directory, FileBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	if _, err := sink.Write([]byte("x")); err != nil || sink.index != 0 || sink.size != FileBytes {
		t.Fatal("exact 2 MiB boundary did not fit", err)
	}
	if _, err := sink.Write([]byte("x")); err != nil || sink.index != 1 || sink.size != 1 {
		t.Fatal("first byte beyond 2 MiB did not rotate", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != FileBytes {
		t.Fatal("default slot exceeded its declared 2 MiB", err)
	}
}
