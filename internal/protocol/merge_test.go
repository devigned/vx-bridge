package protocol

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func sample(id string, ball, club bool) []byte {
	b, c := "null", "null"
	if ball {
		b = `{"Speed":111.125000000000001,"Spin":40000,"Unknown":-0.00e+2}`
	}
	if club {
		c = `{"Speed":88.50000000000001,"Extra":[1.000e-07,{"name":"}雪"}]}`
	}
	return fmt.Appendf(nil, `{"DeviceID":"invented","Units":"Yards","ShotNumber":%s,"APIversion":"1","ClubIndex":7, "Extra": {"precise":9007199254740993123456789},"BallData":%s,"ClubData":%s,"ShotDataOptions":{"ContainsBallData":%t,"ContainsClubData":%t,"LaunchMonitorIsReady":false,"LaunchMonitorBallDetected":false,"IsHeartBeat":false,"unknown":-0.0}}`, id, b, c, ball, club)
}

func TestMergeChangesOnlyClubMembers(t *testing.T) {
	ball, club := sample("9007199254740993123456789", true, false), sample("9007199254740993123456789", false, true)
	merged, err := Merge(ball, club)
	if err != nil {
		t.Fatal(err)
	}
	expected := bytes.Replace(ball, []byte(`"ClubData":null`), []byte(`"ClubData":{"Speed":88.50000000000001,"Extra":[1.000e-07,{"name":"}雪"}]}`), 1)
	expected = bytes.Replace(expected, []byte(`"ContainsClubData":false`), []byte(`"ContainsClubData":true`), 1)
	if !bytes.Equal(merged, expected) {
		t.Fatal("merge altered original envelope or exact numeric text")
	}
	if _, err := Merge(ball, sample("2", false, true)); !errors.Is(err, ErrShape) {
		t.Fatal("cross-ID merge accepted")
	}
}

func TestPersistentFrameBudgetNotLifetimeBudget(t *testing.T) {
	input := strings.Repeat(`{"n":9007199254740993123456789} `, 40000)
	stream := Stream{Reader: bufio.NewReader(strings.NewReader(input))}
	for range 40000 {
		if _, err := stream.Next(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	stream = Stream{Reader: bufio.NewReader(strings.NewReader(`{"s":"` + strings.Repeat("x", MaxFrame) + `"}`))}
	if _, err := stream.Next(); !errors.Is(err, ErrFrameSize) {
		t.Fatal("oversized frame accepted")
	}
}

func TestResponsesAndTruncatedDeparture(t *testing.T) {
	for _, response := range []string{
		`{"Code":200,"Message":"real accepted"}`,
		`{"Code":201,"Message":"real player","Player":{"Handed":"RH","Club":"7I"}}`,
		`{"Code":202,"Message":"actual only","Player":null}`,
	} {
		if _, err := Response([]byte(response)); err != nil {
			t.Fatal(err)
		}
	}

	for _, response := range []string{
		`{"Code":200,"Code":201,"Message":"bad"}`, `{"Code":200.0,"Message":"bad"}`,
		`{"Code":200,"Message":null}`, `{"Code":201,"Message":"x","Player":[]}`, `{"Code":999,"Message":"x"}`,
	} {
		if _, err := Response([]byte(response)); err == nil {
			t.Fatal("ambiguous response accepted")
		}
	}
	stream := Stream{Reader: bufio.NewReader(strings.NewReader(`{"a":`))}
	_, err := stream.Next()
	if !errors.Is(err, io.EOF) || !errors.Is(err, ErrFraming) {
		t.Fatal("truncation lost actual EOF classification")
	}
}

func TestMergeFrameBoundaryAndIndependentRawOwnership(t *testing.T) {
	ball, club := sample("1", true, false), sample("1", false, true)
	merged, err := Merge(ball, club)
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []int{0, 1} {
		padding := []byte(`"Padding":"` + strings.Repeat("x", MaxFrame-len(merged)-len(`"Padding":"",`)+extra) + `",`)
		padded := append([]byte{'{'}, padding...)
		padded = append(padded, ball[1:]...)
		got, err := Merge(padded, club)
		if extra == 1 {
			if !errors.Is(err, ErrFrameSize) {
				t.Fatal("merge accepted a frame one byte above the limit")
			}
			continue
		}
		if err != nil || len(got) != MaxFrame || !StrictJSON(got) {
			t.Fatalf("exact frame limit was not retained: size=%d err=%v", len(got), err)
		}
		want := append([]byte(nil), got...)
		for i := range padded {
			padded[i] = 'x'
		}
		if !bytes.Equal(got, want) {
			t.Fatal("merged frame aliases the ball buffer")
		}
	}
	want := append([]byte(nil), merged...)
	for i := range ball {
		ball[i] = 'x'
	}
	for i := range club {
		club[i] = 'x'
	}
	if !bytes.Equal(merged, want) {
		t.Fatal("merge did not retain independent exact raw bytes")
	}
}

func TestMergeRetainsEnvelopeRejection(t *testing.T) {
	ball, club := sample("1", true, false), sample("1", false, true)
	for _, test := range []struct {
		name       string
		ball, club []byte
	}{
		{"invalid_ball_json", []byte(`{`), club},
		{"invalid_club_json", ball, []byte(`{`)},
		{"null_ball", []byte(`null`), club},
		{"null_club", ball, []byte(`null`)},
		{"combined_ball", sample("1", true, true), club},
		{"club_is_ball", ball, sample("1", true, false)},
		{"different_id", ball, sample("2", false, true)},
		{"missing_flag", ball, bytes.Replace(club, []byte(`"IsHeartBeat":false,`), nil, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Merge(test.ball, test.club); !errors.Is(err, ErrShape) {
				t.Fatalf("invalid envelope accepted: %v", err)
			}
		})
	}
}
