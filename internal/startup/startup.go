// Package startup manages the current user's owned Windows sign-in registration
// for GSPconnect.exe. [Manager] reads without writing and toggles only on an
// explicit desktop action under a cross-process ownership lock.
// Registration is separate from game selection and does not prove that Windows
// policy will launch the bridge. Non-Windows builds require an injected Backend.
package startup

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// Store is used only while Backend.Locked holds the cross-process ownership
// lock. Tests inject a fake or a private HKCU key, never the real Run key.
type Store interface {
	// Read returns the registered command and whether the owned value exists.
	// A missing value is not an error; unsupported value types are errors.
	Read() (string, bool, error)
	// Write replaces the owned value with a command.
	Write(string) error
	// Delete removes the owned value, not the entire registry key.
	Delete() error
}

// Backend serializes ownership-sensitive access to a Store. A Store must not
// escape the Locked callback or be used outside its lock.
type Backend interface {
	// Locked invokes the callback while holding the shared ownership lock
	// and returns callback or lock/cleanup errors. Implementations must not
	// invoke concurrent callbacks against the same registration.
	Locked(func(Store) error) error
}

// Manager compares and changes one exact startup command. Construct it with
// New and a non-nil Backend; the zero value is not usable. Locking and concurrent
// access guarantees depend on the supplied Backend.
type Manager struct {
	backend Backend
	command string
}

// Command returns the quoted desktop startup command for a clean absolute
// GSPconnect.exe path and an Infinite Tees port in 1..65535 other than 12321.
// Invalid paths/arguments or commands exceeding 260 UTF-16 code units return
// errors. Validation uses the host's filepath rules; it does not inspect the
// executable or write registration.
func Command(image string, port int) (string, error) {
	if !filepath.IsAbs(image) || filepath.Clean(image) != image ||
		!strings.EqualFold(filepath.Base(image), "GSPconnect.exe") ||
		strings.ContainsAny(image, "\"\x00\r\n") || port < 1 || port > 65535 || port == 12321 {
		return "", errors.New("invalid_startup_image_or_arguments")
	}
	command := fmt.Sprintf("\"%s\" --desktop --target-port %d", image, port)
	if len(utf16.Encode([]rune(command))) > 260 {
		return "", errors.New("startup_command_exceeds_Windows_Run_limit")
	}
	return command, nil
}

// New validates Command and returns a Manager without reading or changing
// registration. backend must be non-nil; New does not validate that dependency.
func New(backend Backend, image string, port int) (*Manager, error) {
	command, err := Command(image, port)
	if err != nil {
		return nil, err
	}
	return &Manager{backend: backend, command: command}, nil
}

// State returns "startup_off", "startup_registered_policy_applies", or
// "startup_conflict_not_changed" under the ownership lock without writing.
// Read/lock failures return "startup_unavailable" and the underlying error.
func (m *Manager) State() (string, error) {
	var state string
	err := m.backend.Locked(func(store Store) error {
		value, exists, err := store.Read()
		if err != nil {
			return err
		}
		state = m.state(value, exists)
		return nil
	})
	if err != nil {
		return "startup_unavailable", err
	}
	return state, nil
}

func (m *Manager) state(value string, exists bool) string {
	if !exists {
		return "startup_off"
	}
	if value == m.command {
		return "startup_registered_policy_applies"
	}
	return "startup_conflict_not_changed"
}

// Toggle is only called for an explicit tray action. It rechecks ownership
// under the shared lock; no initialization/read path writes registration.
// It adds a missing value or deletes its exact command, then verifies by reading.
// A different command is never overwritten and returns a conflict error.
// Other write/read/lock failures return "startup_change_failed_use_status";
// failure does not promise the previous registration was restored.
func (m *Manager) Toggle() (string, error) {
	var state string
	err := m.backend.Locked(func(store Store) error {
		value, exists, err := store.Read()
		if err != nil {
			return err
		}
		state = m.state(value, exists)
		switch state {
		case "startup_conflict_not_changed":
			return errors.New(state)
		case "startup_registered_policy_applies":
			if err := store.Delete(); err != nil {
				return err
			}
		case "startup_off":
			if err := store.Write(m.command); err != nil {
				return err
			}
		}
		expected := "startup_registered_policy_applies"
		if exists {
			expected = "startup_off"
		}
		value, exists, err = store.Read()
		if err != nil {
			return err
		}
		state = m.state(value, exists)
		if state != expected {
			return errors.New("startup_change_not_verified")
		}
		return nil
	})
	if err != nil {
		if state != "startup_conflict_not_changed" {
			state = "startup_change_failed_use_status"
		}
		return state, err
	}
	return state, nil
}
