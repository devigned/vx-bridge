// Package destination defines the manually selected game profiles and stores
// the per-user game preference. The bridge uses [ID] to choose an endpoint and
// validate outgoing shots and incoming responses; this package neither dials
// games nor detects which game is running. Desktop selection requests a bridge
// transition separately from saving [Preferences].
package destination

import (
	"encoding/json"
	"errors"

	"github.com/devigned/vx-bridge/internal/protocol"
)

// ID identifies a game profile or Standby. The empty ID defaults to InfiniteTees
// for older configurations, but is not itself Valid.
type ID string

const (
	// InfiniteTees forwards source shots to the configured Infinite Tees endpoint.
	InfiniteTees ID = "infinite_tees"
	// GSPro applies the GSPro OpenAPI shot/reply profile at IPv4 loopback port 921.
	GSPro ID = "gspro"
	// Standby disables target connections and discards shots without success ACKs.
	Standby ID = "standby"
)

// Valid reports whether id is an explicitly supported, non-empty selection.
func (id ID) Valid() bool {
	return id == InfiniteTees || id == GSPro || id == Standby
}

// Default maps an empty id to InfiniteTees and leaves all other IDs unchanged,
// including invalid ones. Empty snapshots/configurations predate selection.
func (id ID) Default() ID {
	if id == "" {
		return InfiniteTees
	}
	return id
}

// Name returns id's display label, using Default for an empty id and
// "Unknown destination" for an unsupported id.
func (id ID) Name() string {
	switch id.Default() {
	case InfiniteTees:
		return "Infinite Tees"
	case GSPro:
		return "GSPro OpenAPI"
	case Standby:
		return "Standby"
	default:
		return "Unknown destination"
	}
}

// Endpoint returns infiniteTees unchanged for the default profile,
// "127.0.0.1:921" for GSPro, and an empty address for Standby or an invalid ID.
// It does not validate the supplied Infinite Tees address or open a connection.
func (id ID) Endpoint(infiniteTees string) string {
	switch id.Default() {
	case InfiniteTees:
		return infiniteTees
	case GSPro:
		return "127.0.0.1:921"
	default:
		return ""
	}
}

// ErrShot indicates that a shot does not fit the selected profile or that the
// selected destination cannot accept shots.
var ErrShot = errors.New("destination_shot_profile_unsupported")

// Shot returns a newly allocated newline-terminated copy of frame without
// changing its JSON values. The caller must already have inspected a ball
// envelope. InfiniteTees adds no further validation. GSPro additionally requires
// strict JSON, Yards, API version "1", Speed, SpinAxis, HLA, VLA, and either
// TotalSpin or both BackSpin and SideSpin as JSON numbers. Missing/unsupported
// values, Standby, or an invalid ID return ErrShot. No units are converted.
func (id ID) Shot(frame []byte) ([]byte, error) {
	switch id.Default() {
	case GSPro:
		var root map[string]json.RawMessage
		if !protocol.StrictJSON(frame) || json.Unmarshal(frame, &root) != nil {
			return nil, ErrShot
		}
		var units, version string
		if json.Unmarshal(root["Units"], &units) != nil || units != "Yards" ||
			json.Unmarshal(root["APIversion"], &version) != nil || version != "1" {
			return nil, ErrShot
		}
		var ball map[string]json.RawMessage
		if json.Unmarshal(root["BallData"], &ball) != nil {
			return nil, ErrShot
		}
		for _, name := range []string{"Speed", "SpinAxis", "HLA", "VLA"} {
			if !number(ball[name]) {
				return nil, ErrShot
			}
		}
		if spin, exists := ball["TotalSpin"]; exists {
			if !number(spin) {
				return nil, ErrShot
			}
		} else if !number(ball["BackSpin"]) || !number(ball["SideSpin"]) {
			return nil, ErrShot
		}
	case InfiniteTees:
	default:
		return nil, ErrShot
	}
	return append(frame[:len(frame):len(frame)], '\n'), nil
}

func number(raw []byte) bool {
	return len(raw) != 0 && (raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9') && json.Valid(raw)
}

// Response returns a response code permitted by id's profile or
// [protocol.ErrShape]. InfiniteTees uses [protocol.Response]; GSPro permits
// 200 through 203 and 500 through 599 after envelope validation. Notifications
// 201 through 203 neither accept a shot nor establish game readiness. Standby
// and invalid IDs reject responses.
func (id ID) Response(frame []byte) (int, error) {
	if id.Default() == InfiniteTees {
		return protocol.Response(frame)
	}
	if id != GSPro {
		return 0, protocol.ErrShape
	}
	code, err := protocol.ResponseCode(frame)
	if err != nil || ((code < 200 || code > 203) && (code < 500 || code > 599)) {
		return 0, protocol.ErrShape
	}
	return code, nil
}
