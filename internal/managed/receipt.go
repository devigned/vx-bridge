// Package managed enforces the fixed MSI-owned copy's admission boundary.
// Portable copies outside the owned directory do not acquire a managed identity.
// [Admit] runs before host listeners/windows, checks durable maintenance denial
// and payload/build identity on Windows, and retains native ownership pins.
// It does not invoke Windows Installer or repair files during ordinary startup.
// [DecodeReceipt] also supports offline format checks on other platforms.
package managed

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ReceiptSize is the exact native VX_RECEIPT encoding length, in bytes.
const ReceiptSize = 640

// UpgradeCode is the fixed MSI product-family identity shared with the installer.
const UpgradeCode = "{5A5BB970-EB68-4550-AF42-F013F92DB3D8}"

// ErrDenied indicates that a managed start could not verify maintenance state,
// ownership, or payload identity and requires explicit Setup repair. Admit
// wraps/joins it with detailed failures so callers can use errors.Is.
var ErrDenied = errors.New("Repair required: run Setup and choose Repair / Reinstall; managed installation state or owned files could not be verified; do not force-close the bridge")
var guidPattern = regexp.MustCompile(`^\{[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}\}$`)
var payloadNames = []string{"GSPconnect.exe", "vxbridge.exe", "LICENSE", "THIRD_PARTY_NOTICES.txt", "build-manifest.json", "SHA256SUMS"}

// Receipt is encoded identically to VX_RECEIPT in installer/managed/state.h.
// There are no caller-configurable paths, user SID overrides or admission flags.
type Receipt struct {
	// Product and Package are distinct nonzero uppercase MSI GUIDs.
	// Version is canonical numeric major.minor.build in MSI's component ranges.
	// Revision is the 40-character lowercase hexadecimal Git source revision.
	Product, Package, Version, Revision string
	// Hashes contains lowercase SHA-256 digests in fixed payload order:
	// GSPconnect.exe, vxbridge.exe, LICENSE, THIRD_PARTY_NOTICES.txt,
	// build-manifest.json, and SHA256SUMS.
	Hashes [6]string
}

func hexText(value string, length int) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == length && hex.EncodeToString(data) == value
}

func numericVersion(value string) bool {
	fields := strings.Split(value, ".")
	if len(fields) != 3 {
		return false
	}
	for i, field := range fields {
		bits := 8
		if i == 2 {
			bits = 16
		}
		n, err := strconv.ParseUint(field, 10, bits)
		if err != nil || strconv.FormatUint(n, 10) != field {
			return false
		}
	}
	return true
}

// DecodeReceipt returns a validated native receipt or ErrDenied.
// It requires the exact size/magic/schema, printable NUL-padded fields,
// canonical GUID/version/revision/digest text, and zero reserved bytes.
// The result owns its strings; data is not retained or modified. Decoding proves
// only format, not registration ownership, matching files, or current admission.
func DecodeReceipt(data []byte) (Receipt, error) {
	var r Receipt
	if len(data) != ReceiptSize || !bytes.Equal(data[:8], []byte("VXMSI01\x00")) ||
		binary.LittleEndian.Uint32(data[8:]) != ReceiptSize || binary.LittleEndian.Uint32(data[12:]) != 1 {
		return r, ErrDenied
	}
	at := 16
	read := func(width int) (string, bool) {
		field := data[at : at+width]
		at += width
		end := bytes.IndexByte(field, 0)
		if end < 0 || !bytes.Equal(field[end:], make([]byte, width-end)) {
			return "", false
		}
		for _, b := range field[:end] {
			if b < 0x20 || b > 0x7e {
				return "", false
			}
		}
		return string(field[:end]), true
	}
	for _, field := range []struct {
		out   *string
		width int
	}{{&r.Product, 39}, {&r.Package, 39}, {&r.Version, 32}, {&r.Revision, 41}} {
		value, ok := read(field.width)
		if !ok {
			return Receipt{}, ErrDenied
		}
		*field.out = value
	}
	for i := range r.Hashes {
		value, ok := read(65)
		if !ok || !hexText(value, 32) {
			return Receipt{}, ErrDenied
		}
		r.Hashes[i] = value
	}
	if !bytes.Equal(data[at:], make([]byte, len(data)-at)) ||
		!guidPattern.MatchString(r.Product) || !guidPattern.MatchString(r.Package) ||
		r.Product == "{00000000-0000-0000-0000-000000000000}" || r.Package == "{00000000-0000-0000-0000-000000000000}" ||
		r.Product == r.Package || !numericVersion(r.Version) || !hexText(r.Revision, 20) {
		return Receipt{}, ErrDenied
	}
	return r, nil
}

func checkBuild(r Receipt, revision, version string) error {
	expected := "v" + r.Version
	if version == "development" && r.Version == "0.0.0" {
		expected = "development"
	}
	if !hexText(revision, 20) || r.Revision != revision || version != expected {
		return fmt.Errorf("%w: installed receipt and running build differ", ErrDenied)
	}
	return nil
}

func managedLocation(image, actual, root string) (bool, error) {
	owned := strings.EqualFold(filepath.Dir(image), root) || strings.EqualFold(filepath.Dir(actual), root)
	if owned && (!strings.EqualFold(image, filepath.Join(root, "GSPconnect.exe")) || !strings.EqualFold(actual, image)) {
		return true, ErrDenied
	}
	return owned, nil
}
