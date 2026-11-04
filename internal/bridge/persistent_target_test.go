package bridge

import (
	"net"
	"sync"
	"testing"
	"time"
)

func assertOneTargetConnection(t *testing.T, h *harness, shots uint64) {
	t.Helper()
	state := h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == shots })
	if h.fake.dials.Load() != 1 || h.fake.connections.Load() != 1 ||
		state.TargetGeneration != 1 || state.Counts.Sent != shots ||
		state.Counts.TargetAccept != shots || state.Counts.Dropped != 0 ||
		state.Source != "attached" || state.Target != "connected_ready_unknown" || state.Ready != "unknown" {
		t.Fatalf("healthy shots did not use one uninterrupted target connection: dials=%d connections=%d state=%+v",
			h.fake.dials.Load(), h.fake.connections.Load(), state)
	}
}

func TestPersistentTargetAcceptsImmediateNewShotsWithoutPlayerReplies(t *testing.T) {
	h := newHarness(t, true, nil, nil)
	target := h.target()
	for _, id := range []string{"1", "2", "3"} {
		want := shot(id, true, true)
		h.labs.send(t, want)
		if string(target.frame(t)) != string(want) {
			t.Fatal("same target connection did not receive the next new shot")
		}
		target.send(t, []byte(acceptance))
		if string(h.labs.frame(t)) != acceptance {
			t.Fatal("actual target acceptance changed")
		}
	}
	assertOneTargetConnection(t, h, 3)
}

func TestDelayed201DuringNextPendingShotDoesNotSettleOrDeclareReady(t *testing.T) {
	h := newHarness(t, true, nil, nil)
	target := h.target()
	h.labs.send(t, shot("1", true, true))
	target.frame(t)
	target.send(t, []byte(acceptance))
	h.labs.frame(t)
	h.labs.send(t, shot("2", true, true))
	target.frame(t)
	target.send(t, []byte(player))
	if string(h.labs.frame(t)) != player {
		t.Fatal("delayed real player reply was changed or lost")
	}
	state := h.state(false)
	if state.Counts.TargetAccept != 1 || state.Counts.SourceACK != 1 || state.Ready != "unknown" {
		t.Fatal("player information was mistaken for a shot receipt or readiness")
	}
	h.labs.silent(t, 10*time.Millisecond)
	target.send(t, []byte(acceptance))
	h.labs.frame(t)
	assertOneTargetConnection(t, h, 2)
}

type heldWriteResult struct {
	net.Conn
	once    sync.Once
	release <-chan struct{}
}

func (c *heldWriteResult) Write(data []byte) (int, error) {
	n, err := c.Conn.Write(data)
	c.once.Do(func() { <-c.release })
	return n, err
}

func TestFastReceiptWaitsForWriteResultAndBusyShotNeverReplays(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	h := newHarness(t, true, func(conn net.Conn) net.Conn {
		return &heldWriteResult{Conn: conn, release: release}
	}, nil)
	target := h.target()
	h.labs.send(t, shot("1", true, true))
	target.frame(t)
	target.send(t, []byte(acceptance))
	h.labs.silent(t, 10*time.Millisecond)
	h.labs.send(t, shot("2", true, false))
	h.wait(func(s Snapshot) bool { return s.Counts.Dropped == 1 })
	if state := h.state(false); state.Counts.TargetAccept != 0 || state.Counts.SourceACK != 0 {
		t.Fatal("fast reply was promoted before the complete write result")
	}
	target.silent(t, 10*time.Millisecond)
	unblock()
	h.labs.frame(t)
	h.labs.send(t, shot("3", true, true))
	if string(target.frame(t)) != string(shot("3", true, true)) {
		t.Fatal("busy shot replayed instead of forwarding only the new eligible shot")
	}
	target.send(t, []byte(acceptance))
	h.labs.frame(t)
	state := h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == 2 })
	if h.fake.dials.Load() != 1 || h.fake.connections.Load() != 1 || state.Counts.Dropped != 1 ||
		state.Counts.Sent != 2 || state.Source != "attached" {
		t.Fatal("fast-reply/busy handling reconnected, replayed or closed the source")
	}
}

func TestIdle200PrefixCannotAcquireFreshPendingTokenOrCloseSource(t *testing.T) {
	started := make(chan struct{}, 4)
	h := newHarness(t, true, func(conn net.Conn) net.Conn {
		return &frameNoticeConn{Conn: conn, started: started}
	}, nil)
	target := h.target()
	split := len(acceptance) / 2
	target.send(t, []byte(acceptance[:split]))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("unsolicited target prefix was not observed while idle")
	}
	h.labs.send(t, shot("1", true, true))
	target.frame(t)
	target.send(t, []byte(acceptance[split:]))
	h.wait(func(s Snapshot) bool { return s.Counts.Uncertain == 1 })
	h.labs.silent(t, 10*time.Millisecond)
	h.labs.send(t, shot("0", false, false))
	h.wait(func(s Snapshot) bool { return s.Counts.Status == 1 })
	restored := h.target()
	restored.silent(t, 10*time.Millisecond)
	h.labs.send(t, shot("2", true, true))
	if string(restored.frame(t)) != string(shot("2", true, true)) {
		t.Fatal("uncertain old shot was replayed after recovery")
	}
	restored.send(t, []byte(acceptance))
	h.labs.frame(t)
	state := h.wait(func(s Snapshot) bool { return s.Counts.SourceACK == 1 })
	if state.Counts.TargetAccept != 1 || state.Counts.Dropped != 1 || state.Source != "attached" ||
		h.fake.dials.Load() != 2 || h.fake.connections.Load() != 2 {
		t.Fatal("stale 200 fabricated success, replayed or closed the source")
	}
}
