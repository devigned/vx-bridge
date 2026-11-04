package main

import (
	"archive/zip"
	"bytes"
	"debug/pe"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Synthetic static PE/import tables exercise inspection without native execution.
func installerTestPE() []byte {
	data := make([]byte, 2048)
	copy(data, "MZ")
	binary.LittleEndian.PutUint32(data[0x3c:], 0x80)
	copy(data[0x80:], "PE\x00\x00")
	header := pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_AMD64, NumberOfSections: 1,
		SizeOfOptionalHeader: 240, Characteristics: pe.IMAGE_FILE_EXECUTABLE_IMAGE}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, header)
	copy(data[0x84:], buf.Bytes())
	buf.Reset()
	optional := pe.OptionalHeader64{Magic: 0x20b, Subsystem: pe.IMAGE_SUBSYSTEM_WINDOWS_GUI,
		DllCharacteristics:  pe.IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE | pe.IMAGE_DLLCHARACTERISTICS_NX_COMPAT,
		NumberOfRvaAndSizes: 16}
	optional.DataDirectory[1] = pe.DataDirectory{VirtualAddress: 0x1000, Size: 40}
	_ = binary.Write(&buf, binary.LittleEndian, optional)
	copy(data[0x98:], buf.Bytes())
	buf.Reset()
	section := pe.SectionHeader32{Name: [8]uint8{'.', 'i', 'd', 'a', 't', 'a'},
		VirtualSize: 1536, VirtualAddress: 0x1000, SizeOfRawData: 1536, PointerToRawData: 512}
	_ = binary.Write(&buf, binary.LittleEndian, section)
	copy(data[0x188:], buf.Bytes())
	binary.LittleEndian.PutUint32(data[512:], 0x1100)
	binary.LittleEndian.PutUint32(data[524:], 0x1080)
	binary.LittleEndian.PutUint32(data[528:], 0x1100)
	copy(data[640:], "msi.dll\x00")
	offset := 896
	for i, name := range []string{"MsiInstallProductW", "MsiEnumProductsExW", "TaskDialogIndirect",
		"CreateProcessW", "GetNamedPipeServerProcessId", "RegFlushKey"} {
		binary.LittleEndian.PutUint64(data[768+i*8:], uint64(0x1000+offset-512))
		copy(data[offset+2:], name+"\x00")
		offset += len(name) + 3
	}
	return data
}

func installerTestPayload(t *testing.T) (map[string][]byte, map[string][]byte, string, string) {
	t.Helper()
	revision, version := strings.Repeat("a", 40), "v1.2.3"
	app := map[string][]byte{}
	for _, name := range files {
		app[name] = []byte("test app " + name)
	}
	payload := map[string][]byte{}
	for _, name := range installerFiles {
		payload[name] = []byte("test installer " + name)
	}
	payload["setup.exe"] = installerTestPE()
	payload["bridge.msi"] = []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1, 1}
	for _, name := range []string{"LICENSE", "THIRD_PARTY_NOTICES.txt"} {
		payload[name] = app[name]
	}
	authored := map[string]any{
		"schema": 2, "phase": "authored-msi", "build_profile": normalProfile,
		"source_revision": revision, "version": version, "msi_version": "1.2.3",
		"product": guid("product/" + version + "/" + revision), "upgrade": upgrade,
		"package":    "{01234567-89AB-4CDE-8F01-23456789ABCD}",
		"msi_sha256": digest(payload["bridge.msi"]), "setup_sha256": digest(payload["setup.exe"]),
		"actions_sha256": strings.Repeat("b", 64), "prepared_input_sha256": strings.Repeat("c", 64),
		"warning_exceptions": []any{map[string]any{"tool": "light.exe", "code": 1008,
			"table": "AdvtExecuteSequence", "actions": omittedAdvertise}},
		"ice_exceptions": []string{"ICE91", "ICE96"},
	}
	properties := map[string]string{"ProductCode": authored["product"].(string), "UpgradeCode": upgrade,
		"ProductVersion": "1.2.3", "VX_SOURCE_REVISION": revision, "VX_OWNER_SHA256": digest(payload["setup.exe"]),
		"VX_SETUP_PROTOCOL": "1"}
	for i, name := range files {
		property := []string{"GUI", "CONTROL", "LICENSE", "NOTICES", "MANIFEST", "SUMS"}[i]
		properties["VX_HASH_"+property] = digest(app[name])
	}
	inspection := map[string]any{"schema": 2, "build_profile": normalProfile, "source_revision": revision,
		"package": authored["package"], "msi_sha256": authored["msi_sha256"],
		"prepared_input_sha256": authored["prepared_input_sha256"], "properties": properties,
		"warning_exceptions": authored["warning_exceptions"], "ice_exceptions": authored["ice_exceptions"]}
	var err error
	payload["setup-input.json"], err = json.Marshal(authored)
	if err != nil {
		t.Fatal(err)
	}
	payload["msi-inspection.json"], err = json.Marshal(inspection)
	if err != nil {
		t.Fatal(err)
	}
	payload["SHA256SUMS"] = installerChecksums(payload)
	return payload, app, revision, version
}

func TestInstallerNativePERejectsUntrustedInputs(t *testing.T) {
	for _, name := range []string{"machine", "dll", "console", "clr", "hardening", "import", "forbidden-api", "missing-api", "oversized"} {
		t.Run(name, func(t *testing.T) {
			data := installerTestPE()
			switch name {
			case "machine":
				binary.LittleEndian.PutUint16(data[0x84:], pe.IMAGE_FILE_MACHINE_ARM64)
			case "dll":
				binary.LittleEndian.PutUint16(data[0x96:], pe.IMAGE_FILE_DLL)
			case "console":
				binary.LittleEndian.PutUint16(data[0x98+68:], pe.IMAGE_SUBSYSTEM_WINDOWS_CUI)
			case "clr":
				binary.LittleEndian.PutUint32(data[0x98+112+14*8:], 0x1300)
			case "hardening":
				binary.LittleEndian.PutUint16(data[0x98+70:], 0)
			case "import":
				copy(data[640:], "evil.dll\x00")
			case "forbidden-api":
				copy(data[bytes.Index(data, []byte("RegFlushKey\x00")):], "TerminateProcess\x00")
			case "missing-api":
				copy(data[bytes.Index(data, []byte("TaskDialogIndirect\x00")):], "MissingAPI\x00")
			case "oversized":
				data = make([]byte, (2<<20)+1)
			}
			if err := checkPEBytes(data, false); err == nil {
				t.Fatal("untrusted native Setup image accepted")
			}
		})
	}
}

func TestInstallerBundleContract(t *testing.T) {
	payload, app, revision, version := installerTestPayload(t)
	if err := verifyInstallerPayload(payload, app, revision, version); err != nil {
		t.Fatal(err)
	}
	applicationName, installerName, msiName := installerNames(revision, version)
	if applicationName != "vx-bridge-windows-amd64-"+version+"-"+revision+".zip" ||
		installerName != "vx-bridge-installer-windows-amd64-"+version+"-"+revision+".zip" ||
		msiName != strings.TrimSuffix(applicationName, ".zip")+".msi" {
		t.Fatal("unexpected version/source asset names")
	}
	_, developmentName, _ := installerNames(revision, "development")
	if strings.Contains(developmentName, "development") || !strings.Contains(developmentName, revision) {
		t.Fatal("development identity does not match app package convention")
	}
	path := filepath.Join(t.TempDir(), installerName)
	if err := writeInstallerZIP(path, payload); err != nil {
		t.Fatal(err)
	}
	written, err := readPackageZIP(path, append(slices.Clone(installerFiles), "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyInstallerPayload(written, app, revision, version); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written["bridge.msi"], payload["bridge.msi"]) || !bytes.Equal(written["setup.exe"], payload["setup.exe"]) {
		t.Fatal("bundle changed exact authored Setup/MSI bytes")
	}
	if err := writeInstallerZIP(path, payload); err == nil {
		t.Fatal("existing installer archive was overwritten")
	}
}

func TestInstallerBundleRejectsChangedIdentity(t *testing.T) {
	for _, name := range []string{"checksum", "setup", "msi", "package", "version", "source", "profile",
		"owner", "payload", "notice", "missing-setup", "extra-file", "warning-waiver", "ice-waiver", "prepared-hash"} {
		t.Run(name, func(t *testing.T) {
			payload, app, revision, version := installerTestPayload(t)
			var authored, inspection map[string]any
			if err := json.Unmarshal(payload["setup-input.json"], &authored); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(payload["msi-inspection.json"], &inspection); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "checksum":
				payload["SHA256SUMS"] = []byte("invalid")
			case "setup":
				payload["setup.exe"] = []byte("wrong setup")
			case "msi":
				payload["bridge.msi"] = []byte("wrong MSI")
			case "package":
				authored["package"] = guid("guessed-package")
			case "version":
				authored["version"] = "v1.2.4"
			case "source":
				authored["source_revision"] = strings.Repeat("b", 40)
			case "profile":
				authored["build_profile"] = lifecycleFaultProfile
			case "owner":
				inspection["properties"].(map[string]any)["VX_OWNER_SHA256"] = strings.Repeat("d", 64)
			case "payload":
				app["GSPconnect.exe"] = []byte("different app")
			case "notice":
				payload["LICENSE"] = []byte("different license")
			case "missing-setup":
				delete(payload, "setup.exe")
			case "extra-file":
				payload["private-worker.exe"] = []byte("not a release")
			case "warning-waiver":
				authored["warning_exceptions"] = []any{}
			case "ice-waiver":
				inspection["ice_exceptions"] = []string{"ICE91", "ICE96", "ICE64"}
			case "prepared-hash":
				inspection["prepared_input_sha256"] = strings.Repeat("e", 64)
			}
			payload["setup-input.json"], _ = json.Marshal(authored)
			payload["msi-inspection.json"], _ = json.Marshal(inspection)
			if name != "checksum" {
				payload["SHA256SUMS"] = installerChecksums(payload)
			}
			if err := verifyInstallerPayload(payload, app, revision, version); err == nil {
				t.Fatal("invalid installer bundle accepted")
			}
		})
	}
}

func TestInstallerZIPRejectsUnexpectedEntries(t *testing.T) {
	for _, name := range []string{"traversal", "duplicate", "nonregular", "empty"} {
		t.Run(name, func(t *testing.T) {
			var data bytes.Buffer
			writer := zip.NewWriter(&data)
			for i := range 2 {
				header := &zip.FileHeader{Name: []string{"setup.exe", "bridge.msi"}[i]}
				header.SetMode(0o644)
				if i == 1 {
					switch name {
					case "traversal":
						header.Name = "../bridge.msi"
					case "duplicate":
						header.Name = "setup.exe"
					case "nonregular":
						header.SetMode(os.ModeSymlink | 0o777)
					}
				}
				entry, err := writer.CreateHeader(header)
				if err != nil {
					t.Fatal(err)
				}
				if name != "empty" || i != 1 {
					if _, err := entry.Write([]byte("synthetic entry")); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "invalid.zip")
			if err := os.WriteFile(path, data.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := readPackageZIP(path, []string{"setup.exe", "bridge.msi"}); err == nil {
				t.Fatal("untrusted archive entry accepted")
			}
		})
	}
}

func TestInstallerPackageFilesAreBoundedAndRegular(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPackageFile(path); err == nil {
		t.Fatal("empty package input accepted")
	}
	if _, err := readPackageFile(filepath.Dir(path)); err == nil {
		t.Fatal("directory package input accepted")
	}
}

func TestInstallerPackageWritesExactSixReleaseFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake-tool packaging fixture runs in portable CI; static bundle tests run on Windows")
	}
	payload, app, _, version := installerTestPayload(t)
	root := t.TempDir()
	for _, args := range [][]string{{"init", "--quiet", root}, {"-C", root, "config", "user.name", "Fixture"},
		{"-C", root, "config", "user.email", "fixture@example.invalid"}} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatal(string(output), err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "source"), []byte("synthetic package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-C", root, "add", "source"},
		{"-C", root, "-c", "core.hooksPath=" + filepath.Join(root, "no-hooks"), "commit", "--quiet", "--no-gpg-sign",
			"-m", "Create fixture", "-m", "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"}} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatal(string(output), err)
		}
	}
	revisionBytes, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.TrimSpace(string(revisionBytes))
	var authored, inspection map[string]any
	for name, target := range map[string]*map[string]any{"setup-input.json": &authored, "msi-inspection.json": &inspection} {
		if err := json.Unmarshal(payload[name], target); err != nil {
			t.Fatal(err)
		}
		(*target)["source_revision"] = revision
	}
	authored["product"] = guid("product/" + version + "/" + revision)
	properties := inspection["properties"].(map[string]any)
	properties["ProductCode"], properties["VX_SOURCE_REVISION"] = authored["product"], revision
	payload["setup-input.json"], _ = json.Marshal(authored)
	payload["msi-inspection.json"], _ = json.Marshal(inspection)
	payload["SHA256SUMS"] = installerChecksums(payload)
	media := t.TempDir()
	for name, data := range payload {
		if err := os.WriteFile(filepath.Join(media, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	appName, installerName, msiName := installerNames(revision, version)
	archive := filepath.Join(t.TempDir(), appName)
	var archiveBytes bytes.Buffer
	writer := zip.NewWriter(&archiveBytes)
	for _, name := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(app[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, archiveBytes.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	stubs := t.TempDir()
	// Real application verification has separate end-to-end tests in tools/dist.
	// This stub isolates installer packaging from the synthetic application bytes.
	stub := "#!/bin/sh\n[ \"$1 $2 $3 $4\" = 'run -mod=readonly ./tools/dist -revision' ]\n"
	if err := os.WriteFile(filepath.Join(stubs, "go"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(root)
	out := filepath.Join(t.TempDir(), "release assets")
	if err := installerPackage(revision, version, archive, media, filepath.Join(media, "MINGW-COPYRIGHT.txt"), out, "", 0); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) != 6 {
		t.Fatal("release output must have exactly three assets plus checksums", entries, err)
	}
	for _, name := range []string{appName, installerName, msiName} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		sums, err := os.ReadFile(filepath.Join(out, name+".sha256"))
		if err != nil || string(sums) != digest(data)+"  "+name+"\n" {
			t.Fatal("release checksum must bind the exact version/source filename", string(sums), err)
		}
		if name == appName && !bytes.Equal(data, archiveBytes.Bytes()) {
			t.Fatal("application ZIP was repackaged")
		}
		if name == msiName && !bytes.Equal(data, payload["bridge.msi"]) {
			t.Fatal("direct MSI differs from authored bytes")
		}
	}
	if err := installerPackage(revision, version, filepath.Join(out, appName), "", "", "", filepath.Join(out, installerName), 0); err != nil {
		t.Fatal(err)
	}
}
