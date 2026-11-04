package managed

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func diagnosticTemp(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiagnosticInputsAreBoundedReadOnlyAndRejectLinks(t *testing.T) {
	dir := diagnosticTemp(t)
	path := filepath.Join(dir, "owned.jsonl")
	original := []byte("synthetic original")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadDiagnosticFile(context.Background(), path, int64(len(original)))
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("exact byte limit failed", err)
	}
	if _, err := ReadDiagnosticFile(context.Background(), path, int64(len(original)-1)); !errors.Is(err, ErrDiagnosticSize) {
		t.Fatal("oversized source not explicitly refused", err)
	}
	if _, err := ReadDiagnosticFile(context.Background(), dir, 1024); err == nil {
		t.Fatal("directory accepted as a log")
	}
	link := filepath.Join(dir, "hardlink")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, link} {
		if _, err := ReadDiagnosticFile(context.Background(), name, 1024); err == nil {
			t.Fatal("hardlink accepted")
		}
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("collection mutated its source", err)
	}
	symlink := filepath.Join(dir, "symlink")
	if err := os.Symlink(path, symlink); err != nil {
		t.Skip("symlink permission unavailable")
	}
	if _, err := ReadDiagnosticFile(context.Background(), symlink, 1024); err == nil {
		t.Fatal("source symlink accepted")
	}
}

func TestDiagnosticOutputsRejectExistingFilesAndRedirectedParents(t *testing.T) {
	dir := diagnosticTemp(t)
	path := filepath.Join(dir, "report.zip")
	file, pins, err := CreateDiagnosticFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write([]byte("synthetic"))
	if err := errors.Join(writeErr, file.Close(), pins.Close()); err != nil {
		t.Fatal(err)
	}
	if file, pins, err := CreateDiagnosticFile(path); err == nil {
		file.Close()
		pins.Close()
		t.Fatal("existing output replaced")
	}
	parent := filepath.Join(dir, "redirect")
	if err := os.Symlink(dir, parent); err != nil {
		t.Skip("symlink permission unavailable")
	}
	if file, pins, err := CreateDiagnosticFile(filepath.Join(parent, "escaped.zip")); err == nil {
		file.Close()
		pins.Close()
		t.Fatal("redirected output parent accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.zip")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refusal created a file through a symlink", err)
	}
}
