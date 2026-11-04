package bridge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"time"

	"github.com/devigned/vx-bridge/internal/protocol"
)

func writeAll(conn net.Conn, frame []byte, timeout time.Duration) (int, error) {
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return 0, err
	}
	written := 0
	for written < len(frame) {
		n, err := conn.Write(frame[written:])
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrNoProgress
		}
	}
	return written, nil
}

func (e *engine) readSource(src *source) {
	reader := &observationReader{Reader: src.conn, current: e.available.Load}
	stream := protocol.Stream{Reader: bufio.NewReaderSize(reader, 4096),
		Start: func() error {
			reader.start()
			return src.conn.SetReadDeadline(time.Now().Add(e.config.Frame))
		},
		End: func() error {
			reader.finish()
			return src.conn.SetReadDeadline(time.Time{})
		}}
	for {
		frame, err := stream.Next()
		if err != nil {
			if errors.Is(err, io.EOF) || peerReset(err) {
				e.emit(event{kind: "source_departed", epoch: src.epoch, err: err})
				return
			}
			e.emit(event{kind: "source_fault", epoch: src.epoch, err: err})
			break
		}
		select {
		case <-src.done:
			return
		default:
		}
		if !e.emit(event{kind: "source_frame", epoch: src.epoch, token: reader.first, changed: reader.changed, frame: frame}) {
			return
		}
	}
	// Framing is no longer trustworthy. Bounded draining detects actual peer
	// departure without a planned server close beneath attached Labs.
	if err := src.conn.SetReadDeadline(time.Time{}); err != nil {
		e.emit(event{kind: "source_fault", epoch: src.epoch, err: err})
	}
	var discard [4096]byte
	for {
		_, err := src.conn.Read(discard[:])
		if errors.Is(err, io.EOF) || peerReset(err) {
			e.emit(event{kind: "source_departed", epoch: src.epoch, err: err})
			return
		}
		if err != nil {
			select {
			case <-src.done:
				return
			case <-time.After(time.Second):
			}
		}
	}
}

// Read-ahead retains its observation until the buffer is exhausted. A frame
// pins that first observation; later reads can invalidate it, never retag it.
// Reader is the underlying connection, whose reads this wrapper observes.
type observationReader struct {
	// Reader supplies source or target connection bytes. This wrapper observes
	// its reads but leaves connection cleanup to the engine.
	io.Reader
	current         func() uint64
	observed, first uint64
	active, changed bool
}

// Read fills p through Reader and associates non-empty reads with the current
// eligibility/token observation. A changed observation invalidates an active
// frame without changing its first ticket. It preserves Reader's count/error.
func (r *observationReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.observed = r.current()
		if r.active && r.observed != r.first {
			r.changed = true
		}
	}
	return n, err
}

func (r *observationReader) start() {
	r.first, r.active, r.changed = r.observed, true, false
}

func (r *observationReader) finish() {
	if r.current() != r.first {
		r.changed = true
	}
	r.active = false
}

func (e *engine) writeSource(src *source) {
	last := time.Time{}
	for {
		select {
		case <-src.writeStop:
			return
		case work := <-src.writes:
			result := event{epoch: work.epoch, gen: work.gen, code: work.code, n: len(work.frame)}
			discard := func() {
				result.kind = "source_write_discarded"
				e.emit(result)
			}
			delay := time.Until(last.Add(e.config.Pace))
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-src.writeStop:
					timer.Stop()
					discard()
					return
				}
			}
			select {
			case <-src.writeStop:
				discard()
				return
			default:
			}
			// The owner linearizes admission after pacing. Retired queued
			// replies are discarded; admitted writes cannot safely be
			// retracted mid-JSON if the target subsequently retires.
			permit := make(chan bool, 1)
			ready := result
			ready.kind, ready.permit = "source_write_ready", permit
			if !e.emit(ready) {
				return
			}
			select {
			case allowed := <-permit:
				if !allowed {
					continue
				}
			case <-e.done:
				return
			}
			select {
			case <-src.writeStop:
				discard()
				return
			default:
			}
			_, err := writeAll(src.conn, work.frame, e.config.Write)
			last = time.Now()
			result.kind, result.err = "source_write", err
			e.emit(result)
			if err != nil {
				return
			}
		}
	}
}

func (e *engine) readTarget(epoch uint64, dst *target) {
	reader := &observationReader{Reader: dst.conn, current: dst.token.Load}
	stream := protocol.Stream{Reader: bufio.NewReaderSize(reader, 4096),
		Start: func() error {
			reader.start()
			return dst.conn.SetReadDeadline(time.Now().Add(e.config.Frame))
		},
		End: func() error {
			reader.finish()
			return dst.conn.SetReadDeadline(time.Time{})
		}}
	for {
		frame, err := stream.Next()
		if err != nil {
			e.emit(event{kind: "target_error", epoch: epoch, gen: dst.gen, err: err})
			return
		}
		if !e.emit(event{kind: "target_frame", epoch: epoch, gen: dst.gen, token: reader.first, changed: reader.changed, frame: frame}) {
			return
		}
	}
}

func errorClass(err error) string {
	class := "unknown_io"
	var timeout net.Error
	switch {
	case errors.Is(err, io.EOF):
		class = "EOF"
	case peerReset(err):
		class = "peer_reset"
	case errors.Is(err, context.Canceled), errors.Is(err, net.ErrClosed):
		class = "local_cancel_or_close"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		class = "deadline"
	case errors.Is(err, protocol.ErrFrameSize):
		class = "frame_size"
	case errors.Is(err, protocol.ErrFraming):
		class = "framing"
	}
	if errors.Is(err, protocol.ErrFraming) && class != "framing" {
		class += "_incomplete_frame"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		class += fmt.Sprintf("_errno_%d", uint64(errno))
	}
	return class
}
