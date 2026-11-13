package statesyncsnapshot

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

const (
	formatVersion     = 2
	manifestName      = "manifest.json"
	maxManifestSize   = 16 << 20
	maxProofSize      = 16 << 20
	maxRowChunkSize   = 128 << 20
	maxStateKeySize   = 64 << 10
	maxStateValueSize = 64 << 20
	flatkvPrefix      = "flatkv"
	rowPrefix         = "flatkv/rows"
	proofPrefix       = "proof"
	rowChunkNameWidth = 8
)

// manifest is the first entry of an archive. It describes the logical row stream that follows it.
type manifest struct {
	FormatVersion    int       `json:"format_version"`
	ChainID          string    `json:"chain_id"`
	CheckpointHeight int64     `json:"checkpoint_height"`
	CreatedAt        time.Time `json:"created_at"`
	ProducerVersion  string    `json:"producer_version"`
	RowCount         int64     `json:"row_count"`
	StateBytes       int64     `json:"state_bytes"`
	// StateHash is the checksum of the store-wide FlatKV LtHash of the checkpoint.
	StateHash hexHash `json:"state_hash"`
}

// hexHash is a 32-byte hash that encodes as hex in JSON.
type hexHash [32]byte

func (h hexHash) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(h[:])), nil
}

func (h *hexHash) UnmarshalText(text []byte) error {
	if len(text) != hex.EncodedLen(len(h)) {
		return fmt.Errorf("hash is %d hex digits, want %d", len(text), hex.EncodedLen(len(h)))
	}
	_, err := hex.Decode(h[:], text)
	return err
}

// stats returns the FlatKV snapshot stats that m records.
func (m *manifest) stats() flatkv.SnapshotStats {
	return flatkv.SnapshotStats{RowCount: m.RowCount, Bytes: m.StateBytes, StateHash: [32]byte(m.StateHash)}
}

// formatStats describes FlatKV snapshot stats in an error message.
func formatStats(s flatkv.SnapshotStats) string {
	return fmt.Sprintf("%d rows, %d bytes and state hash %x", s.RowCount, s.Bytes, s.StateHash)
}

// validate checks the rules that both the writer and the reader rely on.
func (m *manifest) validate() error {
	if m.FormatVersion != formatVersion {
		return fmt.Errorf("format_version %d, want %d", m.FormatVersion, formatVersion)
	}
	if m.ChainID == "" {
		return errors.New("chain_id is empty")
	}
	if m.CheckpointHeight < 1 {
		return fmt.Errorf("checkpoint_height %d is below 1", m.CheckpointHeight)
	}
	if m.RowCount < 0 {
		return fmt.Errorf("row_count %d is below 0", m.RowCount)
	}
	if m.StateBytes < 0 {
		return fmt.Errorf("state_bytes %d is below 0", m.StateBytes)
	}
	if m.StateHash == (hexHash{}) {
		return errors.New("state_hash is missing")
	}
	return nil
}

// proofArchivePath returns the archive path of the state sync proof file for height.
func proofArchivePath(height int64) string {
	return path.Join(proofPrefix, statesyncproof.FileName(atypes.RoadIndex(height))) //nolint:gosec // heights are positive.
}

// rowChunkPath returns the archive path of the state row chunk at index.
func rowChunkPath(index int) string {
	return path.Join(rowPrefix, fmt.Sprintf("%0*d", rowChunkNameWidth, index))
}

// validateArchivePath accepts only canonical relative paths.
func validateArchivePath(p string) error {
	if p == "" || path.Clean(p) != p || path.IsAbs(p) || strings.Contains(p, "\\") {
		return fmt.Errorf("path %q is not canonical", p)
	}
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("path %q is not canonical", p)
		}
	}
	return nil
}

// decodeManifest decodes data as a manifest, refusing unknown fields and trailing data.
func decodeManifest(data []byte) (*manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode manifest: trailing data")
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return &m, nil
}
