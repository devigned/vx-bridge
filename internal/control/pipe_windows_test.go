package control

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func TestPrivatePipeACLExclusiveBindAndSameUserAccess(t *testing.T) {
	_, acl, err := pipeIdentity()
	if err != nil {
		t.Fatal(err)
	}

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	if acl != "O:"+sid+"G:"+sid+"D:P(A;;GA;;;SY)(A;;GA;;;"+sid+")" || strings.Contains(acl, "WD") {
		t.Fatal("control pipe ACL grants more than owner and SYSTEM")
	}
	if _, err := windows.SecurityDescriptorFromString(acl); err != nil {
		t.Fatal(err)
	}
	// Never bind or query the runtime's real control path in automated tests.
	path := fmt.Sprintf(`\\.\pipe\VxBridge.P01.TEST.%d.%d`, os.Getpid(), time.Now().UnixNano())
	listener, err := listenPipe(path, acl)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if second, err := listenPipe(path, acl); err == nil {
		second.Close()
		t.Fatal("duplicate pipe server replaced active owner")
	}
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(time.Second))
			_, err = io.WriteString(conn, "private-status")
		}
		done <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client, err := winio.DialPipeContext(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(time.Second))
	data := make([]byte, len("private-status"))
	if _, err := io.ReadFull(client, data); err != nil || string(data) != "private-status" {
		t.Fatal("current user could not read private pipe")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSimultaneousPrivateInstanceAdmissionHasOneOwner(t *testing.T) {
	_, acl, err := pipeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf(`\\.\pipe\VxBridge.P02.ADMISSION.TEST.%d.%d`, os.Getpid(), time.Now().UnixNano())
	start, release := make(chan struct{}), make(chan struct{})
	attempted := make(chan struct{}, 16)
	var winners atomic.Uint64
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Go(func() {
			<-start
			listener, err := listenPipe(path, acl)
			if err == nil {
				winners.Add(1)
			}
			attempted <- struct{}{}
			if err == nil {
				<-release
				listener.Close()
			}
		})
	}
	close(start)
	for i := 0; i < 16; i++ {
		select {
		case <-attempted:
		case <-time.After(2 * time.Second):
			close(release)
			workers.Wait()
			t.Fatal("private instance admission did not finish")
		}
	}
	close(release)
	workers.Wait()
	if winners.Load() != 1 {
		t.Fatal("simultaneous duplicate launches admitted multiple owners")
	}
}
