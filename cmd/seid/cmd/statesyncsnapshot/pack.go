package statesyncsnapshot

import (
	"archive/tar"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/klauspost/compress/zstd"

	cosmosversion "github.com/sei-protocol/sei-chain/sei-cosmos/version"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

const (
	// encoderConcurrency is the number of zstd encoder goroutines.
	encoderConcurrency = 2
	// archiveWindowSize is the zstd window of an archive, and the largest window restore accepts.
	archiveWindowSize = 8 << 20
)

// buildManifest returns the manifest that describes a logical FlatKV row stream.
func buildManifest(chainID string, height int64, stats flatkv.SnapshotStats) (*manifest, error) {
	m := &manifest{
		FormatVersion:    formatVersion,
		ChainID:          chainID,
		CheckpointHeight: height,
		CreatedAt:        time.Now().UTC().Truncate(time.Second),
		ProducerVersion:  producerVersion(),
		RowCount:         stats.RowCount,
		StateBytes:       stats.Bytes,
		StateHash:        hexHash(stats.StateHash),
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return m, nil
}

func producerVersion() string {
	if cosmosversion.Version == "" {
		return "unknown"
	}
	return cosmosversion.Version
}

// writeArchive writes the manifest, proof and FlatKV logical row chunks as tar in zstd.
func writeArchive(ctx context.Context, w io.Writer, m *manifest, proof []byte, checkpoint string) error {
	// A small encoder concurrency keeps packing from taking every core of a running node.
	zw, err := zstd.NewWriter(
		w,
		zstd.WithEncoderConcurrency(encoderConcurrency),
		zstd.WithWindowSize(archiveWindowSize),
		zstd.WithEncoderCRC(true),
	)
	if err != nil {
		return err
	}
	defer func() { _ = zw.Close() }()
	tw := tar.NewWriter(zw)
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg, ModTime: m.CreatedAt}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := writeProofEntry(tw, m, proof); err != nil {
		return err
	}
	rowWriter := newRowChunkWriter(tw, m.CreatedAt)
	stats, err := flatkv.ExportSnapshotDir(ctx, checkpoint, m.CheckpointHeight, rowWriter.add)
	if err != nil {
		return fmt.Errorf("export FlatKV rows: %w", err)
	}
	if err := rowWriter.close(); err != nil {
		return err
	}
	if stats != m.stats() {
		return fmt.Errorf("exported %s, manifest records %s", formatStats(stats), formatStats(m.stats()))
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close zstd: %w", err)
	}
	return nil
}

func writeProofEntry(tw *tar.Writer, m *manifest, proof []byte) error {
	if len(proof) > maxProofSize {
		return fmt.Errorf("state sync proof is %d bytes, more than %d", len(proof), maxProofSize)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:     proofArchivePath(m.CheckpointHeight),
		Mode:     0o600,
		Size:     int64(len(proof)),
		Typeflag: tar.TypeReg,
		ModTime:  m.CreatedAt,
	}); err != nil {
		return err
	}
	_, err := tw.Write(proof)
	return err
}

type rowChunkWriter struct {
	tw      *tar.Writer
	modTime time.Time
	chunk   []byte
	index   int
}

func newRowChunkWriter(tw *tar.Writer, modTime time.Time) *rowChunkWriter {
	return &rowChunkWriter{
		tw:      tw,
		modTime: modTime,
		chunk:   make([]byte, 0, maxRowChunkSize),
	}
}

func (w *rowChunkWriter) add(key, value []byte) error {
	recordLen := recordEncodedLen(len(key), len(value))
	if recordLen > maxRowChunkSize {
		return fmt.Errorf("FlatKV row is %d bytes, more than the %d-byte row chunk limit", recordLen, maxRowChunkSize)
	}
	if len(w.chunk) > 0 && len(w.chunk)+recordLen > maxRowChunkSize {
		if err := w.flush(); err != nil {
			return err
		}
	}
	w.appendUvarint(uint64(len(key)))
	w.chunk = append(w.chunk, key...)
	w.appendUvarint(uint64(len(value)))
	w.chunk = append(w.chunk, value...)
	if len(w.chunk) == maxRowChunkSize {
		return w.flush()
	}
	return nil
}

func (w *rowChunkWriter) close() error {
	if len(w.chunk) == 0 {
		return nil
	}
	return w.flush()
}

func (w *rowChunkWriter) flush() error {
	if len(w.chunk) == 0 {
		return nil
	}
	if err := w.tw.WriteHeader(&tar.Header{
		Name:     rowChunkPath(w.index),
		Mode:     0o600,
		Size:     int64(len(w.chunk)),
		Typeflag: tar.TypeReg,
		ModTime:  w.modTime,
	}); err != nil {
		return err
	}
	if _, err := w.tw.Write(w.chunk); err != nil {
		return err
	}
	w.index++
	w.chunk = w.chunk[:0]
	return nil
}

func recordEncodedLen(keyLen, valueLen int) int {
	return uvarintLen(uint64(keyLen)) + keyLen + uvarintLen(uint64(valueLen)) + valueLen //nolint:gosec // lengths come from byte slices.
}

func uvarintLen(v uint64) int {
	var buf [10]byte
	return binary.PutUvarint(buf[:], v)
}

func (w *rowChunkWriter) appendUvarint(v uint64) {
	var buf [10]byte
	n := binary.PutUvarint(buf[:], v)
	w.chunk = append(w.chunk, buf[:n]...)
}
