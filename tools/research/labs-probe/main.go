// Vx-labs-probe preserves a frozen Windows x64 Labs research instrument, not
// the persistent bridge, an ordinary diagnostic, or an installation tool.
// Do not rerun it against Labs as a development check. Its STOP record and
// source-first-v3 observations remain historical evidence, not run permission.
//
// The historical executable accepts one source, retains bounded redacted
// observations, and writes local 200 acknowledgments without forwarding to a
// game or synthesizing readiness. Its total-input/network/process bounds can
// still force teardown beneath an attached source; the bridge's guarded reset
// policy is not assigned to this probe. Offline invented fixtures are the
// maintenance entry point. A nonfinal goal is inconclusive, and a nonzero process
// exit overrides any saved receiver-local result.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"
)

const (
	maxLifetime    = 5 * time.Minute
	shutdownBudget = 5 * time.Second
)

var buildRevision = "development"

func main() {
	started := time.Now()
	// Also bounds blocked console/filesystem I/O. Exit 13 overrides any saved result.
	time.AfterFunc(time.Until(started.Add(maxLifetime)), func() { os.Exit(hardDeadline.exit) })
	os.Exit(run(started))
}

func run(started time.Time) int {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" || len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "vx-labs-probe: Windows x64 research console only; launch without arguments.")
		return unsupported.exit
	}
	local := os.Getenv("LOCALAPPDATA")
	if !filepath.IsAbs(local) {
		fmt.Fprintln(os.Stderr, "vx-labs-probe: reporting_failed: LOCALAPPDATA must be an absolute Windows path.")
		return reportFailed.exit
	}
	report, err := newReport(filepath.Join(local, "VxBridgeResearch", "probe"), started, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vx-labs-probe: reporting_failed: cannot create the per-run summary.")
		return reportFailed.exit
	}
	if _, err := fmt.Fprintf(os.Stdout,
		"vx-labs-probe: RESEARCH ONLY, not a simulator. No forwarding, 201, 202 or inferred readiness.\n"+
			"MODE: research-only-v3-source-first; native window only after exclusive bind.\n"+
			"Run %s; bind %s; one client; 1 MiB total input; 5 minute hard lifetime.\n"+
			"Absolute network deadline: %s; hard process deadline: %s.\n"+
			"After two-shot evidence, disconnect or close Labs normally well before the deadline. "+
			"Forced teardown while attached remains a crash risk; crash avoidance is NOT proved.\n"+
			"Nonzero process exit overrides any saved result. An unfinished summary is INCONCLUSIVE.\n"+
			"Do not close the console before the result.\n",
		filepath.Base(report.dir), listenAddress, report.NetworkDeadlineUTC, report.HardDeadlineUTC); err != nil {
		return finish(report, reportFailed)
	}
	listener, err := listenExclusive(listenAddress)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vx-labs-probe: bind_failed on %s: %v; no other process was changed.\n", listenAddress, err)
		return finish(report, bindFailed)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result := runDiagnostic(listener, limits{
		context:  ctx,
		deadline: started.Add(maxLifetime - shutdownBudget),
		write:    2 * time.Second,
		budget:   receiveBudget,
	}, report, startCompatibilityWindow)
	return finish(report, result)
}

func finish(report *runReport, result outcome) int {
	if report.Lifecycle.ForcedTeardownRisk {
		if _, err := fmt.Fprintln(report.console,
			"vx-labs-probe: INCONCLUSIVE lifecycle: receiver teardown without observed clean source EOF; "+
				"source may still be attached. Forced teardown risk is not a clean shutdown."); err != nil {
			result = reportFailed
		}
	}
	if report.printWindow() != nil {
		report.warnReportingFailure()
		result = reportFailed
	}
	if _, err := fmt.Fprintf(report.console, "vx-labs-probe: receiver-local candidate %s (exit %d), pending final summary publication; "+
		"distinct ball IDs=%d, complete 200 writes=%d. "+
		"Source EOF observed=%t; forced teardown risk=%t. "+
		"A positive result is receiver-local only: EOF proves neither owner intent nor Labs survival, "+
		"simulator readiness or end-to-end forwarding.\n",
		result.name, result.exit, report.Counts.DistinctBall, report.Counts.Acks,
		report.Lifecycle.SourceEOF, report.Lifecycle.ForcedTeardownRisk); err != nil {
		report.warnReportingFailure()
		result = reportFailed
	}
	if err := report.finish(result); err != nil {
		fmt.Fprintln(os.Stderr, "vx-labs-probe: reporting_failed: cannot write/sync/publish the final summary; result is incomplete.")
		return reportFailed.exit
	}
	return result.exit
}
