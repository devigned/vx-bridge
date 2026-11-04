package protocol

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Merge returns a new ball envelope with ClubData copied from club and
// ContainsClubData set to true, preserving every other original byte.
// Both envelopes must pass Inspect, have matching integer IDs, and represent
// a ball without club data and a club-only fragment respectively; otherwise
// Merge returns ErrShape. Callers must perform strict framing checks first.
// Neither input is modified. A result exceeding MaxFrame returns ErrFrameSize
// along with the oversized result, which must not be sent.
func Merge(ball, club []byte) ([]byte, error) {
	var root, other map[string]json.RawMessage
	if json.Unmarshal(ball, &root) != nil || json.Unmarshal(club, &other) != nil {
		return nil, ErrShape
	}
	b, err := inspectObject(root)
	if err != nil {
		return nil, err
	}
	c, err := inspectObject(other)
	if err != nil || b.ID != c.ID || b.Kind != "ball" || b.Flags.ContainsClubData || c.Kind != "club_only" {
		return nil, ErrShape
	}
	options, err := replace(root["ShotDataOptions"], "ContainsClubData", []byte("true"))
	if err != nil {
		return nil, err
	}
	result, err := replace(ball, "ShotDataOptions", options)
	if err != nil {
		return nil, err
	}
	result, err = replace(result, "ClubData", other["ClubData"])
	if err == nil && len(result) > MaxFrame {
		err = ErrFrameSize
	}
	return result, err
}

func replace(object []byte, name string, value []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(object))
	if _, err := decoder.Token(); err != nil {
		return nil, ErrShape
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, ErrShape
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return nil, ErrShape
		}
		if key == name {
			end := int(decoder.InputOffset())
			start := end - len(raw)
			result := make([]byte, 0, len(object)-len(raw)+len(value))
			result = append(result, object[:start]...)
			result = append(result, value...)
			return append(result, object[end:]...), nil
		}
	}
	return nil, ErrShape
}

// Response returns a validated Infinite Tees response code: 200, 201, 202, 203,
// 500, or 501. Other codes and malformed envelopes return ErrShape.
// It validates the envelope through ResponseCode, never Player's values.
func Response(frame []byte) (int, error) {
	code, err := ResponseCode(frame)
	if err != nil {
		return 0, err
	}
	switch code {
	case 200, 201, 202, 203, 500, 501:
		return code, nil
	default:
		return 0, ErrShape
	}
}

// ResponseCode returns an object's native integer Code after strict JSON
// validation. Message must be a string; optional Player must be null or an
// object, whose contents are not interpreted. An invalid envelope or a Code
// outside the platform's int range returns ErrShape. No allowed-code policy is
// applied here; Response and the destination profiles supply that policy.
func ResponseCode(frame []byte) (int, error) {
	var root map[string]json.RawMessage
	if !StrictJSON(frame) || json.Unmarshal(frame, &root) != nil {
		return 0, ErrShape
	}
	code, ok := ExactInteger(root["Code"])
	if !ok {
		return 0, ErrShape
	}
	n, err := strconv.Atoi(code)
	if err != nil {
		return 0, ErrShape
	}
	var message string
	if len(root["Message"]) == 0 || root["Message"][0] != '"' || json.Unmarshal(root["Message"], &message) != nil {
		return 0, ErrShape
	}
	if player, exists := root["Player"]; exists && !bytes.Equal(player, []byte("null")) && (len(player) == 0 || player[0] != '{') {
		return 0, ErrShape
	}
	return n, nil
}
