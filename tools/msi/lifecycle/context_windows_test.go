package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestKnownFoldersUseActualTokenNotInheritedHarnessEnvironment(t *testing.T) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_QUERY|windows.TOKEN_IMPERSONATE|windows.TOKEN_DUPLICATE, &token); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := token.Close(); err != nil {
			t.Error(err)
		}
	}()
	profile, err := token.GetUserProfileDirectory()
	if err != nil {
		t.Fatal(err)
	}
	profile, err = filepath.EvalSymlinks(profile)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := token.Environ(false)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{}
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			expected[strings.ToUpper(key)] = value
		}
	}
	foreign := t.TempDir()
	for _, key := range []string{"USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP"} {
		t.Setenv(key, foreign)
	}
	var u userContext
	if err := resolveUserFolders(token, &u); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(u.Profile, profile) {
		t.Fatalf("profile=%q actual token=%q", u.Profile, profile)
	}
	for _, path := range []string{u.Local, u.Roaming, u.Programs} {
		if !within(path, profile) || strings.EqualFold(path, foreign) {
			t.Fatalf("Known Folder adopted inherited harness path: %q", path)
		}
	}
	for key, path := range u.Environment {
		if path != expected[key] || os.Getenv(key) != expected[key] || strings.EqualFold(path, foreign) {
			t.Fatalf("%s adopted inherited harness path: %q", key, path)
		}
	}
}
