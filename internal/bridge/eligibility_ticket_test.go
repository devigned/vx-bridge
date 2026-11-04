package bridge

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

func TestSameConnectionReopensWithNewTicketWithoutRevalidatingOldBytes(t *testing.T) {
	for _, readAhead := range []bool{false, true} {
		t.Run(fmt.Sprint(readAhead), func(t *testing.T) {
			e, sourceConn := stateEngine(t)
			labs := newPeer(sourceConn)
			e.events, e.done, e.src.done = make(chan event), make(chan struct{}), make(chan struct{})
			notice := &frameNoticeConn{Conn: e.src.conn, started: make(chan struct{}, 4)}
			var releaseOnce sync.Once
			release := func() {}
			if readAhead {
				notice.firstEnd, notice.release = make(chan struct{}), make(chan struct{})
				release = func() { releaseOnce.Do(func() { close(notice.release) }) }
			}
			e.src.conn = notice
			e.nextEligibility = 7
			e.available.Store(7)
			targetConn, targetPeer := net.Pipe()
			defer targetConn.Close()
			defer targetPeer.Close()
			e.dst = &target{conn: targetConn, gen: 19, cancel: func() {}}
			first := shot("1", true, readAhead)
			e.offer(first, 7)
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

			old := shot("2", true, true)
			if readAhead {
				// Entire status+ball batch is observed while the first receipt
				// is pending; pause before the buffered ball starts parsing.
				labs.send(t, append(shot("0", false, false), old...))
				select {
				case <-notice.firstEnd:
				case <-time.After(time.Second):
					t.Fatal("busy read-ahead barrier was not reached")
				}
			} else {
				// Prefix is observed under ticket 7, then no further read
				// completes until after the held first shot settles.
				labs.send(t, old[:len(old)/2])
				select {
				case <-notice.started:
				case <-time.After(time.Second):
					t.Fatal("eligible prefix was not observed")
				}
				e.tick(e.held.until)
			}

			target := newPeer(targetPeer)
			if string(target.frame(t)) != string(first) {
				t.Fatal("first eligible shot changed")
			}
			e.handle(nextIOEvent(t, e))
			if e.available.Load() != 0 || e.wait == nil {
				t.Fatal("pending receipt did not close the eligibility gate")
			}
			e.handle(event{kind: "target_frame", epoch: e.src.epoch, gen: 19,
				token: e.wait.token, frame: []byte(acceptance)})
			if e.dst == nil || e.dst.conn != targetConn || e.dst.gen != 19 || e.available.Load() != 8 {
				t.Fatal("receipt did not reopen a new ticket on the same connection")
			}

			if readAhead {
				release()
				e.handle(nextIOEvent(t, e))
				if e.state.Counts.Status != 1 {
					t.Fatal("status was not tracked locally")
				}
			} else {
				labs.send(t, old[len(old)/2:])
			}
			ev := nextIOEvent(t, e)
			wantTicket := uint64(7)
			if readAhead {
				wantTicket = 0
			}
			if ev.kind != "source_frame" || ev.token != wantTicket || !ev.changed {
				t.Fatal("old bytes acquired the reopened connection's eligibility")
			}
			e.handle(ev)
			if e.wait != nil || e.held != nil || e.state.Counts.Dropped != 1 || e.src.queuedCount != 1 {
				t.Fatal("old fragmented/buffered ball was forwarded or success-ACKed")
			}

			fresh := shot("3", true, true)
			labs.send(t, fresh)
			e.handle(nextIOEvent(t, e))
			if string(target.frame(t)) != string(fresh) {
				t.Fatal("only the newly eligible next shot should forward")
			}
			e.handle(nextIOEvent(t, e))
			e.handle(event{kind: "target_frame", epoch: e.src.epoch, gen: 19,
				token: e.wait.token, frame: []byte(acceptance)})
			if e.dst == nil || e.dst.gen != 19 || e.available.Load() != 9 ||
				e.state.Counts.TargetAccept != 2 || e.state.Counts.Sent != 2 || e.src.queuedCount != 2 {
				t.Fatal("persistent receipt settlement or exact shot count changed")
			}
			target.silent(t, time.Millisecond)
			labs.silent(t, time.Millisecond)
		})
	}
}
