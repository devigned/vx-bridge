//go:build !windows

package managed

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// The portable implementation is for synthetic fixtures, not Windows qualification.
func diagnosticFile(path string, create bool) (*os.File, io.Closer, error) {
	if !filepath.IsAbs(path) {
		return nil, nil, errors.New("diagnostic_absolute_path_required")
	}
	for at := filepath.Dir(path); ; at = filepath.Dir(at) {
		info, err := os.Lstat(at)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errors.New("diagnostic_unsafe_ancestor")
		}
		if at == filepath.Dir(at) {
			break
		}
	}
	if create {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		return file, noPins{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
		return nil, nil, errors.New("diagnostic_regular_file_required")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return nil, nil, errors.Join(errors.New("diagnostic_source_changed"), file.Close())
	}
	return file, noPins{}, nil
}

type noPins struct{}

func (noPins) Close() error { return nil }

func diagnosticReceipt() (Receipt, string, error) {
	return Receipt{}, "windows_required", errors.New("Windows_required")
}
