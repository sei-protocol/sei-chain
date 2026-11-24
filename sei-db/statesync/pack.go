package statesync

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs"

	cosmosversion "github.com/sei-protocol/sei-chain/sei-cosmos/version"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

const (
	tarBlockSize = 512
	// maxEntrySize is the largest entry that a USTAR header can record.
	maxEntrySize = 1<<33 - 1
)

// BuildManifest returns the manifest of the FlatKV checkpoint in dir, at height of chain chainID.
// dir holds one directory per FlatKV database, as FlatKV writes a checkpoint.
func BuildManifest(chainID string, height int64, dir string) (*Manifest, error) {
	stats, err := flatkv.SnapshotDirStats(dir, height)
	if err != nil {
		return nil, fmt.Errorf("checkpoint stats: %w", err)
	}
	format, err := checkpointFormat(dir)
	if err != nil {
		return nil, err
	}
	files, err := checkpointFiles(dir)
	if err != nil {
		return nil, err
	}
	m := &Manifest{
		FormatVersion:            FormatVersion,
		ChainID:                  chainID,
		Height:                   height,
		RowCount:                 stats.RowCount,
		StateBytes:               stats.Bytes,
		StateHash:                stats.StateHash,
		PebbleFormatMajorVersion: format,
		Files:                    files,
		ProducerVersion:          producerVersion(),
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return m, nil
}

// checkpointFormat returns the Pebble format of the databases in checkpoint dir, or an error when
// they differ.
func checkpointFormat(dir string) (pebble.FormatMajorVersion, error) {
	var format pebble.FormatMajorVersion
	for i, database := range flatkv.DataDBDirs() {
		desc, err := pebble.Peek(filepath.Join(dir, database), vfs.Default)
		if err != nil {
			return 0, fmt.Errorf("checkpoint database %s: %w", database, err)
		}
		if !desc.Exists {
			return 0, fmt.Errorf("checkpoint database %s: no pebble database", database)
		}
		if i > 0 && desc.FormatMajorVersion != format {
			return 0, fmt.Errorf("checkpoint database %s: pebble format %d, want %d", database, desc.FormatMajorVersion, format)
		}
		format = desc.FormatMajorVersion
	}
	return format, nil
}

// checkpointFiles returns every file of checkpoint dir except the Pebble LOCK files, in archive
// order. It returns an error when dir holds anything other than the FlatKV database directories
// and the regular files directly in them.
func checkpointFiles(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || !isDataDB(e.Name()) {
			return nil, fmt.Errorf("checkpoint entry %s is not a FlatKV database directory", e.Name())
		}
	}
	var files []File
	for _, database := range flatkv.DataDBDirs() {
		entries, err := os.ReadDir(filepath.Join(dir, database))
		if err != nil {
			return nil, fmt.Errorf("read checkpoint database %s: %w", database, err)
		}
		for _, e := range entries {
			if e.Name() == lockFileName {
				continue
			}
			if !e.Type().IsRegular() {
				return nil, fmt.Errorf("checkpoint database %s: %s is not a regular file", database, e.Name())
			}
			info, err := e.Info()
			if err != nil {
				return nil, fmt.Errorf("checkpoint database %s: %w", database, err)
			}
			files = append(files, File{Path: filePath(database, e.Name()), Size: info.Size()})
		}
	}
	return files, nil
}

func producerVersion() string {
	if cosmosversion.Version == "" {
		return "unknown"
	}
	return cosmosversion.Version
}

// ArchiveSize returns the exact size in bytes of the archive that WriteArchive writes for m and a
// proof of proofSize bytes.
func ArchiveSize(m *Manifest, proofSize int64) (int64, error) {
	manifest, err := EncodeManifest(m)
	if err != nil {
		return 0, err
	}
	if proofSize > maxProofSize {
		return 0, fmt.Errorf("proof is %d bytes, more than %d", proofSize, maxProofSize)
	}
	size := entrySize(int64(len(manifest))) + entrySize(proofSize)
	for _, f := range m.Files {
		if f.Size > maxEntrySize {
			return 0, fmt.Errorf("file %s is %d bytes, more than %d", f.Path, f.Size, maxEntrySize)
		}
		size += entrySize(f.Size)
	}
	// A tar stream ends with two zero blocks.
	return size + 2*tarBlockSize, nil
}

// entrySize returns the size of a USTAR entry: one header block and the data padded to a block.
func entrySize(dataSize int64) int64 {
	return tarBlockSize + (dataSize+tarBlockSize-1)/tarBlockSize*tarBlockSize
}

// WriteArchive writes the archive of m to w: the manifest, then proof, then each file of m.Files
// read from checkpoint dir. It returns an error when a file does not have the size that m records.
// It writes exactly ArchiveSize(m, len(proof)) bytes.
func WriteArchive(ctx context.Context, w io.Writer, m *Manifest, proof []byte, dir string) error {
	manifest, err := EncodeManifest(m)
	if err != nil {
		return err
	}
	if len(proof) > maxProofSize {
		return fmt.Errorf("proof is %d bytes, more than %d", len(proof), maxProofSize)
	}
	tw := tar.NewWriter(w)
	if err := writeEntry(tw, manifestEntry, manifest); err != nil {
		return err
	}
	if err := writeEntry(tw, proofEntry, proof); err != nil {
		return err
	}
	for _, f := range m.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeFileEntry(tw, f, dir); err != nil {
			return err
		}
	}
	return tw.Close()
}

func writeEntry(tw *tar.Writer, name string, data []byte) error {
	if err := tw.WriteHeader(entryHeader(name, int64(len(data)))); err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}
	return nil
}

func writeFileEntry(tw *tar.Writer, f File, dir string) error {
	src := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(f.Path, flatKVEntryDir+"/")))
	file, err := os.Open(filepath.Clean(src))
	if err != nil {
		return fmt.Errorf("archive %s: %w", f.Path, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("archive %s: %w", f.Path, err)
	}
	if info.Size() != f.Size {
		return fmt.Errorf("archive %s: file is %d bytes, manifest records %d", f.Path, info.Size(), f.Size)
	}
	if err := tw.WriteHeader(entryHeader(f.Path, f.Size)); err != nil {
		return fmt.Errorf("archive %s: %w", f.Path, err)
	}
	if _, err := io.CopyN(tw, file, f.Size); err != nil {
		return fmt.Errorf("archive %s: %w", f.Path, err)
	}
	return nil
}

// entryHeader returns the USTAR header of a regular file entry. It records no time, owner or mode
// of the source file.
func entryHeader(name string, size int64) *tar.Header {
	return &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Size:     size,
		Mode:     0o644,
		ModTime:  time.Unix(0, 0),
		Format:   tar.FormatUSTAR,
	}
}
