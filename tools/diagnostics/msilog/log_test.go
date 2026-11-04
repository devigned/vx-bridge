package msilog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestActualEnginePolicyProof(t *testing.T) {
	good := "MSI (s): Machine policy value 'DisableMsi' is 0\n" +
		"MSI (s): Machine policy value 'AlwaysInstallElevated' is 0\n" +
		"MSI (s): User policy value 'AlwaysInstallElevated' is 0\n"
	for _, newline := range []string{"\n", "\r\n"} {
		text := strings.ReplaceAll(good, "\n", newline)
		if err := VerifyEnginePolicies(text); err != nil {
			t.Fatal(err)
		}
		for _, invalid := range []string{
			"", strings.Replace(text, "'DisableMsi' is 0", "'DisableMsi' is 1", 1),
			strings.Replace(text, "'AlwaysInstallElevated' is 0", "'AlwaysInstallElevated' is 1", 1),
			text + "MSI (s): Machine policy value 'DisableMsi' is 1" + newline,
			strings.Replace(text, "User policy value", "Unproved user value", 1),
		} {
			if VerifyEnginePolicies(invalid) == nil {
				t.Fatalf("unproved/changed policy admitted: %q", invalid)
			}
		}
	}
}

func TestReadTextPreservesBytesAndDecodesNativeMSILog(t *testing.T) {
	text := "MSI: VxSetupAuthorize. Return value 1.\r\nInvented \u00e9vidence\r\n"
	wide := []byte{0xff, 0xfe}
	for _, c := range utf16.Encode([]rune(text)) {
		wide = append(wide, byte(c), byte(c>>8))
	}
	for name, data := range map[string][]byte{"utf8": []byte(text), "utf16": wide, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name+".log")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := ReadText(path)
			want := text
			if name == "empty" {
				want = ""
			}
			if err != nil || got != want {
				t.Fatalf("decode differs: %q, %v", got, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(data) {
				t.Fatal("read-only inspection changed original bytes:", err)
			}
		})
	}
}

func TestReadTextBoundAndMalformedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.log")
	if _, err := ReadText(path); err == nil {
		t.Fatal("missing log silently accepted")
	}
	if _, err := ReadText(filepath.Dir(path)); err == nil {
		t.Fatal("directory accepted as a log")
	}
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 'A'}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadText(path); err == nil {
		t.Fatal("truncated UTF-16 accepted")
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxBytes); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if text, err := ReadText(path); err != nil || len(text) != MaxBytes {
		t.Fatal("exact 32 MiB bound was not readable:", err)
	}
	file, err = os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadText(path); err == nil {
		t.Fatal("oversize log accepted")
	}
}

func TestActionRequiresActualCompletedRecord(t *testing.T) {
	for _, text := range []string{"", "Doing action: InstallExecute", "MSI: InstallExecute. Return value 3.",
		"MSI: NotInstallExecute. Return value 1.", "MSI: InstallExecuteMore. Return value 1."} {
		if ActionCompleted(text, "InstallExecute") {
			t.Fatal("noncompletion was counted:", text)
		}
	}
	if !ActionCompleted("MSI: InstallExecute. Return value 1.\r\n", "InstallExecute") {
		t.Fatal("actual successful action record lost")
	}
}
