package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"unicode/utf16"

	"github.com/devigned/vx-bridge/internal/managed"
)

const pendingSize = 1874

// pendingEvidence decodes the native pending-maintenance denial record.
// Its contents are private recovery evidence, not permission for an ordinary start.
type pendingEvidence struct {
	// Operation is the native install (1), uninstall (2), or repair (3) kind.
	Operation uint32 `json:"operation"`
	// Phase is waiting (1), quiescent (2), mutating (3), rolled back (4), or
	// committed (5). The marker alone does not verify resulting product state.
	Phase uint32 `json:"phase"`
	// HasBefore reports whether Before contains the recorded prior receipt.
	HasBefore bool `json:"has_before"`
	// Nonce is the canonical operation GUID in the durable pending record.
	Nonce string `json:"nonce"`
	// Before is the prior owned receipt when HasBefore is true, otherwise zero.
	Before managed.Receipt `json:"before"`
	// After is the exact receipt for the attempted target media.
	After managed.Receipt `json:"after"`
	// RunSaved reports that startup registration was saved in the native record.
	RunSaved bool `json:"run_saved"`
	// RunChanged reports that native servicing changed startup registration.
	RunChanged bool `json:"run_changed"`
	// RunCommand is the saved UTF-16 startup command decoded to a Go string.
	RunCommand string `json:"run_command"`
	// SHA256 is the digest of the full original native pending bytes.
	SHA256 string `json:"raw_sha256"`
}

// productState records actual per-user unmanaged MSI registration observations.
type productState struct {
	// Product is the queried ProductCode.
	Product string `json:"product"`
	// Registered reports that the queried product is present in this user context.
	Registered bool `json:"registered"`
	// Properties maps successfully retrieved MSI property names to values.
	Properties map[string]string `json:"properties"`
	// PropertyErrors records unavailable installed properties without pretending
	// they were successfully read; installed verification rejects such gaps.
	PropertyErrors map[string]string `json:"unavailable_installed_properties,omitempty"`
}

func mediaReceipt(m media) managed.Receipt {
	r := managed.Receipt{Product: m.Product, Package: m.Package, Version: m.MSIVersion, Revision: m.Source}
	for index, name := range payloadNames {
		r.Hashes[index] = m.Payload[name]
	}
	return r
}

// decodePending validates the fixed native pending ABI and returns owned decoded
// evidence, including the full-byte digest. It does not retain data, mutate state,
// or qualify the operation's outcome.
func decodePending(data []byte) (*pendingEvidence, error) {
	if len(data) != pendingSize || !bytes.Equal(data[:8], []byte("VXTX001\x00")) ||
		binary.LittleEndian.Uint32(data[8:12]) != pendingSize || data[63] != 0 {
		return nil, errors.New("pending ABI/magic/size/reserved differs")
	}
	p := &pendingEvidence{Operation: binary.LittleEndian.Uint32(data[12:16]), Phase: binary.LittleEndian.Uint32(data[16:20])}
	has := binary.LittleEndian.Uint32(data[20:24])
	if p.Operation < 1 || p.Operation > 3 || p.Phase < 1 || p.Phase > 5 || has > 1 || data[62] != 0 {
		return nil, errors.New("pending phase/operation/before/nonce differs")
	}
	p.HasBefore = has == 1
	p.Nonce = string(data[24:62])
	if !guidPattern.MatchString(p.Nonce) {
		return nil, errors.New("pending nonce is not canonical")
	}
	var err error
	if p.HasBefore {
		p.Before, err = managed.DecodeReceipt(data[64:704])
		if err != nil {
			return nil, err
		}
	}
	p.After, err = managed.DecodeReceipt(data[704:1344])
	if err != nil {
		return nil, err
	}
	saved, changed := binary.LittleEndian.Uint32(data[1344:1348]), binary.LittleEndian.Uint32(data[1348:1352])
	if saved > 1 || changed > 1 {
		return nil, errors.New("pending startup flags differ")
	}
	p.RunSaved = saved == 1
	p.RunChanged = changed == 1
	chars := make([]uint16, 261)
	for i := range chars {
		chars[i] = binary.LittleEndian.Uint16(data[1352+i*2 : 1354+i*2])
	}
	end := -1
	for i, c := range chars {
		if c == 0 && end < 0 {
			end = i
		}
		if end >= 0 && c != 0 {
			return nil, errors.New("pending startup padding differs")
		}
	}
	if end < 0 {
		return nil, errors.New("pending startup command not terminated")
	}
	p.RunCommand = string(utf16.Decode(chars[:end]))
	sum := sha256.Sum256(data)
	p.SHA256 = hex.EncodeToString(sum[:])
	return p, nil
}

func verifyFailedPending(a, fault media, receipt managed.Receipt, pending *pendingEvidence) error {
	if pending == nil || pending.After != mediaReceipt(fault) || !pending.HasBefore ||
		pending.Before != mediaReceipt(a) || receipt != mediaReceipt(a) {
		return errors.New("failed operation must retain exact owned recovery denial/source binding")
	}
	return nil
}
