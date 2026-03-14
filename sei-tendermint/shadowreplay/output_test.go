package shadowreplay

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOutputWriter_StdoutOnly(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewOutputWriter("", &buf)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	comp := &BlockComparison{
		Height:       100,
		AppHashMatch: true,
		TxCount:      5,
		Epoch:        EpochClean,
		Divergences:  nil,
	}

	if err := w.WriteBlock(comp); err != nil {
		t.Fatal(err)
	}

	var decoded BlockComparison
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON on stdout: %v\nraw: %s", err, buf.String())
	}
	if decoded.Height != 100 {
		t.Errorf("height: got %d, want 100", decoded.Height)
	}
}

func TestOutputWriter_FileRotation(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	w, err := NewOutputWriter(dir, &buf)
	if err != nil {
		t.Fatal(err)
	}

	// Write blocks in different windows.
	for _, h := range []int64{5000, 5001, 15000} {
		comp := &BlockComparison{
			Height:       h,
			AppHashMatch: true,
			Epoch:        EpochClean,
			Divergences:  nil,
		}
		if err := w.WriteBlock(comp); err != nil {
			t.Fatalf("WriteBlock(%d): %v", h, err)
		}
	}
	w.Close()

	// Check that two gzipped files were created.
	entries, _ := os.ReadDir(filepath.Join(dir, "blocks"))
	if len(entries) != 2 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("expected 2 block files, got %d: %v", len(entries), names)
	}

	// Verify the first file contains valid gzipped NDJSON.
	f, err := os.Open(filepath.Join(dir, "blocks", "0-10000.ndjson.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(gr)

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 2 {
		t.Errorf("expected 2 records in first file, got %d", len(lines))
	}

	var first BlockComparison
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if first.Height != 5000 {
		t.Errorf("first record height: got %d, want 5000", first.Height)
	}
}

func TestOutputWriter_DivergenceFile(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	w, err := NewOutputWriter(dir, &buf)
	if err != nil {
		t.Fatal(err)
	}

	comp := &BlockComparison{
		Height:       42000,
		AppHashMatch: false,
		Epoch:        EpochDiverged,
		Divergences: []Divergence{{
			Scope:     ScopeBlock,
			Severity:  SeverityCritical,
			Field:     "app_hash",
			Canonical: "aaa",
			Replay:    "bbb",
		}},
	}
	if err := w.WriteBlock(comp); err != nil {
		t.Fatal(err)
	}
	w.Close()

	// Check divergence file.
	divPath := filepath.Join(dir, "divergences", "42000.json")
	data, err := os.ReadFile(divPath)
	if err != nil {
		t.Fatalf("divergence file: %v", err)
	}

	var loaded BlockComparison
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("invalid divergence JSON: %v", err)
	}
	if loaded.Height != 42000 {
		t.Errorf("divergence height: got %d, want 42000", loaded.Height)
	}
	if len(loaded.Divergences) != 1 {
		t.Errorf("expected 1 divergence, got %d", len(loaded.Divergences))
	}
}

func TestOutputWriter_NoDivergenceFileForClean(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	w, err := NewOutputWriter(dir, &buf)
	if err != nil {
		t.Fatal(err)
	}

	comp := &BlockComparison{
		Height:       1000,
		AppHashMatch: true,
		Epoch:        EpochClean,
		Divergences:  nil,
	}
	if err := w.WriteBlock(comp); err != nil {
		t.Fatal(err)
	}
	w.Close()

	divPath := filepath.Join(dir, "divergences", "1000.json")
	if _, err := os.Stat(divPath); !os.IsNotExist(err) {
		t.Error("divergence file should not exist for clean block")
	}
}
