package bridge

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func stateEngine(t *testing.T) (*engine, net.Conn) {
	t.Helper()
	conn, peer := net.Pipe()
	t.Cleanup(func() { conn.Close(); peer.Close() })
	e := &engine{
		config: Defaults("127.0.0.1:1"), logs: make(chan Event, 32),
		seen: make(map[[32]byte]bool), backoff: time.Second,
		src:   &source{conn: conn, epoch: 3, writes: make(chan upstreamWrite, queueCount), writeStop: make(chan struct{})},
		state: Snapshot{Source: "attached"},
	}
	return e, peer
}

func TestFastReceiptWaitsForWriteResultAndCannotOverrideUncertainty(t *testing.T) {
	for _, writeErr := range []error{nil, io.ErrUnexpectedEOF} {
		e, peer := stateEngine(t)
		e.dst = &target{gen: 7, cancel: func() {}}
		e.wait = &pending{token: 11, alias: 1}
		e.handle(event{kind: "target_frame", epoch: 3, gen: 7, token: 11, frame: []byte(acceptance)})
		if e.state.Counts.SourceACK != 0 || e.src.queuedCount != 0 || e.state.Counts.TargetAccept != 0 {
			t.Fatal("fast reply ACKed before full write result")
		}
		e.handle(event{kind: "target_write", epoch: 3, gen: 7, token: 11, err: writeErr})
		if writeErr == nil {
			if e.src.queuedCount != 1 || string((<-e.src.writes).frame) != acceptance {
				t.Fatal("actual receipt was not relayed after full write")
			}
		} else if e.src.queuedCount != 0 || e.state.Counts.Uncertain != 1 || e.src.quarantined {
			t.Fatal("partial write plus fast reply fabricated success or quarantined source")
		}
		peer.SetReadDeadline(time.Now().Add(time.Millisecond))
		if _, err := peer.Read(make([]byte, 1)); err == io.EOF {
			t.Fatal("target write outcome closed source")
		}
	}
}

func TestProductionDefaultsRetain600msHoldAndFiniteIO(t *testing.T) {
	cfg := Defaults("127.0.0.1:999")
	if cfg.Hold != 600*time.Millisecond || cfg.Dial != 2*time.Second || cfg.Write != 2*time.Second ||
		cfg.Receipt != 5*time.Second || cfg.RetryMin != 2*time.Second || cfg.RetryMax != 10*time.Second {
		t.Fatal("documented pilot budgets changed")
	}
}

func TestStaleCallbacksAndExpiredEligibilityCannotReleaseOtherShot(t *testing.T) {
	e, _ := stateEngine(t)
	e.dst = &target{gen: 8, cancel: func() {}}
	e.wait = &pending{token: 12, alias: 2}
	e.handle(event{kind: "target_frame", epoch: 3, gen: 7, token: 11, frame: []byte(acceptance)})
	e.handle(event{kind: "target_write", epoch: 2, gen: 8, token: 12})
	if e.wait == nil || e.wait.token != 12 || e.src.queuedCount != 0 || e.state.Counts.Stale != 2 {
		t.Fatal("old epoch/generation released new pending shot")
	}
	e.handle(event{kind: "target_write", epoch: 3, gen: 8, token: 12})
	e.handle(event{kind: "target_frame", epoch: 3, gen: 8, token: 12, frame: []byte(acceptance)})
	firstTicket := e.available.Load()
	if firstTicket == 0 || e.dst.token.Load() != 0 || e.wait != nil {
		t.Fatal("settled receipt did not reopen future-shot eligibility")
	}
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	e.dst.conn = conn
	e.openEligibility()
	e.offer(shot("123", true, true), firstTicket)
	if e.src.quarantined || e.state.Counts.Dropped != 1 || e.src.queuedCount != 1 {
		t.Fatal("an expired eligibility ticket was reused on the same connection")
	}
}

func TestBoundedLedgerResponsesAndExplicitOverload(t *testing.T) {
	e, _ := stateEngine(t)
	for id := range ledgerSize + 50 {
		e.offer(shot(fmt.Sprint(id), true, true), 0)
	}
	if len(e.seen) != ledgerSize || e.ledgerUsed != ledgerSize || e.state.Counts.Dropped != ledgerSize+50 || len(e.state.Recent) > 64 {
		t.Fatal("ledger/events or drop outcomes were not bounded")
	}
	frame := []byte(strings.Repeat("x", queueBytes/queueCount))
	for range queueCount {
		e.relay(frame, 201)
	}
	if e.src.queuedCount != queueCount || e.src.queuedBytes != queueBytes {
		t.Fatal("queue budgets not measured exactly")
	}
	e.relay([]byte("{}"), 201)
	if !e.src.quarantined || e.state.Counts.Quarantines != 1 || !strings.Contains(e.state.Source, "may_still_be_attached") {
		t.Fatal("overload did not visibly quarantine without source teardown")
	}
}

func TestTargetPlayerFloodQuarantinesWithoutPanicOrSourceClose(t *testing.T) {
	e, peer := stateEngine(t)
	e.dst = &target{gen: 7, cancel: func() {}}
	for range queueCount + 1 {
		e.handle(event{kind: "target_frame", epoch: 3, gen: 7, frame: []byte(player)})
	}
	if !e.src.quarantined || e.src.queuedCount != 0 || e.src.queuedBytes != 0 || e.dst != nil {
		t.Fatal("target response flood bypassed bounded quarantine")
	}
	peer.SetReadDeadline(time.Now().Add(time.Millisecond))
	if _, err := peer.Read(make([]byte, 1)); errors.Is(err, io.EOF) {
		t.Fatal("response overload closed attached source")
	}
}

func TestUnknownSourceErrorRemainsMayAttachedNotEOFOrReset(t *testing.T) {
	e, peer := stateEngine(t)
	e.handle(event{kind: "source_fault", epoch: 3, err: errors.New("injected non-terminal I/O")})
	if e.state.LastDeparture != "" || e.state.LastSourceError != "unknown_io" || !e.src.quarantined {
		t.Fatal("unknown I/O became a successful departure")
	}
	peer.SetReadDeadline(time.Now().Add(time.Millisecond))
	if _, err := peer.Read(make([]byte, 1)); err == io.EOF {
		t.Fatal("unknown I/O closed possibly attached source")
	}
}

func TestFragmentedBothDirectionsAndSessionScopedNumbering(t *testing.T) {
	h := newHarness(t, true, nil, nil)
	target := h.target()
	for _, b := range shot("9007199254740993", true, true) {
		h.labs.send(t, []byte{b})
	}
	target.frame(t)
	for _, b := range []byte(" \n" + acceptance + " " + player) {
		target.send(t, []byte{b})
	}
	if string(h.labs.frame(t)) != acceptance || string(h.labs.frame(t)) != player {
		t.Fatal("fragmented/coalesced reply framing changed responses")
	}
	h.labs.Close()
	h.wait(func(s Snapshot) bool { return s.Source == "listening" })
	h.connect()
	h.wait(func(s Snapshot) bool { return s.SourceEpoch == 2 })
	target = h.target()
	h.labs.send(t, shot("9007199254740993", true, true))
	target.frame(t)
	target.send(t, []byte(acceptance))
	h.labs.frame(t)
	if h.state(false).Counts.Duplicate != 0 {
		t.Fatal("new source session inherited old ID ledger")
	}
}
