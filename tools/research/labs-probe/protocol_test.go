package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// Invented fixtures, not captures or a shot-generation feature of the executable.
func envelope(id string, ball, club, heartbeat bool) string {
	ballData, clubData := "null", "null"
	if ball {
		ballData = `{"Speed":111.125,"Text":"brace } quote \" slash \\ snow \u2603"}`
	}
	if club {
		clubData = `{"Speed":88.5}`
	}
	return fmt.Sprintf(`{"DeviceID":"private-device","Units":"Yards","ShotNumber":%s,`+
		`"APIversion":"1","ClubIndex":7,"BallData":%s,"ClubData":%s,"ShotDataOptions":{`+
		`"ContainsBallData":%t,"ContainsClubData":%t,"LaunchMonitorIsReady":false,`+
		`"LaunchMonitorBallDetected":false,"IsHeartBeat":%t}}`,
		id, ballData, clubData, ball, club, heartbeat)
}

func streamFor(reader io.Reader, limit int) (*objectStream, *budgetReader) {
	budget := &budgetReader{reader: reader, limit: limit}
	return &objectStream{reader: bufio.NewReaderSize(budget, 4096)}, budget
}

type oneByteReader struct{ io.Reader }

func (r oneByteReader) Read(p []byte) (int, error) {
	return r.Reader.Read(p[:1])
}

func TestFragmentedConcatenatedNativeMessages(t *testing.T) {
	input := "\r\n " + envelope("0", false, false, false) +
		envelope("9007199254740992", true, true, false) + "\t " +
		envelope("9007199254740993", true, false, false) + " \n"
	readers := []io.Reader{strings.NewReader(input), oneByteReader{strings.NewReader(input)}}
	for split := 0; split <= len(input); split++ {
		readers = append(readers, io.MultiReader(strings.NewReader(input[:split]), strings.NewReader(input[split:])))
	}
	for i, reader := range readers {
		stream, budget := streamFor(reader, receiveBudget)
		for j, kind := range []string{"status", "ball", "ball"} {
			frame, err := stream.next()
			if err != nil {
				t.Fatalf("reader %d object %d: %v", i, j, err)
			}
			msg, err := inspect(frame)
			if err != nil || msg.kind != kind {
				t.Fatalf("reader %d object %d: kind=%s err=%v", i, j, msg.kind, err)
			}
			if j > 0 && (msg.flags.LaunchMonitorIsReady || msg.flags.LaunchMonitorBallDetected) {
				t.Fatal("post-impact readiness flags changed")
			}
		}
		if _, err := stream.next(); !errors.Is(err, io.EOF) {
			t.Fatalf("reader %d: expected EOF, got %v", i, err)
		}
		if budget.read != len(input) {
			t.Fatalf("reader %d: bytes=%d want %d", i, budget.read, len(input))
		}
	}
}

func TestMalformedFraming(t *testing.T) {
	for name, input := range map[string]string{
		"array_root":       `[{}]`,
		"garbage":          `x{}`,
		"incomplete":       `{"a":`,
		"invalid_number":   `{"a":01}`,
		"wrong_closer":     `{"a":[}]}`,
		"duplicate":        `{"a":1,"a":2}`,
		"escaped_key":      `{"a":1,"\u0061":2}`,
		"nested_duplicate": `{"ignored":{"a":1,"a":2}}`,
		"invalid_utf8":     "{\"a\":\"\xff\"}",
		"depth":            `{"a":` + strings.Repeat("[", maxDepth) + `0` + strings.Repeat("]", maxDepth) + `}`,
	} {
		t.Run(name, func(t *testing.T) {
			stream, _ := streamFor(strings.NewReader(input), receiveBudget)
			if _, err := stream.next(); !errors.Is(err, errFraming) {
				t.Fatalf("got %v, want framing failure", err)
			}
		})
	}
}

func TestTotalReceiveBudget(t *testing.T) {
	for name, input := range map[string]string{
		"whitespace": strings.Repeat(" ", receiveBudget+1),
		"object":     `{"a":"` + strings.Repeat("x", receiveBudget) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			stream, budget := streamFor(strings.NewReader(input), receiveBudget)
			if _, err := stream.next(); !errors.Is(err, errBudget) {
				t.Fatalf("got %v, want receive budget failure", err)
			}
			if budget.read != receiveBudget {
				t.Fatalf("read %d bytes; budget %d", budget.read, receiveBudget)
			}
		})
	}
	// Read-ahead does not receive an extra byte to detect an exhausted budget.
	stream, budget := streamFor(strings.NewReader("{}{}x"), 4)
	for range 2 {
		if _, err := stream.next(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := stream.next(); !errors.Is(err, errBudget) || budget.read != 4 {
		t.Fatalf("err=%v read=%d", err, budget.read)
	}
}

func TestExactIntegerIDs(t *testing.T) {
	for _, id := range []string{"0", "-0", "-27", "9007199254740992", "9007199254740993", strings.Repeat("9", 500)} {
		msg, err := inspect([]byte(envelope(id, true, false, false)))
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		expected := id
		if id == "-0" {
			expected = "0"
		}
		if msg.id != expected {
			t.Fatalf("identifier was changed: got %s want %s", msg.id, expected)
		}
	}
	for _, id := range []string{`"1"`, "1.0", "1e3", "true", "null"} {
		if _, err := inspect([]byte(envelope(id, true, false, false))); !errors.Is(err, errShape) {
			t.Fatalf("%s: expected shape failure, got %v", id, err)
		}
	}
}

func TestNativeShapeValidation(t *testing.T) {
	valid := envelope("1", true, true, false)
	for name, input := range map[string]string{
		"missing_options": strings.Replace(valid, `"ShotDataOptions"`, `"UnqualifiedOptions"`, 1),
		"missing_flag":    strings.Replace(valid, `"ContainsBallData":true,`, "", 1),
		"null_flag":       strings.Replace(valid, `"ContainsBallData":true`, `"ContainsBallData":null`, 1),
		"string_flag":     strings.Replace(valid, `"ContainsBallData":true`, `"ContainsBallData":"true"`, 1),
		"wrong_body":      strings.Replace(valid, `"ClubData":{"Speed":88.5}`, `"ClubData":[]`, 1),
		"null_ball":       strings.Replace(envelope("1", false, true, false), `"ContainsBallData":false`, `"ContainsBallData":true`, 1),
		"numeric_api":     strings.Replace(valid, `"APIversion":"1"`, `"APIversion":1`, 1),
		"missing_root":    strings.Replace(valid, `"Units":"Yards",`, "", 1),
		"heartbeat_ball":  envelope("1", true, false, true),
	} {
		t.Run(name, func(t *testing.T) {
			if !strictJSON([]byte(input)) {
				t.Fatal("test fixture is not unambiguous JSON")
			}
			if _, err := inspect([]byte(input)); !errors.Is(err, errShape) {
				t.Fatalf("expected unexpected_shape, got %v", err)
			}
		})
	}
	for _, fixture := range []struct {
		input string
		kind  string
	}{
		{envelope("0", false, false, false), "status"},
		{envelope("0", false, false, true), "heartbeat"},
		{envelope("1", false, true, false), "club_only"},
		{envelope("1", true, false, false), "ball"},
		{valid, "ball"},
	} {
		msg, err := inspect([]byte(fixture.input))
		if err != nil || msg.kind != fixture.kind {
			t.Fatalf("got kind=%s err=%v, want %s", msg.kind, err, fixture.kind)
		}
	}
}

func FuzzObjectStream(f *testing.F) {
	for _, seed := range []string{envelope("1", true, true, false), "{}{}", `{"a":"\\\"}"}`, `{"a":1,"a":2}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 8192 {
			return
		}
		stream, budget := streamFor(bytes.NewReader(input), 8192)
		for {
			frame, err := stream.next()
			if budget.read > 8192 {
				t.Fatal("receive budget exceeded")
			}
			if err != nil {
				break
			}
			if !strictJSON(frame) {
				t.Fatal("returned invalid or ambiguous JSON")
			}
		}
	})
}
