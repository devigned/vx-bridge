package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type goalNotice struct{ reached chan struct{} }

func (w goalNotice) Write(data []byte) (int, error) {
	if strings.Contains(string(data), "TWO-SHOT EVIDENCE REACHED") {
		close(w.reached)
	}
	return len(data), nil
}

func sendBallAndReadAck(t *testing.T, peer net.Conn, id string) {
	t.Helper()
	if _, err := io.WriteString(peer, envelope(id, true, false, false)); err != nil {
		t.Fatal(err)
	}
	ack := make([]byte, len(acknowledgment))
	if _, err := io.ReadFull(peer, ack); err != nil || string(ack) != acknowledgment {
		t.Fatalf("ack=%q err=%v", ack, err)
	}
}

func waitGoal(t *testing.T, reached <-chan struct{}) {
	t.Helper()
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("goal checkpoint not announced")
	}
}

func readSummary(t *testing.T, report *runReport) runReport {
	t.Helper()
	return readRecord(t, report, "summary")
}

func readRecord(t *testing.T, report *runReport, name string) runReport {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(report.dir, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot runReport
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

// The same scenario is used with the real Windows message pump in session-0 tests.
func exercisePersistentGoal(t *testing.T,
	receive func(*net.TCPListener, limits, *runReport) outcome, windowClosed <-chan struct{}) *runReport {
	t.Helper()
	listener, err := listenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	config := testLimits()
	config.deadline = time.Now().Add(5 * time.Second)
	reached := make(chan struct{})
	report := memoryReport(t, goalNotice{reached})
	done := make(chan outcome, 1)
	go func() { done <- receive(listener, config, report) }()
	peer, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err := peer.SetDeadline(time.Now().Add(6 * time.Second)); err != nil {
		t.Fatal(err)
	}
	sendBallAndReadAck(t, peer, "9007199254740992")
	sendBallAndReadAck(t, peer, "9007199254740993")
	waitGoal(t, reached)
	snapshot := readRecord(t, report, "goal")
	if snapshot.Probe != probeIdentity || snapshot.Final || snapshot.ExitCode == 0 ||
		snapshot.Outcome != goalCheckpointOutcome || !snapshot.Lifecycle.GoalReached ||
		snapshot.Lifecycle.SourceEOF || !snapshot.Lifecycle.ForcedTeardownRisk ||
		snapshot.Lifecycle.SourceState != "accepted_no_clean_eof_observed" ||
		snapshot.Counts.DistinctAcked != 2 || snapshot.Counts.ReceivedBytes == 0 ||
		snapshot.NetworkDeadlineUTC != config.deadline.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("invalid running goal checkpoint: %+v", snapshot)
	}
	if _, err := os.Stat(filepath.Join(report.dir, "summary.json")); !os.IsNotExist(err) {
		t.Fatalf("final summary existed before source EOF/cleanup: %v", err)
	}
	runningReader, err := os.Open(filepath.Join(report.dir, "running.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer runningReader.Close()
	goalReader, err := os.Open(filepath.Join(report.dir, "goal.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer goalReader.Close()
	select {
	case result := <-done:
		t.Fatalf("goal ended the receiver: %v", result)
	case <-windowClosed:
		t.Fatal("goal closed the compatibility window")
	case <-time.After(350 * time.Millisecond):
		// Exceeds v2's former 250 ms close without extending the v3 read deadline.
	}
	extra := envelope("9007199254740993", false, true, false) +
		envelope("0", false, false, false) + envelope("0", false, false, true)
	if _, err := io.WriteString(peer, extra); err != nil {
		t.Fatal(err)
	}
	sendBallAndReadAck(t, peer, "9007199254740993") // duplicate, no new distinct ID
	sendBallAndReadAck(t, peer, "9007199254740994") // additional actual test-peer message
	if snapshot := readRecord(t, report, "goal"); snapshot.Final || snapshot.Outcome != goalCheckpointOutcome {
		t.Fatalf("continued traffic converted the checkpoint to success: %+v", snapshot)
	}
	if err := peer.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	tail, err := io.ReadAll(peer)
	if err != nil || len(tail) != 0 {
		t.Fatalf("non-ball traffic caused an extra response: %q err=%v", tail, err)
	}
	select {
	case result := <-done:
		if result != positive {
			t.Fatalf("clean source EOF after goal: result=%v lifecycle=%+v", result, report.Lifecycle)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("source EOF did not release network/window")
	}
	if !report.Lifecycle.SourceEOF || report.Lifecycle.ForcedTeardownRisk ||
		report.Lifecycle.SourceState != "clean_source_eof_observed" ||
		report.Lifecycle.ShutdownReason != positive.name ||
		report.Counts.DistinctBall != 3 || report.Counts.DuplicateBall != 1 ||
		report.Counts.DistinctAcked != 3 || report.Counts.Acks != 4 ||
		report.Counts.ClubOnly != 1 || report.Counts.Status != 1 || report.Counts.Heartbeat != 1 {
		t.Fatalf("source-first result/counts: lifecycle=%+v counts=%+v", report.Lifecycle, report.Counts)
	}
	return report
}

func TestGoalCheckpointKeepsConnectionAndWindowUntilSourceEOF(t *testing.T) {
	closed := make(chan struct{})
	backend := &fakeWindow{state: verifiedWindow(), closedSignal: closed}
	report := exercisePersistentGoal(t,
		func(listener *net.TCPListener, config limits, report *runReport) outcome {
			return runDiagnostic(listener, config, report,
				func(ctx context.Context) *windowSession { return startWindow(ctx, backend) })
		}, closed)
	if !backend.closed || !report.Window.Destroyed || !report.Window.ClassReleased {
		t.Fatalf("source EOF left the window active: %+v", report.Window)
	}
	if code := finish(report, positive); code != 0 {
		t.Fatalf("source-first receiver result=%d", code)
	}
	final := readSummary(t, report)
	if !final.Final || final.ExitCode != 0 || !final.Lifecycle.SourceEOF || !final.Lifecycle.GoalReached {
		t.Fatalf("final receiver-local summary invalid: %+v", final)
	}
}

func TestGoalNeverConvertsDeadlineOrCancelIntoSuccess(t *testing.T) {
	for _, mode := range []string{"deadline", "cancel", "window_failure"} {
		t.Run(mode, func(t *testing.T) {
			reached := make(chan struct{})
			report := memoryReport(t, goalNotice{reached})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			config := testLimits()
			config.context = ctx
			// Setup/ACKs use only a hang guard; each failure is triggered after goal.
			config.deadline = time.Now().Add(maxLifetime)
			if deadline, ok := t.Deadline(); ok {
				config.deadline = deadline
			}
			config.write = time.Until(config.deadline)
			loopFailure := make(chan struct{})
			backend := &fakeWindow{state: verifiedWindow(), loopFailure: loopFailure}
			done := make(chan outcome, 1)
			var peer net.Conn
			var expireRead func(time.Time) error
			if mode == "deadline" {
				// The owned read deadline can be expired after the real goal checkpoint.
				source, client := net.Pipe()
				defer source.Close()
				peer, expireRead = client, source.SetReadDeadline
				go func() { done <- receiveShots(source, config, report) }()
			} else {
				listener, err := listenExclusive("127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				go func() {
					done <- runDiagnostic(listener, config, report,
						func(ctx context.Context) *windowSession { return startWindow(ctx, backend) })
				}()
				peer, err = net.Dial("tcp4", listener.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
			}
			defer peer.Close()
			if err := peer.SetDeadline(config.deadline); err != nil {
				t.Fatal(err)
			}
			sendBallAndReadAck(t, peer, "1")
			sendBallAndReadAck(t, peer, "2")
			waitGoal(t, reached)
			checkpoint := readRecord(t, report, "goal")
			if checkpoint.Final || checkpoint.Outcome != goalCheckpointOutcome || checkpoint.ExitCode == 0 ||
				!checkpoint.Lifecycle.GoalReached || checkpoint.Lifecycle.SourceEOF ||
				!checkpoint.Lifecycle.ForcedTeardownRisk || checkpoint.Counts.Acks != 2 ||
				checkpoint.Counts.DistinctAcked != 2 {
				t.Fatalf("failure must follow a persisted two-ACK inconclusive goal: %+v", checkpoint)
			}
			select {
			case result := <-done:
				t.Fatalf("session ended before the post-goal failure: %v", result)
			default:
			}
			want := sessionTimeout
			switch mode {
			case "deadline":
				if err := expireRead(time.Now()); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
				want = interrupted
			case "window_failure":
				close(loopFailure)
				want = windowLoopFailed
			}
			var result outcome
			select {
			case result = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("failure/cancel/deadline leaked a session")
			}
			if result != want || !report.Lifecycle.GoalReached || report.Lifecycle.SourceEOF ||
				!report.Lifecycle.ForcedTeardownRisk || report.Counts.Acks != 2 ||
				report.Counts.DistinctAcked != 2 {
				t.Fatalf("goal hid a failure: %v %+v", result, report.Lifecycle)
			}
			if mode != "deadline" && (!backend.closed || !report.Window.Verified() ||
				!report.Window.CleanupOwnerThread || !report.Window.Destroyed || !report.Window.ClassReleased) {
				t.Fatalf("post-goal window cleanup incomplete: %+v", report.Window)
			}
			if code := finish(report, result); code == 0 {
				t.Fatal("unsafe forced teardown returned zero")
			}
			final := readSummary(t, report)
			if !final.Final || final.ExitCode == 0 || !final.Lifecycle.ForcedTeardownRisk ||
				!final.Lifecycle.GoalReached || final.Lifecycle.SourceEOF ||
				final.Counts.Acks != 2 || final.Counts.DistinctAcked != 2 ||
				final.Lifecycle.ShutdownReason != result.name {
				t.Fatalf("forced teardown not explicit: %+v", final)
			}
		})
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type scriptedSource struct {
	net.Conn
	input     io.Reader
	output    bytes.Buffer
	writes    int
	failWrite int
}

func (s *scriptedSource) Read(p []byte) (int, error)       { return s.input.Read(p) }
func (s *scriptedSource) SetDeadline(time.Time) error      { return nil }
func (s *scriptedSource) SetWriteDeadline(time.Time) error { return nil }
func (s *scriptedSource) Close() error                     { return nil }
func (s *scriptedSource) Write(data []byte) (int, error) {
	s.writes++
	if s.writes == s.failWrite {
		return 0, io.ErrClosedPipe
	}
	return s.output.Write(data)
}

func TestPostGoalFailuresRequireCleanEOFNotJustTwoAcks(t *testing.T) {
	two := envelope("1", true, false, false) + envelope("2", true, true, false)
	for _, test := range []struct {
		name      string
		tail      string
		readError bool
		failWrite int
		budget    int
		want      outcome
	}{
		{name: "non_eof_read_error", readError: true, want: ioFailed},
		{name: "partial_eof", tail: `{"`, want: invalidFrame},
		{name: "garbage", tail: "x", want: invalidFrame},
		{name: "unexpected_shape", tail: `{}`, want: unexpectedShape},
		{name: "budget", tail: strings.Repeat(" ", 40), budget: len(two) + 32, want: budgetExceeded},
		{name: "failed_later_ack", tail: envelope("3", true, false, false), failWrite: 3, want: ackFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			var input io.Reader = strings.NewReader(two + test.tail)
			if test.readError {
				input = io.MultiReader(input, errorReader{})
			}
			source := &scriptedSource{input: input, failWrite: test.failWrite}
			report := memoryReport(t, io.Discard)
			config := testLimits()
			if test.budget != 0 {
				config.budget = test.budget
			}
			result := receiveShots(source, config, report)
			if result != test.want || !report.Lifecycle.GoalReached || report.Lifecycle.SourceEOF ||
				!report.Lifecycle.ForcedTeardownRisk ||
				source.output.String() != strings.Repeat(acknowledgment, 2) {
				t.Fatalf("post-goal error hidden: %v lifecycle=%+v response=%q", result, report.Lifecycle, source.output.String())
			}
		})
	}
}

type checkpointSaboteur struct {
	dir         string
	failConsole bool
	changed     bool
}

func (w *checkpointSaboteur) Write(data []byte) (int, error) {
	if strings.Contains(string(data), `"shot_alias":"ball-2"`) && !w.changed {
		w.changed = true
		if err := os.Mkdir(filepath.Join(w.dir, "goal.tmp"), 0700); err != nil {
			return 0, err
		}
	}
	if w.failConsole && strings.Contains(string(data), "TWO-SHOT EVIDENCE REACHED") {
		return 0, errors.New("injected_console_error")
	}
	return len(data), nil
}

func TestRunningCheckpointReportingFailureIsNotSuccess(t *testing.T) {
	for _, consoleFailure := range []bool{false, true} {
		report := memoryReport(t, io.Discard)
		report.console = &checkpointSaboteur{dir: report.dir, failConsole: consoleFailure, changed: consoleFailure}
		source := &scriptedSource{input: strings.NewReader(
			envelope("1", true, false, false) + envelope("2", true, false, false))}
		if result := receiveShots(source, testLimits(), report); result != reportFailed ||
			!report.Lifecycle.ForcedTeardownRisk || report.Lifecycle.SourceEOF {
			t.Fatalf("checkpoint persistence/console failure hidden: %v %+v", result, report.Lifecycle)
		}
		name := "running"
		if consoleFailure {
			name = "goal"
		}
		snapshot := readRecord(t, report, name)
		if snapshot.Final || snapshot.ExitCode == 0 || snapshot.Outcome == positive.name {
			t.Fatalf("stale summary claimed success: %+v", snapshot)
		}
	}
}
