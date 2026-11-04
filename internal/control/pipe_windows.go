package control

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func pipeIdentity() (string, string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", "", err
	}
	sid := user.User.Sid.String()
	return `\\.\pipe\VxInfiniteTeesBridge.P01.` + sid,
		"O:" + sid + "G:" + sid + "D:P(A;;GA;;;SY)(A;;GA;;;" + sid + ")", nil
}

// Listen opens the current user's first-instance-exclusive control pipe.
// Its protected ACL permits that user and SYSTEM, and remote clients are
// rejected. Token/pipe creation errors are returned without changing an
// existing host. The caller owns the returned listener and must close it.
func Listen() (net.Listener, error) {
	path, acl, err := pipeIdentity()
	if err != nil {
		return nil, err
	}
	// go-winio v0.6.4 uses FILE_CREATE for first-instance exclusivity and
	// FILE_PIPE_REJECT_REMOTE_CLIENTS for every server handle.
	return listenPipe(path, acl)
}

func listenPipe(path, acl string) (net.Listener, error) {
	return winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: acl, InputBufferSize: 4096, OutputBufferSize: responseLimit,
	})
}

func dial(ctx context.Context) (net.Conn, error) {
	path, _, err := pipeIdentity()
	if err != nil {
		return nil, err
	}
	return winio.DialPipeContext(ctx, path)
}
