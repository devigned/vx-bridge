package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSummaryAndConsoleAreSanitizedAndBounded(t *testing.T) {
	ball1 := envelope("9007199254740992", true, true, false)
	ball1 = strings.Replace(ball1, `"Units":"Yards"`, `"Units":"private-units","private-key":"private-value"`, 1)
	input := strings.Repeat(envelope("0", false, false, false), maxEvents+5) +
		ball1 + ball1 + envelope("9007199254740993", true, false, false)
	result, received, _, console := exchange(t, input, testLimits())
	if result != positive {
		t.Fatal(result)
	}
	var output bytes.Buffer
	report, err := newReport(t.TempDir(), time.Now(), &output)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newReport(filepath.Dir(report.dir), report.started, io.Discard)
	if err != nil || report.dir == second.dir {
		t.Fatal("run directories must be unique")
	}
	initial, err := os.ReadFile(filepath.Join(report.dir, "running.json"))
	if err != nil || !bytes.Contains(initial, []byte("inconclusive_running_no_final_result")) {
		t.Fatalf("initial summary missing: %v", err)
	}
	report.Counts, report.Events, report.OmittedEvents = received.Counts, received.Events, received.OmittedEvents
	if err := report.finish(result); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(report.dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || len(data) > 64*1024 {
		t.Fatalf("invalid/oversized summary: %d bytes", len(data))
	}
	for _, private := range []string{"private-device", "private-units", "private-key", "private-value",
		"9007199254740992", "9007199254740993", "111.125", "88.5", "Yards"} {
		if bytes.Contains(data, []byte(private)) || strings.Contains(console, private) {
			t.Fatalf("private source value/key escaped: %s", private)
		}
	}
	if len(report.Events) != maxEvents || report.OmittedEvents != 8 || report.Counts.DuplicateBall != 1 {
		t.Fatalf("event bound/counts incorrect: events=%d omitted=%d counts=%+v", len(report.Events), report.OmittedEvents, report.Counts)
	}
	if report.Events[maxEvents-1].ShotAlias != "ball-2" || !report.Events[maxEvents-2].Duplicate {
		t.Fatal("summary must retain the latest ball/duplicate diagnostics after status traffic")
	}
}

func TestSummaryFailuresAreNotSuccess(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newReport(file, time.Now(), io.Discard); err == nil {
		t.Fatal("creation failure was hidden")
	}
	report, err := newReport(root, time.Now(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(report.dir, "summary.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	if code := finish(report, positive); code != reportFailed.exit {
		t.Fatalf("summary failure returned exit %d", code)
	}
}

type brokenConsole struct{}

func (brokenConsole) Write([]byte) (int, error) { return 0, errors.New("console_failure") }

func TestConsoleFailureIsExplicit(t *testing.T) {
	result, _, response, _ := exchange(t, envelope("1", true, false, false), testLimits())
	if result != earlyEOF || response != acknowledgment {
		t.Fatal("baseline exchange failed")
	}
	report, err := newReport(t.TempDir(), time.Now(), brokenConsole{})
	if err != nil {
		t.Fatal(err)
	}
	if report.record(sourceEvent{Kind: "status"}) == nil {
		t.Fatal("console write failure was hidden")
	}
	if code := finish(report, positive); code != reportFailed.exit {
		t.Fatalf("console failure returned exit %d", code)
	}
	data, err := os.ReadFile(filepath.Join(report.dir, "summary.json"))
	if err != nil || !bytes.Contains(data, []byte(`"outcome": "reporting_failed"`)) {
		t.Fatalf("console failure not persisted: %v", err)
	}
}

func TestProductionBounds(t *testing.T) {
	if maxLifetime > 5*time.Minute || shutdownBudget <= 0 || shutdownBudget >= maxLifetime ||
		receiveBudget != 1<<20 || listenAddress != "127.0.0.1:12321" {
		t.Fatal("production bounds changed")
	}
}

func TestImmutableRecordsAllowOrdinaryPollingAndHeldReaders(t *testing.T) {
	report := memoryReport(t, io.Discard)
	runningPath := filepath.Join(report.dir, "running.json")
	initial, err := os.ReadFile(runningPath)
	if err != nil {
		t.Fatal(err)
	}
	runningReader, err := os.Open(runningPath)
	if err != nil {
		t.Fatal(err)
	}
	defer runningReader.Close()
	stop := make(chan struct{})
	started := make(chan struct{})
	type readerResult struct {
		err              error
		transientSharing int
	}
	done := make(chan readerResult, 1)
	go func() {
		sharing := 0
		close(started)
		for {
			select {
			case <-stop:
				done <- readerResult{transientSharing: sharing}
				return
			default:
			}
			for _, name := range []string{"running.json", "goal.json", "summary.json"} {
				data, err := os.ReadFile(filepath.Join(report.dir, name))
				if os.IsNotExist(err) && name != "running.json" {
					continue // A not-yet-published immutable record is not ready.
				}
				if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(32)) && name != "running.json" {
					sharing++ // A read racing first publication can be temporarily unavailable.
					continue
				}
				if err != nil {
					done <- readerResult{err: err, transientSharing: sharing}
					return
				}
				if !json.Valid(data) {
					done <- readerResult{err: errors.New("partial JSON visible during publication")}
					return
				}
			}
		}
	}()
	<-started
	writeErr := report.goalCheckpoint()
	var goalReader *os.File
	if writeErr == nil {
		goalReader, writeErr = os.Open(filepath.Join(report.dir, "goal.json"))
		if writeErr == nil {
			defer goalReader.Close()
			writeErr = report.finish(earlyEOF)
		}
	}
	close(stop)
	result := <-done
	if writeErr != nil || result.err != nil {
		t.Fatalf("ordinary reader interfered with publication: write=%v read=%v", writeErr, result.err)
	}
	if result.transientSharing != 0 {
		t.Logf("%d transient read-side sharing conflicts during first publication; all writes succeeded", result.transientSharing)
	}
	if data, err := os.ReadFile(runningPath); err != nil || !bytes.Equal(data, initial) {
		t.Fatalf("immutable running record was changed: %v", err)
	}
	if err := report.publishRecord("goal"); err == nil {
		t.Fatal("an existing goal record was silently overwritten")
	}
	if err := report.finish(positive); err == nil {
		t.Fatal("the already-published final record was overwritten")
	}
	if snapshot := readRecord(t, report, "goal"); snapshot.Final || snapshot.Outcome != goalCheckpointOutcome {
		t.Fatalf("immutable goal was changed: %+v", snapshot)
	}
	if snapshot := readSummary(t, report); !snapshot.Final || snapshot.Outcome != earlyEOF.name {
		t.Fatalf("immutable final record was changed: %+v", snapshot)
	}
}
