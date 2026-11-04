//go:build !windows

package desktop

import (
	"errors"
	"fmt"
	"os"

	"github.com/devigned/vx-bridge/internal/identity"
)

func nativeTray(*Runtime) identity.Tray { return nil }
func openLogDirectory(string) error     { return errors.New("Windows_required") }

// Notify writes text to standard error on non-Windows systems, returning any
// write error. It does not create a native dialog.
func Notify(text string) error {
	_, err := fmt.Fprintln(os.Stderr, text)
	return err
}

// DuplicateMessage formats a non-Windows notice that the existing host was not
// changed, with state and the distinct status/Quit command names.
func DuplicateMessage(state string) string {
	return "Existing host unchanged; " + state + "; use vxbridge --status / --quit."
}
