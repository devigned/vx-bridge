package managed

import (
	"encoding/binary"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func receiptFixture() []byte {
	data := make([]byte, ReceiptSize)
	copy(data, "VXMSI01\x00")
	binary.LittleEndian.PutUint32(data[8:], ReceiptSize)
	binary.LittleEndian.PutUint32(data[12:], 1)
	at := 16
	write := func(value string, width int) { copy(data[at:], value); at += width }
	write("{09A2F71C-E8C6-46A5-BD31-A835F38334E8}", 39)
	write("{8A2E5118-4F91-469B-815D-187EB8477AF9}", 39)
	write("1.0.1", 32)
	write(strings.Repeat("a", 40), 41)
	for range 6 {
		write(strings.Repeat("b", 64), 65)
	}
	return data
}

func TestReceiptWireFormatAndBuildIdentity(t *testing.T) {
	r, err := DecodeReceipt(receiptFixture())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "1.0.1" || r.Revision != strings.Repeat("a", 40) || r.Hashes[5] != strings.Repeat("b", 64) {
		t.Fatal("wire offsets differ from native receipt")
	}
	if err := checkBuild(r, strings.Repeat("a", 40), "v1.0.1"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct{ revision, version string }{
		{strings.Repeat("c", 40), "v1.0.1"}, {strings.Repeat("a", 40), "v1.0.2"},
		{"development", "development"}, {strings.Repeat("a", 40), "development"},
	} {
		if err := checkBuild(r, value.revision, value.version); !errors.Is(err, ErrDenied) {
			t.Fatal("another build/receipt accepted", value, err)
		}
	}
	r.Version = "0.0.0"
	if err := checkBuild(r, strings.Repeat("a", 40), "development"); err != nil {
		t.Fatal("honest development identity refused", err)
	}
}

func TestMalformedOwnershipNeverAdmits(t *testing.T) {
	for _, name := range []string{"short", "trailing", "magic", "size", "schema", "nul-padding", "reserved",
		"product", "zero-guid", "same-guid", "version", "revision", "hash", "non-ascii"} {
		t.Run(name, func(t *testing.T) {
			data := receiptFixture()
			switch name {
			case "short":
				data = data[:639]
			case "trailing":
				data = append(data, 0)
			case "magic":
				data[0] = 'x'
			case "size":
				binary.LittleEndian.PutUint32(data[8:], 641)
			case "schema":
				binary.LittleEndian.PutUint32(data[12:], 2)
			case "nul-padding":
				data[16+39+39+10] = 'x'
			case "reserved":
				data[639] = 1
			case "product":
				data[16] = 'X'
			case "zero-guid":
				copy(data[16:], "{00000000-0000-0000-0000-000000000000}")
			case "same-guid":
				copy(data[55:94], data[16:55])
			case "version":
				copy(data[94:], "256.1.1")
			case "revision":
				data[126] = 'Z'
			case "hash":
				data[167] = 'A'
			case "non-ascii":
				data[94] = 0xff
			}
			if _, err := DecodeReceipt(data); !errors.Is(err, ErrDenied) {
				t.Fatal("malformed owned state accepted", err)
			}
		})
	}
	for _, value := range []string{"0.0.0", "255.255.65535"} {
		if !numericVersion(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"1.0", "1.0.1.0", "1.0.1-beta", "1.0.65536", "1.256.0", "01.0.1", "+1.0.1"} {
		if numericVersion(value) {
			t.Fatal("unsupported MSI version accepted", value)
		}
	}
}

func TestPortableAndManagedLocationBoundary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Programs", "VxInfiniteTeesBridge")
	image := filepath.Join(root, "GSPconnect.exe")
	other := filepath.Join(t.TempDir(), "GSPconnect.exe")
	for _, test := range []struct {
		image, actual string
		managed, fail bool
	}{
		{image, image, true, false},
		{other, other, false, false},
		{other, image, true, true},
		{image, other, true, true},
		{filepath.Join(root, "vxbridge.exe"), filepath.Join(root, "vxbridge.exe"), true, true},
	} {
		got, err := managedLocation(test.image, test.actual, root)
		if got != test.managed || (err != nil) != test.fail {
			t.Fatal(test, got, err)
		}
	}
}
