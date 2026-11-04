package bridge

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

type writeNoticeConn struct {
	net.Conn
	attempted chan struct{}
}

func (c *writeNoticeConn) Write(data []byte) (int, error) {
	c.attempted <- struct{}{}
	return c.Conn.Write(data)
}

func TestRetiredQueuedRepliesDiscardedWithoutUndoingTargetAcceptance(t *testing.T) {
	for _, reconnect := range []bool{false, true} {
		t.Run(fmt.Sprint(reconnect), func(t *testing.T) {
			e, sourceConn := stateEngine(t)
			labs := newPeer(sourceConn)
			e.events, e.done = make(chan event), make(chan struct{})
			e.config.Pace = 0
			notice := &writeNoticeConn{Conn: e.src.conn, attempted: make(chan struct{}, 4)}
			e.src.conn = notice
			e.dst = &target{gen: 7, cancel: func() {}}
			writerDone := make(chan struct{})
			go func() { defer close(writerDone); e.writeSource(e.src) }()
			t.Cleanup(func() {
				close(e.done)
				if !e.src.quarantined {
					close(e.src.writeStop)
				}
				notice.Close()
				labs.Close()
				select {
				case <-writerDone:
				case <-time.After(time.Second):
					t.Error("source writer leaked")
				}
			})

			// This already-admitted frame blocks the sole writer in net.Pipe.
			initial := `{"Code":201,"Message":"already admitted","Player":{"Club":"initial"}}`
			e.relay([]byte(initial), 201)
			ev := nextIOEvent(t, e)
			if ev.kind != "source_write_ready" || ev.gen != 7 || ev.epoch != e.src.epoch {
				t.Fatal("upstream work lacks origin/admission metadata")
			}
			e.handle(ev)
			select {
			case <-notice.attempted:
			case <-time.After(time.Second):
				t.Fatal("initial write did not enter its bounded in-flight phase")
			}

			stale := `{"Code":201,"Message":"queued old context","Player":{"Club":"must-not-arrive"}}`
			e.relay([]byte(stale), 201)
			e.wait = &pending{token: 11, alias: 1}
			e.handle(event{kind: "target_write", epoch: e.src.epoch, gen: 7, token: 11})
			e.handle(event{kind: "target_frame", epoch: e.src.epoch, gen: 7, token: 11, frame: []byte(acceptance)})
			if e.state.Counts.TargetAccept != 1 || e.src.queuedCount != 3 {
				t.Fatal("test did not queue a genuine validated target acceptance")
			}
			e.closeTarget("injected_target_failure")
			if reconnect {
				e.dst = &target{gen: 8, cancel: func() {}}
			}

			if string(labs.frame(t)) != initial {
				t.Fatal("the already-admitted frame was changed mid-write")
			}
			e.handle(nextIOEvent(t, e))
			ev = nextIOEvent(t, e)
			if ev.kind != "source_write_ready" || ev.code != 201 {
				t.Fatal("queued stale player response was not checked after writer delay")
			}
			e.handle(ev)
			ev = nextIOEvent(t, e)
			if ev.kind != "source_write_ready" || ev.code != 200 {
				t.Fatal("validated acceptance disappeared behind stale player context")
			}
			e.handle(ev)
			labs.silent(t, 10*time.Millisecond)
			if e.src.quarantined || e.src.queuedCount != 0 || e.src.queuedBytes != 0 ||
				e.state.Counts.ReplyDiscarded != 2 || e.state.Counts.TargetAccept != 1 ||
				e.state.Counts.AcceptedACKAbandoned != 1 || e.state.Counts.SourceACK != 0 ||
				e.state.Counts.ReplyCompletedAfterRetire != 1 {
				t.Fatalf("wrong explicit discard/acceptance outcomes: %+v", e.state.Counts)
			}
			events := fmt.Sprint(e.state.Recent)
			if !strings.Contains(events, "upstream_response_discarded_target_generation_retired") ||
				!strings.Contains(events, "accepted_200_not_written_target_generation_retired") ||
				strings.Contains(events, "must-not-arrive") {
				t.Fatal("retirement outcomes were hidden or disclosed raw player values")
			}
		})
	}
}

func TestAccepted200CannotCrossSourceEpoch(t *testing.T) {
	e, labs := stateEngine(t)
	e.src.epoch = 4
	permit := make(chan bool, 1)
	e.handle(event{kind: "source_write_ready", epoch: 3, gen: 7, code: 200, n: len(acceptance), permit: permit})
	if <-permit || e.state.Counts.AcceptedACKAbandoned != 1 || e.state.Counts.SourceACK != 0 ||
		e.src.queuedCount != 0 || e.src.queuedBytes != 0 {
		t.Fatal("old accepted receipt crossed source epoch or altered new source queue budgets")
	}
	newPeer(labs).silent(t, time.Millisecond)
	if !strings.Contains(fmt.Sprint(e.state.Recent), "accepted_200_not_written_source_epoch_ended") {
		t.Fatal("abandoned old-source acceptance was silently discarded")
	}
}

func TestAdmittedAccepted200CompletionRemainsAnActualSourceACK(t *testing.T) {
	e, sourceConn := stateEngine(t)
	labs := newPeer(sourceConn)
	e.events, e.done = make(chan event), make(chan struct{})
	e.config.Pace = 0
	notice := &writeNoticeConn{Conn: e.src.conn, attempted: make(chan struct{}, 1)}
	e.src.conn = notice
	e.dst = &target{gen: 7, cancel: func() {}}
	e.wait = &pending{token: 11, alias: 1}
	e.handle(event{kind: "target_write", epoch: e.src.epoch, gen: 7, token: 11})
	e.handle(event{kind: "target_frame", epoch: e.src.epoch, gen: 7, token: 11, frame: []byte(acceptance)})
	writerDone := make(chan struct{})
	go func() { defer close(writerDone); e.writeSource(e.src) }()
	t.Cleanup(func() {
		close(e.done)
		close(e.src.writeStop)
		notice.Close()
		labs.Close()
		select {
		case <-writerDone:
		case <-time.After(time.Second):
			t.Error("source writer leaked")
		}
	})
	e.handle(nextIOEvent(t, e))
	select {
	case <-notice.attempted:
	case <-time.After(time.Second):
		t.Fatal("accepted response never entered its admitted in-flight phase")
	}
	e.closeTarget("injected_target_failure")
	e.dst = &target{gen: 8, cancel: func() {}}
	if string(labs.frame(t)) != acceptance {
		t.Fatal("already-admitted actual acceptance was retracted mid-JSON")
	}
	e.handle(nextIOEvent(t, e))
	if e.state.Counts.SourceACK != 1 || e.state.Counts.TargetAccept != 1 ||
		e.state.Counts.AcceptedACKAbandoned != 0 || e.state.Counts.ReplyCompletedAfterRetire != 1 {
		t.Fatal("genuine completed source ACK was silently removed after retirement")
	}
}

func TestDuplicate200BeforeWriteAdmissionExplicitlyAbandonsQueuedACK(t *testing.T) {
	e, labs := stateEngine(t)
	e.dst = &target{gen: 7, cancel: func() {}}
	e.wait = &pending{token: 11, alias: 1}
	e.handle(event{kind: "target_write", epoch: e.src.epoch, gen: 7, token: 11})
	response := event{kind: "target_frame", epoch: e.src.epoch, gen: 7, token: 11, frame: []byte(acceptance)}
	e.handle(response)
	e.handle(response)
	work := <-e.src.writes
	permit := make(chan bool, 1)
	e.handle(event{kind: "source_write_ready", epoch: work.epoch, gen: work.gen,
		code: work.code, n: len(work.frame), permit: permit})
	if <-permit || e.src.quarantined || e.state.Counts.TargetAccept != 1 ||
		e.state.Counts.SourceACK != 0 || e.state.Counts.AcceptedACKAbandoned != 1 ||
		!strings.Contains(e.state.LastError, "accepted_200_not_written_target_generation_retired") {
		t.Fatal("duplicate response silently erased acceptance or released a retired queued ACK")
	}
	newPeer(labs).silent(t, time.Millisecond)
}

func TestCompletedACKFromDepartedEpochPreservesActualOutcomeOnly(t *testing.T) {
	e, labs := stateEngine(t)
	e.src.epoch = 4
	e.state.Counts.TargetAccept = 1
	e.handle(event{kind: "source_write", epoch: 3, gen: 7, code: 200, n: len(acceptance)})
	if e.src.epoch != 4 || e.src.queuedCount != 0 || e.src.queuedBytes != 0 ||
		e.state.Counts.SourceACK != 1 || e.state.Counts.AcceptedACKAbandoned != 0 ||
		e.state.Counts.TargetAccept != 1 || e.src.quarantined {
		t.Fatal("actual old-source completion was erased or mutated the new source session")
	}
	newPeer(labs).silent(t, time.Millisecond)
}
