// Package protocol frames and inspects the JSON exchanged between Labs, the
// bridge, and a selected game. [Stream] checks framing and duplicate keys;
// [Inspect] checks the source envelope, while [ResponseCode] checks a reply.
// These checks do not qualify game readiness or validate every shot value.
//
// The bridge retains original frames instead of re-encoding numeric data.
// [Merge] changes only club data and its presence flag in a matching ball frame.
package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

const (
	// MaxFrame is the maximum frame size, in bytes, accepted by Stream or Merge.
	MaxFrame = 1 << 20
	// MaxDepth is the maximum combined object/array nesting accepted by Stream.
	MaxDepth = 64
)

var (
	// ErrFrameSize indicates that a frame exceeds MaxFrame.
	ErrFrameSize = errors.New("frame_size_exceeded")
	// ErrFraming indicates invalid JSON framing, excessive nesting, invalid
	// UTF-8, or duplicate object keys. It may be joined with a read error.
	ErrFraming = errors.New("invalid_framing")
	// ErrShape indicates an unsupported source or response envelope.
	ErrShape = errors.New("unexpected_shape")
)

// Stream reads consecutive JSON objects without requiring newline delimiters.
// Reader must be non-nil; the zero value is not usable. A Stream and its Reader
// must not be read concurrently. After Next fails, the caller must decide how
// to retire or drain the underlying connection; Stream does not resynchronize it.
type Stream struct {
	// Reader supplies the bytes, including any read-ahead retained between frames.
	Reader *bufio.Reader
	// Start, if non-nil, runs after the opening brace is consumed. It can set
	// a frame deadline or record arrival state; an error stops Next.
	Start func() error
	// End, if non-nil, runs after a complete object passes StrictJSON.
	// It is not called for a failed frame, and its error is returned by Next.
	End func() error
}

// Next returns a newly allocated complete JSON object, excluding leading
// whitespace and any following delimiter. It preserves the object's bytes.
// Next returns ErrFrameSize for an oversized frame and ErrFraming for malformed
// or overly nested input. A read failure within a frame is joined with
// ErrFraming; a failure between frames is returned unchanged. Hook errors are
// returned unchanged. Neither a read deadline nor connection cleanup is supplied.
func (s *Stream) Next() ([]byte, error) {
	var frame []byte
	depth := 0
	inString, escaped := false, false
	for {
		b, err := s.Reader.ReadByte()
		if err != nil {
			if len(frame) != 0 {
				return nil, errors.Join(ErrFraming, err)
			}
			return nil, err
		}
		if len(frame) == 0 {
			if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
				continue
			}
			if b != '{' {
				return nil, ErrFraming
			}
			if s.Start != nil {
				if err := s.Start(); err != nil {
					return nil, err
				}
			}
		}
		frame = append(frame, b)
		if len(frame) > MaxFrame {
			return nil, ErrFrameSize
		}
		if inString {
			switch {
			case escaped:
				escaped = false
			case b == '\\':
				escaped = true
			case b == '"':
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > MaxDepth {
				return nil, ErrFraming
			}
		case '}', ']':
			depth--
			if depth == 0 {
				if !StrictJSON(frame) {
					return nil, ErrFraming
				}
				if s.End != nil {
					if err := s.End(); err != nil {
						return nil, err
					}
				}
				return frame, nil
			}
		}
	}
}

// StrictJSON reports whether frame is one valid UTF-8 JSON value with no
// duplicate keys, including escaped equivalents and keys in nested objects.
// Unlike Stream, it accepts non-object values and imposes no MaxFrame or
// MaxDepth bound of its own.
func StrictJSON(frame []byte) bool {
	if !utf8.Valid(frame) || !json.Valid(frame) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.UseNumber()
	return uniqueValue(decoder) == nil
}

// Reject duplicate keys, including escaped spellings and keys in ignored data.
func uniqueValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrFraming
	}
	switch token {
	case json.Delim('{'):
		keys := make(map[string]bool)
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return ErrFraming
			}
			key, ok := token.(string)
			if !ok || keys[key] {
				return ErrFraming
			}
			keys[key] = true
			if err := uniqueValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
	case json.Delim('['):
		for decoder.More() {
			if err := uniqueValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
	}
	return err
}

// Flags contains the source's ShotDataOptions booleans. They describe the
// launch monitor's report, not the selected game's readiness or acceptance.
type Flags struct {
	// ContainsBallData reports that BallData contains a shot.
	ContainsBallData bool
	// ContainsClubData reports that ClubData contains club measurements.
	ContainsClubData bool
	// LaunchMonitorIsReady is the source's readiness flag.
	LaunchMonitorIsReady bool
	// LaunchMonitorBallDetected is the source's ball-detection flag.
	LaunchMonitorBallDetected bool
	// IsHeartBeat identifies a heartbeat, which must not also contain shot data.
	IsHeartBeat bool
}

// Message is source-envelope metadata returned by Inspect, not a decoded copy
// of the shot's numeric measurements.
type Message struct {
	// ID is the exact ShotNumber integer spelling, with negative zero normalized.
	ID string
	// Flags contains the validated ShotDataOptions booleans.
	Flags Flags
	// Fields names the required source members. The slice is shared package
	// data and must not be modified by callers.
	Fields []string
	// Kind is "ball", "club_only", "heartbeat", or "status", in that precedence.
	Kind string
}

var sourceFields = []string{
	"DeviceID", "Units", "ShotNumber", "APIversion", "ClubIndex",
	"BallData", "ClubData", "ShotDataOptions",
}

// Inspect returns metadata for a source object or ErrShape if required members,
// primitive types, or flag combinations are unsupported. It checks native
// integer IDs but does not interpret numeric measurements inside BallData or
// ClubData. Callers must first apply Stream or StrictJSON when duplicate-key
// and UTF-8 rejection is required; Inspect alone does not enforce those checks.
func Inspect(frame []byte) (Message, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(frame, &root) != nil {
		return Message{}, ErrShape
	}
	return inspectObject(root)
}

func inspectObject(root map[string]json.RawMessage) (Message, error) {
	for _, field := range sourceFields {
		if _, present := root[field]; !present {
			return Message{}, ErrShape
		}
	}
	for _, field := range []string{"DeviceID", "Units", "APIversion"} {
		var value string
		if len(root[field]) == 0 || root[field][0] != '"' || json.Unmarshal(root[field], &value) != nil {
			return Message{}, ErrShape
		}
	}
	id, ok := ExactInteger(root["ShotNumber"])
	if !ok {
		return Message{}, ErrShape
	}
	if _, ok := ExactInteger(root["ClubIndex"]); !ok {
		return Message{}, ErrShape
	}
	var options map[string]json.RawMessage
	if json.Unmarshal(root["ShotDataOptions"], &options) != nil || options == nil {
		return Message{}, ErrShape
	}
	var flags Flags
	for _, field := range []struct {
		name  string
		value *bool
	}{
		{"ContainsBallData", &flags.ContainsBallData},
		{"ContainsClubData", &flags.ContainsClubData},
		{"LaunchMonitorIsReady", &flags.LaunchMonitorIsReady},
		{"LaunchMonitorBallDetected", &flags.LaunchMonitorBallDetected},
		{"IsHeartBeat", &flags.IsHeartBeat},
	} {
		raw := options[field.name]
		if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
			return Message{}, ErrShape
		}
		*field.value = bytes.Equal(raw, []byte("true"))
	}
	for _, field := range []struct {
		name     string
		contains bool
	}{
		{"BallData", flags.ContainsBallData},
		{"ClubData", flags.ContainsClubData},
	} {
		raw := root[field.name]
		if bytes.Equal(raw, []byte("null")) && !field.contains {
			continue
		}
		if len(raw) == 0 || raw[0] != '{' {
			return Message{}, ErrShape
		}
	}
	if flags.IsHeartBeat && (flags.ContainsBallData || flags.ContainsClubData) {
		return Message{}, ErrShape
	}
	kind := "status"
	switch {
	case flags.ContainsBallData:
		kind = "ball"
	case flags.ContainsClubData:
		kind = "club_only"
	case flags.IsHeartBeat:
		kind = "heartbeat"
	}
	return Message{ID: id, Flags: flags, Fields: sourceFields, Kind: kind}, nil
}

// ExactInteger returns raw's native JSON integer spelling and reports whether
// it is valid. Negative zero becomes "0"; other values retain their digits
// without a machine-integer range limit. Whitespace, leading zeros, fractions,
// exponents, and quoted numbers are rejected; no floating-point coercion occurs.
func ExactInteger(raw []byte) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	digits := raw
	if digits[0] == '-' {
		digits = digits[1:]
	}
	if len(digits) == 0 || (len(digits) > 1 && digits[0] == '0') {
		return "", false
	}
	for _, b := range digits {
		if b < '0' || b > '9' {
			return "", false
		}
	}
	if string(raw) == "-0" {
		return "0", true
	}
	return string(raw), true
}
