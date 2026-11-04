package main

import (
	"os"
	"strings"
	"testing"
)

func TestRecoveryFirstQualificationKeepsFullSupportedRecoveryAndCleanup(t *testing.T) {
	data, err := os.ReadFile("worker_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"os-trace-ready.json", "os-trace-request.json", "classifyRecovery(", "compareFault(", "retainSecurity(", "c.Lane"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatal("full qualification retained discarded rollback/collector gate", forbidden)
		}
	}
	if !strings.Contains(string(data), "recovery-first-v2") ||
		!strings.Contains(string(data), `setup("08-supported-recovery-exact-b-fault-media"`) ||
		!strings.Contains(string(data), `uninstall("09-final-uninstall-b-fault"`) ||
		!strings.Contains(string(data), `setup("08-fresh-install-after-complete-uninstall"`) {
		t.Fatal("real mutation failure must be followed by supported recovery and clean uninstall")
	}
}
