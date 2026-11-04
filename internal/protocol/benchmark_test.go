package protocol

import (
	"bufio"
	"bytes"
	"testing"
)

func BenchmarkSourceFrameInspect(b *testing.B) {
	for _, test := range []struct {
		name       string
		ball, club bool
	}{
		{"combined", true, true},
		{"status", false, false},
	} {
		b.Run(test.name, func(b *testing.B) {
			frame := sample("9007199254740993123456789", test.ball, test.club)
			input := bytes.NewReader(frame)
			stream := Stream{Reader: bufio.NewReaderSize(input, 4096)}
			b.ReportAllocs()
			b.SetBytes(int64(len(frame)))
			for b.Loop() {
				input.Reset(frame)
				stream.Reader.Reset(input)
				raw, err := stream.Next()
				if err != nil {
					b.Fatal(err)
				}
				if _, err := Inspect(raw); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSplitMerge(b *testing.B) {
	ball := sample("9007199254740993123456789", true, false)
	club := sample("9007199254740993123456789", false, true)
	b.ReportAllocs()
	b.SetBytes(int64(len(ball) + len(club)))
	for b.Loop() {
		if _, err := Merge(ball, club); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReceiptFrameResponse(b *testing.B) {
	frame := []byte(`{"Code":200,"Message":"invented actual acceptance"}`)
	input := bytes.NewReader(frame)
	stream := Stream{Reader: bufio.NewReaderSize(input, 4096)}
	b.ReportAllocs()
	b.SetBytes(int64(len(frame)))
	for b.Loop() {
		input.Reset(frame)
		stream.Reader.Reset(input)
		raw, err := stream.Next()
		if err != nil {
			b.Fatal(err)
		}
		if code, err := Response(raw); err != nil || code != 200 {
			b.Fatalf("response: %d %v", code, err)
		}
	}
}
