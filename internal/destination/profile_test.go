package destination

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const syntheticShot = `{"DeviceID":"synthetic-test","Units":"Yards","ShotNumber":900719925474099312345,"APIversion":"1","BallData":{"Speed":111.125000000000001,"SpinAxis":-1.000e+1,"TotalSpin":2400,"HLA":2,"VLA":12,"Extra":-0.00},"ClubData":null,"ShotDataOptions":{"ContainsBallData":true,"ContainsClubData":false},"Unknown":{"n":1234567890123456789012345}}`

func TestActualPreferenceTempNamesFitTheNativeCleanupInventory(t *testing.T) {
	directory := t.TempDir()
	for range 16 {
		file, err := os.CreateTemp(directory, ".destination-*.tmp")
		if err != nil {
			t.Fatal(err)
		}
		name := file.Name()
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		suffix := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(name), ".destination-"), ".tmp")
		n, parseErr := strconv.ParseUint(suffix, 10, 32)
		if parseErr != nil || strconv.FormatUint(n, 10) != suffix {
			t.Fatal("actual Go temp-name format no longer matches native uninstall inventory", suffix, parseErr)
		}
		if err := os.Remove(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProfilesPreserveExactShotBytesAndRejectUnsupportedData(t *testing.T) {
	for _, profile := range []ID{InfiniteTees, GSPro} {
		got, err := profile.Shot([]byte(syntheticShot))
		if err != nil || !bytes.Equal(got, []byte(syntheticShot+"\n")) {
			t.Fatal("profile normalized raw shot", profile, err)
		}
	}
	for _, replacement := range []string{
		`"Speed":null`, `"Speed":"111"`, `"Speed":true`, `"OtherSpeed":111`,
	} {
		bad := strings.Replace(syntheticShot, `"Speed":111.125000000000001`, replacement, 1)
		if _, err := GSPro.Shot([]byte(bad)); err == nil {
			t.Fatal("unsupported required metric accepted", replacement)
		}
		if _, err := InfiniteTees.Shot([]byte(bad)); err != nil {
			t.Fatal("GSPro validation changed IT passthrough")
		}
	}
	alternative := strings.Replace(syntheticShot, `"TotalSpin":2400`, `"BackSpin":2400,"SideSpin":-100`, 1)
	if _, err := GSPro.Shot([]byte(alternative)); err != nil {
		t.Fatal("documented spin-component alternative rejected", err)
	}
	for _, bad := range []string{
		strings.Replace(syntheticShot, `"Yards"`, `"Meters"`, 1),
		strings.Replace(syntheticShot, `"APIversion":"1"`, `"APIversion":"2"`, 1),
		strings.Replace(alternative, `,"SideSpin":-100`, "", 1),
	} {
		if _, err := GSPro.Shot([]byte(bad)); err == nil {
			t.Fatal("unqualified units/version/spin accepted")
		}
	}
	if _, err := Standby.Shot([]byte(syntheticShot)); err == nil {
		t.Fatal("standby encoded a shot")
	}
}

func TestResponseProfilesDoNotInventReadinessOrBroadenIT(t *testing.T) {
	for _, test := range []struct {
		code   string
		it, gs bool
	}{
		{"200", true, true}, {"201", true, true}, {"202", true, true},
		{"203", true, true}, {"204", false, false}, {"500", true, true}, {"501", true, true},
		{"503", false, true}, {"599", false, true}, {"600", false, false},
		{`"200"`, false, false}, {"200.0", false, false},
	} {
		frame := []byte(`{"Code":` + test.code + `,"Message":"synthetic reply"}`)
		for _, profile := range []ID{InfiniteTees, GSPro} {
			_, err := profile.Response(frame)
			want := test.it
			if profile == GSPro {
				want = test.gs
			}
			if (err == nil) != want {
				t.Fatal("wrong response admission", profile, test.code, err)
			}
		}
	}
}

func TestGSProNotificationsStillRequireStrictResponseEnvelopes(t *testing.T) {
	for _, code := range []string{"202", "203"} {
		for _, bad := range []string{
			`{"Code":` + code + `}`,
			`{"Code":` + code + `,"Message":null}`,
			`{"Code":` + code + `,"Message":"synthetic","Player":[]}`,
			`{"Code":` + code + `,"Code":` + code + `,"Message":"synthetic"}`,
			`{"Code":"` + code + `","Message":"synthetic"}`,
			`{"Code":` + code + `.0,"Message":"synthetic"}`,
			`{"Code":` + code + `,"Message":"synthetic","Extra":{"x":1,"x":2}}`,
		} {
			if _, err := GSPro.Response([]byte(bad)); err == nil {
				t.Fatal("notification bypassed strict response validation", bad)
			}
		}
	}
}

func TestPreferenceIsBoundedExplicitAndReplacedAtomically(t *testing.T) {
	p := &Preferences{Path: filepath.Join(t.TempDir(), "settings", "destination.json")}
	if id, err := p.Load(); err != nil || id != InfiniteTees {
		t.Fatal("first launch changed IT baseline", id, err)
	}
	for _, id := range []ID{GSPro, Standby, InfiniteTees} {
		if err := p.Save(id); err != nil {
			t.Fatal(err)
		}
		if got, err := p.Load(); err != nil || got != id {
			t.Fatal("choice not remembered", got, err)
		}
	}
	if err := p.Save("unknown"); err == nil {
		t.Fatal("invalid choice persisted")
	}
	for _, bad := range []string{
		`{"destination":"gspro","destination":"standby"}`,
		`{"destination":"unknown"}`, `{"destination":"gspro","extra":1}`,
		strings.Repeat(" ", 1025), `{}`, `{"destination":null}`,
	} {
		if err := os.WriteFile(p.Path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if id, err := p.Load(); err == nil || id != "" {
			t.Fatal("corrupt preference silently selected a game", id, err)
		}
	}
	entries, err := filepath.Glob(filepath.Join(filepath.Dir(p.Path), ".destination-*.tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatal("preference temporary files leaked", entries, err)
	}
}
