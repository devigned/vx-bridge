package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devigned/vx-bridge/internal/destination"
)

func gameShot(id string) []byte {
	return bytes.Replace(shot(id, true, true), []byte(`"Spin":40000`),
		[]byte(`"SpinAxis":-3,"TotalSpin":2400,"HLA":2,"VLA":12`), 1)
}

func selectGame(t *testing.T, h *harness, id destination.ID) Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state, err := h.control.Select(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestInitialDestinationUsesOnlySelectedProfile(t *testing.T) {
	for _, initial := range []destination.ID{destination.GSPro, destination.Standby} {
		t.Run(string(initial), func(t *testing.T) {
			h := configuredHarness(t, true, nil, nil, func(config *Config) {
				config.Destination = initial
			})
			state := h.state(false)
			if state.Destination != initial || state.SourceEpoch != 1 {
				t.Fatal("configured startup choice lost", state)
			}
			if initial == destination.Standby {
				if state.Target != "standby" || h.fake.dials.Load() != 0 {
					t.Fatal("initial standby dialed a destination")
				}
				selectGame(t, h, destination.GSPro)
			}
			target := h.target()
			h.labs.send(t, gameShot("1"))
			if !bytes.Equal(target.frame(t), gameShot("1")) {
				t.Fatal("initial profile changed raw data")
			}
			target.send(t, []byte(acceptance))
			h.labs.frame(t)
			h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == 1 })
			target.Close()
		})
	}
}

func TestGameSwitchSettlesOldReceiptRetainsSourceAndNeverFansOut(t *testing.T) {
	addresses := make(chan string, 8)
	h := configuredHarness(t, true, nil, nil, func(config *Config) {
		dial := config.DialContext
		config.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			addresses <- address
			return dial(ctx, network, address)
		}
	})
	first := h.target()
	source := h.labs
	h.labs.send(t, gameShot("1"))
	if !bytes.Equal(first.frame(t), gameShot("1")) {
		t.Fatal("IT raw data changed")
	}
	h.wait(func(s Snapshot) bool { return s.Counts.Sent == 1 })
	state := selectGame(t, h, destination.GSPro)
	if state.Destination != destination.InfiniteTees || state.Switch != "waiting_current_delivery" {
		t.Fatal("switch bypassed old receipt", state)
	}
	selectGame(t, h, destination.GSPro) // Repeated same choice must not reset its timeout.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := h.control.Select(ctx, destination.Standby); err == nil {
		t.Fatal("a second choice replaced the pending switch")
	}
	h.labs.send(t, gameShot("2"))
	h.wait(func(s Snapshot) bool { return s.Counts.Dropped == 1 })
	first.send(t, []byte(acceptance))
	if string(h.labs.frame(t)) != acceptance {
		t.Fatal("old genuine receipt was not completed before switching")
	}
	second := h.target()
	state = h.wait(func(s Snapshot) bool {
		return s.Destination == destination.GSPro && s.Target == "connected_ready_unknown"
	})
	if h.labs != source || state.SourceEpoch != 1 || state.Counts.SourceACK != 1 {
		t.Fatal("switch replaced source or crossed its ACK barrier", state)
	}
	h.labs.send(t, gameShot("1"))
	h.wait(func(s Snapshot) bool { return s.Counts.Duplicate == 1 })
	second.silent(t, 10*time.Millisecond)
	h.labs.send(t, gameShot("900719925474099312345"))
	if !bytes.Equal(second.frame(t), gameShot("900719925474099312345")) {
		t.Fatal("GSPro normalized numbers, flags or unknown fields")
	}
	second.send(t, []byte(acceptance))
	h.labs.frame(t)
	h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == 2 })
	selectGame(t, h, destination.Standby)
	h.wait(func(s Snapshot) bool { return s.Target == "standby" })
	h.labs.send(t, gameShot("3"))
	h.wait(func(s Snapshot) bool { return s.Counts.Dropped == 2 })
	h.labs.silent(t, 10*time.Millisecond)
	selectGame(t, h, destination.InfiniteTees)
	third := h.target()
	h.labs.send(t, gameShot("4"))
	if !bytes.Equal(third.frame(t), gameShot("4")) {
		t.Fatal("return to IT replayed previous work")
	}
	third.send(t, []byte(acceptance))
	h.labs.frame(t)
	state = h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == 3 })
	if state.SourceEpoch != 1 || state.Ready != "unknown" || h.fake.connections.Load() != 3 {
		t.Fatal("source retention/readiness/single-target invariant changed", state)
	}
	for _, want := range []string{"127.0.0.1:1", "127.0.0.1:921", "127.0.0.1:1"} {
		if got := <-addresses; got != want {
			t.Fatal("wrong selected endpoint", got, want)
		}
	}
	first.Close()
	second.Close()
	third.Close()
}

func TestSwitchDuringPartiallySentShotWaitsForWriteFailureWithoutReplay(t *testing.T) {
	h := newHarness(t, true, nil, nil)
	first := h.target()
	h.labs.send(t, gameShot("1"))
	prefix := make([]byte, 5)
	if err := first.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(first.Conn, prefix); err != nil {
		t.Fatal(err)
	}
	selectGame(t, h, destination.GSPro)
	second := h.target()
	state := h.wait(func(s Snapshot) bool {
		return s.Destination == destination.GSPro && s.Target == "connected_ready_unknown"
	})
	if !bytes.Equal(prefix, gameShot("1")[:5]) || state.Counts.Sent != 0 ||
		state.Counts.Uncertain != 1 || state.Counts.SourceACK != 0 || state.SourceEpoch != 1 {
		t.Fatal("partial old write became full delivery or closed Labs", state)
	}
	second.silent(t, 10*time.Millisecond)
	h.labs.silent(t, 10*time.Millisecond)
	first.Close()
	second.Close()
}

func TestGameSwitchTimeoutDoesNotReplayOrFabricateSuccess(t *testing.T) {
	h := newHarness(t, true, nil, nil)
	first := h.target()
	h.labs.send(t, gameShot("1"))
	first.frame(t)
	selectGame(t, h, destination.GSPro)
	second := h.target()
	state := h.wait(func(s Snapshot) bool {
		return s.Destination == destination.GSPro && s.Target == "connected_ready_unknown"
	})
	if state.Counts.Uncertain != 1 || state.Counts.Dropped != 1 || state.Counts.SourceACK != 0 ||
		state.Counts.TargetAccept != 0 || state.SourceEpoch != 1 {
		t.Fatal("missing old receipt became success or replay", state)
	}
	second.silent(t, 10*time.Millisecond)
	h.labs.silent(t, 10*time.Millisecond)
	h.labs.send(t, shot("2", true, true)) // Missing GSPro-required fields; no synthesized spin.
	h.wait(func(s Snapshot) bool { return s.Counts.Dropped == 2 })
	second.silent(t, 10*time.Millisecond)
	h.labs.send(t, gameShot("3"))
	second.frame(t)
	second.send(t, []byte(`{"Code":503,"Message":"synthetic target failure"}`))
	state = h.wait(func(s Snapshot) bool { return s.Counts.Uncertain == 2 })
	if state.Source != "attached" || state.Counts.SourceACK != 0 || state.Counts.TargetAccept != 0 {
		t.Fatal("GSPro failure closed Labs or fabricated an ACK", state)
	}
	first.Close()
	second.Close()
}

func TestSwitchWaitsForActualAdmittedUpstreamCompletion(t *testing.T) {
	e, sourceConn := stateEngine(t)
	labs := newPeer(sourceConn)
	e.events, e.done = make(chan event), make(chan struct{})
	e.config.Pace = 0
	notice := &writeNoticeConn{Conn: e.src.conn, attempted: make(chan struct{}, 1)}
	e.src.conn = notice
	e.state.Destination = destination.InfiniteTees
	e.dst = &target{gen: 7, cancel: func() {}}
	e.wait = &pending{token: 11, alias: 1}
	e.handle(event{kind: "target_write", epoch: e.src.epoch, gen: 7, token: 11})
	e.handle(event{kind: "target_frame", epoch: e.src.epoch, gen: 7, token: 11, frame: []byte(acceptance)})
	writerDone := make(chan struct{})
	go func() { defer close(writerDone); e.writeSource(e.src) }()
	t.Cleanup(func() {
		close(e.done)
		if !e.src.quarantined {
			close(e.src.writeStop)
		}
		notice.Close()
		labs.Close()
		<-writerDone
	})
	e.handle(nextIOEvent(t, e))
	<-notice.attempted
	if err := e.selectDestination(destination.GSPro); err != nil {
		t.Fatal(err)
	}
	e.closeTarget("synthetic_target_loss")
	e.advanceSwitch()
	if e.state.Destination != destination.InfiniteTees || !e.switching {
		t.Fatal("admitted source JSON was abandoned for a new game")
	}
	if string(labs.frame(t)) != acceptance {
		t.Fatal("old actual acceptance retracted mid-JSON")
	}
	e.handle(nextIOEvent(t, e))
	if e.state.Destination != destination.GSPro || e.switching ||
		e.state.Counts.SourceACK != 1 || e.state.Counts.ReplyCompletedAfterRetire != 1 {
		t.Fatal("switch failed to separate actual old ACK from new destination")
	}
}

func TestSwitchPreservesFragmentAndReadAheadOrigin(t *testing.T) {
	for _, readAhead := range []bool{false, true} {
		t.Run(fmt.Sprint(readAhead), func(t *testing.T) {
			e, sourceConn := stateEngine(t)
			labs := newPeer(sourceConn)
			e.events, e.done, e.src.done = make(chan event), make(chan struct{}), make(chan struct{})
			notice := &frameNoticeConn{Conn: e.src.conn, started: make(chan struct{}, 4)}
			var once sync.Once
			release := func() {}
			if readAhead {
				notice.firstEnd, notice.release = make(chan struct{}), make(chan struct{})
				release = func() { once.Do(func() { close(notice.release) }) }
			}
			e.src.conn = notice
			e.state.Destination = destination.InfiniteTees
			e.nextEligibility = 7
			e.available.Store(7)
			readerDone := make(chan struct{})
			go func() { defer close(readerDone); e.readSource(e.src) }()
			t.Cleanup(func() {
				release()
				close(e.done)
				close(e.src.done)
				notice.Close()
				labs.Close()
				<-readerDone
			})
			old := gameShot("1")
			if readAhead {
				labs.send(t, append(shot("0", false, false), old...))
				<-notice.firstEnd
			} else {
				labs.send(t, old[:len(old)/2])
				<-notice.started
			}
			if err := e.selectDestination(destination.GSPro); err != nil {
				t.Fatal(err)
			}
			conn, peerConn := net.Pipe()
			defer conn.Close()
			defer peerConn.Close()
			e.dst = &target{conn: conn, gen: 20, profile: destination.GSPro, cancel: func() {}}
			e.openEligibility()
			if readAhead {
				release()
				e.handle(nextIOEvent(t, e))
			} else {
				labs.send(t, old[len(old)/2:])
			}
			e.handle(nextIOEvent(t, e))
			if e.wait != nil || e.held != nil || e.src.queuedCount != 0 || e.state.Counts.Dropped != 1 {
				t.Fatal("old buffered/fragmented ball crossed the game switch")
			}
		})
	}
}

func TestSwitchDropsHeldSplitKeepsLedgerAndRejectsLateOldEvents(t *testing.T) {
	e, _ := stateEngine(t)
	e.state.Destination = destination.InfiniteTees
	e.available.Store(7)
	e.nextEligibility = 7
	conn, peerConn := net.Pipe()
	defer conn.Close()
	defer peerConn.Close()
	e.dst = &target{conn: conn, gen: 19, cancel: func() {}}
	e.offer(shot("1", true, false), 7)
	if err := e.selectDestination(destination.GSPro); err != nil {
		t.Fatal(err)
	}
	e.offer(shot("1", false, true), 7)
	e.offer(gameShot("1"), 7)
	e.dst = &target{gen: 20, profile: destination.GSPro, cancel: func() {}}
	e.wait = &pending{token: 22, alias: 2}
	e.handle(event{kind: "target_frame", epoch: e.src.epoch, gen: 19, token: 22, frame: []byte(acceptance)})
	e.handle(event{kind: "target_write", epoch: e.src.epoch, gen: 19, token: 22})
	late, latePeer := net.Pipe()
	defer latePeer.Close()
	e.handle(event{kind: "dial", epoch: e.src.epoch, gen: 19, conn: late})
	if e.held != nil || e.wait.token != 22 || e.src.queuedCount != 0 ||
		e.state.Counts.Duplicate != 1 || e.state.Counts.LateClub != 1 ||
		e.state.Counts.Dropped != 1 || e.state.Counts.Stale != 3 {
		t.Fatal("split/duplicate/late old work escaped destination retirement")
	}
	if err := e.selectDestination("unknown"); err == nil {
		t.Fatal("unknown selection accepted")
	}
	e.quitting = true
	if err := e.selectDestination(destination.Standby); err == nil {
		t.Fatal("selection bypassed pending guarded Quit")
	}
}

func TestPendingSwitchDiscardsNewOldGameStatusAndStandbyNeverDials(t *testing.T) {
	e, _ := stateEngine(t)
	e.state.Destination = destination.InfiniteTees
	e.dst = &target{gen: 7, cancel: func() {}}
	e.wait = &pending{token: 11, alias: 1, until: time.Now().Add(time.Second)}
	if err := e.selectDestination(destination.Standby); err != nil {
		t.Fatal(err)
	}
	e.handle(event{kind: "target_frame", epoch: e.src.epoch, gen: 7, frame: []byte(player)})
	if e.src.queuedCount != 0 || e.state.Counts.ReplyDiscarded != 1 {
		t.Fatal("old player stream could indefinitely delay the switch")
	}
	e.config.DialContext = func(context.Context, string, string) (net.Conn, error) {
		t.Error("standby dialed a target")
		return nil, errors.New("unexpected_dial")
	}
	e.tick(time.Now().Add(2 * time.Second))
	e.tick(time.Now().Add(time.Hour))
	if e.state.Destination != destination.Standby || e.state.Target != "standby" ||
		e.state.Counts.Uncertain != 1 || e.available.Load() != 0 || e.dst != nil {
		t.Fatal("standby did not retain source and close only the destination")
	}
	if strings.Contains(fmt.Sprint(e.state.Recent), `"Handed"`) {
		t.Fatal("discarded game context leaked into diagnostics")
	}
}
