package managed

import (
	"context"
	"errors"
	"io"
	"os"
)

// ErrDiagnosticSize means the fixed diagnostic input byte budget was exceeded.
var ErrDiagnosticSize = errors.New("diagnostic_source_size_limit")

// ReadDiagnosticFile reads a bounded regular single-link file under pinned,
// non-reparse ancestors on Windows. It never creates or modifies a source.
// Callers select only fixed application-owned names, not vendor or user input.
func ReadDiagnosticFile(ctx context.Context, path string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, pins, err := diagnosticFile(path, false)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	err = errors.Join(readErr, file.Close(), pins.Close(), ctx.Err())
	if len(data) > int(limit) {
		err = errors.Join(err, ErrDiagnosticSize)
	}
	return data, err
}

// CreateDiagnosticFile exclusively creates a new local report under verified
// ancestors. The caller must close both file and pins. It never overwrites an
// existing file, follows a reparse point, or creates parent directories.
func CreateDiagnosticFile(path string) (*os.File, io.Closer, error) {
	return diagnosticFile(path, true)
}

// DiagnosticReceipt reads only the installed receipt and Pending presence.
// It does not acquire a host lease, admit a host, query MSI, or alter state.
// state is a fixed observation code; a valid receipt is not proof of health.
func DiagnosticReceipt() (receipt Receipt, state string, err error) {
	return diagnosticReceipt()
}
