package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/devigned/vx-bridge/tools/diagnostics/msilog"
	"golang.org/x/sys/windows/registry"
)

// operation records one real disposable-host child invocation and its evidence.
// Timeout is not forced termination or proof that the product stopped changing.
type operation struct {
	// Name identifies the scenario operation.
	Name string `json:"name"`
	// Program is the actual child executable path.
	Program string `json:"program"`
	// Arguments is the argument vector excluding the executable.
	Arguments []string `json:"arguments"`
	// CommandLine retains an explicit raw Windows command line when supplied.
	CommandLine string `json:"raw_command_line,omitempty"`
	// StartedUTC is the invocation-start timestamp.
	StartedUTC string `json:"started_utc"`
	// EndedUTC is the time the worker's bounded invocation wait returned.
	EndedUTC string `json:"ended_utc"`
	// PID is the child process ID after successful start.
	PID int `json:"pid"`
	// ExitCode is the observed exit status, or -1 without a completed child result.
	ExitCode int `json:"exit_code"`
	// TimedOut reports a 10-minute wait expiry without a kill, cleanup, or retry.
	TimedOut bool `json:"timed_out_without_forced_kill"`
	// Native contains decoded Setup result fields; caller checks establish their
	// validity. A partial record may accompany an operation error.
	Native map[string]string `json:"native_result,omitempty"`
	// MSILog is the retained operation log path when available.
	MSILog string `json:"msi_log,omitempty"`
	// MSILogStart is the decoded-byte offset excluding prior accumulated log content.
	MSILogStart int `json:"current_operation_log_start"`
	// ResultLinesBefore counts result lines before this operation.
	ResultLinesBefore int `json:"native_result_lines_before"`
	// ResultLinesAfter counts result lines afterward; Setup must append exactly one.
	ResultLinesAfter int `json:"native_result_lines_after"`
	// Policies is the actual pre-operation registry policy evidence.
	Policies map[string]policyValue `json:"actual_policies_before_product_operation"`
}

// lifecycleResult summarizes one fixed private servicing sequence.
// It is not a qualification of bridge/game behavior or another source artifact.
type lifecycleResult struct {
	// Schema is the private lifecycle report format version.
	Schema int `json:"schema"`
	// Source is the exact worker/media source revision.
	Source string `json:"source_revision"`
	// Run is the manually approved hosted run ID.
	Run string `json:"run_id"`
	// Attempt is the first hosted attempt identity.
	Attempt string `json:"run_attempt"`
	// Context is independently verified primary-token/profile evidence.
	Context userContext `json:"context"`
	// Success reports scenario verification before final report-write errors;
	// the worker's exit/error still governs whether publication succeeded.
	Success bool `json:"success"`
	// Phase is the current/final scenario boundary, retained on failure.
	Phase string `json:"phase"`
	// Failure contains the scenario's returned error, if any.
	Failure string `json:"failure,omitempty"`
	// ExpectedFail retains the separately verified deliberate-mutation classification.
	ExpectedFail string `json:"expected_failure_classification,omitempty"`
	// Operations contains observed child invocations in scenario order.
	Operations []operation `json:"operations"`
	// Scope records disposable servicing-only limits, excluding live application use.
	Scope string `json:"scope"`
	// StateUncertain reports that a timed-out operation may still be changing state.
	StateUncertain bool `json:"state_may_still_be_changing"`
}

func execute(c configuration) (result error) {
	u, contextErr := inspectUser(c)
	evidence, err := filepath.EvalSymlinks(c.Evidence)
	if err != nil || !strings.EqualFold(evidence, filepath.Clean(c.Evidence)) {
		return fmt.Errorf("canonical existing evidence root required: %v", err)
	}
	for _, root := range []string{u.Root, u.LocalData, u.Config} {
		if root != "" && (within(evidence, root) || within(root, evidence)) {
			return errors.New("evidence must be independent of all product cleanup roots")
		}
	}
	if err := writeJSON(filepath.Join(c.Evidence, "actual-user-context.json"), u); err != nil {
		return err
	}
	report := lifecycleResult{Schema: 1, Source: c.Source, Run: c.Run, Attempt: c.Attempt, Context: u,
		Phase: "actual-user-context", Operations: []operation{},
		Scope: "DISPOSABLE HOST ONLY; quiet real Setup/MSI operations; no bridge/game/sign-in/hardware"}
	defer func() {
		if result != nil {
			report.Failure = result.Error()
			for _, op := range report.Operations {
				report.StateUncertain = report.StateUncertain || op.TimedOut
			}
			// Preserve the first actual state before any supported recovery, never
			// delete app residue or the account to manufacture a cleanup result.
			if report.Phase != "actual-user-context" && !report.StateUncertain {
				_, snapshotErr := capture("unexpected-failure-"+report.Phase, c.Evidence, u, c.Media)
				result = errors.Join(result, snapshotErr)
			}
		}
		report.Success = result == nil
		result = errors.Join(result, writeJSON(filepath.Join(c.Evidence, "lifecycle-result.json"), report))
	}()
	if contextErr != nil {
		return contextErr
	}
	report.Phase = "preflight"
	byRole := map[string]media{}
	for _, m := range c.Media {
		if err := verifyMedia(m); err != nil {
			return err
		}
		for _, path := range []string{m.MSI, m.Setup, m.Archive} {
			if within(path, u.Root) || within(path, u.LocalData) || within(path, u.Config) {
				return errors.New("original test media must be outside all cleanup roots")
			}
		}
		byRole[m.Role] = m
	}
	a, b, fault := byRole["a"], byRole["b"], byRole["b-fault"]
	if _, err := capture("00-fresh-account", c.Evidence, u, c.Media); err != nil {
		return err
	}
	if err := verifyAbsent(u, c.Media); err != nil {
		return err
	}
	if err := seedForeign(u); err != nil {
		return err
	}
	setup := func(name string, m media, expectedFailure bool) error {
		report.Phase = name
		policies, err := executionPolicies()
		if err != nil {
			return err
		}
		if _, err := capture(name+"-before", c.Evidence, u, c.Media); err != nil {
			return err
		}
		previousLog, err := readOptionalText(filepath.Join(u.State, "setup-msi.log"))
		if err != nil {
			return err
		}
		previousResult, err := readOptionalText(filepath.Join(u.State, "setup-result.log"))
		if err != nil {
			return err
		}
		op, err := invoke(name, m.Setup, []string{"/quiet", "/package", m.MSI}, c.Evidence)
		op.Policies = policies
		op.MSILogStart = len(previousLog)
		op.ResultLinesBefore = len(strings.FieldsFunc(previousResult, func(r rune) bool { return r == '\n' }))
		if err != nil {
			report.Operations = append(report.Operations, op)
			if op.TimedOut {
				report.StateUncertain = true
				err = errors.Join(err, retainUncertainSetupLogs(name, c.Evidence, u))
			}
			return err
		}
		op.Native, op.MSILog, err = retainNativeLogs(name, c.Evidence, u, m.Source)
		currentResult, resultErr := readOptionalText(filepath.Join(u.State, "setup-result.log"))
		op.ResultLinesAfter = len(strings.FieldsFunc(currentResult, func(r rune) bool { return r == '\n' }))
		err = errors.Join(err, resultErr)
		if op.ResultLinesAfter != op.ResultLinesBefore+1 {
			err = errors.Join(err, errors.New("native operation did not append exactly one new result record"))
		}
		report.Operations = append(report.Operations, op)
		if _, snapshotErr := capture(name+"-after", c.Evidence, u, c.Media); snapshotErr != nil {
			err = errors.Join(err, snapshotErr)
		}
		if err != nil {
			return err
		}
		if err := verifyNativeOutcome(op, expectedFailure); err != nil {
			return err
		}
		currentLog, logErr := msilog.ReadText(op.MSILog)
		if logErr != nil {
			return logErr
		}
		if op.MSILogStart > len(currentLog) || !strings.Contains(currentLog[op.MSILogStart:], u.SID) ||
			!strings.Contains(currentLog[op.MSILogStart:], m.Product) || !strings.Contains(currentLog[op.MSILogStart:], m.Source) {
			return errors.New("actual Setup MSI session log does not identify this user/product operation")
		}
		if err := verifyForeign(u); err != nil {
			return err
		}
		return nil
	}
	uninstall := func(name string, m media, original, complete bool) error {
		report.Phase = name
		policies, err := executionPolicies()
		if err != nil {
			return err
		}
		if _, err := capture(name+"-before", c.Evidence, u, c.Media); err != nil {
			return err
		}
		log := filepath.Join(c.Evidence, name+"-msi.log")
		args := []string{"/x", m.Product, "/qn", "REBOOT=ReallySuppress", "MSIRESTARTMANAGERCONTROL=Disable", "/L*vx!", log}
		if original {
			args = []string{"/i", m.MSI, "REMOVE=ALL", "/qn", "REBOOT=ReallySuppress", "MSIRESTARTMANAGERCONTROL=Disable", "/L*vx!", log}
		}
		op, err := invoke(name, filepath.Join(os.Getenv("SystemRoot"), "System32", "msiexec.exe"), args, c.Evidence)
		op.MSILog = log
		op.Policies = policies
		report.Operations = append(report.Operations, op)
		if op.TimedOut {
			report.StateUncertain = true
			return err
		}
		_, snapshotErr := capture(name+"-after", c.Evidence, u, c.Media)
		var exitErr error
		if op.ExitCode != 0 {
			exitErr = fmt.Errorf("actual uninstall exit=%d (reboot/incomplete is not success)", op.ExitCode)
		}
		if err := errors.Join(err, snapshotErr, exitErr); err != nil {
			return err
		}
		text, err := msilog.ReadText(log)
		if err != nil {
			return err
		}
		if err := msilog.VerifyEnginePolicies(text); err != nil {
			return err
		}
		actions := []string{"VxSetupAuthorize", "VxRemoveBegin", "VxRemoveFinish", "VxSetupPreFinalize"}
		if original && !complete && m.Role == "a" {
			actions = []string{"VxSetupAuthorize", "VxSetupPreFinalize"}
		}
		for _, action := range actions {
			if !msilog.ActionCompleted(text, action) {
				return fmt.Errorf("actual uninstall action/session evidence missing: %s", action)
			}
		}
		if !strings.Contains(text, m.Product) || !strings.Contains(text, u.SID) {
			return errors.New("uninstall log does not identify the actual target product/user")
		}
		if complete {
			if err := verifyAbsent(u, c.Media); err != nil {
				return err
			}
		} else if err := verifyProductAbsent(m, u); err != nil {
			return err
		}
		if !complete {
			clients, err := componentClients(u, c.Media)
			if err != nil {
				return err
			}
			for component, products := range clients {
				for _, product := range products {
					if product == m.Product {
						return fmt.Errorf("removed product still owns component client %s", component)
					}
				}
			}
		}
		return errors.Join(verifyForeign(u), verifyMedia(m))
	}
	if err := setup("01-install-a", a, false); err != nil {
		return err
	}
	if err := verifyInstalled(a, u, c.Media); err != nil {
		return err
	}
	if _, present, err := ownedRun(u); err != nil || present {
		return fmt.Errorf("fresh startup must be off: present=%t: %v", present, err)
	}
	if _, err := os.Lstat(filepath.Join(u.Config, "destination.json")); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("fresh install must not invent a destination preference: %v", err)
	}
	if err := seedOwned(u); err != nil {
		return err
	}
	report.Phase = "02-damage-known-resources"
	if err := os.Remove(filepath.Join(u.Root, "vxbridge.exe")); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(u.Root, "LICENSE"), []byte("controlled damaged known resource\n"), 0o600); err != nil {
		return err
	}
	if _, err := capture("02-damaged-known-resources", c.Evidence, u, c.Media); err != nil {
		return err
	}
	if err := setup("03-repair-a", a, false); err != nil {
		return err
	}
	if err := errors.Join(verifyInstalled(a, u, c.Media), verifyOwned(u)); err != nil {
		return err
	}
	if err := setup("04-upgrade-normal-a-to-b", b, false); err != nil {
		return err
	}
	if err := errors.Join(verifyInstalled(b, u, c.Media), verifyOwned(u), verifyProductAbsent(a, u)); err != nil {
		return err
	}
	if err := uninstall("05-uninstall-normal-b", b, false, true); err != nil {
		return err
	}
	if err := setup("06-fresh-install-a-after-wipe", a, false); err != nil {
		return err
	}
	if err := verifyInstalled(a, u, c.Media); err != nil {
		return err
	}
	if _, present, err := ownedRun(u); err != nil || present {
		return fmt.Errorf("fresh reinstall reused removed startup preference: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(u.Config, "destination.json")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("fresh reinstall reused removed destination/settings")
	}
	if err := seedOwned(u); err != nil {
		return err
	}
	if err := setFault(true); err != nil {
		return err
	}
	if err := setup("07-expected-failed-upgrade", fault, true); err != nil {
		return err
	}
	failed := report.Operations[len(report.Operations)-1]
	text, err := msilog.ReadText(failed.MSILog)
	if err != nil {
		return err
	}
	if failed.MSILogStart > len(text) {
		return errors.New("expected-failure log was truncated")
	}
	text = text[failed.MSILogStart:]
	marker := strings.Index(text, "PRIVATE_LIFECYCLE_EXPECTED_POST_EXECUTE_FAILURE")
	if marker < 0 {
		return errors.New("expected private Type19 phase was not reached")
	}
	beforeFault := text[:marker]
	if !msilog.ActionCompleted(beforeFault, "InstallExecute") || strings.Count(beforeFault, "Executing op: FileCopy") < 6 ||
		!strings.Contains(beforeFault, "Executing op: RegAddValue") || !strings.Contains(beforeFault, fault.Product) ||
		!strings.Contains(text, "VX_LIFECYCLE_FAIL = 1") {
		return errors.New("deliberate fault did not establish actual B file/registry mutation after InstallExecute")
	}
	products, err := productInventory(u)
	if err != nil {
		return err
	}
	receipt, valid, pending, err := receiptState()
	if err != nil || !valid {
		return fmt.Errorf("exact original receipt evidence missing: %v", err)
	}
	pendingState, err := inspectPending()
	if err != nil || pending != (pendingState != nil) {
		return fmt.Errorf("typed pending evidence differs: %v", err)
	}
	if err := errors.Join(verifyPassiveCache(a, u), verifyPassiveCache(fault, u)); err != nil {
		return err
	}
	if err := verifyFailedPending(a, fault, receipt, pendingState); err != nil {
		return err
	}
	report.ExpectedFail = "recovery-first-v2: failed actual mutation; Repair required; previous-version restoration not required"
	if err := writeJSON(filepath.Join(c.Evidence, "07-expected-failure-classification.json"), map[string]any{
		"contract": "recovery-first-v2", "classification": report.ExpectedFail, "native": failed.Native, "actual_catalog": products,
		"original_receipt": receipt, "typed_pending": pendingState, "exact_media_cache_checked": true,
		"rollback_skipped_errors": strings.Count(text, "Error in rollback skipped"),
		"recovery_media":          fault.Package, "recovery_msi_sha256": fault.MSISHA256,
		"file_copy_operations_before_fault": strings.Count(beforeFault, "Executing op: FileCopy"),
		"registry_writes_before_fault":      strings.Count(beforeFault, "Executing op: RegAddValue"),
		"current_operation_log_start":       failed.MSILogStart,
	}); err != nil {
		return err
	}
	// The deliberate failure is preserved above. This is a separate planned
	// recovery scenario using identical media, not a workflow retry to green.
	if err := setFault(false); err != nil {
		return err
	}
	recoveryErr := setup("08-supported-recovery-exact-b-fault-media", fault, false)
	if recoveryErr != nil {
		if report.StateUncertain {
			return recoveryErr
		}
		if err := writeJSON(filepath.Join(c.Evidence, "08-supported-repair-incomplete.json"), map[string]any{
			"contract": "recovery-first-v2", "error": recoveryErr.Error(),
			"next_explicit_action": "Complete Uninstall of the known recorded products using original media, then fresh Install",
		}); err != nil {
			return err
		}
		products, err := productInventory(u)
		if err != nil {
			return err
		}
		for _, product := range products {
			if product.Product != a.Product && product.Product != fault.Product {
				return errors.New("unknown product in recovery; no cleanup takeover")
			}
		}
		for _, m := range []media{a, fault} {
			current, err := inspectProduct(m.Product, u)
			if err != nil {
				return err
			}
			if current.Registered {
				if err := uninstall("08-explicit-uninstall-"+m.Role, m, true, false); err != nil {
					return err
				}
			}
		}
		if err := verifyAbsent(u, c.Media); err != nil {
			return err
		}
		if err := setup("08-fresh-install-after-complete-uninstall", fault, false); err != nil {
			return err
		}
		if _, present, err := ownedRun(u); err != nil || present {
			return errors.New("fresh recovery install must not reuse removed startup preference")
		}
		if _, err := os.Lstat(filepath.Join(u.Config, "destination.json")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("fresh recovery install must not reuse removed destination settings")
		}
	} else if err := verifyOwned(u); err != nil {
		return err
	}
	if err := errors.Join(verifyInstalled(fault, u, c.Media), verifyProductAbsent(a, u)); err != nil {
		return err
	}
	if err := uninstall("09-final-uninstall-b-fault", fault, false, true); err != nil {
		return err
	}
	for _, m := range c.Media {
		if err := verifyMedia(m); err != nil {
			return err
		}
	}
	if _, err := executionPolicies(); err != nil {
		return err
	}
	if _, err := capture("10-final-clean-uninstall-audit", c.Evidence, u, c.Media); err != nil {
		return err
	}
	report.Phase = "complete"
	return nil
}

func within(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
}

func invoke(name, program string, args []string, out string) (op operation, result error) {
	return invokeEnvironment(name, program, args, out, append(os.Environ(), "VX_SETUP_DIAGNOSTICS=1"))
}

func invokeEnvironment(name, program string, args []string, out string, environment []string) (op operation, result error) {
	command := exec.Command(program, args...)
	command.Env = environment
	return invokeCommand(name, command, out)
}

// invokeCommand owns the child wait and private process log. A nonzero completed
// exit is recorded for caller-specific outcome checks. After 10 minutes it
// returns uncertain state without killing the child or initiating recovery.
func invokeCommand(name string, command *exec.Cmd, out string) (op operation, result error) {
	op = operation{Name: name, Program: command.Path, Arguments: command.Args[1:], ExitCode: -1,
		StartedUTC: time.Now().UTC().Format(time.RFC3339Nano)}
	if command.SysProcAttr != nil {
		op.CommandLine = command.SysProcAttr.CmdLine
	}
	log, err := os.OpenFile(filepath.Join(out, name+"-process.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return op, err
	}
	defer func() {
		op.EndedUTC = time.Now().UTC().Format(time.RFC3339Nano)
		result = errors.Join(result, log.Sync(), log.Close())
	}()
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		return op, err
	}
	op.PID = command.Process.Pid
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	select {
	case err := <-finished:
		op.ExitCode = command.ProcessState.ExitCode()
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			return op, err
		}
		return op, nil
	case <-time.After(10 * time.Minute):
		op.TimedOut = true
		return op, errors.New("product operation timed out; no kill/cleanup/retry performed; state may still be changing")
	}
}

func retainNativeLogs(name, out string, u userContext, revision string) (map[string]string, string, error) {
	records := map[string]string{}
	var result error
	for _, filename := range []string{"setup-result.log", "setup-msi.log"} {
		source := filepath.Join(u.State, filename)
		text, err := msilog.ReadText(source)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		destination := filepath.Join(out, name+"-"+filename)
		raw, rawErr := os.ReadFile(source)
		if rawErr != nil {
			result = errors.Join(result, rawErr)
			continue
		}
		copy, copyErr := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if copyErr != nil {
			result = errors.Join(result, copyErr)
			continue
		}
		_, writeErr := copy.Write(raw)
		if err := errors.Join(writeErr, copy.Sync(), copy.Close()); err != nil {
			result = errors.Join(result, err)
		}
		if filename == "setup-result.log" {
			lines := strings.Split(strings.TrimSpace(text), "\n")
			for _, field := range strings.Fields(lines[len(lines)-1]) {
				key, value, ok := strings.Cut(field, "=")
				if !ok || records[key] != "" {
					return records, "", errors.New("native operation result has malformed/duplicate fields")
				}
				records[key] = value
			}
			if records["source"] != revision {
				result = errors.Join(result, errors.New("native result source differs"))
			}
		}
	}
	return records, filepath.Join(out, name+"-setup-msi.log"), result
}

func retainUncertainSetupLogs(name, out string, u userContext) error {
	var result error
	var copied, missing []string
	for _, filename := range []string{"setup-msi.log", "setup-result.log"} {
		input, err := os.Open(filepath.Join(u.State, filename))
		if errors.Is(err, os.ErrNotExist) {
			missing = append(missing, filename)
			continue
		}
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(input, (32<<20)+1))
		readErr = errors.Join(readErr, input.Close())
		if len(data) > 32<<20 {
			readErr = errors.Join(readErr, errors.New("uncertain native log exceeds limit"))
		}
		if readErr != nil {
			result = errors.Join(result, readErr)
			continue
		}
		file, err := os.OpenFile(filepath.Join(out, name+"-potentially-changing-"+filename), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		_, err = file.Write(data)
		copyErr := errors.Join(err, file.Sync(), file.Close())
		result = errors.Join(result, copyErr)
		if copyErr == nil {
			copied = append(copied, filename)
		}
	}
	return errors.Join(result, writeJSON(filepath.Join(out, name+"-uncertain-state.json"), map[string]any{
		"state_may_still_change": true, "no_current_native_result_assumed": true,
		"copied_partial_logs": copied, "missing_logs": missing,
		"no_post_timeout_msi_inventory_or_recovery": true, "scope": "partial raw log observation only; not stable cleanup/rollback proof",
	}))
}

func verifyNativeOutcome(op operation, expectedFailure bool) error {
	if op.Native["contract"] != "recovery-first-v2" || op.Native["install_called"] != "1" {
		return errors.New("actual single-call recovery-first MSI outcome missing")
	}
	text, err := msilog.ReadText(op.MSILog)
	if err != nil {
		return err
	}
	if op.MSILogStart > len(text) {
		return errors.New("MSI operation log was truncated/replaced")
	}
	text = text[op.MSILogStart:]
	if err := msilog.VerifyEnginePolicies(text); err != nil {
		return err
	}
	if !strings.Contains(text, "VX_SETUP_AUTH") || !msilog.ActionCompleted(text, "VxSetupAuthorize") {
		return errors.New("actual owning-API/native MSI session association is unproved")
	}
	if expectedFailure {
		code, err := strconv.Atoi(op.Native["install"])
		if err != nil || code != 1603 || op.ExitCode == 0 || op.Native["usable"] != "0" || op.Native["result"] == "0" {
			return errors.New("deliberate failed upgrade falsely reported success or never reached an actual failing API")
		}

		return nil
	}
	if op.ExitCode != 0 || op.Native["install"] != "0" || op.Native["usable"] != "1" ||
		op.Native["result"] != "0" {
		return fmt.Errorf("actual Setup/MSI/postcondition failed: exit=%d native=%+v", op.ExitCode, op.Native)
	}
	return nil
}

func readOptionalText(path string) (string, error) {
	text, err := msilog.ReadText(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return text, err
}

func seedOwned(u userContext) error {
	files := map[string]string{
		filepath.Join(u.Config, "destination.json"):          `{"destination":"gspro"}` + "\n",
		filepath.Join(u.Config, ".destination-12345.tmp"):    "controlled partial preference\n",
		filepath.Join(u.LocalData, "logs", "bridge.0.jsonl"): "{\"fixture\":\"owned-slot-0\"}\n",
		filepath.Join(u.LocalData, "logs", "bridge.4.jsonl"): "{\"fixture\":\"owned-slot-4\"}\n",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := file.WriteString(content)
		if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
			return err
		}
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	return errors.Join(key.SetStringValue("VxInfiniteTeesBridge", `"`+filepath.Join(u.Root, "GSPconnect.exe")+`" --desktop --target-port 999`), key.Close())
}

func verifyOwned(u userContext) error {
	for path, want := range map[string]string{
		filepath.Join(u.Config, "destination.json"):          `{"destination":"gspro"}` + "\n",
		filepath.Join(u.Config, ".destination-12345.tmp"):    "controlled partial preference\n",
		filepath.Join(u.LocalData, "logs", "bridge.0.jsonl"): "{\"fixture\":\"owned-slot-0\"}\n",
		filepath.Join(u.LocalData, "logs", "bridge.4.jsonl"): "{\"fixture\":\"owned-slot-4\"}\n",
	} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			return fmt.Errorf("owned settings/log fixture not preserved: %s: %v", path, err)
		}
	}
	value, present, err := ownedRun(u)
	if err != nil || !present || value != `"`+filepath.Join(u.Root, "GSPconnect.exe")+`" --desktop --target-port 999` {
		return fmt.Errorf("owned startup fixture not preserved: %v", err)
	}
	return nil
}

func foreignFiles(u userContext) []string {
	return []string{filepath.Join(u.Local, "Programs", "VXBridgeLifecycleForeign", "vendor-like.txt"),
		filepath.Join(u.Roaming, "VXBridgeLifecycleForeign", "keep.txt"),
		filepath.Join(u.Programs, "VXBridgeLifecycleForeign.txt")}
}

func seedForeign(u userContext) error {
	for _, path := range foreignFiles(u) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := file.WriteString("foreign sentinel; never product-owned\n")
		if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
			return err
		}
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, faultKey, registry.SET_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	return errors.Join(key.SetStringValue("ForeignSentinel", "preserve foreign state"), key.Close())
}

func verifyForeign(u userContext) error {
	for _, path := range foreignFiles(u) {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "foreign sentinel; never product-owned\n" {
			return fmt.Errorf("foreign sibling data changed: %s: %v", path, err)
		}
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, faultKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	value, kind, readErr := key.GetStringValue("ForeignSentinel")
	closeErr := key.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	if kind != registry.SZ || value != "preserve foreign state" {
		return errors.New("foreign sibling registry sentinel changed")
	}
	return nil
}

func setFault(armed bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, faultKey, registry.QUERY_VALUE|registry.SET_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	if armed {
		return errors.Join(key.SetStringValue("FailUpgrade", "1"), key.Close())
	}
	value, kind, readErr := key.GetStringValue("FailUpgrade")
	if readErr != nil || kind != registry.SZ || value != "1" {
		return errors.Join(errors.New("test-only failure control is not the expected owned armed value"), readErr, key.Close())
	}
	return errors.Join(key.DeleteValue("FailUpgrade"), key.Close())
}
