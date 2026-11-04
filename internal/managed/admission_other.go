//go:build !windows

package managed

import "io"

// Admit returns (nil, nil) on non-Windows systems without inspecting or
// authorizing a Windows installation.
// The executable separately refuses host mode outside Windows amd64.
func Admit(image, revision, version string) (io.Closer, error) { return nil, nil }
