package main

import (
	"net"
	"syscall"
	"testing"
)

func TestWindowsExclusiveAddressUseRejectsReuseBind(t *testing.T) {
	listener, err := listenExclusive("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	raw, err := listener.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var option int
	var socketErr error
	if err := raw.Control(func(socket uintptr) {
		option, socketErr = syscall.GetsockoptInt(syscall.Handle(socket), syscall.SOL_SOCKET, ^syscall.SO_REUSEADDR)
	}); err != nil {
		t.Fatal(err)
	}
	if socketErr != nil || option != 1 {
		t.Fatalf("SO_EXCLUSIVEADDRUSE=%d err=%v", option, socketErr)
	}
	socket, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Closesocket(socket)
	if err := syscall.SetsockoptInt(socket, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Bind(socket, &syscall.SockaddrInet4{
		Port: listener.Addr().(*net.TCPAddr).Port,
		Addr: [4]byte{127, 0, 0, 1},
	}); err == nil {
		t.Fatal("SO_REUSEADDR socket acquired the probe's exclusive address")
	}
}
