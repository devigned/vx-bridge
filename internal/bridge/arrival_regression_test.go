package bridge

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type frameNoticeConn struct {
	net.Conn
	started  chan struct{}
	firstEnd chan struct{}
	release  chan struct{}
	endOnce  sync.Once
}

func (c *frameNoticeConn) SetReadDeadline(deadline time.Time) error {
	if deadline.IsZero() && c.firstEnd != nil {
		c.endOnce.Do(func() {
			close(c.firstEnd)
			<-c.release
		})
	}
	if !deadline.IsZero() {
		c.started <- struct{}{}
	}
	return c.Conn.SetReadDeadline(deadline)
}

func nextIOEvent(t *testing.T, e *engine) event {
	t.Helper()
	select {
	case ev := <-e.events:
		return ev
	case <-time.After(time.Second):
		t.Fatal("expected I/O event was not emitted")
		return event{}
	}
}

func TestSourceFrameKeepsFirstObservationAcrossAvailabilityChange(t *testing.T) {
	for _, test := range []struct {
		name      string
		initial   uint64
		next      uint64
		coalesced bool
	}{
		{"unavailable_first_fragment", 0, 7, false},
		{"retired_generation_first_fragment", 7, 8, false},
		{"coalesced_status_does_not_retag_buffered_ball", 0, 7, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			e, sourceConn := stateEngine(t)
			labs := newPeer(sourceConn)
			e.events, e.done, e.src.done = make(chan event), make(chan struct{}), make(chan struct{})
			notice := &frameNoticeConn{Conn: e.src.conn, started: make(chan struct{}, 4)}
			var releaseOnce sync.Once
			release := func() {}
			if test.coalesced {
				notice.firstEnd, notice.release = make(chan struct{}), make(chan struct{})
				release = func() { releaseOnce.Do(func() { close(notice.release) }) }
			}

			e.src.conn = notice
			e.available.Store(test.initial)
			e.nextEligibility = test.initial
			readerDone := make(chan struct{})
			go func() { defer close(readerDone); e.readSource(e.src) }()
			t.Cleanup(func() {
				release()
				close(e.done)
				close(e.src.done)
				notice.Close()
				labs.Close()
				select {
				case <-readerDone:
				case <-time.After(time.Second):
					t.Error("source reader leaked")
				}
			})
			old := shot("1", true, true)
			if test.coalesced {
				labs.send(t, append(shot("0", false, false), old...))
				select {
				case <-notice.firstEnd:
				case <-time.After(time.Second):
					t.Fatal("status read-ahead barrier was not reached")
				}
			} else {
				labs.send(t, old[:len(old)/2])
				select {
				case <-notice.started:
				case <-time.After(time.Second):
					t.Fatal("first fragment was not observed")
				}
			}
			targetConn, targetPeer := net.Pipe()
			defer targetConn.Close()
			defer targetPeer.Close()
			e.dst = &target{conn: targetConn, gen: test.next, cancel: func() {}}
			e.available.Store(test.next)
			e.nextEligibility = test.next
			if test.coalesced {
				release()
				ev := nextIOEvent(t, e)
				e.handle(ev)
				if e.state.Counts.Status != 1 {
					t.Fatal("coalesced status not tracked locally")
				}
			} else {
				labs.send(t, old[len(old)/2:])
			}
			ev := nextIOEvent(t, e)
			if ev.kind != "source_frame" || ev.token != test.initial {
				t.Fatalf("frame lost first-observation ticket: got %d want %d", ev.token, test.initial)
			}
			e.handle(ev)
			if e.wait != nil || e.src.queuedCount != 0 || e.state.Counts.Dropped != 1 {
				t.Fatal("old/ineligible frame was forwarded or success-ACKed")
			}
			fresh := shot("2", true, true)
			labs.send(t, fresh)
			e.handle(nextIOEvent(t, e))
			target := newPeer(targetPeer)
			if !bytes.Equal(target.frame(t), fresh) {
				t.Fatal("newly eligible shot was not the sole forwarded payload")
			}
			e.handle(nextIOEvent(t, e))
			e.handle(event{kind: "target_frame", epoch: e.src.epoch, gen: test.next,
				token: e.wait.token, frame: []byte(acceptance)})
			if e.state.Counts.TargetAccept != 1 || e.src.queuedCount != 1 || (<-e.src.writes).code != 200 {
				t.Fatal("only the newly eligible shot should have an actual accepted receipt")
			}
		})
	}
}

func TestTarget200KeepsFirstObservationAcrossTokenChange(t *testing.T) {
	for _, first := range []uint64{0, 10} {
		t.Run(fmt.Sprint(first), func(t *testing.T) {
			e, labs := stateEngine(t)
			e.events, e.done = make(chan event), make(chan struct{})
			hostConn, targetConn := net.Pipe()
			notice := &frameNoticeConn{Conn: hostConn, started: make(chan struct{}, 2)}
			dst := &target{conn: notice, gen: 7, cancel: func() {}}
			dst.token.Store(first)
			e.dst = dst
			readerDone := make(chan struct{})
			go func() { defer close(readerDone); e.readTarget(e.src.epoch, dst) }()
			t.Cleanup(func() {
				close(e.done)
				notice.Close()
				targetConn.Close()
				select {
				case <-readerDone:
				case <-time.After(time.Second):
					t.Error("target reader leaked")
				}
			})
			target := newPeer(targetConn)
			split := len(acceptance) / 2
			target.send(t, []byte(acceptance[:split]))
			select {
			case <-notice.started:
			case <-time.After(time.Second):
				t.Fatal("old/unsolicited reply prefix was not observed")
			}
			e.wait = &pending{token: 11, alias: 1, written: true}
			dst.token.Store(11)
			target.send(t, []byte(acceptance[split:]))
			ev := nextIOEvent(t, e)
			if ev.kind != "target_frame" || ev.token != first || !ev.changed {
				t.Fatal("fragmented target response was retagged to a later pending write")
			}
			e.handle(ev)
			if e.state.Counts.TargetAccept != 0 || e.src.queuedCount != 0 || e.src.quarantined {
				t.Fatal("old/unsolicited prefix became acceptance or closed the source")
			}
			newPeer(labs).silent(t, time.Millisecond)
		})
	}
}

func TestFrameObservationRemembersIntermediateEligibilityLoss(t *testing.T) {
	current := uint64(7)
	reader := observationReader{Reader: strings.NewReader("abc"), current: func() uint64 { return current }}
	buffer := make([]byte, 1)
	reader.Read(buffer)
	reader.start()
	current = 0
	reader.Read(buffer)
	current = 7
	reader.Read(buffer)
	reader.finish()
	if reader.first != 7 || !reader.changed {
		t.Fatal("frame regained eligibility after an intermediate loss")
	}
}

func TestChangedClubFragmentCannotReleaseHeldBall(t *testing.T) {
	e, labs := stateEngine(t)
	host, targetConn := net.Pipe()
	defer host.Close()
	defer targetConn.Close()
	e.dst = &target{conn: host, gen: 7, cancel: func() {}}
	e.available.Store(7)
	e.nextEligibility = 7
	e.offer(shot("1", true, false), 7)
	if e.held == nil {
		t.Fatal("test ball was not held")
	}
	e.handle(event{kind: "source_frame", epoch: e.src.epoch, token: 7, changed: true, frame: shot("1", false, true)})
	if e.held != nil || e.wait != nil || e.state.Counts.Merged != 0 ||
		e.state.Counts.Dropped != 1 || e.src.queuedCount != 0 || e.src.quarantined {
		t.Fatal("ineligible club fragment released or ACKed held work")
	}
	newPeer(targetConn).silent(t, time.Millisecond)
	newPeer(labs).silent(t, time.Millisecond)
}
