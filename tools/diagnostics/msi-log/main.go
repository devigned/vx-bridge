// Msi-log summarizes an existing local installer log as JSON without running
// Windows Installer. It uses [msilog] for bounded decoding and recorded
// policy/action checks, not current product-state or recovery verification.
// With -require-policy, missing/nonzero policy proof produces a nonzero exit
// after the observation report is printed.
//
// Usage:
//
//	msi-log [-require-policy] LOG
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/devigned/vx-bridge/tools/diagnostics/msilog"
)

// summary contains offline observations, not qualification of an installed host.
type summary struct {
	// Scope states what the report does not prove.
	Scope string `json:"scope"`
	// DecodedBytes is the UTF-8/byte-string length after ReadText decoding.
	DecodedBytes int `json:"decoded_bytes"`
	// EnginePoliciesProved reports that all three recorded policies are zero.
	EnginePoliciesProved bool `json:"engine_policies_all_zero_proved"`
	// CompletedActions maps the selected action names to observed success lines.
	CompletedActions map[string]bool `json:"completed_actions_observed"`
	// FileCopies counts FileCopy operation markers, not verified final files.
	FileCopies int `json:"file_copy_records"`
	// RegistryWrites counts RegAddValue operation markers.
	RegistryWrites int `json:"registry_write_records"`
	// PrivateFaults counts the deliberate post-execute fixture-failure marker.
	PrivateFaults int `json:"private_fault_markers"`
	// RollbackErrors counts "Error in rollback skipped" markers.
	RollbackErrors int `json:"rollback_errors_skipped"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, stderr io.Writer) int {
	flags := flag.NewFlagSet("msi-log", flag.ContinueOnError)
	flags.SetOutput(stderr)
	requirePolicy := flags.Bool("require-policy", false, "fail unless all three recorded engine policies are present and zero")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: msi-log [-require-policy] LOG")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return 2
	}
	text, err := msilog.ReadText(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "msi-log:", err)
		return 1
	}
	report := summary{
		Scope:        "offline log observations only; not current state, successful recovery, cleanup or live qualification",
		DecodedBytes: len(text), EnginePoliciesProved: msilog.VerifyEnginePolicies(text) == nil,
		CompletedActions: map[string]bool{},
		FileCopies:       strings.Count(text, "Executing op: FileCopy"),
		RegistryWrites:   strings.Count(text, "Executing op: RegAddValue"),
		PrivateFaults:    strings.Count(text, "PRIVATE_LIFECYCLE_EXPECTED_POST_EXECUTE_FAILURE"),
		RollbackErrors:   strings.Count(text, "Error in rollback skipped"),
	}
	for _, action := range []string{"InstallExecute", "InstallFinalize", "RemoveExistingProducts",
		"VxSetupAuthorize", "VxSetupPreFinalize", "VxRemoveBegin", "VxRemoveFinish"} {
		report.CompletedActions[action] = msilog.ActionCompleted(text, action)
	}
	if err := json.NewEncoder(out).Encode(report); err != nil {
		fmt.Fprintln(stderr, "msi-log:", err)
		return 1
	}
	if *requirePolicy && !report.EnginePoliciesProved {
		fmt.Fprintln(stderr, "msi-log: recorded engine policy proof is missing or nonzero; report is not a pass")
		return 1
	}
	return 0
}
