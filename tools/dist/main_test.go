package main

import (
	"archive/zip"
	"bytes"
	"debug/pe"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWindowsDistribution(t *testing.T) {
	root, revision := newDistributionFixture(t)
	t.Chdir(root)
	output := filepath.Join(t.TempDir(), "first")
	if err := run(revision, developmentVersion, output, "", 0); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 2 {
		t.Fatalf("want only ZIP and external checksum: %v %v", entries, err)
	}
	archive := filepath.Join(output, archiveName(revision, developmentVersion))
	if err := run(revision, developmentVersion, "", archive, 0); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	external, err := os.ReadFile(archive + ".sha256")
	if err != nil || string(external) != digest(data)+"  "+filepath.Base(archive)+"\n" {
		t.Fatal("external archive checksum mismatch", err)
	}
	second := filepath.Join(t.TempDir(), "second")
	if err := run(revision, developmentVersion, second, "", 0); err != nil {
		t.Fatal(err)
	}
	repeated, err := os.ReadFile(filepath.Join(second, filepath.Base(archive)))
	if err != nil || !bytes.Equal(data, repeated) {
		t.Fatal("same source/toolchain must reproduce the ZIP byte for byte", err)
	}
	tagged := filepath.Join(t.TempDir(), "tagged")
	if err := run(revision, "v1.0.1", tagged, "", 0); err != nil {
		t.Fatal(err)
	}
	tagArchive := filepath.Join(tagged, archiveName(revision, "v1.0.1"))
	if err := verifyArchive(tagArchive, revision, "v1.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := verifyArchive(tagArchive, revision, developmentVersion); err == nil {
		t.Fatal("tagged package accepted as development")
	}
	tagFiles := readTestArchive(t, tagArchive)
	for _, name := range payloadNames[:2] {
		record, err := inspectBinary(tagFiles[name], name, revision, "v1.0.1", runtime.Version())
		if err != nil || record.ReleaseVersion != "v1.0.1" {
			t.Fatal("actual linked release-version missing", name, record, err)
		}
		if _, err := inspectBinary(tagFiles[name], name, revision, "v1.0.2", runtime.Version()); err == nil {
			t.Fatal("different linked release version accepted")
		}
	}
	if err := run(strings.Repeat("0", 40), developmentVersion, filepath.Join(t.TempDir(), "wrong"), "", 0); err == nil {
		t.Fatal("wrong HEAD accepted")
	}
	if err := run(revision, developmentVersion, filepath.Join(root, "inside"), "", 0); err == nil {
		t.Fatal("output inside source accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "unexpected.go"), []byte("package unwanted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cleanRevision(root, revision); err == nil {
		t.Fatal("untracked source accepted")
	}
	files := readTestArchive(t, archive)
	for _, test := range []struct {
		name string
		edit func(map[string][]byte)
	}{
		{"corrupt_binary", func(files map[string][]byte) { files["vxbridge.exe"][0] ^= 1 }},
		{"missing_notice", func(files map[string][]byte) { delete(files, "LICENSE") }},
		{"manifest_identity", func(files map[string][]byte) {
			var m manifest
			if err := json.Unmarshal(files["build-manifest.json"], &m); err != nil {
				t.Fatal(err)
			}
			m.Binaries[0].DefaultDesktop = "false"
			files["build-manifest.json"], err = json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"manifest_version", func(files map[string][]byte) {
			files["build-manifest.json"] = bytes.ReplaceAll(files["build-manifest.json"],
				[]byte(`"release_version": "development"`), []byte(`"release_version": "v1.0.1"`))
		}},
		{"wrong_subsystem", func(files map[string][]byte) {
			image := files["GSPconnect.exe"]
			header := binary.LittleEndian.Uint32(image[0x3c:])
			binary.LittleEndian.PutUint16(image[int(header)+24+68:], 3)
		}},
		{"clr_header", func(files map[string][]byte) {
			image := files["GSPconnect.exe"]
			header := binary.LittleEndian.Uint32(image[0x3c:])
			binary.LittleEndian.PutUint32(image[int(header)+24+112+14*8:], 1)
		}},
		{"actual_desktop", func(files map[string][]byte) {
			image, err := pe.NewFile(bytes.NewReader(files["GSPconnect.exe"]))
			if err != nil {
				t.Fatal(err)
			}
			defer image.Close()
			for _, symbol := range image.Symbols {
				if symbol.Name == "main.defaultDesktop" {
					section := image.Sections[symbol.SectionNumber-1]
					at := uint64(section.Offset) + uint64(symbol.Value) + 8
					binary.LittleEndian.PutUint64(files["GSPconnect.exe"][at:at+8], 0)
					return
				}
			}
			t.Fatal("fixture lacks defaultDesktop symbol")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := make(map[string][]byte)
			for name, data := range files {
				changed[name] = slices.Clone(data)
			}
			test.edit(changed)
			path := filepath.Join(t.TempDir(), "bad.zip")
			if err := writeArchive(path, changed, time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := verifyArchive(path, revision, developmentVersion); err == nil {
				t.Fatal("tampered archive passed static verification")
			}
		})
	}
	t.Run("unexpected_archive_entry", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "extra.zip")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		writer := zip.NewWriter(file)
		for _, name := range append(slices.Clone(payloadNames), "SHA256SUMS", "../unwanted") {
			entry, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte("invented")); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if err := verifyArchive(path, revision, developmentVersion); err == nil {
			t.Fatal("extra/path-traversal entry accepted")
		}
	})
	info := files["vxbridge.exe"]
	record, err := inspectBinary(info, "vxbridge.exe", revision, developmentVersion, runtime.Version())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"CGO_ENABLED", "vcs.modified", "vcs.revision", "GOOS"} {
		t.Run("metadata_"+key, func(t *testing.T) {
			clone := *record.BuildInfo
			clone.Settings = slices.Clone(clone.Settings)
			for i := range clone.Settings {
				if clone.Settings[i].Key == key {
					clone.Settings[i].Value = "wrong"
				}
			}
			if err := verifyBuildInfo(&clone, revision, runtime.Version()); err == nil {
				t.Fatal("invalid embedded metadata accepted")
			}
		})
	}
}

func newDistributionFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		"go.mod": "module " + modulePath + "\n\ngo 1.27.0\n\n" +
			"require example.invalid/unused v0.0.0\nreplace example.invalid/unused => ./unused\n",
		"unused/go.mod":           "module example.invalid/unused\n\ngo 1.27.0\n",
		".go-version":             strings.TrimPrefix(runtime.Version(), "go") + "\n",
		"LICENSE":                 "Invented distribution-test license\n",
		"THIRD_PARTY_NOTICES.txt": "Invented distribution-test notices\n",
		"cmd/vxbridge/main.go": `package main
import "fmt"
var buildRevision = "development"
var defaultDesktop = "false"
var releaseVersion = "development"
func main() { fmt.Println(buildRevision, defaultDesktop, releaseVersion) }
`,
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"config", "user.name", "Distribution Test"},
		{"config", "user.email", "dist@example.invalid"},
		{"add", "."},
		{"-c", "user.name=Distribution Test", "-c", "user.email=dist@example.invalid",
			"-c", "core.hooksPath=" + filepath.Join(root, "no-hooks"), "commit", "--quiet", "--no-gpg-sign",
			"-m", "Create invented static test fixture", "-m",
			"Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"},
	} {
		if _, err := command(root, nil, "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	revision, err := command(root, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return root, revision
}

func readTestArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	files := make(map[string][]byte)
	for _, entry := range reader.File {
		data, err := reader.Open(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		var buffer bytes.Buffer
		if _, err := buffer.ReadFrom(data); err != nil {
			t.Fatal(err)
		}
		if err := data.Close(); err != nil {
			t.Fatal(err)
		}
		files[entry.Name] = buffer.Bytes()
	}
	return files
}

func TestRevisionArguments(t *testing.T) {
	for _, revision := range []string{"main", "HEAD", strings.Repeat("A", 40), strings.Repeat("a", 39), ""} {
		if validRevision(revision) {
			t.Fatalf("invalid exact revision accepted: %q", revision)
		}
	}
	if err := run(strings.Repeat("a", 40), developmentVersion, "", "", 0); err == nil {
		t.Fatal("missing build/verify mode accepted")
	}
	for _, version := range []string{"", "1.0.1", "v01.0.1", "v1.0.1-pilot.1", "v1.0.1+meta", "v1.0.1;exit 0", "v" + strings.Repeat("1", 65) + ".0.1"} {
		if validVersion(version) {
			t.Fatalf("unsupported release version accepted: %q", version)
		}
	}
	for _, version := range []string{developmentVersion, "v0.0.0", "v1.0.1", "v10.20.30"} {
		if !validVersion(version) {
			t.Fatalf("supported release version rejected: %q", version)
		}
	}
}
