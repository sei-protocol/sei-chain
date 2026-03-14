package shadowreplay

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckpoint_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	now := time.Now().Truncate(time.Second)
	cp := &Checkpoint{
		LastHeight:     12345,
		LastAppHash:    "abc123",
		StartedAt:      now,
		BlocksReplayed: 500,
		Divergences:    3,
		Epoch:          EpochDiverged,
		EpochOrigin:    12000,
	}

	if err := SaveCheckpoint(path, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	loaded, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected checkpoint, got nil")
	}

	if loaded.LastHeight != 12345 {
		t.Errorf("LastHeight: got %d, want 12345", loaded.LastHeight)
	}
	if loaded.LastAppHash != "abc123" {
		t.Errorf("LastAppHash: got %s, want abc123", loaded.LastAppHash)
	}
	if loaded.BlocksReplayed != 500 {
		t.Errorf("BlocksReplayed: got %d, want 500", loaded.BlocksReplayed)
	}
	if loaded.Divergences != 3 {
		t.Errorf("Divergences: got %d, want 3", loaded.Divergences)
	}
	if loaded.Epoch != EpochDiverged {
		t.Errorf("Epoch: got %s, want %s", loaded.Epoch, EpochDiverged)
	}
	if loaded.EpochOrigin != 12000 {
		t.Errorf("EpochOrigin: got %d, want 12000", loaded.EpochOrigin)
	}
	if !loaded.StartedAt.Equal(now) {
		t.Errorf("StartedAt: got %v, want %v", loaded.StartedAt, now)
	}
}

func TestCheckpoint_LoadMissing(t *testing.T) {
	loaded, err := LoadCheckpoint("/nonexistent/checkpoint.json")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if loaded != nil {
		t.Error("expected nil checkpoint for missing file")
	}
}

func TestCheckpoint_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "checkpoint.json")

	cp := &Checkpoint{LastHeight: 1, Epoch: EpochClean}
	if err := SaveCheckpoint(path, cp); err != nil {
		t.Fatalf("SaveCheckpoint with nested dir: %v", err)
	}

	// Verify no .tmp file left behind.
	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("temp file should be cleaned up, but stat returned: %v", err)
	}

	loaded, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	if loaded.LastHeight != 1 {
		t.Errorf("LastHeight: got %d, want 1", loaded.LastHeight)
	}
}

func TestCheckpoint_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadCheckpoint(path)
	if err == nil {
		t.Error("expected error for corrupt checkpoint file")
	}
}
