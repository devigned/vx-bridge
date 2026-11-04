package diagnostics

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
	"github.com/devigned/vx-bridge/internal/destination"
	"github.com/devigned/vx-bridge/internal/managed"
)

func testBuild() Build {
	return Build{Version: "vxbridge/prototype-multigame-v1", Release: "v0.2.0", Revision: strings.Repeat("a", 40),
		Go: "go1.27.2", OS: "windows", Architecture: "amd64"}
}

func fixtureCollector() *Collector {
	return &Collector{build: testBuild(), local: "synthetic-local", config: "synthetic-config",
		query: func(ctx context.Context, quit bool) (bridge.Snapshot, error) {
			if quit {
				panic("diagnostics must not request Quit")
			}
			if _, ok := ctx.Deadline(); !ok {
				panic("diagnostics must bound control")
			}
			return bridge.Snapshot{Version: "PRIVATE_COMPOSITE", Release: "v0.1.0", Revision: strings.Repeat("b", 40),
				Architecture: "amd64", GoVersion: "go1.27.2", Destination: destination.GSPro, TargetEndpoint: "127.0.0.1:921",
				Source: "attached", Target: "connected_ready_unknown", LocalLogs: "local_logs", LocalLogLost: 3,
				Counts: bridge.Counts{TargetAccept: 4}}, nil
		},
		read:    func(context.Context, string, int64) ([]byte, error) { return nil, os.ErrNotExist },
		receipt: func() (managed.Receipt, string, error) { return managed.Receipt{}, "receipt_missing", os.ErrNotExist },
		windows: func() (*Windows, error) { return &Windows{Major: 10, Build: 26100}, nil },
	}
}

func unzipReport(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 4 || len(data) > ReportBytes {
		t.Fatal("wrong report shape or size", len(reader.File), len(data))
	}
	result := make(map[string][]byte)
	for _, entry := range reader.File {
		if !map[string]bool{"manifest.json": true, "report.json": true, "summary.txt": true, "events.jsonl": true}[entry.Name] {
			t.Fatal("unexpected ZIP entry", entry.Name)
		}
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		result[entry.Name], err = io.ReadAll(io.LimitReader(file, ReportBytes+1))
		if err := errors.Join(err, file.Close()); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func TestReportRunningAndUnavailableHostsAreHonestAndDistinctFromReporter(t *testing.T) {
	for _, running := range []bool{true, false} {
		t.Run(fmt.Sprint(running), func(t *testing.T) {
			c := fixtureCollector()
			if !running {
				c.query = func(context.Context, bool) (bridge.Snapshot, error) {
					return bridge.Snapshot{Source: "PRIVATE_partial_decode"}, errors.New("PRIVATE_path_or_pipe")
				}
			}
			r, err := c.Collect(context.Background())
			if err != nil || !r.Manifest.Partial || (r.Host != nil) != running {
				t.Fatal("host failure hidden or partial data used", r.Host, err)
			}
			if running && (r.Manifest.Producer.Release != "v0.2.0" || r.Host.Build.Release != "v0.1.0" ||
				r.Host.Counts.TargetAccept != 4 || r.Host.LogLost != 3 || r.Host.Endpoint != "127.0.0.1:921") {
				t.Fatal("reporter identity substituted for reported host")
			}
			data, err := r.ZIP()
			if err != nil {
				t.Fatal(err)
			}
			for name, content := range unzipReport(t, data) {
				if bytes.Contains(content, []byte("PRIVATE")) {
					t.Fatal("private raw source escaped", name)
				}
			}
		})
	}
}

func TestReportAllowlistRejectsPrivateStringsInEveryExportedStringSurface(t *testing.T) {
	private := `PRIVATE_PLAYER_C:\Users\private\license=secret@example.test`
	c := fixtureCollector()
	c.build = safeBuild(Build{Version: private, Release: private, Revision: private, Go: private, OS: private, Architecture: private})
	c.query = func(context.Context, bool) (bridge.Snapshot, error) {
		return bridge.Snapshot{
			Version: private, Release: private, Revision: private, GoVersion: private, Architecture: private,
			Source: private, Target: private, Ready: private, Quit: private, Identity: private, Diagnostics: private,
			Desktop: private, LocalLogs: private, Startup: private, DesktopAction: private, LastError: private,
			LastSourceError: private, LastDeparture: private, LastEvent: private, Destination: destination.ID(private),
			RequestedDestination: destination.ID(private), Switch: private, DestinationPreference: private, TargetEndpoint: private,
			Recent: []bridge.Event{{At: time.Now(), Reason: private, Alias: 123456789}},
		}, nil
	}
	logRecord, err := json.Marshal(record{Event: bridge.Event{At: time.Now(), Reason: private, Alias: 123456789},
		Build: Build{Version: private, Release: private, Revision: private, Go: private, OS: private, Architecture: private}})
	if err != nil {
		t.Fatal(err)
	}
	c.read = func(_ context.Context, path string, _ int64) ([]byte, error) {
		switch filepath.Base(path) {
		case "bridge.0.jsonl":
			return append(logRecord, '\n'), nil
		case "destination.json":
			return []byte(`{"destination":"gspro","private":"` + private + `"}`), nil
		case "setup-result.log":
			return []byte("contract=recovery-first-v2 engine_namespace=conventional install_called=1 install=0 usable=1 result=0 source=" +
				strings.Repeat("a", 40) + " ui_field0_hex=" + private), nil
		}
		return nil, errors.New(private)
	}
	r, err := c.Collect(context.Background())
	if err != nil || r.Installer == nil || len(r.Events) != 1 {
		t.Fatal("safe observations lost", err)
	}
	data, err := r.ZIP()
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range unzipReport(t, data) {
		for _, forbidden := range []string{"PRIVATE", "secret@example", "123456789", "ui_field0_hex", "shot_alias", `C:\\Users`} {
			if bytes.Contains(content, []byte(forbidden)) {
				t.Fatal("private input exported", name, forbidden)
			}
		}
	}
}

func TestReportBoundsMalformedLogsAndNewestRecords(t *testing.T) {
	var data bytes.Buffer
	for i := 0; i < ReportEvents+50; i++ {
		entry := record{Event: bridge.Event{At: time.Unix(int64(i+1), 0).UTC(), Reason: "source_attached"}, Build: testBuild()}
		if err := json.NewEncoder(&data).Encode(entry); err != nil {
			t.Fatal(err)
		}
	}
	data.WriteString("{incomplete\n")
	events, omitted, truncated := parseEvents(data.Bytes())
	if len(events) != ReportEvents || omitted != 51 || !truncated || events[0].At.Unix() != 51 {
		t.Fatal("event budget/chronology wrong", len(events), omitted)
	}
	events, omitted, truncated = parseEvents([]byte(strings.Repeat("X", 4096) + "\n"))
	if len(events) != 0 || omitted == 0 || !truncated {
		t.Fatal("oversized log record not rejected")
	}
	c := fixtureCollector()
	c.read = func(_ context.Context, path string, _ int64) ([]byte, error) {
		if strings.HasPrefix(filepath.Base(path), "bridge.") {
			return data.Bytes(), nil
		}
		return nil, os.ErrNotExist
	}
	r, err := c.Collect(context.Background())
	if err != nil || len(r.Events) != ReportEvents || !r.Manifest.Partial {
		t.Fatal("global event budget not applied", err)
	}
	bundle, err := r.ZIP()
	if err != nil {
		t.Fatal(err)
	}
	unzipReport(t, bundle)
}

func TestReportDeadlineAndCancellationDoNotAccumulateSourceWorkers(t *testing.T) {
	c := fixtureCollector()
	entered, release := make(chan struct{}), make(chan struct{})
	c.windows = func() (*Windows, error) { close(entered); <-release; return nil, errors.New("synthetic blocked IO") }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.Collect(ctx); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("cancel looked like successful report", err)
	}
	if _, err := c.Collect(context.Background()); err == nil || err.Error() != "diagnostic_collection_busy" {
		t.Fatal("blocked source spawned another worker", err)
	}
	close(release)
	until := time.Now().Add(time.Second)
	for collecting.Load() {
		if time.Now().After(until) {
			t.Fatal("source worker did not retire")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestReportMissingCorruptSettingsAndInstallerRecords(t *testing.T) {
	for _, input := range []string{"", "PRIVATE", "source=PRIVATE", "contract=recovery-first-v2 source=" + strings.Repeat("a", 40)} {
		if _, err := parseInstallerResult([]byte(input)); err == nil {
			t.Fatal("malformed installer record accepted")
		}
	}
	c := fixtureCollector()
	c.read = func(_ context.Context, path string, _ int64) ([]byte, error) {
		if filepath.Base(path) == "destination.json" {
			return []byte(`{"destination":"gspro"}`), nil
		}
		return []byte("{incomplete"), nil
	}
	r, err := c.Collect(context.Background())
	if err != nil || r.Settings == nil || r.Settings.Destination != "gspro" || r.Installer != nil || len(r.Events) != 0 {
		t.Fatal("corrupt source made the export fail or invented success", err)
	}
}

func TestReportInstalledFileChecksDoNotUseTheReporterIdentity(t *testing.T) {
	c := fixtureCollector()
	names := []string{"GSPconnect.exe", "vxbridge.exe", "LICENSE", "THIRD_PARTY_NOTICES.txt", "build-manifest.json", "SHA256SUMS"}
	receipt := managed.Receipt{Version: "0.1.0", Revision: strings.Repeat("c", 40)}
	for i, name := range names {
		hash := sha256.Sum256([]byte("invented-" + name))
		receipt.Hashes[i] = hex.EncodeToString(hash[:])
	}
	c.receipt = func() (managed.Receipt, string, error) { return receipt, "maintenance_pending", nil }
	c.read = func(_ context.Context, path string, limit int64) ([]byte, error) {
		name := filepath.Base(path)
		for _, candidate := range names {
			if name == candidate {
				if limit != 32<<20 {
					t.Fatal("payload reads are not bounded")
				}
				if name == "vxbridge.exe" {
					return []byte("invented damage"), nil
				}
				return []byte("invented-" + name), nil
			}
		}
		if name == "bridge.0.jsonl" {
			return nil, managed.ErrDiagnosticSize
		}
		return nil, os.ErrNotExist
	}
	r, err := c.Collect(context.Background())
	if err != nil || r.Installation == nil || len(r.Installation.Files) != 6 ||
		r.Installation.Release != "v0.1.0" || r.Installation.State != "maintenance_pending" ||
		r.Installation.Files[1].State != "hash_mismatch" || r.Installation.Files[0].State != "matches_local_receipt" {
		t.Fatal("installed observation was replaced with reporter or admission success", r.Installation, err)
	}
	for _, source := range r.Manifest.Sources {
		if source.Name == "bridge.0.jsonl" && (!source.Truncated || source.Code != "source_size_limit") {
			t.Fatal("oversized source not explicitly recorded")
		}
	}
}

func TestControlDeadlineProducesPartialReportNotAHostStart(t *testing.T) {
	c := fixtureCollector()
	c.query = func(ctx context.Context, quit bool) (bridge.Snapshot, error) {
		if quit {
			t.Fatal("collector requested Quit")
		}
		<-ctx.Done()
		return bridge.Snapshot{Source: "PRIVATE_incomplete"}, ctx.Err()
	}
	start := time.Now()
	r, err := c.Collect(context.Background())
	if err != nil || r.Host != nil || !r.Manifest.Partial || time.Since(start) > 3*time.Second {
		t.Fatal("unavailable control did not yield bounded honest partial results", err)
	}
}

func TestInstallerResultOperationIsTypedAndBackwardCompatible(t *testing.T) {
	base := "contract=recovery-first-v2 engine_namespace=conventional install_called=1 install=0 usable=1 result=0 source=" + strings.Repeat("a", 40)
	for field, expected := range map[string]string{"": "unavailable", " operation=1": "install", " operation=2": "uninstall", " operation=3": "repair"} {
		result, err := parseInstallerResult([]byte(base + field))
		if err != nil || result.Operation != expected {
			t.Fatal("installer operation summary differs", result, err)
		}
	}
	if _, err := parseInstallerResult([]byte(base + " operation=PRIVATE")); err == nil {
		t.Fatal("private operation text accepted")
	}
}

type outputFixture struct {
	short, writeFail, syncFail, closeFail bool
	closed, synced                        bool
}

func (f *outputFixture) Write(data []byte) (int, error) {
	if f.writeFail {
		return 0, errors.New("synthetic disk full")
	}
	if f.short {
		return len(data) - 1, nil
	}
	return len(data), nil
}

func (f *outputFixture) Sync() error {
	f.synced = true
	if f.syncFail {
		return errors.New("synthetic sync failure")
	}
	return nil
}

func (f *outputFixture) Close() error {
	f.closed = true
	if f.closeFail {
		return errors.New("synthetic close failure")
	}
	return nil
}

func TestReportWriteSyncCloseAndPinFailuresNeverLookSuccessful(t *testing.T) {
	for _, file := range []*outputFixture{{short: true}, {writeFail: true}, {syncFail: true}, {closeFail: true}} {
		pins := new(outputFixture)
		if err := writeReport(file, pins, []byte("invented ZIP bytes")); err == nil || !file.closed || !pins.closed {
			t.Fatal("output failure hidden or handles leaked", file, err)
		}
	}
	file, pins := new(outputFixture), &outputFixture{closeFail: true}
	if err := writeReport(file, pins, []byte("invented ZIP bytes")); err == nil || !file.closed || !file.synced || !pins.closed {
		t.Fatal("pin cleanup failure hidden")
	}
}

func TestReportRejectsZIPOverExactByteBudget(t *testing.T) {
	random := rand.New(rand.NewSource(42))
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	text := make([]byte, ReportBytes*2)
	for i := range text {
		text[i] = alphabet[random.Intn(len(alphabet))]
	}
	r := Report{Events: []record{{Event: bridge.Event{At: time.Now(), Reason: string(text)}}}}
	if _, err := r.ZIP(); err == nil || err.Error() != "diagnostic_zip_size_limit" {
		t.Fatal("oversized ZIP accepted", err)
	}
}

func canonicalTemp(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReportSaveCancellationExistingAndOutputErrors(t *testing.T) {
	r, err := fixtureCollector().Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(canonicalTemp(t), "report.zip")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Save(ctx, path, r); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel was ignored", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancel created an output file")
	}
	if err := Save(context.Background(), path, r); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unzipReport(t, before)
	if err := Save(context.Background(), path, r); err == nil {
		t.Fatal("existing report silently overwritten")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("existing report changed", err)
	}
	for _, output := range []string{"relative.zip", filepath.Dir(path), filepath.Join(path, "missing", "report.zip")} {
		if err := Save(context.Background(), output, r); err == nil {
			t.Fatal("bad output accepted", output)
		}
	}
}

func TestReportReadsConcurrentRotatingSlotsWithoutChangingThem(t *testing.T) {
	dir := canonicalTemp(t)
	logDirectory := filepath.Join(dir, "VxInfiniteTeesBridge", "logs")
	if err := os.MkdirAll(logDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	slots, err := openSlots(logDirectory, 1024)
	if err != nil {
		t.Fatal(err)
	}
	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for i := 0; i < 100; i++ {
			data, _ := json.Marshal(record{Event: bridge.Event{At: time.Now().UTC(), Reason: "source_attached"}, Build: testBuild()})
			if _, err := slots.Write(append(data, '\n')); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	c := fixtureCollector()
	c.local, c.read = dir, managed.ReadDiagnosticFile
	r, err := c.Collect(context.Background())
	writers.Wait()
	if closeErr := slots.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil || r.Host == nil {
		t.Fatal("concurrent logs blocked other sources", err)
	}
	for i := 0; i < FileCount; i++ {
		path := filepath.Join(logDirectory, fmt.Sprintf("bridge.%d.jsonl", i))
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 || info.Size() > 1024 {
			t.Fatal("collection cleared or expanded source", err)
		}
	}
}
