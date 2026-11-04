// Lifecycle is a private disposable-host qualification worker, not a product
// entry point or an ordinary development diagnostic. It requires matching
// compiled/configured source, first-attempt manual run identity, approval scope,
// and an independently verified standard-account Windows primary token.
// Non-Windows execution is refused.
//
// The guarded Windows worker performs real quiet Setup/MSI install, repair,
// upgrade, deliberate-failure, recovery, and uninstall scenarios using exact
// same-core media. It captures private file/registry/product/cache evidence and
// checks foreign sentinels without starting the bridge, game, sign-in, or hardware.
// Failed mutation requires explicit recovery, not assumed previous-version
// restoration. An operation timeout does not kill, retry, or manufacture cleanup;
// the product state may still be changing. Offline fixtures can test validation
// and decoding without authorizing this worker's real servicing operations.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const approval = "DISPOSABLE-GH-WINDOWS-LIFECYCLE"

var sourceRevision string
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var guidPattern = regexp.MustCompile(`^\{[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}\}$`)
var payloadNames = []string{"GSPconnect.exe", "vxbridge.exe", "LICENSE", "THIRD_PARTY_NOTICES.txt", "build-manifest.json", "SHA256SUMS"}

// media binds one qualification role to delivered bytes and native identities.
type media struct {
	// Role is "a", "b", or "b-fault" in the fixed qualification sequence.
	Role string `json:"role"`
	// Source is the exact source revision shared by all media and the worker.
	Source string `json:"source_revision"`
	// Version is the linked application version tag.
	Version string `json:"version"`
	// MSIVersion is the corresponding numeric three-part installer version.
	MSIVersion string `json:"msi_version"`
	// Product is the actual MSI ProductCode.
	Product string `json:"product"`
	// Package is the distinct emitted MSI PackageCode.
	Package string `json:"package"`
	// MSI is the absolute original installer-media path outside cleanup roots.
	MSI string `json:"msi"`
	// MSISHA256 is the full delivered MSI digest.
	MSISHA256 string `json:"msi_sha256"`
	// Setup is the absolute original native Setup path.
	Setup string `json:"setup"`
	// SetupSHA256 is the full native Setup digest, shared across this same-core run.
	SetupSHA256 string `json:"setup_sha256"`
	// ActionsSHA256 is the native callback DLL digest shared across these media.
	ActionsSHA256 string `json:"actions_sha256"`
	// Archive is the absolute authoritative application ZIP path.
	Archive string `json:"archive"`
	// ArchiveSHA256 is that ZIP's full-byte digest.
	ArchiveSHA256 string `json:"archive_sha256"`
	// Profile is "normal" or the separately gated "private-lifecycle-fault".
	Profile string `json:"build_profile"`
	// Payload maps the six fixed application filenames to exact SHA-256 digests.
	Payload map[string]string `json:"payload_sha256"`
	// Components maps eight fixed component IDs to actual GUIDs.
	Components map[string]string `json:"component_guids"`
}

// configuration is a strictly decoded private approval/source/media envelope.
// Validation binds its claimed values to compiled and run environment identities;
// inspectUser independently checks the actual primary token before operations.
type configuration struct {
	// Schema is the private configuration format version.
	Schema int `json:"schema"`
	// Approval is the exact disposable-host lifecycle scope marker.
	Approval string `json:"approval_scope"`
	// Source is the exact application/worker source revision.
	Source string `json:"source_revision"`
	// Workflow is the run's workflow revision, required to equal Source.
	Workflow string `json:"workflow_revision"`
	// Run is the current manual hosted run ID.
	Run string `json:"run_id"`
	// Attempt is required to be the first run attempt, "1".
	Attempt string `json:"run_attempt"`
	// ExpectedSID identifies the independent disposable product account.
	ExpectedSID string `json:"expected_sid"`
	// Account is the expected product-account name, checked against its token.
	Account string `json:"account"`
	// HarnessSID identifies the different provisioning/harness account.
	HarnessSID string `json:"harness_sid"`
	// Evidence is an existing canonical absolute directory outside cleanup roots.
	Evidence string `json:"evidence"`
	// Media contains exactly the normal A, normal B, and private fault B inputs.
	Media []media `json:"media"`
}

func main() {
	configPath := flag.String("config", "", "private exact-source lifecycle configuration")
	flag.Parse()
	if err := run(*configPath, flag.NArg()); err != nil {
		fmt.Fprintln(os.Stderr, "private lifecycle:", err)
		os.Exit(1)
	}
}

func run(path string, extra int) error {
	if extra != 0 || !filepath.IsAbs(path) {
		return errors.New("require one absolute private configuration")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	var config configuration
	decoder := json.NewDecoder(io.LimitReader(file, 128<<10))
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&config)
	var tail any
	if decodeErr == nil && decoder.Decode(&tail) != io.EOF {
		decodeErr = errors.New("unexpected trailing configuration")
	}
	if err := errors.Join(decodeErr, file.Close()); err != nil {
		return err
	}
	if err := validate(config, sourceRevision, os.Getenv); err != nil {
		return err
	}
	return execute(config)
}

func validate(c configuration, compiled string, env func(string) string) error {
	if env("DIAGNOSTIC_LANE") != "" {
		return errors.New("retired comparative diagnostic lanes are not supported")
	}
	if c.Schema != 1 || c.Approval != approval || !revisionPattern.MatchString(c.Source) ||
		c.Source != compiled || c.Workflow != c.Source || c.Source != env("REVISION") ||
		c.Source != env("WORKFLOW_REVISION") || c.Run != env("GITHUB_RUN_ID") ||
		c.Attempt != env("GITHUB_RUN_ATTEMPT") || c.Attempt != "1" ||
		c.Run == "" || env("GITHUB_REPOSITORY") != "devigned/vx-bridge" ||
		env("GITHUB_ACTIONS") != "true" || env("RUNNER_ENVIRONMENT") != "github-hosted" ||
		env("GITHUB_EVENT_NAME") != "workflow_dispatch" || env("GITHUB_WORKFLOW") != "VX Bridge disposable lifecycle" {
		return errors.New("exact first-attempt manual disposable-host approval/source identity required")
	}
	if !strings.HasPrefix(c.ExpectedSID, "S-1-5-21-") || c.ExpectedSID == c.HarnessSID ||
		c.Account == "" || !filepath.IsAbs(c.Evidence) || len(c.Media) != 3 {
		return errors.New("distinct disposable test account and bounded media/evidence required")
	}
	expectedVersions := map[string]string{"a": "v0.0.1", "b": "v0.0.2", "b-fault": "v0.0.2"}
	seen := map[string]bool{}
	packages := map[string]bool{}
	msiHashes := map[string]bool{}
	var owner, actions string
	byRole := map[string]media{}
	for _, m := range c.Media {
		want, ok := expectedVersions[m.Role]
		if !ok || seen[m.Role] || m.Source != c.Source || m.Version != want ||
			m.MSIVersion != strings.TrimPrefix(want, "v") || !guidPattern.MatchString(m.Product) ||
			!guidPattern.MatchString(m.Package) || m.Product == m.Package || packages[m.Package] ||
			!filepath.IsAbs(m.MSI) || !filepath.IsAbs(m.Setup) || !filepath.IsAbs(m.Archive) {
			return errors.New("media must have real increasing same-source versions and distinct actual package identities")
		}
		if (m.Role == "b-fault") != (m.Profile == "private-lifecycle-fault") ||
			(m.Role != "b-fault" && m.Profile != "normal") || len(m.Payload) != 6 || len(m.Components) != 8 {
			return errors.New("private fault media must be distinct from both normal production profiles")
		}
		componentSeen := map[string]bool{}
		for _, id := range []string{"VxGUI", "VxControl", "VxLicense", "VxNotices", "VxManifest", "VxSums", "Shortcut", "Maintenance"} {
			guid := m.Components[id]
			if !guidPattern.MatchString(guid) || componentSeen[guid] {
				return errors.New("actual eight component identities required")
			}
			componentSeen[guid] = true
		}
		for _, hash := range []string{m.MSISHA256, m.SetupSHA256, m.ActionsSHA256, m.ArchiveSHA256} {
			if !validHash(hash) {
				return errors.New("media identity requires actual full-byte hashes")
			}
		}
		if msiHashes[m.MSISHA256] {
			return errors.New("distinct real packages cannot reuse one delivered MSI hash")
		}
		msiHashes[m.MSISHA256] = true
		for _, name := range payloadNames {
			if !validHash(m.Payload[name]) {
				return errors.New("all six real payload hashes required")
			}
		}
		if owner == "" {
			owner, actions = m.SetupSHA256, m.ActionsSHA256
		} else if owner != m.SetupSHA256 || actions != m.ActionsSHA256 {
			return errors.New("this lifecycle covers same native core only")
		}
		seen[m.Role], packages[m.Package] = true, true
		byRole[m.Role] = m
	}
	a, b, fault := byRole["a"], byRole["b"], byRole["b-fault"]
	if a.Product == b.Product || b.Product != fault.Product || b.ArchiveSHA256 != fault.ArchiveSHA256 ||
		a.Payload["GSPconnect.exe"] == b.Payload["GSPconnect.exe"] || a.Payload["vxbridge.exe"] == b.Payload["vxbridge.exe"] {
		return errors.New("normal A/B must contain distinct linked version/Product bytes; fault B keeps B identity")
	}
	for _, name := range payloadNames {
		if b.Payload[name] != fault.Payload[name] {
			return errors.New("private fault media cannot replace the actual B runtime payload")
		}
	}
	for id, guid := range a.Components {
		if b.Components[id] != guid || fault.Components[id] != guid {
			return errors.New("same-core component identities differ")
		}
	}
	return nil
}

func validHash(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == 32 && hex.EncodeToString(data) == value
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	n, readErr := io.Copy(digest, io.LimitReader(file, (64<<20)+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return "", err
	}
	if n > 64<<20 {
		return "", errors.New("file exceeds bounded audit hashing limit")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func verifyMedia(m media) error {
	for path, want := range map[string]string{m.MSI: m.MSISHA256, m.Setup: m.SetupSHA256, m.Archive: m.ArchiveSHA256} {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("media stat %s: %w", path, err)
		}
		if !info.Mode().IsRegular() || info.Size() > 64<<20 {
			return fmt.Errorf("media must be regular/bounded: %s", path)
		}
		got, err := hashFile(path)
		if err != nil {
			return fmt.Errorf("media hash %s: %w", path, err)
		}
		if got != want {
			return fmt.Errorf("media bytes differ: %s", path)
		}
	}
	archive, err := zip.OpenReader(m.Archive)
	if err != nil {
		return err
	}
	defer archive.Close()
	if len(archive.File) != 6 {
		return errors.New("unexpected authoritative payload inventory")
	}
	seen := map[string]bool{}
	for _, entry := range archive.File {
		if seen[entry.Name] || !validHash(m.Payload[entry.Name]) || entry.UncompressedSize64 > 32<<20 {
			return errors.New("unexpected payload archive entry")
		}
		file, err := entry.Open()
		if err != nil {
			return err
		}
		digest := sha256.New()
		_, readErr := io.Copy(digest, io.LimitReader(file, (32<<20)+1))
		if err := errors.Join(readErr, file.Close()); err != nil {
			return err
		}
		if hex.EncodeToString(digest.Sum(nil)) != m.Payload[entry.Name] {
			return errors.New("actual payload archive hash differs")
		}
		seen[entry.Name] = true
	}
	return nil
}
