package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineSummaryKeepsFailureSeparateFromCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invented-msi.log")
	text := "MSI: Machine policy value 'DisableMsi' is 0\r\n" +
		"MSI: Machine policy value 'AlwaysInstallElevated' is 0\r\n" +
		"MSI: User policy value 'AlwaysInstallElevated' is 0\r\n" +
		"MSI: InstallExecute. Return value 1.\r\n" +
		"MSI: InstallFinalize. Return value 3.\r\n" +
		"Executing op: FileCopy\r\nExecuting op: RegAddValue\r\n" +
		"PRIVATE_LIFECYCLE_EXPECTED_POST_EXECUTE_FAILURE\r\nError in rollback skipped\r\n" +
		"Private fake account S-1-5-21-1234 and path C:\\Invented\\Private\\\r\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"-require-policy", path}, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	var got summary
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.EnginePoliciesProved || !got.CompletedActions["InstallExecute"] ||
		got.CompletedActions["InstallFinalize"] || got.FileCopies != 1 || got.RegistryWrites != 1 ||
		got.PrivateFaults != 1 || got.RollbackErrors != 1 || got.DecodedBytes != len(text) ||
		!strings.Contains(got.Scope, "not current state") {
		t.Fatalf("failure was relabelled or observations differ: %+v", got)
	}
	for _, private := range []string{"S-1-5-21-1234", "C:\\Invented", path} {
		if strings.Contains(out.String(), private) {
			t.Fatal("raw log/path data exported:", private)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != text {
		t.Fatal("original log changed:", err)
	}
}

func TestOfflineFailuresAreExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incomplete.log")
	if err := os.WriteFile(path, []byte("unproved policy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		args []string
		code int
	}{
		{[]string{"-h"}, 0},
		{nil, 2},
		{[]string{path, path}, 2},
		{[]string{"-config", path}, 2},
		{[]string{path + ".missing"}, 1},
		{[]string{"-require-policy", path}, 1},
		{[]string{path}, 0},
	} {
		var out, stderr bytes.Buffer
		if code := run(test.args, &out, &stderr); code != test.code {
			t.Fatal(test.args, code, stderr.String())
		}
		if test.code != 0 && stderr.Len() == 0 {
			t.Fatal("failure was silent:", test.args)
		}
	}
	var stderr bytes.Buffer
	if code := run([]string{path}, failedOutput{}, &stderr); code != 1 || stderr.Len() == 0 {
		t.Fatal("output failure was not reported:", code, stderr.String())
	}
}

type failedOutput struct{}

func (failedOutput) Write([]byte) (int, error) { return 0, errors.New("invented output failure") }
