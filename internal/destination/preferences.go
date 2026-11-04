package destination

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/devigned/vx-bridge/internal/protocol"
)

// Preferences reads and replaces one user's saved destination selection.
// Configure Path before use and serialize writes; this type does not coordinate
// concurrent changes, switch a running bridge, or manage startup registration.
type Preferences struct {
	// Path is the destination.json file to read or replace.
	Path string
}

// preference is the on-disk envelope; Destination is the explicit saved ID.
type preference struct {
	// Destination is the explicit saved profile ID; Load rejects empty or invalid IDs.
	Destination ID `json:"destination"`
}

// Load returns the saved ID, or InfiniteTees if Path does not exist.
// Unreadable files, read/close failures, or invalid preferences return an error;
// invalid preferences do not silently fall back. The file must be at most
// 1024 bytes of strict JSON with no unknown fields and a Valid destination.
func (p *Preferences) Load() (ID, error) {
	file, err := os.Open(p.Path)
	if errors.Is(err, os.ErrNotExist) {
		return InfiniteTees, nil
	}
	if err != nil {
		return "", err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 1025))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return "", err
	}
	if len(data) > 1024 || !protocol.StrictJSON(data) {
		return "", errors.New("invalid_destination_preference")
	}
	var stored preference
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&stored) != nil || !stored.Destination.Valid() {
		return "", errors.New("invalid_destination_preference")
	}
	return stored.Destination, nil
}

// Save replaces Path with a Valid selection via a synced temporary file in the
// same directory, creating parent directories as needed. Invalid IDs and
// directory, write, sync, close, rename, or temporary-file cleanup failures
// return errors. Save does not confirm that a requested bridge switch completed,
// and a cleanup error can be returned after the preference was replaced.
func (p *Preferences) Save(id ID) (result error) {
	if !id.Valid() {
		return errors.New("invalid_destination")
	}
	directory := filepath.Dir(p.Path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".destination-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			result = errors.Join(result, err)
		}
		if err := os.Remove(file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}()
	if err := json.NewEncoder(file).Encode(preference{Destination: id}); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), p.Path)
}
