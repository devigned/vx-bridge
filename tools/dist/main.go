// Dist builds and statically verifies the unsigned Windows application ZIP.
// Build mode requires an exact clean HEAD, the pinned compiler, and a new
// absolute output directory outside the checkout. It cross-builds the GUI
// GSPconnect.exe and distinct console vxbridge.exe, inspects their actual PE/Go
// metadata and linked identities, and packages notices, a manifest, and hashes.
//
// Verify mode inspects an existing ZIP rather than building. Neither mode
// executes product binaries, installs an MSI, publishes a release, or qualifies
// live compatibility. Native installer construction belongs to the separate
// setup tool. See the contributor command guide for ordinary build entry points.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"
)

const modulePath = "github.com/devigned/vx-bridge"
const developmentVersion = "development"

var versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

var payloadNames = []string{"GSPconnect.exe", "vxbridge.exe", "LICENSE", "THIRD_PARTY_NOTICES.txt", "build-manifest.json"}

// binaryRecord describes inspected executable bytes, not merely build flags.
type binaryRecord struct {
	// Name is one of the two fixed distribution executable basenames.
	Name string `json:"name"`
	// Bytes is the full executable length.
	Bytes int `json:"bytes"`
	// SHA256 is the lowercase digest of those full bytes.
	SHA256 string `json:"sha256"`
	// Subsystem is the actual PE GUI (2) or console (3) subsystem.
	Subsystem uint16 `json:"pe_subsystem"`
	// DefaultDesktop is the linked main.defaultDesktop string read from the PE.
	DefaultDesktop string `json:"default_desktop"`
	// BuildRevision is the linked exact source revision.
	BuildRevision string `json:"build_revision"`
	// ReleaseVersion is the linked release tag or "development".
	ReleaseVersion string `json:"release_version"`
	// BuildInfo is the decoded embedded Go module/toolchain/settings metadata.
	BuildInfo *debug.BuildInfo `json:"go_build_info"`
}

// manifest binds the archive's executable records to one source/build identity.
type manifest struct {
	// Schema is the distribution manifest format version.
	Schema int `json:"schema"`
	// Revision is the exact source commit used for both executables.
	Revision string `json:"source_revision"`
	// Version is the requested release tag or "development".
	Version string `json:"release_version"`
	// Toolchain is the pinned Go compiler identity.
	Toolchain string `json:"toolchain"`
	// Binaries contains GSPconnect.exe then vxbridge.exe inspection records.
	Binaries []binaryRecord `json:"binaries"`
}

func main() {
	revision := flag.String("revision", "", "required exact 40-character source commit")
	version := flag.String("version", developmentVersion, "version tag vMAJOR.MINOR.PATCH, or development for non-release builds")
	out := flag.String("out", "", "new absolute output directory outside the checkout")
	verify := flag.String("verify", "", "verify an existing distribution ZIP instead of building")
	flag.Parse()
	if err := run(*revision, *version, *out, *verify, flag.NArg()); err != nil {
		fmt.Fprintln(os.Stderr, "dist:", err)
		os.Exit(1)
	}
}

func run(revision, version, out, verify string, extra int) error {
	if !validRevision(revision) || !validVersion(version) || extra != 0 || (out == "") == (verify == "") {
		return errors.New("use -revision <full SHA>, a supported -version, and exactly one of -out or -verify")
	}
	if verify != "" {
		return verifyArchive(verify, revision, version)
	}
	root, err := command("", nil, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if err := cleanRevision(root, revision); err != nil {
		return err
	}
	pinned, err := os.ReadFile(filepath.Join(root, ".go-version"))
	if err != nil {
		return err
	}
	toolchain := "go" + strings.TrimSpace(string(pinned))
	if runtime.Version() != toolchain {
		return fmt.Errorf("toolchain is %s; require %s", runtime.Version(), toolchain)
	}
	if !filepath.IsAbs(out) {
		return errors.New("output must be absolute")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(out))
	if err != nil {
		return err
	}
	out = filepath.Join(parent, filepath.Base(out))
	relative, err := filepath.Rel(root, out)
	if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return errors.New("output must be outside the source checkout")
	}
	if err := os.Mkdir(out, 0o755); err != nil {
		return fmt.Errorf("output must not already exist: %w", err)
	}
	packages, err := command(root, buildEnv(), "go", "list", "-mod=readonly", "-deps", "-json=Module", "./cmd/vxbridge")
	if err != nil {
		return err
	}
	var dependencies []debug.Module
	seen := make(map[string]bool)
	decoder := json.NewDecoder(strings.NewReader(packages))
	for {
		var pkg struct {
			Module *struct {
				Path, Version, Sum string
				Main               bool
				Replace            *debug.Module
			}
		}
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return err
		}
		m := pkg.Module
		if m == nil || m.Main || seen[m.Path] {
			continue
		}
		if m.Replace != nil {
			return errors.New("linked module replacements are not distribution inputs")
		}
		seen[m.Path] = true
		dependencies = append(dependencies, debug.Module{Path: m.Path, Version: m.Version, Sum: m.Sum})
	}
	files := make(map[string][]byte)
	m := manifest{Schema: 2, Revision: revision, Version: version, Toolchain: toolchain}
	for _, name := range payloadNames[:2] {
		flags := "-X main.buildRevision=" + revision
		if version != developmentVersion {
			flags += " -X main.releaseVersion=" + version
		}
		if name == "GSPconnect.exe" {
			flags += " -H=windowsgui -X main.defaultDesktop=true"
		}
		path := filepath.Join(out, name)
		if _, err := command(root, buildEnv(), "go", "build", "-mod=readonly", "-trimpath", "-buildvcs=true",
			"-ldflags="+flags, "-o", path, "./cmd/vxbridge"); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		record, err := inspectBinary(data, name, revision, version, toolchain)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := verifyDependencies(record.BuildInfo, dependencies); err != nil {
			return err
		}
		files[name] = data
		m.Binaries = append(m.Binaries, record)
	}
	for _, name := range payloadNames[2:4] {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		files[name] = data
	}
	files["build-manifest.json"], err = json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	files["build-manifest.json"] = append(files["build-manifest.json"], '\n')
	if err := cleanRevision(root, revision); err != nil {
		return err
	}
	stamp, err := command(root, nil, "git", "show", "-s", "--format=%cI", revision)
	if err != nil {
		return err
	}
	when, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return err
	}
	archive := filepath.Join(out, archiveName(revision, version))
	if err := writeArchive(archive, files, when); err != nil {
		return err
	}
	if err := verifyArchive(archive, revision, version); err != nil {
		return err
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		return err
	}
	if err := os.WriteFile(archive+".sha256", []byte(digest(data)+"  "+filepath.Base(archive)+"\n"), 0o644); err != nil {
		return err
	}
	// Leave only the two uploadable files, not loose executables that could be
	// accidentally included by a broad workflow upload pattern.
	for _, name := range payloadNames[:2] {
		if err := os.Remove(filepath.Join(out, name)); err != nil {
			return err
		}
	}
	fmt.Println(archive)
	return nil
}

func validRevision(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == 20 && value == strings.ToLower(value)
}

func validVersion(value string) bool {
	return value == developmentVersion || (len(value) <= 64 && versionPattern.MatchString(value))
}

func archiveName(revision, version string) string {
	if version != developmentVersion {
		revision = version + "-" + revision
	}
	return "vx-bridge-windows-amd64-" + revision + ".zip"
}

func command(dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Env = dir, env
	data, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %v: %w\n%s", name, args, err, data)
	}
	return strings.TrimSpace(string(data)), nil
}

func buildEnv() []string {
	env := slices.DeleteFunc(os.Environ(), func(value string) bool {
		key, _, _ := strings.Cut(value, "=")
		return slices.Contains([]string{"GOOS", "GOARCH", "GOAMD64", "CGO_ENABLED", "GOTOOLCHAIN", "GOFLAGS", "GOWORK", "GOEXPERIMENT"}, key)
	})
	return append(env, "GOOS=windows", "GOARCH=amd64", "GOAMD64=v1", "CGO_ENABLED=0",
		"GOTOOLCHAIN=local", "GOFLAGS=", "GOWORK=off", "GOEXPERIMENT=")
}

func cleanRevision(root, revision string) error {
	head, err := command(root, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != revision {
		return errors.New("HEAD does not match the requested exact revision")
	}
	status, err := command(root, nil, "git", "status", "--porcelain", "--untracked-files=all", "--ignored=matching")
	if err != nil {
		return err
	}
	if status != "" {
		return errors.New("source checkout is not clean (including untracked/ignored files)")
	}
	return nil
}

func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func inspectBinary(data []byte, name, revision, version, toolchain string) (binaryRecord, error) {
	record := binaryRecord{Name: name, Bytes: len(data), SHA256: digest(data)}
	subsystem, desktop := uint16(3), "false"
	if name == "GSPconnect.exe" {
		subsystem, desktop = 2, "true"
	} else if name != "vxbridge.exe" {
		return record, errors.New("unexpected executable basename")
	}
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return record, err
	}
	defer image.Close()
	header, ok := image.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || image.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || header.Subsystem != subsystem ||
		header.DataDirectory[14].VirtualAddress != 0 || header.DataDirectory[14].Size != 0 {
		return record, errors.New("require Windows amd64, expected GUI/console subsystem, and no CLR directory")
	}
	imports, err := image.ImportedLibraries()
	if err != nil {
		return record, err
	}
	for _, library := range imports {
		if strings.EqualFold(library, "mscoree.dll") {
			return record, errors.New("CLR import is forbidden")
		}
	}
	info, err := buildinfo.Read(bytes.NewReader(data))
	if err != nil {
		return record, err
	}
	if err := verifyBuildInfo(info, revision, toolchain); err != nil {
		return record, err
	}
	actualRevision, err := linkedString(image, header.ImageBase, "main.buildRevision")
	if err != nil {
		return record, err
	}
	actualDesktop, err := linkedString(image, header.ImageBase, "main.defaultDesktop")
	if err != nil {
		return record, err
	}
	actualVersion, err := linkedString(image, header.ImageBase, "main.releaseVersion")
	if err != nil {
		return record, err
	}
	if actualRevision != revision || actualDesktop != desktop || actualVersion != version {
		return record, errors.New("actual linked buildRevision/defaultDesktop/releaseVersion mismatch")
	}
	record.Subsystem, record.DefaultDesktop, record.BuildRevision, record.BuildInfo = subsystem, actualDesktop, actualRevision, info
	record.ReleaseVersion = actualVersion
	return record, nil
}

func verifyBuildInfo(info *debug.BuildInfo, revision, toolchain string) error {
	if info.GoVersion != toolchain || info.Path != modulePath+"/cmd/vxbridge" ||
		info.Main.Path != modulePath || info.Main.Replace != nil {
		return errors.New("unexpected toolchain or main module")
	}
	settings := make(map[string]string)
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	for key, value := range map[string]string{
		"GOOS": "windows", "GOARCH": "amd64", "GOAMD64": "v1", "CGO_ENABLED": "0",
		"-trimpath": "true", "-buildmode": "exe", "vcs": "git", "vcs.revision": revision, "vcs.modified": "false",
	} {
		if settings[key] != value {
			return fmt.Errorf("embedded %s=%q, want %q", key, settings[key], value)
		}
	}
	for _, dependency := range info.Deps {
		if dependency.Replace != nil {
			return errors.New("linked module replacement is forbidden")
		}
	}
	return nil
}

func verifyDependencies(info *debug.BuildInfo, expected []debug.Module) error {
	if len(info.Deps) != len(expected) {
		return errors.New("linked dependency graph differs from go list -deps ./cmd/vxbridge")
	}
	for _, want := range expected {
		if !slices.ContainsFunc(info.Deps, func(got *debug.Module) bool {
			return got.Path == want.Path && got.Version == want.Version && got.Sum == want.Sum && got.Replace == nil
		}) {
			return fmt.Errorf("linked dependency mismatch: %s", want.Path)
		}
	}
	return nil
}

// Go's retained COFF symbols locate string headers. Read the referenced bytes,
// not the ldflags text or an arbitrary occurrence of "true" in the image.
func linkedString(image *pe.File, base uint64, name string) (string, error) {
	for _, symbol := range image.Symbols {
		if symbol.Name != name {
			continue
		}
		if symbol.SectionNumber < 1 || int(symbol.SectionNumber) > len(image.Sections) {
			break
		}
		data, err := image.Sections[symbol.SectionNumber-1].Data()
		if err != nil {
			return "", err
		}
		offset := uint64(symbol.Value)
		if offset+16 > uint64(len(data)) {
			break
		}
		address := binary.LittleEndian.Uint64(data[offset : offset+8])
		length := binary.LittleEndian.Uint64(data[offset+8 : offset+16])
		if address < base || length > 128 {
			break
		}
		rva := address - base
		for _, section := range image.Sections {
			if rva < uint64(section.VirtualAddress) {
				continue
			}
			at := rva - uint64(section.VirtualAddress)
			if at > uint64(section.Size) || length > uint64(section.Size)-at {
				continue
			}
			content, err := section.Data()
			if err != nil {
				return "", err
			}
			return string(content[at : at+length]), nil
		}
		break
	}
	return "", fmt.Errorf("cannot read linked Go string %s", name)
}

func checksums(files map[string][]byte) []byte {
	var out strings.Builder
	for _, name := range payloadNames {
		fmt.Fprintf(&out, "%s  %s\n", digest(files[name]), name)
	}
	return []byte(out.String())
}

func writeArchive(path string, files map[string][]byte, when time.Time) (result error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, file.Close()) }()
	writer := zip.NewWriter(file)
	defer func() { result = errors.Join(result, writer.Close()) }()
	for _, name := range append(slices.Clone(payloadNames), "SHA256SUMS") {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: when.UTC()}
		header.SetMode(0o644)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		data := files[name]
		if name == "SHA256SUMS" {
			data = checksums(files)
		}
		if _, err := entry.Write(data); err != nil {
			return err
		}
	}
	return nil
}

func verifyArchive(path, revision, version string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	if len(reader.File) != len(payloadNames)+1 {
		return errors.New("unexpected archive entry count")
	}
	files := make(map[string][]byte)
	for _, entry := range reader.File {
		if (!slices.Contains(payloadNames, entry.Name) && entry.Name != "SHA256SUMS") ||
			files[entry.Name] != nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 > 32<<20 {
			return errors.New("unexpected, duplicate, nonregular or oversized archive entry")
		}
		stream, err := entry.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(stream, (32<<20)+1))
		if err := errors.Join(readErr, stream.Close()); err != nil {
			return err
		}
		if len(data) == 0 || len(data) > 32<<20 {
			return errors.New("empty or oversized archive entry")
		}
		files[entry.Name] = data
	}
	if !bytes.Equal(files["SHA256SUMS"], checksums(files)) {
		return errors.New("archive SHA256SUMS mismatch")
	}
	var m manifest
	if err := json.Unmarshal(files["build-manifest.json"], &m); err != nil {
		return err
	}
	if m.Schema != 2 || m.Revision != revision || m.Version != version || len(m.Binaries) != 2 || m.Toolchain != runtime.Version() {
		return errors.New("manifest identity/schema mismatch")
	}
	for i, name := range payloadNames[:2] {
		record, err := inspectBinary(files[name], name, revision, version, m.Toolchain)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		got, err := json.Marshal(record)
		if err != nil {
			return err
		}
		want, err := json.Marshal(m.Binaries[i])
		if err != nil || !bytes.Equal(got, want) {
			return errors.New("manifest does not match actual executable inspection")
		}
	}
	return nil
}
