package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const listenAddress = "127.0.0.1:12321"

type limits struct {
	context  context.Context
	deadline time.Time
	write    time.Duration
	budget   int
}

type outcome struct {
	name string
	exit int
}

var (
	positive            = outcome{"two_distinct_ball_shots_acked_then_clean_source_eof_without_201_202", 0}
	noConnection        = outcome{"inconclusive_no_connection_timeout", 2}
	sessionTimeout      = outcome{"inconclusive_session_timeout", 3}
	earlyEOF            = outcome{"inconclusive_early_eof", 4}
	invalidFrame        = outcome{"invalid_framing", 5}
	unexpectedShape     = outcome{"unexpected_shape", 6}
	budgetExceeded      = outcome{"receive_budget_exhausted", 7}
	bindFailed          = outcome{"bind_failed", 8}
	ioFailed            = outcome{"inconclusive_socket_io_failure", 9}
	ackFailed           = outcome{"inconclusive_ack_write_failure", 10}
	reportFailed        = outcome{"reporting_failed", 11}
	unsupported         = outcome{"unsupported_platform_or_arguments", 12}
	hardDeadline        = outcome{"inconclusive_hard_lifetime_exit", 13}
	interrupted         = outcome{"inconclusive_interrupted", 14}
	closeFailed         = outcome{"inconclusive_socket_close_failure", 15}
	windowSetupFailed   = outcome{"inconclusive_window_setup_failure", 17}
	windowLoopFailed    = outcome{"inconclusive_window_message_loop_failure", 18}
	windowCleanupFailed = outcome{"inconclusive_window_cleanup_failure", 19}
)

func receiveOne(listener *net.TCPListener, config limits, report *runReport) (result outcome) {
	stop := context.AfterFunc(config.context, func() { _ = listener.Close() })
	defer stop()
	defer func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = closeFailed
			return
		}
		if config.context.Err() != nil && (result == ioFailed || result == positive || result == noConnection || result == sessionTimeout) {
			result = contextOutcome(config.context, report.Connected)
		}
	}()
	if listener.SetDeadline(config.deadline) != nil {
		return ioFailed
	}
	conn, err := listener.AcceptTCP()
	if err != nil {
		if isTimeout(err) {
			return noConnection
		}
		return ioFailed
	}
	report.Connected = true
	report.Lifecycle.SourceState = "accepted_no_clean_eof_observed"
	report.Lifecycle.ForcedTeardownRisk = true
	// No second accept, reconnect, or waiting client is part of this experiment.
	if listener.Close() != nil {
		_ = conn.Close()
		return closeFailed
	}
	return receiveShots(conn, config, report)
}

func receiveShots(conn net.Conn, config limits, report *runReport) (result outcome) {
	stop := context.AfterFunc(config.context, func() { _ = conn.Close() })
	defer stop()
	defer func() {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = closeFailed
		}
		if config.context.Err() != nil && (result == ioFailed || result == positive || result == sessionTimeout) {
			result = contextOutcome(config.context, true)
		}
		report.Lifecycle.ShutdownReason = result.name
		if report.Lifecycle.SourceEOF {
			report.Lifecycle.SourceState = "clean_source_eof_observed"
		} else {
			report.Lifecycle.SourceState = "teardown_without_clean_source_eof"
		}
		report.Lifecycle.ForcedTeardownRisk = !report.Lifecycle.SourceEOF
	}()
	report.Connected = true
	report.Lifecycle.SourceState = "accepted_no_clean_eof_observed"
	report.Lifecycle.ShutdownReason = "awaiting_two_shot_goal"
	report.Lifecycle.ForcedTeardownRisk = true
	report.NetworkDeadlineUTC = config.deadline.UTC().Format(time.RFC3339Nano)
	if _, err := fmt.Fprintln(report.console,
		"vx-labs-probe: client accepted; waiting for native objects; no simulator forwarding. "+
			"Disconnect or close Labs normally well before the network deadline; forced teardown remains a risk."); err != nil {
		report.warnReportingFailure()
		return reportFailed
	}
	if conn.SetDeadline(config.deadline) != nil {
		return ioFailed
	}
	reader := &budgetReader{reader: conn, limit: config.budget}
	defer func() { report.Counts.ReceivedBytes = reader.read }()
	stream := objectStream{reader: bufio.NewReaderSize(reader, 4096)}
	aliases := make(map[string]string)
	for {
		frame, err := stream.next()
		if err != nil {
			switch {
			case errors.Is(err, errBudget):
				return budgetExceeded
			case errors.Is(err, errFraming):
				return invalidFrame
			case errors.Is(err, io.EOF):
				report.Lifecycle.SourceEOF = true
				if report.Lifecycle.GoalReached {
					return positive
				}
				return earlyEOF
			case isTimeout(err):
				return sessionTimeout
			default:
				return ioFailed
			}
		}
		report.Counts.Objects++
		msg, err := inspect(frame)
		if err != nil {
			return unexpectedShape
		}
		event := sourceEvent{
			ElapsedMS: time.Since(report.started).Milliseconds(),
			Kind:      msg.kind,
			Fields:    msg.fields,
			Flags:     msg.flags,
		}
		switch msg.kind {
		case "status":
			report.Counts.Status++
		case "heartbeat":
			report.Counts.Heartbeat++
		case "club_only":
			report.Counts.ClubOnly++
		case "ball":
			report.Counts.BallMessages++
			alias, duplicate := aliases[msg.id]
			if duplicate {
				report.Counts.DuplicateBall++
				event.Duplicate = true
			} else {
				alias = fmt.Sprintf("ball-%d", len(aliases)+1)
				aliases[msg.id] = alias
				report.Counts.DistinctBall++
			}
			event.ShotAlias = alias
			deadline := time.Now().Add(config.write)
			if config.deadline.Before(deadline) {
				deadline = config.deadline
			}
			n, err := writeAck(conn, deadline)
			report.Counts.AckBytes += n
			event.Ack = "complete_local_write"
			if err != nil {
				event.Ack = "not_written"
				if n > 0 {
					event.Ack = "partial_or_uncertain"
				}
				if report.record(event) != nil {
					report.warnReportingFailure()
					return reportFailed
				}
				return ackFailed
			}
			report.Counts.Acks++
			if !duplicate {
				report.Counts.DistinctAcked++
			}
		}
		if report.record(event) != nil {
			report.warnReportingFailure()
			return reportFailed
		}
		if report.Counts.DistinctAcked >= 2 && !report.Lifecycle.GoalReached {
			report.Counts.ReceivedBytes = reader.read
			if report.goalCheckpoint() != nil {
				report.warnReportingFailure()
				return reportFailed
			}
		}
	}
}

func writeAck(conn net.Conn, deadline time.Time) (int, error) {
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return 0, err
	}
	written := 0
	for written < len(acknowledgment) {
		n, err := conn.Write([]byte(acknowledgment[written:]))
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

func isTimeout(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}
