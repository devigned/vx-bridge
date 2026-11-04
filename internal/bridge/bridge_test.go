package bridge

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/devigned/vx-bridge/internal/identity"
	"github.com/devigned/vx-bridge/internal/protocol"
)

// All payloads and peers are invented; no installed simulator or fixed port.
func shot(id string, ball, club bool) []byte {
	b, c := "null", "null"
	if ball {
		b = `{"Speed":111.125000000000001,"Spin":40000,"Unknown":-0.00e+2}`
	}
	if club {
		c = `{"Speed":88.50000000000001,"Extra":9007199254740993123456789}`
	}
	return fmt.Appendf(nil, `{"DeviceID":"private-test-device","Units":"Yards","ShotNumber":%s,"APIversion":"1","ClubIndex":7,"Unknown":{"text":"brace } quote \" 雪","n":1.23000000000001e+2},"BallData":%s,"ClubData":%s,"ShotDataOptions":{"ContainsBallData":%t,"ContainsClubData":%t,"LaunchMonitorIsReady":false,"LaunchMonitorBallDetected":false,"IsHeartBeat":false}}`, id, b, c, ball, club)
}

const acceptance = `{"Code":200,"Message":"actual target acceptance"}`
const player = `{"Code":201,"Message":"actual partial player","Player":{"Handed":"RH","Club":"7I"}}`

type fakeTarget struct {
	up          atomic.Bool
	dials       atomic.Uint64
	connections atomic.Uint64
	peers       chan net.Conn
	wrap        func(net.Conn) net.Conn
}

func (f *fakeTarget) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	f.dials.Add(1)
	if !f.up.Load() {
		return nil, syscall.ECONNREFUSED
	}
	host, peer := net.Pipe()
	if f.wrap != nil {
		host = f.wrap(host)
	}
	select {
	case f.peers <- peer:
		f.connections.Add(1)
		return host, nil
	case <-ctx.Done():
		host.Close()
		peer.Close()
		return nil, ctx.Err()
	}
}

type peer struct {
	net.Conn
	stream protocol.Stream
}

func newPeer(conn net.Conn) *peer {
	return &peer{Conn: conn, stream: protocol.Stream{Reader: bufio.NewReader(conn)}}
}

func (p *peer) send(t *testing.T, data []byte) {
	t.Helper()
	p.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := p.Write(data); err != nil {
		t.Fatal(err)
	}
}

func (p *peer) frame(t *testing.T) []byte {
	t.Helper()
	p.SetReadDeadline(time.Now().Add(time.Second))
	data, err := p.stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (p *peer) silent(t *testing.T, duration time.Duration) {
	t.Helper()
	p.SetReadDeadline(time.Now().Add(duration))
	_, err := p.stream.Next()
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("expected live/silent stream, got %v", err)
	}
	p.SetReadDeadline(time.Time{})
}

type harness struct {
	t       *testing.T
	control *Controller
	done    chan error
	labs    *peer
	fake    *fakeTarget
	address string
	faults  chan string
}

func newHarness(t *testing.T, available bool, wrap func(net.Conn) net.Conn, sink io.Writer) *harness {
	return configuredHarness(t, available, wrap, sink, nil)
}

func configuredHarness(t *testing.T, available bool, wrap func(net.Conn) net.Conn, sink io.Writer, configure func(*Config)) *harness {
	t.Helper()
	listener, err := identity.ListenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, control: NewController(), done: make(chan error, 1),
		fake:    &fakeTarget{peers: make(chan net.Conn, 8), wrap: wrap},
		address: listener.Addr().String(), faults: make(chan string, 8)}
	h.fake.up.Store(available)
	config := Defaults("127.0.0.1:1")
	config.DialContext = h.fake.dial
	config.Hold, config.Receipt, config.Write = 80*time.Millisecond, 350*time.Millisecond, 150*time.Millisecond
	config.RetryMin, config.RetryMax, config.Pace, config.Frame = 30*time.Millisecond, 60*time.Millisecond, time.Millisecond, 200*time.Millisecond
	if configure != nil {
		configure(&config)
	}
	if sink == nil {
		sink = io.Discard
	}
	go func() { h.done <- Run(listener, config, h.control, h.faults, sink, "test-build") }()
	h.connect()
	h.wait(func(s Snapshot) bool { return s.Source == "attached" })
	t.Cleanup(func() {
		if h.labs != nil {
			h.labs.Close()
		}
		select {
		case <-h.control.Done:
		default:
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			h.control.Query(ctx, true)
			cancel()
		}
		select {
		case <-h.done:
		case <-time.After(2 * time.Second):
			t.Error("guarded engine leaked")
		}
		for {
			select {
			case conn := <-h.fake.peers:
				conn.Close()
			default:
				return
			}
		}
	})
	return h
}

func (h *harness) connect() {
	h.t.Helper()
	conn, err := net.Dial("tcp4", h.address)
	if err != nil {
		h.t.Fatal(err)
	}
	h.labs = newPeer(conn)
}

func (h *harness) state(quit bool) Snapshot {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state, err := h.control.Query(ctx, quit)
	if err != nil {
		h.t.Fatal(err)
	}
	return state
}

func (h *harness) wait(predicate func(Snapshot) bool) Snapshot {
	h.t.Helper()
	until := time.Now().Add(2 * time.Second)
	for {
		state := h.state(false)
		if predicate(state) {
			return state
		}
		if time.Now().After(until) {
			h.t.Fatalf("state condition missed: %+v", state)
		}
		time.Sleep(3 * time.Millisecond)
	}
}

func (h *harness) target() *peer {
	h.t.Helper()
	select {
	case conn := <-h.fake.peers:
		p := newPeer(conn)
		h.t.Cleanup(func() { p.Close() })
		h.wait(func(s Snapshot) bool { return s.Target == "connected_ready_unknown" })
		return p
	case <-time.After(time.Second):
		h.t.Fatal("target did not retry without a physical shot")
		return nil
	}
}

func TestMergedShotAndActualReceiptBeforeSourceACK(t *testing.T) {
	h := newHarness(t, true, nil, nil)
	target := h.target()
	ball, club := shot("9007199254740993123456789", true, false), shot("9007199254740993123456789", false, true)
	h.labs.send(t, ball)
	h.labs.send(t, shot("0", false, false))
	target.silent(t, 20*time.Millisecond)
	h.labs.send(t, club)
	actual := target.frame(t)
	want, err := protocol.Merge(ball, club)
	if err != nil || !bytes.Equal(actual, want) {
		t.Fatal("target did not get one exact raw-preserving merged shot")
	}
	// Target lane is JSON plus newline, source replies remain bare JSON.
	newline, err := target.stream.Reader.ReadByte()
	if err != nil || newline != '\n' {
		t.Fatal("target lane omitted newline")
	}
	h.labs.silent(t, 25*time.Millisecond)
	target.send(t, []byte(" \n"+acceptance+player))
	if got := string(h.labs.frame(t)); got != acceptance {
		t.Fatal("200 shape/message changed")
	}
	if got := string(h.labs.frame(t)); got != player {
		t.Fatal("partial Player shape or real message changed")
	}
	state := h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == 1 })
	if state.Counts.Merged != 1 || state.Counts.Sent != 1 || state.Counts.TargetAccept != 1 || state.Ready != "unknown" {
		t.Fatalf("bad counts/readiness: %+v", state)
	}
	h.labs.send(t, club)
	h.labs.send(t, ball)
	h.wait(func(s Snapshot) bool { return s.Counts.Duplicate == 1 && s.Counts.LateClub == 1 })
	target.silent(t, 30*time.Millisecond)
	h.labs.silent(t, 20*time.Millisecond)
	if strings.Contains(fmt.Sprint(h.state(false)), "private-test-device") || strings.Contains(fmt.Sprint(h.state(false)), "9007199254740993123456789") {
		t.Fatal("status disclosed raw identifiers")
	}
}

func TestUnavailableShotNeverResurrectsAndIdlePersists(t *testing.T) {
	h := newHarness(t, false, nil, nil)
	h.labs.send(t, shot("1", true, false))
	h.wait(func(s Snapshot) bool { return s.Counts.Dropped == 1 })
	h.fake.up.Store(true)
	target := h.target()
	target.silent(t, 100*time.Millisecond)
	h.labs.silent(t, 20*time.Millisecond)
	h.labs.send(t, shot("2", true, true))
	target.frame(t)
	target.send(t, []byte(acceptance+player))
	h.labs.frame(t)
	h.labs.frame(t)
	// Longer than frame/receipt timeouts; a healthy idle socket has no deadline.
	time.Sleep(400 * time.Millisecond)
	state := h.state(false)
	if state.Source != "attached" || state.Target != "connected_ready_unknown" || state.Counts.SourceACK != 1 {
		t.Fatalf("outage/idle closed source or invented ACK: %+v", state)
	}
}

func TestHoldInvalidationAndReceiptTimeoutNeverReplay(t *testing.T) {
	h := newHarness(t, true, nil, nil)
	old := h.target()
	h.labs.send(t, shot("1", true, false))
	h.wait(func(s Snapshot) bool { return s.Counts.Balls == 1 })
	old.Close()
	next := h.target()
	next.silent(t, 100*time.Millisecond)
	h.labs.send(t, shot("1", false, true))
	h.labs.send(t, shot("2", true, true))
	next.frame(t)
	h.wait(func(s Snapshot) bool { return s.Counts.Uncertain == 1 })
	restored := h.target()
	restored.silent(t, 50*time.Millisecond)
	h.labs.silent(t, 20*time.Millisecond)
	h.labs.send(t, shot("3", true, true))
	restored.frame(t)
	restored.send(t, []byte(acceptance))
	h.labs.frame(t)
	state := h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == 1 })
	if state.Source != "attached" || state.Counts.Dropped != 2 || state.Counts.LateClub != 1 {
		t.Fatalf("old held/uncertain work replayed: %+v", state)
	}
}

func TestNewBallReleasesOldInOrderAndBusyLaneDrops(t *testing.T) {
	h := newHarness(t, true, nil, nil)
	target := h.target()
	h.labs.send(t, shot("1", true, false))
	h.labs.send(t, shot("2", true, true))
	if !bytes.Equal(target.frame(t), shot("1", true, false)) {
		t.Fatal("new ball reordered old hold")
	}
	h.wait(func(s Snapshot) bool { return s.Counts.Dropped == 1 })
	target.send(t, []byte(acceptance+player))
	h.labs.frame(t)
	h.labs.frame(t)
	h.labs.send(t, shot("3", true, true))
	if !bytes.Equal(target.frame(t), shot("3", true, true)) {
		t.Fatal("identical measurements of distinct IDs were deduplicated")
	}
	target.send(t, []byte(acceptance))
	h.labs.frame(t)
}

type partialWrite struct{ net.Conn }

func (p partialWrite) Write(frame []byte) (int, error) {
	n, _ := p.Conn.Write(frame[:5])
	return n, io.ErrUnexpectedEOF
}

func TestPartialTargetWriteClosesOnlyTargetAndDoesNotACK(t *testing.T) {
	h := newHarness(t, true, func(c net.Conn) net.Conn { return partialWrite{c} }, nil)
	target := h.target()
	h.labs.send(t, shot("1", true, true))
	target.SetReadDeadline(time.Now().Add(time.Second))
	raw, err := io.ReadAll(target)
	if err != nil || len(raw) != 5 {
		t.Fatalf("partial stream was not invalidated: %d %v", len(raw), err)
	}
	h.wait(func(s Snapshot) bool { return s.Counts.Uncertain == 1 })
	h.labs.silent(t, 20*time.Millisecond)
	h.labs.send(t, shot("0", false, false))
	state := h.wait(func(s Snapshot) bool { return s.Counts.Status == 1 })
	if state.Source != "attached" || state.Counts.SourceACK != 0 {
		t.Fatal("target failure closed/ACKed source")
	}
}

func TestSourceDepartureKeepsListenerAndGuardedQuitWaits(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprint(reset), func(t *testing.T) {
			h := newHarness(t, true, nil, nil)
			target := h.target()
			firstEpoch := h.state(false).SourceEpoch
			if reset {
				h.labs.Conn.(*net.TCPConn).SetLinger(0)
			}
			h.labs.Close()
			state := h.wait(func(s Snapshot) bool { return s.Source == "listening" })
			if (reset && !strings.HasPrefix(state.LastDeparture, "peer_reset")) || (!reset && state.LastDeparture != "EOF") {
				t.Fatalf("departure misclassified: %+v", state)
			}
			target.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := target.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
				t.Fatalf("source loss did not close target: %v", err)
			}
			h.connect()
			h.wait(func(s Snapshot) bool { return s.SourceEpoch > firstEpoch })
			target = h.target()
			h.labs.send(t, shot("1", true, true))
			target.frame(t)
			target.send(t, []byte(acceptance))
			h.labs.frame(t)
			for range 3 {
				state = h.state(true)
				if state.Quit != "pending_source_departure" {
					t.Fatal("repeated Quit forced attached source")
				}
			}
			h.labs.silent(t, 40*time.Millisecond)
			select {
			case <-h.control.Done:
				t.Fatal("quit ended identity lifetime while source attached")
			default:
			}
		})
	}
}

func TestSecondSourceRejectedAndMalformedQuarantineStaysAttached(t *testing.T) {
	h := newHarness(t, true, nil, nil)
	h.target()
	second, err := net.Dial("tcp4", h.address)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := second.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatal("second source not rejected")
	}
	h.labs.send(t, []byte(`{"duplicate":1,"duplicate":2}`))
	h.wait(func(s Snapshot) bool { return s.Counts.Quarantines == 1 })
	state := h.state(true)
	if state.Quit != "pending_source_departure" || state.Counts.RejectedSource != 1 {
		t.Fatal("invalid source input triggered unsafe exit")
	}
	h.labs.silent(t, 20*time.Millisecond)
	h.labs.send(t, []byte("discarded junk after quarantining"))
	h.labs.silent(t, 20*time.Millisecond)
}

func TestUnsolicitedAndStaleGenerationCannotACK(t *testing.T) {
	h := newHarness(t, true, nil, nil)
	old := h.target()
	old.send(t, []byte(acceptance))
	h.wait(func(s Snapshot) bool { return s.Counts.Stale >= 1 })
	target := h.target()
	h.labs.send(t, shot("1", true, true))
	target.frame(t)
	h.labs.silent(t, 20*time.Millisecond)
	target.send(t, []byte(acceptance))
	h.labs.frame(t)
	target.send(t, []byte(acceptance))
	state := h.wait(func(s Snapshot) bool { return s.Counts.Stale >= 2 && s.Counts.SourceACK == 1 })
	if state.Counts.SourceACK != 1 {
		t.Fatal("unsolicited/duplicate acceptance ACKed another shot")
	}
}

type brokenSink struct{}

func (brokenSink) Write([]byte) (int, error) { return 0, errors.New("injected_sink_failure") }

func TestDiagnosticsFailureAndPlatformFaultNeverCloseAttachedSource(t *testing.T) {
	h := newHarness(t, true, nil, brokenSink{})
	h.target()
	h.wait(func(s Snapshot) bool { return s.Diagnostics == "console_failed_use_status" })
	h.labs.send(t, shot("0", false, false))
	h.wait(func(s Snapshot) bool { return s.Counts.Status == 1 })
	h.faults <- "identity_message_loop_failed"
	h.wait(func(s Snapshot) bool { return s.Identity == "fault" })
	h.labs.silent(t, 20*time.Millisecond)
	if h.state(false).Quit != "pending_source_departure" {
		t.Fatal("platform failure did not guard identity lifetime")
	}
}
