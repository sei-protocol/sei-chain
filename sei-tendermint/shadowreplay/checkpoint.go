package shadowreplay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Checkpoint persists replay progress for crash recovery.
type Checkpoint struct {
	LastHeight     int64     `json:"last_height"`
	LastAppHash    string    `json:"last_app_hash"`
	StartedAt      time.Time `json:"started_at"`
	BlocksReplayed int64     `json:"blocks_replayed"`
	Divergences    int64     `json:"divergences"`
	Epoch          string    `json:"epoch"`
	EpochOrigin    int64     `json:"epoch_origin_height,omitempty"`
}

// LoadCheckpoint reads checkpoint state from path.
// Returns nil without error if the file does not exist.
func LoadCheckpoint(path string) (*Checkpoint, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading checkpoint %q: %w", path, err)
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("parsing checkpoint %q: %w", path, err)
	}
	return &cp, nil
}

// SaveCheckpoint atomically writes checkpoint state to path.
// It writes to a temp file first, then renames to avoid partial writes.
func SaveCheckpoint(path string, cp *Checkpoint) error {
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling checkpoint: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating checkpoint dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing checkpoint tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming checkpoint: %w", err)
	}
	return nil
}
