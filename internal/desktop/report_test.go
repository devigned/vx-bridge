package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
)

func TestReportingControlsRemainAvailableWithoutHostStatus(t *testing.T) {
	for _, available := range []bool{false, true} {
		seen := map[Action]bool{}
		for _, item := range menu(bridge.Snapshot{}, available).Items {
			if item.Action == SaveReport || item.Action == ReportBug {
				if item.Disabled {
					t.Fatal("reporting gated on a healthy host")
				}
				seen[item.Action] = true
			}
		}
		if !seen[SaveReport] || !seen[ReportBug] {
			t.Fatal("reporting controls absent")
		}
	}
	for _, result := range []string{"saved", "saved_partial", "cancelled"} {
		if !strings.Contains(reportNotice(result, nil), "Nothing was sent") {
			t.Fatal("reporting copy suggests an upload")
		}
	}
	text := reportNotice("", errors.New("PRIVATE_path"))
	if strings.Contains(text, "PRIVATE") || !strings.Contains(text, "without a ZIP") || !strings.Contains(text, "new file name") {
		t.Fatal("failure is private, success-shaped, or unactionable")
	}
}

func TestBlockedDiagnosticDialogDoesNotBlockStatusSelectionOrQuit(t *testing.T) {
	r := testRuntime(t)
	entered, release := make(chan struct{}), make(chan struct{})
	r.saveReport = func() (string, error) { close(entered); <-release; return "saved_partial", nil }
	r.reportBug = func() error { return errors.New("PRIVATE_browser_failure") }
	quitSeen, selectSeen, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case request := <-r.controller.Requests:
				if request.Quit {
					close(quitSeen)
				}
				if request.Destination != "" {
					request.Result <- nil
					close(selectSeen)
				}
				request.Reply <- bridge.Snapshot{Version: "synthetic", Source: "attached"}
			case <-r.controller.Done:
				return
			}
		}
	}()
	r.Start()
	until := time.Now().Add(time.Second)
	for {
		r.Request(SaveReport)
		select {
		case <-entered:
			goto saving
		case <-time.After(time.Millisecond):
		}
		if time.Now().After(until) {
			t.Fatal("support worker did not admit request")
		}
	}
saving:
	r.Request(SelectStandby)
	r.Request(Quit)
	for _, observed := range []chan struct{}{selectSeen, quitSeen} {
		select {
		case <-observed:
		case <-time.After(time.Second):
			t.Fatal("diagnostics blocked bridge controls")
		}
	}
	until = time.Now().Add(time.Second)
	for {
		var state bridge.Snapshot
		r.Decorate(&state)
		if state.DestinationPreference == "destination_preference_save_failed" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("selection worker did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	until = time.Now().Add(time.Second)
	for !strings.Contains(fmt.Sprint(r.View().Items), "Partial diagnostic report saved") {
		if time.Now().After(until) {
			t.Fatal("partial result hidden")
		}
		time.Sleep(time.Millisecond)
	}
	close(r.controller.Done)
	<-done
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
