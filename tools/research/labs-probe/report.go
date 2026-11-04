package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const maxEvents = 32

const goalCheckpointOutcome = "inconclusive_running_two_shot_evidence_reached_awaiting_source_first_disconnect"

// sourceLifecycle distinguishes a two-shot checkpoint from observed clean EOF.
type sourceLifecycle struct {
	// GoalReached reports two distinct ball IDs with complete local ACK writes.
	GoalReached bool `json:"goal_reached"`
	// GoalElapsedMS is the checkpoint time relative to process start.
	GoalElapsedMS int64 `json:"goal_elapsed_ms"`
	// SourceEOF reports EOF at an object boundary, not reset or Labs survival.
	SourceEOF bool `json:"clean_source_eof_observed"`
	// SourceState describes admission/EOF/teardown observations.
	SourceState string `json:"source_state"`
	// ShutdownReason is the pending checkpoint or final outcome classification.
	ShutdownReason string `json:"shutdown_reason"`
	// ForcedTeardownRisk reports teardown without observed clean source EOF.
	ForcedTeardownRisk bool `json:"forced_teardown_risk"`
}

// counts records receiver-local probe observations within one accepted session.
type counts struct {
	// ReceivedBytes includes read-ahead and whitespace charged to the input budget.
	ReceivedBytes int `json:"received_bytes"`
	// Objects counts complete framed objects before envelope inspection.
	Objects int `json:"objects"`
	// Status counts inspected status-only envelopes.
	Status int `json:"status"`
	// Heartbeat counts inspected heartbeat envelopes.
	Heartbeat int `json:"heartbeat"`
	// ClubOnly counts club-only envelopes, which receive no ACK.
	ClubOnly int `json:"club_only"`
	// BallMessages counts all inspected balls, including duplicates.
	BallMessages int `json:"ball_messages"`
	// DuplicateBall counts ball IDs previously observed in this session.
	DuplicateBall int `json:"duplicate_ball_messages"`
	// DistinctBall counts session-local unique normalized integer ball IDs.
	DistinctBall int `json:"distinct_ball_ids"`
	// Acks counts completed local 200 writes, not peer consumption.
	Acks int `json:"complete_200_writes"`
	// DistinctAcked counts distinct IDs with a completed local ACK.
	DistinctAcked int `json:"distinct_ball_ids_acked"`
	// AckBytes includes bytes from partial or failed ACK writes.
	AckBytes int `json:"ack_bytes_written"`
}

// sourceEvent is redacted envelope metadata, never raw shot/player values.
type sourceEvent struct {
	// ElapsedMS is the observation time relative to process start.
	ElapsedMS int64 `json:"elapsed_ms"`
	// Kind is the inspected ball/club_only/status/heartbeat classification.
	Kind string `json:"kind"`
	// Fields contains required member names; the shared slice must not be mutated.
	Fields []string `json:"source_fields"`
	// Flags is the source's ShotDataOptions report, not game readiness.
	Flags sourceFlags `json:"source_flags"`
	// ShotAlias is a session-local "ball-N" alias, not the source ID.
	ShotAlias string `json:"shot_alias,omitempty"`
	// Duplicate reports a previously observed ball ID.
	Duplicate bool `json:"duplicate,omitempty"`
	// Ack is empty for non-balls, or the local ACK write outcome classification.
	Ack string `json:"ack,omitempty"`
}

// runReport is a bounded local observation snapshot. Network/session code owns
// its mutable fields; publication/finish occurs after that worker is joined.
// No report field proves Labs survival or end-to-end delivery.
type runReport struct {
	// Probe is the frozen instrument/mode identity.
	Probe string `json:"probe"`
	// Build is its stamped source revision.
	Build string `json:"build"`
	// StartedUTC is the process-start timestamp.
	StartedUTC string `json:"started_utc"`
	// ElapsedMS is the elapsed time at record publication.
	ElapsedMS int64 `json:"elapsed_ms"`
	// Outcome is an inconclusive running/checkpoint or final local result.
	Outcome string `json:"outcome"`
	// ExitCode is the candidate process result; actual nonzero exit overrides it.
	ExitCode int `json:"exit_code"`
	// Connected reports whether a source was accepted.
	Connected bool `json:"connected"`
	// Counts contains cumulative receiver-local session counters.
	Counts counts `json:"counts"`
	// Events retains at most maxEvents observations, oldest first.
	Events []sourceEvent `json:"events"`
	// OmittedEvents counts observations displaced from Events after it fills.
	OmittedEvents int `json:"omitted_events"`
	// Window is the setup/loop/cleanup evidence for the native identity.
	Window windowVerification `json:"compatibility_window"`
	// Lifecycle records checkpoint and source-first departure observations.
	Lifecycle sourceLifecycle `json:"source_lifecycle"`
	// NetworkDeadlineUTC is the absolute 295-second network cutoff.
	NetworkDeadlineUTC string `json:"network_deadline_utc"`
	// HardDeadlineUTC is the absolute 300-second process watchdog cutoff.
	HardDeadlineUTC string `json:"hard_deadline_utc"`
	// Final distinguishes summary publication from inconclusive running/goal records.
	Final bool `json:"final"`

	started time.Time
	dir     string
	console io.Writer
}

func newReport(root string, started time.Time, console io.Writer) (*runReport, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(root, started.UTC().Format("20060102T150405Z-"))
	if err != nil {
		return nil, err
	}
	report := &runReport{
		Probe:              probeIdentity,
		Build:              buildRevision,
		StartedUTC:         started.UTC().Format(time.RFC3339Nano),
		Outcome:            "inconclusive_running_no_final_result",
		ExitCode:           hardDeadline.exit,
		started:            started,
		dir:                dir,
		console:            console,
		Window:             windowVerification{Requested: true},
		Lifecycle:          sourceLifecycle{SourceState: "not_accepted", ShutdownReason: "not_started"},
		NetworkDeadlineUTC: started.Add(maxLifetime - shutdownBudget).UTC().Format(time.RFC3339Nano),
		HardDeadlineUTC:    started.Add(maxLifetime).UTC().Format(time.RFC3339Nano),
	}
	if err := report.publishRecord("running"); err != nil {
		return nil, err
	}
	return report, nil
}

func (r *runReport) goalCheckpoint() error {
	r.Lifecycle.GoalReached = true
	r.Lifecycle.GoalElapsedMS = time.Since(r.started).Milliseconds()
	r.Lifecycle.ShutdownReason = "awaiting_source_first_disconnect"
	r.Outcome, r.ExitCode, r.Final = goalCheckpointOutcome, hardDeadline.exit, false
	if err := r.publishRecord("goal"); err != nil {
		return err
	}
	_, err := fmt.Fprintln(r.console,
		"vx-labs-probe: TWO-SHOT EVIDENCE REACHED; RUNNING/INCONCLUSIVE, awaiting source-first disconnect. "+
			"Do not request more shots. Disconnect or close Labs normally well before the absolute network deadline. "+
			"No goal-triggered close; hard-limit forced teardown remains a risk. EOF alone does not prove Labs survival.")
	return err
}

func (r *runReport) warnReportingFailure() {
	fmt.Fprintln(os.Stderr,
		"vx-labs-probe: reporting_failed: cannot persist/announce running source state; "+
			"teardown without clean source EOF may be unsafe. Result is INCONCLUSIVE.")
}

func (r *runReport) printWindow() error {
	data, err := json.Marshal(r.Window)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.console, "vx-labs-probe: native window verification (not source acceptance):", string(data))
	return err
}

func (r *runReport) record(event sourceEvent) error {
	if len(r.Events) == maxEvents {
		copy(r.Events, r.Events[1:])
		r.Events[maxEvents-1] = event
		r.OmittedEvents++
		return nil
	}
	r.Events = append(r.Events, event)
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.console, string(data))
	return err
}

func (r *runReport) publishRecord(name string) error {
	r.ElapsedMS = time.Since(r.started).Milliseconds()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	// Each of the three records is published once, never over a file held by a reader.
	path := filepath.Join(r.dir, name+".tmp")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	return publishReportFile(path, filepath.Join(r.dir, name+".json"))
}

func (r *runReport) finish(result outcome) error {
	r.Outcome, r.ExitCode = result.name, result.exit
	r.Final = true
	r.Lifecycle.ShutdownReason = result.name
	return r.publishRecord("summary")
}
