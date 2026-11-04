//go:build !windows

package desktop

import "github.com/devigned/vx-bridge/internal/diagnostics"

func reportDialog(diagnostics.Build) (string, error) { return "", errReportUnsupported }
func openBugForm() error                             { return errReportUnsupported }
