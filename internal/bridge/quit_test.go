package bridge

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// Model Accept returning an already accepted source as listener Close races it.
type racingListener struct {
	conn     net.Conn
	release  chan struct{}
	once     sync.Once
	accepted bool
}

func (l *racingListener) Accept() (net.Conn, error) {
	<-l.release
	if !l.accepted {
		l.accepted = true
		return l.conn, nil
	}
	return nil, net.ErrClosed
}
func (l *racingListener) Close() error {
	l.once.Do(func() { close(l.release) })
	return nil
}
func (l *racingListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestQuitWaitsForRacingAcceptedSource(t *testing.T) {
	host, peer := net.Pipe()
	defer peer.Close()
	listener := &racingListener{conn: host, release: make(chan struct{})}
	controller := NewController()
	done := make(chan error, 1)
	cfg := Defaults("127.0.0.1:1")
	cfg.DialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("invented_unavailable_target")
	}
	go func() { done <- Run(listener, cfg, controller, nil, io.Discard, "test") }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := controller.Query(ctx, true); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Second)
	for {
		state, err := controller.Query(ctx, false)
		if err != nil {
			t.Fatal("quit skipped accepted-source admission barrier")
		}
		if state.Source == "attached" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("racing accepted peer not admitted")
		}
		time.Sleep(time.Millisecond)
	}
	peer.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, err := peer.Read(make([]byte, 1)); errors.Is(err, io.EOF) {
		t.Fatal("guarded Quit closed racing source")
	}
	peer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("source departure did not finish guarded Quit")
	}
}
