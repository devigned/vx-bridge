package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func testLimits() limits {
	return limits{
		context: context.Background(), deadline: time.Now().Add(2 * time.Second),
		write: 100 * time.Millisecond, budget: receiveBudget,
	}
}

func memoryReport(t *testing.T, console io.Writer) *runReport {
	t.Helper()
	report, err := newReport(t.TempDir(), time.Now(), console)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func exchange(t *testing.T, input string, config limits) (outcome, *runReport, string, string) {
	t.Helper()
	return exchangeUsing(t, input, config, receiveOne)
}

func exchangeUsing(t *testing.T, input string, config limits,
	receive func(*net.TCPListener, limits, *runReport) outcome) (outcome, *runReport, string, string) {
	t.Helper()
	return exchangeSource(t, input, config, receive, true)
}

func exchangeSource(t *testing.T, input string, config limits,
	receive func(*net.TCPListener, limits, *runReport) outcome, sourceEOF bool) (outcome, *runReport, string, string) {
	t.Helper()
	listener, err := listenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	var console bytes.Buffer
	report := memoryReport(t, &console)
	results := make(chan outcome, 1)
	go func() { results <- receive(listener, config, report) }()
	peer, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err := peer.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(peer, input); err != nil {
		t.Fatal(err)
	}
	// Rejected inputs must finish while the source is still write-open.
	if sourceEOF {
		if err := peer.CloseWrite(); err != nil {
			t.Fatal(err)
		}
	}
	response, err := io.ReadAll(peer)
	result := <-results
	if !sourceEOF && (result == positive || report.Lifecycle.SourceEOF) {
		t.Fatal("receiver must reject this stream without source EOF")
	}
	if err != nil && result == positive {
		t.Fatal(err)
	}
	if err != nil {
		t.Logf("negative outcome %s closed the rejected stream: %v", result.name, err)
	}
	return result, report, string(response), console.String()
}

func TestOnlyBallMessagesReceiveExactlyObserved200(t *testing.T) {
	input := envelope("9007199254740992", false, false, false) +
		envelope("9007199254740992", false, true, false) +
		envelope("0", false, false, true) +
		envelope("9007199254740992", true, true, false) +
		envelope("9007199254740992", true, true, false) +
		envelope("9007199254740993", true, false, false)
	result, report, response, _ := exchange(t, input, testLimits())
	if result != positive {
		t.Fatalf("got %v", result)
	}
	if response != strings.Repeat(acknowledgment, 3) {
		t.Fatalf("unexpected response bytes: %q", response)
	}
	if strings.ContainsAny(response, "\r\n") || strings.Contains(response, `"Code":201`) || strings.Contains(response, `"Code":202`) {
		t.Fatal("unapproved response")
	}
	want := counts{
		ReceivedBytes: len(input), Objects: 6, Status: 1, Heartbeat: 1, ClubOnly: 1,
		BallMessages: 3, DuplicateBall: 1, DistinctBall: 2, Acks: 3, DistinctAcked: 2,
		AckBytes: 3 * len(acknowledgment),
	}
	if report.Counts != want {
		t.Fatalf("counts: %+v want %+v", report.Counts, want)
	}
	if report.Events[3].ShotAlias != "ball-1" || report.Events[4].ShotAlias != "ball-1" ||
		!report.Events[4].Duplicate || report.Events[5].ShotAlias != "ball-2" {
		t.Fatal("duplicate alias or exact identifier accounting failed")
	}
}

func TestDistinctFailureOutcomes(t *testing.T) {
	ball := envelope("1", true, true, false)
	two := ball + envelope("2", true, false, false)
	for _, test := range []struct {
		name      string
		input     string
		want      outcome
		acks      int
		sourceEOF bool
	}{
		{"empty_eof", "", earlyEOF, 0, true},
		{"status_eof", envelope("0", false, false, false), earlyEOF, 0, true},
		{"one_shot_eof", ball, earlyEOF, 1, true},
		{"duplicates_only", ball + ball, earlyEOF, 2, true},
		{"malformed", `{"bad":`, invalidFrame, 0, true},
		{"duplicate_keys", `{"a":1,"a":2}`, invalidFrame, 0, false},
		{"shape", `{}`, unexpectedShape, 0, false},
		{"after_two_garbage", two + "x", invalidFrame, 2, false},
		{"after_two_partial", two + `{"`, invalidFrame, 2, true},
		{"after_two_shape", two + `{}`, unexpectedShape, 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, report, response, _ := exchangeSource(t, test.input, testLimits(), receiveOne, test.sourceEOF)
			if result != test.want || response != strings.Repeat(acknowledgment, test.acks) {
				t.Fatalf("outcome=%v counts=%+v response=%q", result, report.Counts, response)
			}
			if !test.sourceEOF && !report.Lifecycle.ForcedTeardownRisk {
				t.Fatal("rejected stream without clean source EOF lost its teardown risk")
			}
		})
	}
	t.Run("budget_without_source_eof", func(t *testing.T) {
		config := testLimits()
		config.budget = 100
		result, report, response, _ := exchangeSource(t, strings.Repeat(" ", 101), config, receiveOne, false)
		if result != budgetExceeded || report.Counts.ReceivedBytes != 100 || response != "" ||
			report.Counts.Acks != 0 || !report.Lifecycle.ForcedTeardownRisk ||
			report.Lifecycle.ShutdownReason != budgetExceeded.name {
			t.Fatalf("budget result=%v counts=%+v lifecycle=%+v response=%q", result, report.Counts, report.Lifecycle, response)
		}
	})
}

func TestNoConnectionDeadlineAndPortRelease(t *testing.T) {
	listener, err := listenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	config := testLimits()
	config.deadline = time.Now().Add(30 * time.Millisecond)
	start := time.Now()
	result := receiveOne(listener, config, memoryReport(t, io.Discard))
	if result != noConnection || time.Since(start) > time.Second {
		t.Fatalf("result=%v duration=%s", result, time.Since(start))
	}
	rebound, err := listenExclusive(address)
	if err != nil {
		t.Fatalf("port was not released: %v", err)
	}
	rebound.Close()
}

func TestExclusiveEphemeralBind(t *testing.T) {
	listener, err := listenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	second, err := listenExclusive(listener.Addr().String())
	if err == nil {
		second.Close()
		t.Fatal("second listener unexpectedly acquired the exclusive address")
	}
	if listener.Addr().(*net.TCPAddr).IP.String() != "127.0.0.1" {
		t.Fatal("listener is not loopback-only")
	}
}

func TestReadAndWriteDeadlines(t *testing.T) {
	for _, test := range []struct {
		name string
		ball bool
		want outcome
	}{
		{"read", false, sessionTimeout},
		{"blocked_ack", true, ackFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, peer := net.Pipe()
			defer peer.Close()
			config := testLimits()
			if deadline, ok := t.Deadline(); ok {
				config.deadline = deadline
			}
			config.write = 30 * time.Millisecond
			report := memoryReport(t, io.Discard)
			results := make(chan outcome, 1)
			source := &phaseDeadlineConn{Conn: server, expireRead: !test.ball, expireWrite: test.ball}
			go func() { results <- receiveShots(source, config, report) }()
			if test.ball {
				if _, err := io.WriteString(peer, envelope("1", true, false, false)); err != nil {
					t.Fatal(err)
				}
			}
			if result := <-results; result != test.want {
				t.Fatalf("result=%v", result)
			}
			if test.ball && (report.Counts.Acks != 0 || report.Counts.AckBytes != 0 ||
				report.Counts.BallMessages != 1 || report.Events[0].Ack != "not_written") {
				t.Fatal("blocked acknowledgment reported as complete")
			}
			if report.Lifecycle.SourceEOF || !report.Lifecycle.ForcedTeardownRisk {
				t.Fatal("deadline without source EOF lost its teardown risk")
			}
		})
	}
}

// Expire a real net.Pipe deadline at the intended I/O phase, not during setup
// merely because another test package or the hosted runner delayed a goroutine.
type phaseDeadlineConn struct {
	net.Conn
	expireRead, expireWrite bool
	expireAfterRead         string
}

func (c *phaseDeadlineConn) Read(p []byte) (int, error) {
	if c.expireRead {
		if err := c.Conn.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
			return 0, err
		}
	}
	n, err := c.Conn.Read(p)
	if c.expireAfterRead != "" && string(p[:n]) == c.expireAfterRead {
		if deadlineErr := c.Conn.SetReadDeadline(time.Now().Add(-time.Second)); deadlineErr != nil {
			return n, deadlineErr
		}
	}
	return n, err
}

func (c *phaseDeadlineConn) SetWriteDeadline(deadline time.Time) error {
	if c.expireWrite {
		deadline = time.Now().Add(-time.Second)
	}
	return c.Conn.SetWriteDeadline(deadline)
}

func TestShotIdleHasNoShortWriteDeadline(t *testing.T) {
	server, peer := net.Pipe()
	defer peer.Close()
	config := testLimits()
	config.write = 15 * time.Millisecond
	report := memoryReport(t, io.Discard)
	results := make(chan outcome, 1)
	go func() { results <- receiveShots(server, config, report) }()
	for _, id := range []string{"1", "2"} {
		if _, err := io.WriteString(peer, envelope(id, true, true, false)); err != nil {
			t.Fatal(err)
		}
		ack := make([]byte, len(acknowledgment))
		if _, err := io.ReadFull(peer, ack); err != nil || string(ack) != acknowledgment {
			t.Fatalf("ack=%q err=%v", ack, err)
		}
		if id == "1" {
			time.Sleep(60 * time.Millisecond)
		}
	}
	if err := peer.Close(); err != nil {
		t.Fatal(err)
	}
	if result := <-results; result != positive {
		t.Fatalf("idle between shots failed: %v", result)
	}
}

func TestPartialObjectAfterGoalIsInconclusive(t *testing.T) {
	server, peer := net.Pipe()
	defer peer.Close()
	report := memoryReport(t, io.Discard)
	results := make(chan outcome, 1)
	config := testLimits()
	if deadline, ok := t.Deadline(); ok {
		config.deadline = deadline
	}
	config.write = time.Until(config.deadline)
	source := &phaseDeadlineConn{Conn: server, expireAfterRead: `{"`}
	go func() { results <- receiveShots(source, config, report) }()
	for _, id := range []string{"1", "2"} {
		if _, err := io.WriteString(peer, envelope(id, true, true, false)); err != nil {
			t.Fatal(err)
		}
		if _, err := io.CopyN(io.Discard, peer, int64(len(acknowledgment))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := io.WriteString(peer, `{"`); err != nil {
		t.Fatal(err)
	}
	if result := <-results; result != sessionTimeout || !report.Lifecycle.GoalReached ||
		report.Lifecycle.SourceEOF || !report.Lifecycle.ForcedTeardownRisk ||
		report.Counts.DistinctAcked != 2 || report.Counts.Acks != 2 || report.Counts.AckBytes != 2*len(acknowledgment) {
		t.Fatalf("got %v", result)
	}
}

func TestInterruptClosesAcceptAndConnectedRead(t *testing.T) {
	t.Run("accept", func(t *testing.T) {
		listener, err := listenExclusive("127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		config := testLimits()
		config.context = ctx
		cancel()
		if result := receiveOne(listener, config, memoryReport(t, io.Discard)); result != interrupted {
			t.Fatalf("got %v", result)
		}
	})
	t.Run("connected", func(t *testing.T) {
		server, peer := net.Pipe()
		defer peer.Close()
		ctx, cancel := context.WithCancel(context.Background())
		config := testLimits()
		config.context = ctx
		results := make(chan outcome, 1)
		go func() { results <- receiveShots(server, config, memoryReport(t, io.Discard)) }()
		cancel()
		if result := <-results; result != interrupted {
			t.Fatalf("got %v", result)
		}
	})
}

type writeConn struct {
	net.Conn
	output bytes.Buffer
	chunk  int
	fail   error
}

func (c *writeConn) SetWriteDeadline(time.Time) error { return nil }
func (c *writeConn) Write(p []byte) (int, error) {
	n := min(len(p), c.chunk)
	c.output.Write(p[:n])
	return n, c.fail
}

func TestAckHandlesPartialAndZeroWrites(t *testing.T) {
	conn := &writeConn{chunk: 3}
	n, err := writeAck(conn, time.Now().Add(time.Second))
	if err != nil || n != len(acknowledgment) || conn.output.String() != acknowledgment {
		t.Fatalf("n=%d err=%v", n, err)
	}
	conn = &writeConn{chunk: 3, fail: io.ErrClosedPipe}
	if n, err := writeAck(conn, time.Now().Add(time.Second)); n != 3 || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("partial failure: n=%d err=%v", n, err)
	}
	conn = &writeConn{chunk: 0}
	if n, err := writeAck(conn, time.Now().Add(time.Second)); n != 0 || !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("zero-progress failure: n=%d err=%v", n, err)
	}
}
