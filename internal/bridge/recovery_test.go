package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type shortWriteConn struct{ net.Conn }

func (c shortWriteConn) Write(data []byte) (int, error) {
	return c.Conn.Write(data[:min(7, len(data))])
}

type noProgressConn struct{ net.Conn }

func (c noProgressConn) Write([]byte) (int, error) { return 0, nil }

func TestWriteAllShortProgressAndStalledPartialDeadline(t *testing.T) {
	frame := shot("1", true, true)
	t.Run("short_success", func(t *testing.T) {
		host, peer := net.Pipe()
		defer host.Close()
		defer peer.Close()
		read := make(chan error, 1)
		go func() {
			got := make([]byte, len(frame))
			_, err := io.ReadFull(peer, got)
			if err == nil && !bytes.Equal(got, frame) {
				err = errors.New("short writes changed bytes")
			}
			read <- err
		}()
		n, err := writeAll(shortWriteConn{host}, frame, time.Second)
		if err != nil || n != len(frame) {
			t.Fatalf("short write progress: %d %v", n, err)
		}
		if err := <-read; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("stalled_after_prefix", func(t *testing.T) {
		host, peer := net.Pipe()
		defer host.Close()
		defer peer.Close()
		result := make(chan event, 1)
		go func() {
			n, err := writeAll(host, frame, 50*time.Millisecond)
			result <- event{n: n, err: err}
		}()
		prefix := make([]byte, 5)
		if _, err := io.ReadFull(peer, prefix); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-result:
			var timeout net.Error
			if result.n != len(prefix) || !bytes.Equal(prefix, frame[:5]) ||
				!errors.As(result.err, &timeout) || !timeout.Timeout() {
				t.Fatalf("stalled write lost partial/deadline outcome: %+v", result)
			}
		case <-time.After(time.Second):
			t.Fatal("write deadline did not release stalled worker")
		}
	})
	t.Run("no_progress", func(t *testing.T) {
		host, peer := net.Pipe()
		defer host.Close()
		defer peer.Close()
		if n, err := writeAll(noProgressConn{host}, frame, time.Second); n != 0 || !errors.Is(err, io.ErrNoProgress) {
			t.Fatalf("zero-progress write: %d %v", n, err)
		}
	})
}

func TestStalledTargetWriterRecoversWithoutSourceCloseOrReplay(t *testing.T) {
	attempted := make(chan struct{}, 4)
	h := newHarness(t, true, func(conn net.Conn) net.Conn {
		return &writeNoticeConn{Conn: conn, attempted: attempted}
	}, nil)
	h.target()
	h.labs.send(t, shot("1", true, true))
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("target writer was not entered")
	}
	state := h.wait(func(s Snapshot) bool { return s.Counts.Uncertain == 1 })
	if !slices.ContainsFunc(state.Recent, func(ev Event) bool {
		return ev.Reason == "target_write_partial_or_uncertain"
	}) {
		t.Fatal("stalled write escaped its write deadline and waited for another timeout")
	}
	if state.Counts.Sent != 0 || state.Counts.TargetAccept != 0 || state.Counts.SourceACK != 0 ||
		state.Counts.Dropped != 1 || state.Source != "attached" {
		t.Fatalf("stalled write invented success or closed the source: %+v", state)
	}
	if h.state(true).Quit != "pending_source_departure" {
		t.Fatal("stalled target blocked guarded Quit")
	}
	h.labs.send(t, shot("0", false, false))
	h.wait(func(s Snapshot) bool { return s.Counts.Status == 1 })
	target := h.target()
	h.labs.send(t, shot("2", true, true))
	if !bytes.Equal(target.frame(t), shot("2", true, true)) {
		t.Fatal("stalled shot replayed after recovery")
	}
	target.send(t, []byte(acceptance))
	h.labs.frame(t)
	h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == 1 })
}

func TestInvalidTargetStreamsRecoverOnlyForFreshShots(t *testing.T) {
	for _, test := range []struct {
		name, response, reason string
		close                  bool
	}{
		{"duplicate_key", `{"Code":200,"Code":200,"Message":"ambiguous"}`, "target_stream_framing", false},
		{"invalid_shape", `{"Code":200,"Message":null}`, "target_response_profile_invalid", false},
		{"invalid_utf8", "{\"Code\":200,\"Message\":\"\xff\"}", "target_stream_framing", false},
		{"truncated_eof", `{"Code":200`, "target_stream_EOF_incomplete_frame", true},
		{"incomplete_frame_deadline", `{"Code":200`, "target_stream_deadline_incomplete_frame", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, true, nil, nil)
			target := h.target()
			h.labs.send(t, shot("1", true, true))
			target.frame(t)
			target.send(t, []byte(test.response))
			if test.close {
				target.Close()
			}
			state := h.wait(func(s Snapshot) bool { return s.Counts.Uncertain == 1 })
			if !slices.ContainsFunc(state.Recent, func(ev Event) bool { return ev.Reason == test.reason }) {
				t.Fatalf("stream was not retired for %s: %+v", test.reason, state.Recent)
			}
			if state.Source != "attached" || state.Counts.TargetAccept != 0 ||
				state.Counts.SourceACK != 0 || state.Counts.Quarantines != 0 || state.Counts.Dropped != 1 {
				t.Fatalf("invalid target stream fabricated success or quarantined source: %+v", state)
			}
			h.labs.send(t, shot("0", false, false))
			h.wait(func(s Snapshot) bool { return s.Counts.Status == 1 })
			target = h.target()
			h.labs.send(t, shot("2", true, true))
			if !bytes.Equal(target.frame(t), shot("2", true, true)) {
				t.Fatal("invalid-stream shot was replayed")
			}
			target.send(t, []byte(acceptance))
			if string(h.labs.frame(t)) != acceptance {
				t.Fatal("fresh applicable acceptance changed")
			}
			h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == 1 })
		})
	}
}

func TestTruncatedSourceDepartureEndsOnlyItsEpoch(t *testing.T) {
	h := newHarness(t, true, nil, nil)
	target := h.target()
	h.labs.send(t, []byte(`{"ShotNumber":1`))
	h.labs.Close()
	state := h.wait(func(s Snapshot) bool { return s.Source == "listening" })
	if state.LastDeparture != "EOF_incomplete_frame" || state.Counts.Quarantines != 0 {
		t.Fatalf("truncated source departure lost EOF/framing evidence: %+v", state)
	}
	if _, err := target.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("old epoch target survived departure: %v", err)
	}
	h.connect()
	h.wait(func(s Snapshot) bool { return s.SourceEpoch == 2 })
	target = h.target()
	h.labs.send(t, shot("1", true, true))
	target.frame(t)
	target.send(t, []byte(acceptance))
	h.labs.frame(t)
	h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == 1 })
}

func TestCanceledDialResultCannotReplaceNewSourceTarget(t *testing.T) {
	e, _ := stateEngine(t)
	e.events, e.done = make(chan event), make(chan struct{})
	defer close(e.done)
	type attempt struct {
		ctx     context.Context
		release chan net.Conn
	}
	attempts := make(chan attempt)
	finished := make(chan struct{}, 2)
	e.config.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		defer func() { finished <- struct{}{} }()
		release := make(chan net.Conn, 1)
		attempts <- attempt{ctx: ctx, release: release}
		// Hold a completion past cancellation to model an already-returning dial.
		return <-release, nil
	}
	e.tick(time.Now())
	old := <-attempts
	e.closeTarget("invented_source_departure")
	select {
	case <-old.ctx.Done():
	default:
		t.Fatal("retired dial context was not canceled")
	}
	e.src.epoch++
	e.tick(e.retryAt)
	current := <-attempts
	currentHost, currentPeer := net.Pipe()
	defer currentPeer.Close()
	defer currentHost.Close()
	current.release <- currentHost
	e.handle(nextIOEvent(t, e))
	ticket, generation := e.available.Load(), e.dst.gen
	oldHost, oldPeer := net.Pipe()
	defer oldPeer.Close()
	defer oldHost.Close()
	old.release <- oldHost
	e.handle(nextIOEvent(t, e))
	if ticket == 0 || e.available.Load() != ticket || e.dst.conn != currentHost ||
		e.dst.gen != generation || e.state.Counts.Stale != 1 {
		t.Fatal("late old-source dial replaced the current target or eligibility")
	}
	oldPeer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := oldPeer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("stale dial socket leaked: %v", err)
	}
	for range 2 {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("dial worker did not return")
		}
	}
}

type trackedTargetConn struct {
	net.Conn
	once   sync.Once
	closed chan struct{}
	active *atomic.Int64
}

func (c *trackedTargetConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.active.Add(-1); close(c.closed) })
	return err
}

func TestBoundedSourceTargetChurnKeepsOneTargetAndNoOldACK(t *testing.T) {
	var active, maximum atomic.Int64
	owned := make(chan *trackedTargetConn, 32)
	h := newHarness(t, true, func(conn net.Conn) net.Conn {
		count := active.Add(1)
		for previous := maximum.Load(); count > previous && !maximum.CompareAndSwap(previous, count); previous = maximum.Load() {
		}
		tracked := &trackedTargetConn{Conn: conn, active: &active, closed: make(chan struct{})}
		owned <- tracked
		return tracked
	}, nil)
	target := h.target()
	for cycle := range 12 {
		h.labs.send(t, shot(fmt.Sprint(cycle+1), true, true))
		target.frame(t)
		target.send(t, []byte(acceptance))
		h.labs.frame(t)
		h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == uint64(cycle+1) })
		if cycle%2 == 0 {
			target.Close()
		} else {
			epoch := h.state(false).SourceEpoch
			h.labs.Close()
			h.wait(func(s Snapshot) bool { return s.Source == "listening" })
			h.connect()
			h.wait(func(s Snapshot) bool { return s.SourceEpoch == epoch+1 })
		}
		target = h.target()
	}
	h.labs.Close()
	state := h.wait(func(s Snapshot) bool { return s.Source == "listening" })
	if maximum.Load() != 1 || active.Load() != 0 || h.fake.connections.Load() != 13 ||
		state.Counts.SourceACK != 12 || state.Counts.TargetAccept != 12 || state.Counts.Sent != 12 ||
		state.Counts.Dropped != 0 || state.Counts.Uncertain != 0 || len(state.Recent) > 64 {
		t.Fatalf("churn exceeded bounds or leaked/replayed work: active=%d max=%d connections=%d state=%+v",
			active.Load(), maximum.Load(), h.fake.connections.Load(), state)
	}
	for range 13 {
		conn := <-owned
		select {
		case <-conn.closed:
		default:
			t.Fatal("retired target socket was not closed")
		}
	}
}

func TestTargetResetRetiresPendingButNotSource(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	host, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	if err := client.(*net.TCPConn).SetLinger(0); err != nil {
		t.Fatal(err)
	}
	client.Close()
	host.SetReadDeadline(time.Now().Add(time.Second))
	_, reset := host.Read(make([]byte, 1))
	if !peerReset(reset) {
		t.Fatalf("private test peer did not produce an identified reset: %v", reset)
	}
	e, _ := stateEngine(t)
	e.dst = &target{gen: 7, cancel: func() {}}
	e.wait = &pending{token: 1, alias: 1}
	e.handle(event{kind: "target_error", epoch: e.src.epoch, gen: 7, err: reset})
	if e.dst != nil || e.wait != nil || e.src.quarantined || e.state.Counts.Uncertain != 1 ||
		e.state.Counts.SourceACK != 0 || !strings.Contains(e.state.Target, "peer_reset") {
		t.Fatalf("target reset escaped its generation: %+v", e.state)
	}
}

type pipeSourceListener struct {
	conn     net.Conn
	accepted bool
	stopped  chan struct{}
	once     sync.Once
}

func (l *pipeSourceListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return l.conn, nil
	}
	<-l.stopped
	return nil, net.ErrClosed
}

func (l *pipeSourceListener) Close() error {
	l.once.Do(func() { close(l.stopped) })
	return nil
}

func (l *pipeSourceListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestBlockedSourceConsumerReplyBurstKeepsControlResponsive(t *testing.T) {
	host, labs := net.Pipe()
	attempted := make(chan struct{}, 1)
	listener := &pipeSourceListener{conn: &writeNoticeConn{Conn: host, attempted: attempted}, stopped: make(chan struct{})}
	h := &harness{t: t, control: NewController(), done: make(chan error, 1), labs: newPeer(labs),
		fake: &fakeTarget{peers: make(chan net.Conn, 1)}}
	h.fake.up.Store(true)
	config := Defaults("invented")
	config.DialContext = h.fake.dial
	go func() { h.done <- Run(listener, config, h.control, nil, io.Discard, "test") }()
	t.Cleanup(func() {
		labs.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := h.control.Query(ctx, true); err != nil {
			// A pending Quit may already have completed after peer departure.
			select {
			case <-h.control.Done:
			default:
				t.Error(err)
			}
		}
		select {
		case err := <-h.done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("blocked source prevented guarded worker cleanup")
		}
	})
	target := h.target()
	target.send(t, []byte(player))
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("source writer never entered its admitted blocked write")
	}
	target.send(t, []byte(strings.Repeat(player, queueCount)))
	state := h.wait(func(s Snapshot) bool { return s.Counts.Quarantines == 1 })
	if state.Counts.ReplyDiscarded != queueCount || state.Counts.Relayed != 0 ||
		state.Counts.SourceACK != 0 || state.Counts.TargetAccept != 0 {
		t.Fatalf("blocked-consumer overflow outcomes changed: %+v", state)
	}
	for range 3 {
		if h.state(true).Quit != "pending_source_departure" {
			t.Fatal("reply overload forced source teardown")
		}
	}
	h.labs.send(t, shot("0", false, false))
}

func TestRetryBoundaryAndCappedBackoffRemainShotIndependent(t *testing.T) {
	e, _ := stateEngine(t)
	e.events, e.done = make(chan event), make(chan struct{})
	defer close(e.done)
	e.backoff = e.config.RetryMin
	e.retryAt = time.Now().Add(time.Hour)
	e.config.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > e.config.Dial {
			return nil, errors.New("missing finite dial budget")
		}
		return nil, syscall.ECONNREFUSED
	}
	for _, next := range []time.Duration{4, 8, 10, 10, 10} {
		e.tick(e.retryAt.Add(-time.Nanosecond))
		if e.dst != nil || e.dialing {
			t.Fatal("retry began before its deadline")
		}
		e.tick(e.retryAt)
		if e.dst == nil || !e.dialing {
			t.Fatal("retry required a new shot or missed its exact deadline")
		}
		ev := nextIOEvent(t, e)
		if !errors.Is(ev.err, syscall.ECONNREFUSED) {
			t.Fatalf("dial budget/refusal: %v", ev.err)
		}
		e.handle(ev)
		if e.backoff != next*time.Second || e.dst != nil || e.dialing || e.src.quarantined {
			t.Fatalf("capped retry policy changed: backoff=%s", e.backoff)
		}
	}

}

func TestPacedWriterCancellationDiscardsAcceptedACKAndStopsWorker(t *testing.T) {
	e, labsConn := stateEngine(t)
	labs := newPeer(labsConn)
	e.events, e.done = make(chan event), make(chan struct{})
	e.config.Pace = time.Hour
	// An unbuffered test lane guarantees the second work item was dequeued
	// before quarantine, so stopping the pacing wait must account for it.
	e.src.writes = make(chan upstreamWrite)
	e.dst = &target{gen: 7, cancel: func() {}}
	writerDone := make(chan struct{})
	go func() { defer close(writerDone); e.writeSource(e.src) }()
	t.Cleanup(func() {
		close(e.done)
		if !e.src.quarantined {
			close(e.src.writeStop)
		}
		e.src.conn.Close()
		labs.Close()
		select {
		case <-writerDone:
		case <-time.After(time.Second):
			t.Error("pacing timer/source writer leaked")
		}
	})
	e.relay([]byte(player), 201)
	e.handle(nextIOEvent(t, e))
	if string(labs.frame(t)) != player {
		t.Fatal("initial admitted response changed")
	}
	e.handle(nextIOEvent(t, e))
	e.wait = &pending{token: 1, alias: 1}
	e.handle(event{kind: "target_write", epoch: e.src.epoch, gen: 7, token: 1})
	e.handle(event{kind: "target_frame", epoch: e.src.epoch, gen: 7, token: 1, frame: []byte(acceptance)})
	e.quarantine("invented_fault_during_pacing")
	ev := nextIOEvent(t, e)
	if ev.kind != "source_write_discarded" || ev.code != 200 {
		t.Fatalf("paced acceptance was admitted or lost: %+v", ev)
	}
	e.handle(ev)
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("canceled pacing wait retained its worker")
	}
	if e.src.queuedCount != 0 || e.src.queuedBytes != 0 || e.state.Counts.TargetAccept != 1 ||
		e.state.Counts.SourceACK != 0 || e.state.Counts.AcceptedACKAbandoned != 1 ||
		e.state.Counts.ReplyDiscarded != 1 || e.state.Counts.Relayed != 1 {
		t.Fatalf("canceled pacing conflated target acceptance and source ACK: %+v", e.state.Counts)
	}
}
