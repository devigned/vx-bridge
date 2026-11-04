package desktop

import (
	"context"
	"errors"
	"time"

	"github.com/devigned/vx-bridge/internal/diagnostics"
)

// ReportDialog saves one diagnostic ZIP through native save/cancel controls.
// It returns saved, saved_partial, or cancelled, or an explicit failure.
// The caller must run it outside the engine and native tray owner threads.
func ReportDialog(build diagnostics.Build) (string, error) {
	return reportDialog(build)
}

func saveReport(path string, collector *diagnostics.Collector) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), diagnostics.CollectionTimeout+2*time.Second)
	defer cancel()
	report, err := collector.Collect(ctx)
	if err != nil {
		return "", err
	}
	if err := diagnostics.Save(ctx, path, report); err != nil {
		return "", err
	}
	if report.Manifest.Partial {
		return "saved_partial", nil
	}
	return "saved", nil
}

func reportNotice(result string, err error) string {
	if err != nil {
		return "The diagnostic report could not be saved.\n" +
			"Choose a new file name in a local folder you can write to, then try again. An incomplete file may remain.\n" +
			"Nothing was sent. You can still report the bug without a ZIP."
	}
	if result == "saved_partial" {
		return "A partial diagnostic report was saved locally.\n" +
			"Some sources were missing or unavailable; summary.txt explains which ones.\n" +
			"Nothing was sent. Review the ZIP before attaching it to your bug report."
	}
	return "The diagnostic report was saved locally.\n" +
		"Nothing was sent. Review the ZIP before attaching it to your bug report."
}

var errReportUnsupported = errors.New("diagnostic_native_dialog_requires_windows")
