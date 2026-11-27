// Package statesync writes giga state sync archives and moves them to and from an object store.
package statesync

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/cockroachdb/pebble/v2"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

const (
	// FormatVersion is the archive format that this package writes.
	FormatVersion = 1

	manifestEntry   = "manifest.json"
	proofEntry      = "proof.bin"
	flatKVEntryDir  = "flatkv"
	lockFileName    = "LOCK"
	maxManifestSize = 16 << 20
	maxProofSize    = 16 << 20
)

// errUnsupportedPebbleFormat is returned for a manifest whose Pebble format this build cannot open.
var errUnsupportedPebbleFormat = errors.New("unsupported pebble format")

// Manifest is the first entry of an archive. It describes the FlatKV checkpoint that follows it.
type Manifest struct {
	FormatVersion int    `json:"format_version"`
	ChainID       string `json:"chain_id"`
	Height        int64  `json:"height"`
	RowCount      int64  `json:"row_count"`
	StateBytes    int64  `json:"state_bytes"`
	// StateHash is the checksum of the store-wide FlatKV LtHash of the checkpoint.
	StateHash HexHash `json:"state_hash"`
	// PebbleFormatMajorVersion is the Pebble on-disk format of every database in the checkpoint.
	PebbleFormatMajorVersion pebble.FormatMajorVersion `json:"pebble_format_major_version"`
	// Files lists the checkpoint files in archive order, by archive path.
	Files           []File `json:"files"`
	ProducerVersion string `json:"producer_version"`
}

// File is one checkpoint file of an archive.
type File struct {
	// Path is the archive path: flatkv/<database>/<name>.
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// HexHash is a 32-byte hash that encodes as hex in JSON.
type HexHash [32]byte

// MarshalText returns h as lowercase hex.
func (h HexHash) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(h[:])), nil
}

// UnmarshalText sets h from hex text of exactly 64 digits.
func (h *HexHash) UnmarshalText(text []byte) error {
	if len(text) != hex.EncodedLen(len(h)) {
		return fmt.Errorf("hash is %d hex digits, want %d", len(text), hex.EncodedLen(len(h)))
	}
	_, err := hex.Decode(h[:], text)
	return err
}

// Stats returns the FlatKV statistics that m records.
func (m *Manifest) Stats() flatkv.SnapshotStats {
	return flatkv.SnapshotStats{RowCount: m.RowCount, Bytes: m.StateBytes, StateHash: m.StateHash}
}

// Validate reports whether m is a manifest that this package can read.
func (m *Manifest) Validate() error {
	switch {
	case m.FormatVersion != FormatVersion:
		return fmt.Errorf("format version %d, want %d", m.FormatVersion, FormatVersion)
	case m.ChainID == "":
		return errors.New("chain ID is empty")
	case m.Height < 1:
		return fmt.Errorf("height %d is not positive", m.Height)
	case m.RowCount < 0 || m.StateBytes < 0:
		return fmt.Errorf("negative stats: %d rows, %d bytes", m.RowCount, m.StateBytes)
	case m.PebbleFormatMajorVersion < pebble.FormatMinSupported || m.PebbleFormatMajorVersion > pebbledb.FormatMajorVersion:
		return fmt.Errorf("%w: %d, want %d to %d", errUnsupportedPebbleFormat, m.PebbleFormatMajorVersion, pebble.FormatMinSupported, pebbledb.FormatMajorVersion)
	}
	seen := make(map[string]bool, len(m.Files))
	databases := make(map[string]bool)
	for _, f := range m.Files {
		database, err := parseFilePath(f.Path)
		if err != nil {
			return err
		}
		if f.Size < 0 {
			return fmt.Errorf("file %s has negative size %d", f.Path, f.Size)
		}
		if seen[f.Path] {
			return fmt.Errorf("file %s is listed twice", f.Path)
		}
		seen[f.Path] = true
		databases[database] = true
	}
	for _, database := range flatkv.DataDBDirs() {
		if !databases[database] {
			return fmt.Errorf("no file of database %s", database)
		}
	}
	return nil
}

// filePath returns the archive path of file name in FlatKV database dir database.
func filePath(database, name string) string {
	return path.Join(flatKVEntryDir, database, name)
}

// parseFilePath returns the FlatKV database of archive path p, or an error when p is not
// flatkv/<database>/<name> with a known database and a plain file name.
func parseFilePath(p string) (string, error) {
	parts := strings.Split(p, "/")
	if len(parts) != 3 || parts[0] != flatKVEntryDir {
		return "", fmt.Errorf("file path %q is not %s/<database>/<name>", p, flatKVEntryDir)
	}
	database, name := parts[1], parts[2]
	if !isDataDB(database) {
		return "", fmt.Errorf("file path %q names unknown database %q", p, database)
	}
	if name == "" || name == "." || name == ".." || name == lockFileName {
		return "", fmt.Errorf("file path %q has a file name that is not allowed", p)
	}
	return database, nil
}

func isDataDB(name string) bool {
	for _, database := range flatkv.DataDBDirs() {
		if name == database {
			return true
		}
	}
	return false
}

// EncodeManifest returns the JSON encoding of m.
func EncodeManifest(m *Manifest) ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return json.Marshal(m)
}

// DecodeManifest decodes and validates a manifest. It refuses unknown fields and trailing data.
func DecodeManifest(data []byte) (*Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("manifest: trailing data after the JSON object")
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return &m, nil
}
