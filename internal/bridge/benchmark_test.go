package bridge

import (
	"bufio"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/devigned/vx-bridge/internal/protocol"
)

// Measures only deadline checks, not OS wakeups or idle CPU consumption.
func BenchmarkIdleTick(b *testing.B) {
	for _, state := range []string{"source_absent", "target_connected", "retry_waiting"} {
		b.Run(state, func(b *testing.B) {
			now := time.Now()
			e := &engine{retryAt: now.Add(time.Hour)}
			if state != "source_absent" {
				e.src = &source{}
			}
			if state == "target_connected" {
				e.dst = &target{}
			}
			b.ReportAllocs()
			for b.Loop() {
				e.tick(now)
			}
		})
	}
}

// Includes target read/write, source write and net.Pipe scheduling, but excludes
// source ingress, intentional pacing/hold/retry waits and physical transport.
func BenchmarkForwardReceiptPipe(b *testing.B) {
	sourceHost, sourcePeer := net.Pipe()
	targetHost, targetPeer := net.Pipe()
	e := &engine{
		config: Defaults("invented"), events: make(chan event), done: make(chan struct{}),
		logs: make(chan Event, 32),
		src: &source{conn: sourceHost, epoch: 1, done: make(chan struct{}),
			writeStop: make(chan struct{}), writes: make(chan upstreamWrite, queueCount)},
		dst: &target{conn: targetHost, gen: 1, cancel: func() {}},
	}
	e.config.Pace = 0
	e.openEligibility()
	workerDone := make(chan struct{}, 4)
	start := func(work func()) {
		go func() { defer func() { workerDone <- struct{}{} }(); work() }()
	}
	start(func() { e.writeSource(e.src) })
	start(func() { e.readTarget(e.src.epoch, e.dst) })
	errs := make(chan error, 2)
	start(func() {
		stream := protocol.Stream{Reader: bufio.NewReader(targetPeer)}
		for {
			if _, err := stream.Next(); err != nil {
				errs <- err
				return
			}
			if _, err := stream.Reader.ReadByte(); err != nil {
				errs <- err
				return
			}
			if _, err := targetPeer.Write([]byte(acceptance)); err != nil {
				errs <- err
				return
			}
		}
	})
	start(func() {
		stream := protocol.Stream{Reader: bufio.NewReader(sourcePeer)}
		for {
			frame, err := stream.Next()
			if err != nil {
				errs <- err
				return
			}
			if string(frame) != acceptance {
				errs <- fmt.Errorf("changed acceptance")
				return
			}
		}
	})
	b.Cleanup(func() {
		close(e.done)
		close(e.src.writeStop)
		sourceHost.Close()
		sourcePeer.Close()
		targetHost.Close()
		targetPeer.Close()
		for range 4 {
			<-workerDone
		}
	})
	frame := shot("1", true, true)
	b.ReportAllocs()
	for b.Loop() {
		acked := e.state.Counts.SourceACK
		e.forward(&candidate{frame: frame, gen: e.dst.gen, eligibility: e.available.Load()})
		for e.state.Counts.SourceACK == acked {
			select {
			case ev := <-e.events:
				e.handle(ev)
			case err := <-errs:
				b.Fatal(err)
			}
		}
		if e.wait != nil || e.src.queuedCount != 0 || e.state.Counts.TargetAccept != acked+1 {
			b.Fatal("receipt/queue invariant failed")
		}
	}
}
