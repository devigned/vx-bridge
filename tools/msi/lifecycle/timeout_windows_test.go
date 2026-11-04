package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUncertainLogsPreserveRawBytesWithoutInterpretingPriorResult(t *testing.T) {
	state, out := t.TempDir(), t.TempDir()
	raw := []byte{0xff, 0xfe, 'A', 0, '\r', 0, '\n', 0}
	writer, err := os.OpenFile(filepath.Join(state, "setup-msi.log"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	prior := []byte("usable=1 result=0 source=prior-operation\r\n")
	if err := os.WriteFile(filepath.Join(state, "setup-result.log"), prior, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := retainUncertainSetupLogs("timed-out", out, userContext{State: state}); err != nil {
		t.Fatal(err)
	}
	for filename, want := range map[string][]byte{"setup-msi.log": raw, "setup-result.log": prior} {
		got, err := os.ReadFile(filepath.Join(out, "timed-out-potentially-changing-"+filename))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("raw partial %s differs: %v", filename, err)
		}
	}
	evidence, err := os.ReadFile(filepath.Join(out, "timed-out-uncertain-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var observation map[string]any
	if err := json.Unmarshal(evidence, &observation); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"state_may_still_change", "no_current_native_result_assumed", "no_post_timeout_msi_inventory_or_recovery"} {
		if observation[field] != true {
			t.Fatalf("uncertainty boundary missing: %s", field)
		}
	}
	if err := retainUncertainSetupLogs("timed-out", out, userContext{State: state}); err == nil {
		t.Fatal("existing timeout evidence was overwritten")
	}
	if _, err := writer.Write([]byte{'B', 0}); err != nil {
		t.Fatalf("observation interfered with still-open writer: %v", err)
	}
}

func TestUncertainLogAbsenceAndSizeLimitAreExplicit(t *testing.T) {
	state, out := t.TempDir(), t.TempDir()
	if err := retainUncertainSetupLogs("absent", out, userContext{State: state}); err != nil {
		t.Fatal(err)
	}
	evidence, err := os.ReadFile(filepath.Join(out, "absent-uncertain-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var observation struct {
		Missing []string `json:"missing_logs"`
	}
	if err := json.Unmarshal(evidence, &observation); err != nil || len(observation.Missing) != 2 {
		t.Fatalf("missing partial logs not recorded: %v", err)
	}
	file, err := os.Create(filepath.Join(state, "setup-msi.log"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((32 << 20) + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := retainUncertainSetupLogs("oversize", out, userContext{State: state}); err == nil {
		t.Fatal("oversize raw observation was not refused")
	}
	if _, err := os.Stat(filepath.Join(out, "oversize-uncertain-state.json")); err != nil {
		t.Fatalf("retention failure lost the uncertainty record: %v", err)
	}
}
