// Package msilog reads existing installer logs without invoking Windows Installer.
// It decodes bounded local files and checks recorded action/policy observations
// for the offline msi-log command and private qualification worker. Log text
// alone does not prove current registration, recovery, cleanup, or live safety.
package msilog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"
)

// MaxBytes is the maximum input log size, in bytes, read by ReadText.
const MaxBytes = 32 << 20

// ReadText reads a regular local file of at most MaxBytes and closes it.
// A UTF-16LE BOM selects UTF-16 decoding; an odd byte count then returns a
// truncation error. Other files are returned as byte-preserving strings without
// encoding detection. Stat/open/read/close and size/type errors are returned.
// The path-level regular-file check is not a handle-pinning security boundary.
func ReadText(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return "", errors.New("require a regular MSI log no larger than 32 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return "", err
	}
	if len(data) > MaxBytes {
		return "", errors.New("bounded MSI log exceeded")
	}
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		if len(data)%2 != 0 {
			return "", errors.New("truncated UTF-16 MSI log")
		}
		values := make([]uint16, (len(data)-2)/2)
		for index := range values {
			values[index] = uint16(data[2+index*2]) | uint16(data[3+index*2])<<8
		}
		return string(utf16.Decode(values)), nil
	}
	return string(data), nil
}

// ActionCompleted reports whether a line in log contains action's completion
// marker and "Return value 1.". It is a text observation, not verification that
// the whole installation succeeded or its resulting state is still intact.
func ActionCompleted(log, action string) bool {
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, ": "+action+".") && strings.Contains(line, "Return value 1.") {
			return true
		}
	}
	return false
}

// VerifyEnginePolicies requires recorded machine DisableMsi, machine
// AlwaysInstallElevated, and user AlwaysInstallElevated values to be present
// and zero. A missing or nonzero observation returns an error containing log
// context. It does not read current policy, authenticate the log, or redact it;
// callers must keep potentially sensitive installer evidence private.
func VerifyEnginePolicies(text string) error {
	for _, prefix := range []string{
		"Machine policy value 'DisableMsi' is ",
		"Machine policy value 'AlwaysInstallElevated' is ",
		"User policy value 'AlwaysInstallElevated' is ",
	} {
		found := false
		for _, line := range strings.Split(text, "\n") {
			at := strings.Index(line, prefix)
			if at < 0 {
				continue
			}
			found = true
			if strings.TrimSpace(line[at+len(prefix):]) != "0" {
				return fmt.Errorf("actual MSI engine policy is not0: %s", line)
			}
		}
		if !found {
			return fmt.Errorf("actual MSI engine policy proof missing: %s", prefix)
		}
	}
	return nil
}
