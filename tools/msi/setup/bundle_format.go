package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

const bundleFooterSize = 448
const bundleFileLimit = 32 << 20

func bundleNames() []string {
	return append(slices.Clone(installerFiles), "SHA256SUMS", "application.zip")
}

func checkBundleStub(stub []byte, revision string) error {
	if err := checkNativePE(stub, false, []string{"CreateProcessW", "MsiOpenDatabaseW",
		"BCryptHashData", "SetFileInformationByHandle", "GetFileInformationByHandle"}); err != nil {
		return fmt.Errorf("native bundle stub: %w", err)
	}
	if !bytes.Contains(stub, []byte("VX_BUNDLE_SOURCE="+revision+"\x00")) {
		return errors.New("bundle stub source differs")
	}
	return nil
}

func installerEXE(stub []byte, payload map[string][]byte, revision string) ([]byte, error) {
	if len(payload) != len(bundleNames()) {
		return nil, errors.New("unexpected embedded installer entry count")
	}
	if err := checkBundleStub(stub, revision); err != nil {
		return nil, err
	}
	footer := make([]byte, bundleFooterSize)
	copy(footer, "VXBDL001")
	binary.LittleEndian.PutUint64(footer[8:], uint64(len(stub)))
	copy(footer[16:56], revision)
	hash := sha256.Sum256(stub)
	copy(footer[56:88], hash[:])
	data := slices.Clone(stub)
	for i, name := range bundleNames() {
		entry := payload[name]
		if len(entry) == 0 || len(entry) > bundleFileLimit {
			return nil, fmt.Errorf("missing or oversized embedded file: %s", name)
		}
		offset := 88 + i*36
		binary.LittleEndian.PutUint32(footer[offset:], uint32(len(entry)))
		hash := sha256.Sum256(entry)
		copy(footer[offset+4:], hash[:])
		data = append(data, entry...)
	}
	data = append(data, footer...)
	if len(data) > 64<<20 {
		return nil, errors.New("installer exceeds bundle size limit")
	}
	return data, nil
}

func readInstallerEXE(data []byte, revision string) (map[string][]byte, error) {
	if len(data) < bundleFooterSize || len(data) > 64<<20 {
		return nil, errors.New("installer size outside bundle limits")
	}
	footer := data[len(data)-bundleFooterSize:]
	stubSize := binary.LittleEndian.Uint64(footer[8:])
	if string(footer[:8]) != "VXBDL001" || string(footer[16:56]) != revision ||
		stubSize == 0 || stubSize > 2<<20 || stubSize > uint64(len(data)-bundleFooterSize) {
		return nil, errors.New("installer bundle header/source differs")
	}
	stub := data[:stubSize]
	if digest(stub) != hex.EncodeToString(footer[56:88]) {
		return nil, errors.New("bundle stub hash differs")
	}
	if err := checkBundleStub(stub, revision); err != nil {
		return nil, err
	}
	payload := make(map[string][]byte)
	offset := int(stubSize)
	for i, name := range bundleNames() {
		record := footer[88+i*36 : 88+(i+1)*36]
		size := int(binary.LittleEndian.Uint32(record))
		if size == 0 || size > bundleFileLimit || size > len(data)-bundleFooterSize-offset {
			return nil, fmt.Errorf("invalid embedded file bounds: %s", name)
		}
		entry := data[offset : offset+size]
		if digest(entry) != hex.EncodeToString(record[4:]) {
			return nil, fmt.Errorf("embedded file hash differs: %s", name)
		}
		payload[name] = entry
		offset += size
	}
	if offset != len(data)-bundleFooterSize {
		return nil, errors.New("unexpected bytes outside bundle entries")
	}
	return payload, nil
}

func verifyInstallerEXE(data []byte, revision, version string) (result error) {
	payload, err := readInstallerEXE(data, revision)
	if err != nil {
		return err
	}
	appBytes := payload["application.zip"]
	delete(payload, "application.zip")
	app, err := readPackageZIPBytes(appBytes, files)
	if err != nil {
		return err
	}
	if err := verifyInstallerPayload(payload, app, revision, version); err != nil {
		return err
	}
	directory, err := os.MkdirTemp("", "vx-bridge-verify-")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, os.RemoveAll(directory)) }()
	applicationName, _ := installerNames(revision, version)
	path := filepath.Join(directory, applicationName)
	if err := os.WriteFile(path, appBytes, 0o600); err != nil {
		return err
	}
	if err := command("go", "run", "-mod=readonly", "./tools/dist", "-revision", revision,
		"-version", version, "-verify", path); err != nil {
		return fmt.Errorf("embedded authoritative application verification: %w", err)
	}
	return nil
}
