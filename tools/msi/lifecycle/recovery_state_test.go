package main

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/devigned/vx-bridge/internal/managed"
)

func encodedTestReceipt(r managed.Receipt) []byte {
	data := make([]byte, managed.ReceiptSize)
	copy(data, "VXMSI01\x00")
	binary.LittleEndian.PutUint32(data[8:], managed.ReceiptSize)
	binary.LittleEndian.PutUint32(data[12:], 1)
	at := 16
	for _, field := range []struct {
		value string
		width int
	}{
		{r.Product, 39}, {r.Package, 39}, {r.Version, 32}, {r.Revision, 41},
	} {
		copy(data[at:], field.value)
		at += field.width
	}
	for _, hash := range r.Hashes {
		copy(data[at:], hash)
		at += 65
	}
	return data
}

func recoveryFixture(t *testing.T) (media, media, *pendingEvidence, []byte) {
	t.Helper()
	c, _ := testConfiguration(t)
	a, fault := c.Media[0], c.Media[2]
	root := `C:\Invented\Bridge`
	data := make([]byte, pendingSize)
	copy(data, "VXTX001\x00")
	for offset, value := range map[int]uint32{8: pendingSize, 12: 3, 16: 3, 20: 1, 1344: 1, 1348: 1} {
		binary.LittleEndian.PutUint32(data[offset:], value)
	}
	copy(data[24:], "{44444444-4444-4444-8444-444444444444}")
	copy(data[64:], encodedTestReceipt(mediaReceipt(a)))
	copy(data[704:], encodedTestReceipt(mediaReceipt(fault)))
	for i, ch := range utf16.Encode([]rune(`"` + root + `\GSPconnect.exe" --desktop --target-port 999`)) {
		binary.LittleEndian.PutUint16(data[1352+i*2:], ch)
	}
	pending, err := decodePending(data)
	if err != nil {
		t.Fatal(err)
	}
	return a, fault, pending, data
}

func TestFailedUpgradeKeepsExactRecoveryBinding(t *testing.T) {
	a, fault, pending, _ := recoveryFixture(t)
	if err := verifyFailedPending(a, fault, mediaReceipt(a), pending); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"product", "package", "source", "before", "after", "has-before", "no-pending", "payload"} {
		t.Run(name, func(t *testing.T) {
			a, fault, pending, _ := recoveryFixture(t)
			receipt := mediaReceipt(a)
			switch name {
			case "product":
				receipt.Product = fault.Product
			case "package":
				receipt.Package = fault.Package
			case "source":
				receipt.Revision = strings.Repeat("b", 40)
			case "before":
				pending.Before.Package = fault.Package
			case "after":
				pending.After.Package = a.Package
			case "has-before":
				pending.HasBefore = false
			case "no-pending":
				pending = nil
			case "payload":
				pending.After.Hashes[0] = strings.Repeat("f", 64)
			}
			if err := verifyFailedPending(a, fault, receipt, pending); err == nil {
				t.Fatal("failed operation lost its exact recovery denial/source binding")
			}
		})
	}
}

func TestTypedPendingBeforeAfterABI(t *testing.T) {
	_, _, pending, data := recoveryFixture(t)
	if pending.Operation != 3 || pending.Phase != 3 || !pending.RunSaved || !pending.RunChanged ||
		pending.RunCommand != `"C:\Invented\Bridge\GSPconnect.exe" --desktop --target-port 999` ||
		len(pending.SHA256) != 64 {
		t.Fatalf("typed pending observation differs: %+v", pending)
	}
	for _, name := range []string{"short", "magic", "size", "operation", "reserved", "nonce", "receipt", "flags", "padding"} {
		t.Run(name, func(t *testing.T) {
			bad := append([]byte(nil), data...)
			switch name {
			case "short":
				bad = bad[:len(bad)-1]
			case "magic":
				bad[0] = 'x'
			case "size":
				binary.LittleEndian.PutUint32(bad[8:], 1873)
			case "operation":
				binary.LittleEndian.PutUint32(bad[12:], 9)
			case "reserved":
				bad[63] = 1
			case "nonce":
				bad[24] = 'x'
			case "receipt":
				bad[704] = 'x'
			case "flags":
				binary.LittleEndian.PutUint32(bad[1344:], 2)
			case "padding":
				bad[len(bad)-1] = 1
			}

			if _, err := decodePending(bad); err == nil {
				t.Fatal("malformed pending ABI accepted")
			}
		})
	}
}
