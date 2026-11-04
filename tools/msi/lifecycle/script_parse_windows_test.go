package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLifecycleScriptsParseWithoutExecution(t *testing.T) {
	for _, name := range []string{"setup-lifecycle-author.ps1", "setup-lifecycle-run.ps1", "setup-lifecycle-policy.ps1"} {
		path, err := filepath.Abs(name)
		if err != nil {
			t.Fatal(err)
		}
		script := "$tokens=$null;$errors=$null;[void][System.Management.Automation.Language.Parser]::ParseFile('" +
			strings.ReplaceAll(path, "'", "''") + "',[ref]$tokens,[ref]$errors);if(@($errors).Count){$errors|Out-String|Write-Output;exit 1}"
		command := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
			"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("parse-only %s: %v\n%s", name, err, output)
		}
	}
}
