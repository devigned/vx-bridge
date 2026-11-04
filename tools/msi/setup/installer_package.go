package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var installerFiles = []string{"setup.exe", "bridge.msi", "setup-input.json", "msi-inspection.json",
	"INSTALLER-NOTICE.txt", "MINGW-COPYRIGHT.txt", "LICENSE", "THIRD_PARTY_NOTICES.txt"}

var packageCodePattern = regexp.MustCompile(`^\{[0-9A-F]{8}(?:-[0-9A-F]{4}){3}-[0-9A-F]{12}\}$`)
var packageHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func installerNames(revision, version string) (string, string) {
	identity := revision
	publicIdentity := revision
	if version != "development" {
		identity = version + "-" + revision
		publicIdentity = version
	}
	return "vx-bridge-windows-amd64-" + identity + ".zip",
		"vx-bridge-installer-windows-amd64-" + publicIdentity + ".exe"
}

func installerPackage(revision, version, archive, authored, copyright, stubPath, out, verify string, extra int) error {
	if len(revision) != 40 || strings.Trim(revision, "0123456789abcdef") != "" || extra != 0 ||
		(authored == "") == (verify == "") {
		return errors.New("require exact revision/version and either -installer or -verify-installer")
	}
	if _, err := numeric(version); err != nil {
		return err
	}
	if (verify != "" && (out != "" || copyright != "" || stubPath != "" || archive != "")) ||
		(authored != "" && (out == "" || copyright == "" || stubPath == "" || archive == "")) {
		return errors.New("only installer packaging requires -archive, -out, -copyright and -bundle-stub")
	}
	applicationName, installerName := installerNames(revision, version)
	if verify != "" {
		if filepath.Base(verify) != installerName {
			return errors.New("installer filename differs from exact version")
		}
		data, err := readPackageFile(verify)
		if err != nil {
			return err
		}
		return verifyInstallerEXE(data, revision, version)
	}
	if err := command("go", "run", "-mod=readonly", "./tools/dist", "-revision", revision, "-version", version, "-verify", archive); err != nil {
		return fmt.Errorf("authoritative application verification: %w", err)
	}
	app, err := readPackageZIP(archive, files)
	if err != nil {
		return err
	}
	if filepath.Base(archive) != applicationName {
		return errors.New("archive filename differs from exact source/version")
	}
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != revision {
		return errors.New("installer packaging requires exact HEAD")
	}
	status, err := exec.Command("git", "status", "--porcelain", "--untracked-files=all", "--ignored=matching").Output()
	if err != nil || len(status) != 0 {
		return errors.New("installer packaging requires clean committed source")
	}
	payload := make(map[string][]byte)
	for _, name := range installerFiles {
		path := filepath.Join(authored, name)
		switch name {
		case "MINGW-COPYRIGHT.txt":
			path = copyright
		case "LICENSE", "THIRD_PARTY_NOTICES.txt":
			payload[name] = app[name]
			continue
		}
		payload[name], err = readPackageFile(path)
		if err != nil {
			return err
		}
	}
	payload["SHA256SUMS"] = installerChecksums(payload)
	if err := verifyInstallerPayload(payload, app, revision, version); err != nil {
		return err
	}
	stub, err := readPackageFile(stubPath)
	if err != nil {
		return err
	}
	payload["application.zip"], err = readPackageFile(archive)
	if err != nil {
		return err
	}
	bundle, err := installerEXE(stub, payload, revision)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(out) {
		return errors.New("installer output must be a new absolute external directory")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(out))
	if err != nil {
		return err
	}
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return err
	}
	out = filepath.Join(parent, filepath.Base(out))
	relative, err := filepath.Rel(strings.TrimSpace(string(root)), out)
	if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return errors.New("installer output must be outside the source checkout")
	}
	if err := os.Mkdir(out, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(out, installerName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(bundle)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, installerName+".sha256"),
		[]byte(digest(bundle)+"  "+installerName+"\n"), 0o644); err != nil {
		return err
	}
	written, err := readPackageFile(filepath.Join(out, installerName))
	if err != nil {
		return err
	}
	if err := verifyInstallerEXE(written, revision, version); err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func readPackageFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<20 {
		return nil, fmt.Errorf("nonregular, empty or oversized package input: %s", path)
	}
	return os.ReadFile(path)
}

func readPackageZIP(path string, names []string) (map[string][]byte, error) {
	data, err := readPackageFile(path)
	if err != nil {
		return nil, err
	}
	return readPackageZIPBytes(data, names)
}

func readPackageZIPBytes(data []byte, names []string) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(reader.File) != len(names) {
		return nil, errors.New("unexpected package entry count")
	}
	payload := make(map[string][]byte)
	for _, entry := range reader.File {
		if !slices.Contains(names, entry.Name) || payload[entry.Name] != nil ||
			!entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > 64<<20 {
			return nil, errors.New("unexpected, duplicate, nonregular, empty or oversized package entry")
		}
		stream, err := entry.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(stream, (64<<20)+1))
		if err := errors.Join(readErr, stream.Close()); err != nil {
			return nil, err
		}
		if len(data) > 64<<20 {
			return nil, errors.New("oversized package entry")
		}
		payload[entry.Name] = data
	}
	return payload, nil
}

func installerChecksums(payload map[string][]byte) []byte {
	var sums strings.Builder
	for _, name := range installerFiles {
		fmt.Fprintf(&sums, "%s  %s\n", digest(payload[name]), name)
	}
	return []byte(sums.String())
}

func verifyInstallerPayload(payload, app map[string][]byte, revision, version string) error {
	if len(payload) != len(installerFiles)+1 || !bytes.Equal(payload["SHA256SUMS"], installerChecksums(payload)) {
		return errors.New("installer entries/checksums differ")
	}
	for _, name := range installerFiles {
		if len(payload[name]) == 0 {
			return fmt.Errorf("missing installer input %s", name)
		}
	}
	if err := checkPEBytes(payload["setup.exe"], false); err != nil {
		return fmt.Errorf("installer Setup identity: %w", err)
	}
	if !bytes.HasPrefix(payload["bridge.msi"], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
		return errors.New("installer is not an MSI compound database")
	}
	var authored struct {
		Schema                           int
		Phase, Version, Product, Package string
		Source                           string `json:"source_revision"`
		Profile                          string `json:"build_profile"`
		MSIVersion                       string `json:"msi_version"`
		MSIHash                          string `json:"msi_sha256"`
		SetupHash                        string `json:"setup_sha256"`
		ActionsHash                      string `json:"actions_sha256"`
		PreparedHash                     string `json:"prepared_input_sha256"`
		Upgrade                          string
	}
	var inspection struct {
		Schema       int
		Package      string
		Source       string `json:"source_revision"`
		Profile      string `json:"build_profile"`
		MSIHash      string `json:"msi_sha256"`
		PreparedHash string `json:"prepared_input_sha256"`
		Properties   map[string]string
	}
	// Windows emits UTF-8 with a BOM; decode the actual bytes, not reserialized metadata.
	for name, target := range map[string]any{"setup-input.json": &authored, "msi-inspection.json": &inspection} {
		data := bytes.TrimPrefix(payload[name], []byte{0xef, 0xbb, 0xbf})
		if err := json.Unmarshal(data, target); err != nil {
			return err
		}
		if err := verifyValidationPolicy(data); err != nil {
			return err
		}
	}
	number, err := numeric(version)
	if err != nil {
		return err
	}
	if authored.Schema != 2 || inspection.Schema != 2 || authored.Phase != "authored-msi" ||
		authored.Source != revision || inspection.Source != revision || authored.Version != version || authored.MSIVersion != number ||
		authored.Profile != normalProfile || inspection.Profile != normalProfile ||
		authored.Product != guid("product/"+version+"/"+revision) || authored.Upgrade != upgrade ||
		!packageCodePattern.MatchString(authored.Package) || authored.Package == "{00000000-0000-0000-0000-000000000000}" ||
		authored.Package != inspection.Package || authored.Package == authored.Product || authored.Package == authored.Upgrade ||
		authored.MSIHash != digest(payload["bridge.msi"]) || inspection.MSIHash != authored.MSIHash ||
		authored.SetupHash != digest(payload["setup.exe"]) || !packageHashPattern.MatchString(authored.ActionsHash) ||
		!packageHashPattern.MatchString(authored.PreparedHash) || inspection.PreparedHash != authored.PreparedHash {
		return errors.New("installer source/version/profile/emitted package/hash identity differs")
	}
	for name, expected := range map[string]string{"ProductCode": authored.Product, "ProductVersion": number, "UpgradeCode": upgrade,
		"VX_SOURCE_REVISION": revision, "VX_OWNER_SHA256": authored.SetupHash, "VX_SETUP_PROTOCOL": "1"} {
		if inspection.Properties[name] != expected {
			return fmt.Errorf("inspected installer property differs: %s", name)
		}
	}
	for i, name := range files {
		property := []string{"GUI", "CONTROL", "LICENSE", "NOTICES", "MANIFEST", "SUMS"}[i]
		if inspection.Properties["VX_HASH_"+property] != digest(app[name]) {
			return fmt.Errorf("inspected MSI payload differs from application ZIP: %s", name)
		}
	}
	for _, name := range []string{"LICENSE", "THIRD_PARTY_NOTICES.txt"} {
		if !bytes.Equal(payload[name], app[name]) {
			return errors.New("installer application notices differ")
		}
	}
	return nil
}
