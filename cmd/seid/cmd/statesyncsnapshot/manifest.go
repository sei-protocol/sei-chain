package statesyncsnapshot

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

const (
	formatVersion   = 1
	manifestName    = "manifest.json"
	maxManifestSize = 16 << 20
	flatkvPrefix    = "flatkv"
	epochsPrefix    = "epochs"
)

// manifest is the first entry of an archive. It lists every other entry in archive order.
type manifest struct {
	FormatVersion    int            `json:"format_version"`
	ChainID          string         `json:"chain_id"`
	CheckpointHeight int64          `json:"checkpoint_height"`
	CheckpointDir    string         `json:"checkpoint_dir"`
	CreatedAt        time.Time      `json:"created_at"`
	ProducerVersion  string         `json:"producer_version"`
	Files            []manifestFile `json:"files"`
}

type manifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
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
	if want := flatkv.SnapshotDirName(m.CheckpointHeight); m.CheckpointDir != want {
		return fmt.Errorf("checkpoint_dir %q, want %q", m.CheckpointDir, want)
	}
	seen := make(map[string]struct{}, len(m.Files))
	for _, f := range m.Files {
		if err := validatePath(f.Path, m.CheckpointDir); err != nil {
			return err
		}
		if _, dup := seen[f.Path]; dup {
			return fmt.Errorf("file %q is listed twice", f.Path)
		}
		seen[f.Path] = struct{}{}
		if f.Size < 0 {
			return fmt.Errorf("file %q has size %d", f.Path, f.Size)
		}
		if b, err := hex.DecodeString(f.SHA256); err != nil || len(b) != 32 {
			return fmt.Errorf("file %q has a bad sha256 %q", f.Path, f.SHA256)
		}
	}
	return nil
}

// totalSize returns the sum of the listed file sizes.
func (m *manifest) totalSize() int64 {
	var total int64
	for _, f := range m.Files {
		total += f.Size
	}
	return total
}

// epochFileNames returns the names of the listed files under epochs/, in manifest order.
func (m *manifest) epochFileNames() []string {
	var names []string
	for _, f := range m.Files {
		if name, ok := strings.CutPrefix(f.Path, epochsPrefix+"/"); ok {
			names = append(names, name)
		}
	}
	return names
}

// validatePath accepts only canonical relative paths under flatkv/<checkpointDir>/ or directly under
// epochs/.
func validatePath(p, checkpointDir string) error {
	if p == "" || path.Clean(p) != p || path.IsAbs(p) || strings.Contains(p, "\\") {
		return fmt.Errorf("path %q is not canonical", p)
	}
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("path %q is not canonical", p)
		}
	}
	switch {
	case parts[0] == flatkvPrefix && len(parts) >= 3 && parts[1] == checkpointDir:
	case parts[0] == epochsPrefix && len(parts) == 2:
	default:
		return fmt.Errorf("path %q is outside flatkv/%s/ and epochs/", p, checkpointDir)
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
	if dec.More() {
		return nil, errors.New("decode manifest: trailing data")
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return &m, nil
}
