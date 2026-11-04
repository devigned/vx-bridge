package diagnostics

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/devigned/vx-bridge/internal/bridge"
	"github.com/devigned/vx-bridge/internal/control"
	"github.com/devigned/vx-bridge/internal/destination"
	"github.com/devigned/vx-bridge/internal/managed"
	"github.com/devigned/vx-bridge/internal/protocol"
)

const (
	// ReportSchema versions the public ZIP manifest and typed report records.
	ReportSchema = 1
	// ReportBytes limits the complete ZIP; sources have smaller independent limits.
	ReportBytes = 2 << 20
	// ReportEvents is the maximum number of sanitized exported log records.
	ReportEvents = 2048
	// CollectionTimeout bounds waiting for sources, not an uninterruptible OS call.
	CollectionTimeout = 8 * time.Second
	// BugURL opens an empty bug form; no report data is placed in the URL.
	BugURL = "https://github.com/devigned/vx-bridge/issues/new?template=bug.yml"
)

var collecting atomic.Bool

// Source describes an observation, including skipped or unavailable evidence.
// Code never contains raw OS errors, paths, or input text.
type Source struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Code      string `json:"code,omitempty"`
	Records   int    `json:"records,omitempty"`
	Omitted   int    `json:"omitted,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Manifest describes collection scope, not installation or game readiness.
type Manifest struct {
	Schema      int       `json:"schema"`
	Producer    Build     `json:"producer"`
	CollectedAt time.Time `json:"collected_at"`
	Partial     bool      `json:"partial"`
	Sources     []Source  `json:"sources"`
}

// Status is an allowlisted snapshot. It deliberately omits raw composite build
// text and source payload flags, handles, and identifiers.
type Status struct {
	Build       Build          `json:"build"`
	Source      string         `json:"source"`
	Target      string         `json:"target"`
	Ready       string         `json:"game_readiness"`
	Quit        string         `json:"quit"`
	Identity    string         `json:"identity"`
	Diagnostics string         `json:"diagnostics"`
	Desktop     string         `json:"desktop"`
	LocalLogs   string         `json:"local_logs"`
	LogLost     uint64         `json:"local_log_lost"`
	Startup     string         `json:"startup"`
	Action      string         `json:"desktop_action"`
	Destination string         `json:"destination"`
	Requested   string         `json:"requested_destination"`
	Endpoint    string         `json:"target_endpoint"`
	Switch      string         `json:"switch"`
	Preference  string         `json:"destination_preference"`
	SourceEpoch uint64         `json:"source_epoch"`
	TargetGen   uint64         `json:"target_generation"`
	Counts      bridge.Counts  `json:"counts"`
	LastError   string         `json:"last_error"`
	LastSource  string         `json:"last_source_error"`
	Departure   string         `json:"last_departure"`
	LastEvent   string         `json:"last_event"`
	Recent      []bridge.Event `json:"recent"`
}

// Settings records only the saved destination and fixed local source endpoint.
type Settings struct {
	Destination string `json:"saved_destination"`
	Source      string `json:"source_endpoint"`
}

// Windows records numeric OS facts, never machine, account, or device identity.
type Windows struct {
	Major uint32 `json:"major"`
	Minor uint32 `json:"minor"`
	Build uint32 `json:"build"`
}

// FileCheck names one fixed payload file; digests and private paths stay local.
type FileCheck struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// Installation separates receipt identity from the reporter and running host.
// Hash matches are observations against a local receipt, not MSI/catalog trust.
type Installation struct {
	State    string      `json:"state"`
	Release  string      `json:"receipt_release"`
	Revision string      `json:"receipt_revision"`
	Files    []FileCheck `json:"files"`
}

// InstallerResult contains only selected numeric result-log fields.
type InstallerResult struct {
	Operation     string `json:"operation"`
	InstallCalled bool   `json:"install_called"`
	InstallCode   uint32 `json:"install_code"`
	UsableThen    bool   `json:"usable_at_recording"`
	Result        int32  `json:"result"`
	Revision      string `json:"source_revision"`
}

// Report is a best-effort point-in-time observation. Events come from existing
// bridge slots; collection never flushes, rotates, or clears the live logger.
type Report struct {
	Manifest     Manifest         `json:"-"`
	Windows      *Windows         `json:"windows,omitempty"`
	Host         *Status          `json:"host,omitempty"`
	Settings     *Settings        `json:"settings,omitempty"`
	Installation *Installation    `json:"installation,omitempty"`
	Installer    *InstallerResult `json:"installer_result,omitempty"`
	Events       []record         `json:"-"`
}

// Collector selects fixed owned locations and the existing status-only query.
// NewCollector uses native known folders; fields are private to prevent UI or
// command-line callers from broadening collection to arbitrary inputs.
type Collector struct {
	build   Build
	local   string
	config  string
	query   func(context.Context, bool) (bridge.Snapshot, error)
	read    func(context.Context, string, int64) ([]byte, error)
	receipt func() (managed.Receipt, string, error)
	windows func() (*Windows, error)
}

// NewCollector creates a read-only collector. Unavailable known folders remain
// unavailable; they never fall back to cwd or another user's directories.
func NewCollector(build Build) *Collector {
	local, config := reportFolders()
	return &Collector{build: safeBuild(build), local: local, config: config,
		query: control.Query, read: managed.ReadDiagnosticFile,
		receipt: managed.DiagnosticReceipt, windows: windowsFacts}
}

type observation struct {
	source Source
	apply  func(*Report)
}

// Collect waits at most CollectionTimeout (or ctx's earlier deadline). One
// process-wide source worker is allowed; if OS I/O outlives the deadline, later
// collections fail busy until it returns, rather than accumulating goroutines.
// Cancellation returns an error; source failures yield an explicitly partial ZIP.
func (c *Collector) Collect(ctx context.Context) (Report, error) {
	if !collecting.CompareAndSwap(false, true) {
		return Report{}, errors.New("diagnostic_collection_busy")
	}
	if err := ctx.Err(); err != nil {
		collecting.Store(false)
		return Report{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, CollectionTimeout)
	defer cancel()
	names := []string{"windows", "host_status", "settings", "installation", "installer_result"}
	for i := 0; i < FileCount; i++ {
		names = append(names, fmt.Sprintf("bridge.%d.jsonl", i))
	}
	results := make(chan observation, len(names))
	go func() {
		defer close(results)
		defer collecting.Store(false)
		for _, name := range names {
			if bounded.Err() != nil {
				return
			}
			results <- c.observe(bounded, name)
		}
	}()
	r := Report{Manifest: Manifest{Schema: ReportSchema, Producer: c.build, CollectedAt: time.Now().UTC()}}
	seen := make(map[string]bool)
	finish := func() (Report, error) {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		for _, name := range names {
			if !seen[name] {
				r.Manifest.Sources = append(r.Manifest.Sources, Source{Name: name, State: "unavailable", Code: "collection_deadline"})
			}
		}
		for _, source := range r.Manifest.Sources {
			if source.State != "included" || source.Omitted != 0 || source.Truncated {
				r.Manifest.Partial = true
			}
		}
		r.Manifest.Sources = append(r.Manifest.Sources,
			Source{Name: "vendor_versions", State: "skipped", Code: "ask_user_no_vendor_scan"},
			Source{Name: "raw_msi_log", State: "skipped", Code: "private_input_not_collected"})
		sort.SliceStable(r.Events, func(i, j int) bool { return r.Events[i].At.Before(r.Events[j].At) })
		if len(r.Events) > ReportEvents {
			r.Manifest.Partial = true
			r.Manifest.Sources = append(r.Manifest.Sources, Source{Name: "event_budget", State: "included",
				Omitted: len(r.Events) - ReportEvents, Truncated: true, Code: "newest_events_retained"})
			r.Events = r.Events[len(r.Events)-ReportEvents:]
		}
		return r, nil
	}
	for {
		select {
		case observation, ok := <-results:
			if !ok {
				return finish()
			}
			seen[observation.source.Name] = true
			r.Manifest.Sources = append(r.Manifest.Sources, observation.source)
			if observation.apply != nil {
				observation.apply(&r)
			}
		case <-bounded.Done():
			return finish()
		}
	}
}

func (c *Collector) observe(ctx context.Context, name string) observation {
	o := observation{source: Source{Name: name, State: "included"}}
	unavailable := func(code string) observation {
		o.source.State, o.source.Code = "unavailable", code
		o.source.Truncated = code == "source_size_limit"
		return o
	}
	switch name {
	case "windows":
		facts, err := c.windows()
		if err != nil {
			return unavailable("windows_version_unavailable")
		}
		o.apply = func(r *Report) { r.Windows = facts }
	case "host_status":
		query, cancel := context.WithTimeout(ctx, time.Second)
		state, err := c.query(query, false)
		cancel()
		if err != nil {
			return unavailable("host_unavailable_no_start_attempted")
		}
		status := safeStatus(state)
		o.apply = func(r *Report) { r.Host = &status }
	case "settings":
		if c.config == "" {
			return unavailable("known_folder_unavailable")
		}
		data, err := c.read(ctx, filepath.Join(c.config, "VxInfiniteTeesBridge", "destination.json"), 1024)
		if err != nil {
			return unavailable(sourceError(err))
		}
		var saved struct {
			Destination destination.ID `json:"destination"`
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if !protocol.StrictJSON(data) || decoder.Decode(&saved) != nil || !saved.Destination.Valid() {
			return unavailable("invalid_settings")
		}
		o.apply = func(r *Report) {
			r.Settings = &Settings{Destination: string(saved.Destination), Source: "127.0.0.1:12321"}
		}
	case "installation":
		if c.local == "" {
			return unavailable("known_folder_unavailable")
		}
		receipt, state, err := c.receipt()
		installed := Installation{State: state}
		if err != nil {
			o = unavailable("installed_receipt_unavailable_or_invalid")
			o.apply = func(r *Report) { r.Installation = &installed }
			return o
		}
		installed.Release, installed.Revision = "v"+receipt.Version, receipt.Revision
		names := []string{"GSPconnect.exe", "vxbridge.exe", "LICENSE", "THIRD_PARTY_NOTICES.txt", "build-manifest.json", "SHA256SUMS"}
		for i, filename := range names {
			data, err := c.read(ctx, filepath.Join(c.local, "Programs", "VxInfiniteTeesBridge", filename), 32<<20)
			check := FileCheck{Name: filename, State: "matches_local_receipt"}
			if err != nil {
				check.State = sourceError(err)
			} else if hash := sha256.Sum256(data); hex.EncodeToString(hash[:]) != receipt.Hashes[i] {
				check.State = "hash_mismatch"
			}
			if check.State != "matches_local_receipt" {
				o.source.Code = "payload_not_fully_verified"
				o.source.Omitted++
			}
			installed.Files = append(installed.Files, check)
		}
		o.apply = func(r *Report) { r.Installation = &installed }
	case "installer_result":
		if c.local == "" {
			return unavailable("known_folder_unavailable")
		}
		data, err := c.read(ctx, filepath.Join(c.local, "VxInfiniteTeesBridge", "installer", "setup-result.log"), 256<<10)
		if err != nil {
			return unavailable(sourceError(err))
		}
		result, err := parseInstallerResult(data)
		if err != nil {
			return unavailable("invalid_installer_result")
		}
		o.apply = func(r *Report) { r.Installer = &result }
	default:
		if c.local == "" {
			return unavailable("known_folder_unavailable")
		}
		data, err := c.read(ctx, filepath.Join(c.local, "VxInfiniteTeesBridge", "logs", name), FileBytes)
		if err != nil {
			return unavailable(sourceError(err))
		}
		events, invalid, truncated := parseEvents(data)
		o.source.Records, o.source.Omitted = len(events), invalid
		o.source.Truncated = truncated
		if invalid > 0 {
			o.source.Code = "invalid_incomplete_or_budgeted_records_omitted"
		}
		o.apply = func(r *Report) { r.Events = append(r.Events, events...) }
	}
	return o
}

func sourceError(err error) string {
	if errors.Is(err, managed.ErrDiagnosticSize) {
		return "source_size_limit"
	}
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if errors.Is(err, os.ErrPermission) {
		return "access_denied"
	}
	if errors.Is(err, managed.ErrDenied) {
		return "unsafe_source_refused"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "collection_deadline"
	}
	return "read_refused_or_failed"
}

var (
	releaseText  = regexp.MustCompile(`^v[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,5}$`)
	revisionText = regexp.MustCompile(`^[0-9a-f]{40}$`)
	goText       = regexp.MustCompile(`^go1\.[0-9]{1,3}(\.[0-9]{1,3})?$`)
)

func safeBuild(b Build) Build {
	allow := func(value string, pattern *regexp.Regexp) string {
		if value == "development" || pattern.MatchString(value) {
			return value
		}
		return "unavailable"
	}
	b.Version = choice(b.Version, "vxbridge/prototype-multigame-v1")
	b.Release = allow(b.Release, releaseText)
	b.Revision = allow(b.Revision, revisionText)
	if !goText.MatchString(b.Go) {
		b.Go = "unavailable"
	}
	b.OS = choice(b.OS, "windows", "darwin", "linux")
	b.Architecture = choice(b.Architecture, "amd64", "arm64", "386")
	return b
}

func choice(value string, allowed ...string) string {
	for _, v := range allowed {
		if value == v {
			return v
		}
	}
	return "unavailable"
}

func safeCode(code string) string {
	if code == "" {
		return ""
	}
	return sanitizeEvent(bridge.Event{Reason: code}).Reason
}

func exportedEvent(event bridge.Event) bridge.Event {
	event = sanitizeEvent(event)
	event.At = event.At.UTC()
	event.Alias = 0 // Correlation aliases are not needed in a public report.
	return event
}

func safeStatus(s bridge.Snapshot) Status {
	protocolVersion, _, _ := strings.Cut(s.Version, " ")
	endpoint := "unavailable"
	if strings.HasPrefix(s.TargetEndpoint, "127.0.0.1:") {
		port := strings.TrimPrefix(s.TargetEndpoint, "127.0.0.1:")
		if n, err := strconv.ParseUint(port, 10, 16); err == nil && n > 0 && strconv.FormatUint(n, 10) == port {
			endpoint = "127.0.0.1:" + port
		}
	} else if s.TargetEndpoint == "none" {
		endpoint = "none"
	}
	status := Status{
		Build: safeBuild(Build{Version: protocolVersion, Release: s.Release, Revision: s.Revision,
			Go: s.GoVersion, OS: "windows", Architecture: s.Architecture}),
		Source: choice(s.Source, "listening", "attached", "quarantined_may_still_be_attached_disconnect_Labs"),
		Target: choice(s.Target, "absent_source", "connecting", "retrying", "standby", "connected_ready_unknown", "target_unavailable",
			"target_write_partial_or_uncertain", "target_response_profile_invalid", "target_reported_failure_no_success_ACK", "target_receipt_deadline_delivery_uncertain"),
		Ready: "unknown", Quit: choice(s.Quit, "not_requested", "pending_source_departure", "pending_source_admission_barrier", "complete"),
		Identity: choice(s.Identity, "verified", "fault"),
		Diagnostics: choice(s.Diagnostics, "local_logs", "local_logs_failed_use_status", "local_logs_overload_use_status",
			"stdout", "stdout_failed_use_status", "stdout_overload_use_status", "control_listener_failed", "control_request_failed"),
		Desktop: safeCode(s.Desktop), LocalLogs: choice(s.LocalLogs, "local_logs", "local_logs_opening", "local_logs_slow_use_status",
			"local_logs_open_failed_use_status", "local_logs_write_or_rotation_failed_use_status", "local_logs_close_failed_use_status",
			"local_logs_closed_use_status", "local_logs_queue_full_use_status", "local_logs_flush_deadline_use_status"),
		LogLost: s.LocalLogLost, Startup: choice(s.Startup, "startup_off", "startup_registered_policy_applies", "startup_conflict_not_changed", "startup_unavailable"),
		Action: safeCode(s.DesktopAction), Destination: choice(string(s.Destination), "infinite_tees", "gspro", "standby"),
		Requested: choice(string(s.RequestedDestination), "infinite_tees", "gspro", "standby"), Endpoint: endpoint,
		Switch:     choice(s.Switch, "idle", "waiting_current_delivery", "blocked_source_fault_disconnect_Labs"),
		Preference: safeCode(s.DestinationPreference), SourceEpoch: s.SourceEpoch, TargetGen: s.TargetGeneration, Counts: s.Counts,
		LastError: safeCode(s.LastError), LastSource: safeCode(s.LastSourceError), Departure: safeCode(s.LastDeparture), LastEvent: safeCode(s.LastEvent),
	}
	recent := s.Recent
	if len(recent) > 64 {
		recent = recent[len(recent)-64:]
	}
	for _, event := range recent {
		status.Recent = append(status.Recent, exportedEvent(event))
	}
	return status
}

func parseEvents(data []byte) ([]record, int, bool) {
	var events []record
	invalid := 0
	truncated := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 2048), 2048)
	for scanner.Scan() {
		line := scanner.Bytes()
		var entry record
		if !protocol.StrictJSON(line) || json.Unmarshal(line, &entry) != nil || entry.At.IsZero() {
			invalid++
			continue
		}
		entry.Event, entry.Build = exportedEvent(entry.Event), safeBuild(entry.Build)
		events = append(events, entry)
	}
	if scanner.Err() != nil {
		invalid++
		truncated = true
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	if len(events) > ReportEvents {
		invalid += len(events) - ReportEvents
		truncated = true
		events = events[len(events)-ReportEvents:]
	}
	return events, invalid, truncated
}

func parseInstallerResult(data []byte) (InstallerResult, error) {
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) == 0 || len(lines[len(lines)-1]) > 2048 {
		return InstallerResult{}, errors.New("invalid_installer_result")
	}
	fields := make(map[string]string)
	for _, field := range strings.Fields(string(lines[len(lines)-1])) {
		key, value, ok := strings.Cut(field, "=")
		if _, exists := fields[key]; !ok || exists {
			return InstallerResult{}, errors.New("invalid_installer_result")
		}
		fields[key] = value
	}
	if fields["contract"] != "recovery-first-v2" || fields["engine_namespace"] != "conventional" ||
		!revisionText.MatchString(fields["source"]) ||
		(fields["install_called"] != "0" && fields["install_called"] != "1") ||
		(fields["usable"] != "0" && fields["usable"] != "1") {
		return InstallerResult{}, errors.New("invalid_installer_result")
	}
	install, err := strconv.ParseUint(fields["install"], 10, 32)
	result, resultErr := strconv.ParseInt(fields["result"], 10, 32)
	if err != nil || resultErr != nil {
		return InstallerResult{}, errors.New("invalid_installer_result")
	}
	operation := "unavailable"
	switch fields["operation"] {
	case "1":
		operation = "install"
	case "2":
		operation = "uninstall"
	case "3":
		operation = "repair"
	case "", "0":
	default:
		return InstallerResult{}, errors.New("invalid_installer_result")
	}
	return InstallerResult{Operation: operation, InstallCalled: fields["install_called"] == "1", InstallCode: uint32(install),
		UsableThen: fields["usable"] == "1", Result: int32(result), Revision: fields["source"]}, nil
}

// ZIP serializes only allowlisted records. The four fixed entries have no paths
// to source files, hostname, user, raw vendor/MSI text, or automatic upload.
func (r Report) ZIP() ([]byte, error) {
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	add := func(name string, data []byte) error {
		entry, err := writer.Create(name)
		if err != nil {
			return err
		}
		_, err = entry.Write(data)
		return err
	}
	manifest, err := json.MarshalIndent(r.Manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	report, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	var events bytes.Buffer
	encoder := json.NewEncoder(&events)
	for _, event := range r.Events {
		if err := encoder.Encode(event); err != nil {
			return nil, err
		}
	}
	for _, entry := range []struct {
		name string
		data []byte
	}{
		{"manifest.json", manifest}, {"report.json", report}, {"summary.txt", []byte(r.summary())}, {"events.jsonl", events.Bytes()},
	} {
		if err := add(entry.name, entry.data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if out.Len() > ReportBytes {
		return nil, errors.New("diagnostic_zip_size_limit")
	}
	return out.Bytes(), nil
}

func (r Report) summary() string {
	var out strings.Builder
	fmt.Fprintf(&out, "VX Bridge diagnostic report\nCollected (UTC): %s\nSchema: %d\nReporter release: %s\nReporter revision: %s\nPartial: %t\n\n",
		r.Manifest.CollectedAt.Format(time.RFC3339), r.Manifest.Schema, r.Manifest.Producer.Release, r.Manifest.Producer.Revision, r.Manifest.Partial)
	fmt.Fprintf(&out, "Reporter platform: %s/%s; %s\n", r.Manifest.Producer.OS, r.Manifest.Producer.Architecture, r.Manifest.Producer.Go)
	if r.Windows != nil {
		fmt.Fprintf(&out, "Windows: %d.%d build %d\n", r.Windows.Major, r.Windows.Minor, r.Windows.Build)
	}
	if r.Host != nil {
		fmt.Fprintf(&out, "Running host release: %s\nRunning host revision: %s\nSelected game: %s\nLabs: %s\nTarget: %s (%s)\nLocal logs: %s; rejected records: %d\n",
			r.Host.Build.Release, r.Host.Build.Revision, r.Host.Destination, r.Host.Source, r.Host.Target, r.Host.Endpoint, r.Host.LocalLogs, r.Host.LogLost)
		fmt.Fprintf(&out, "Target acceptances: %d; missed/discarded: %d; uncertain subset: %d\nLast error: %s\n",
			r.Host.Counts.TargetAccept, r.Host.Counts.Dropped, r.Host.Counts.Uncertain, r.Host.LastError)
	} else {
		out.WriteString("Running host: unavailable. No host was started or repaired.\n")
	}
	if r.Installation != nil {
		fmt.Fprintf(&out, "Installed receipt: %s; release: %s; revision: %s\n", r.Installation.State, r.Installation.Release, r.Installation.Revision)
		for _, file := range r.Installation.Files {
			fmt.Fprintf(&out, "  %s: %s\n", file.Name, file.State)
		}
	}
	if r.Installer != nil {
		fmt.Fprintf(&out, "Last installer result: operation=%s result=%d install_code=%d usable_at_recording=%t (not current health)\n",
			r.Installer.Operation, r.Installer.Result, r.Installer.InstallCode, r.Installer.UsableThen)
	}
	out.WriteString("\nSources (missing/unavailable does not mean healthy):\n")
	for _, source := range r.Manifest.Sources {
		fmt.Fprintf(&out, "%s: %s %s; records=%d omitted=%d truncated=%t\n",
			source.Name, source.State, source.Code, source.Records, source.Omitted, source.Truncated)
	}
	out.WriteString("\nSaved locally. Nothing was sent. Review this ZIP before attaching it to a public issue.\n" +
		"Add the approximate problem time and time zone, Windows/Labs/game versions, steps, expected and observed behavior.\n" +
		"Vendor versions are not scanned. Raw MSI/vendor logs, shots, player/device/license/account details, credentials and private paths are excluded.\n" +
		"Best-effort observations only: not proof of game readiness, exactly-once delivery, install health or audit completeness.\n")
	return out.String()
}

// Save writes a complete ZIP to an exclusively created local file. Existing
// files are never replaced. A failed write is reported as failure, not success;
// its incomplete file is left for the user rather than unlinking a changed path.
func Save(ctx context.Context, path string, report Report) error {
	data, err := report.ZIP()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, pins, err := managed.CreateDiagnosticFile(path)
	if err != nil {
		return errors.New("diagnostic_output_create_refused_or_failed")
	}
	if err := writeReport(file, pins, data); err != nil || ctx.Err() != nil {
		return errors.New("diagnostic_output_incomplete")
	}
	return nil
}

type reportFile interface {
	io.WriteCloser
	Sync() error
}

func writeReport(file reportFile, pins io.Closer, data []byte) error {
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	var syncErr error
	if writeErr == nil {
		syncErr = file.Sync()
	}
	return errors.Join(writeErr, syncErr, file.Close(), pins.Close())
}
