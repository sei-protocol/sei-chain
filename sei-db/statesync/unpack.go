package statesync

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

// Errors that ReadArchive returns for an archive that does not match its manifest.
var (
	errUnexpectedEntry = errors.New("unexpected archive entry")
	errEntryType       = errors.New("archive entry is not a regular file")
	errEntryTooLarge   = errors.New("archive entry is too large")
	errEntrySize       = errors.New("archive entry size differs from the manifest")
	errArchiveEnded    = errors.New("archive ends early")
	errUnlistedEntry   = errors.New("archive entry is not in the manifest")
	errTrailingData    = errors.New("archive has data after the end of the tar stream")
	errArchiveSize     = errors.New("archive size differs from the manifest")
)

// ReadArchive reads an archive from r. It decodes the manifest, calls accept with it, and when
// accept returns nil, writes the checkpoint files to dir, which must not exist. It returns the
// manifest and the proof. It returns an error when the archive holds an entry that the manifest
// does not list, misses one, holds one of another size or type, or does not end where the
// manifest says it ends.
func ReadArchive(ctx context.Context, r io.Reader, dir string, accept func(*Manifest) error) (*Manifest, []byte, error) {
	counted := &countingReader{r: r}
	tr := tar.NewReader(counted)
	manifest, err := readEntry(tr, manifestEntry, maxManifestSize)
	if err != nil {
		return nil, nil, err
	}
	m, err := DecodeManifest(manifest)
	if err != nil {
		return nil, nil, err
	}
	if err := accept(m); err != nil {
		return nil, nil, err
	}
	proof, err := readEntry(tr, proofEntry, maxProofSize)
	if err != nil {
		return nil, nil, err
	}
	if err := createCheckpointDirs(dir); err != nil {
		return nil, nil, err
	}
	for _, f := range m.Files {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if err := readFileEntry(tr, f, dir); err != nil {
			return nil, nil, err
		}
	}
	if err := requireArchiveEnd(tr, counted, m, int64(len(proof))); err != nil {
		return nil, nil, err
	}
	return m, proof, nil
}

// VerifyCheckpoint returns an error unless the checkpoint in dir is a FlatKV checkpoint at the
// height of m, in the Pebble format of m, whose metadata records the stats of m.
func VerifyCheckpoint(dir string, m *Manifest) error {
	if err := flatkv.VerifySnapshotDir(dir, m.Height); err != nil {
		return err
	}
	format, err := checkpointFormat(dir)
	if err != nil {
		return err
	}
	if format != m.PebbleFormatMajorVersion {
		return fmt.Errorf("checkpoint has pebble format %d, the manifest records %d", format, m.PebbleFormatMajorVersion)
	}
	got, err := flatkv.SnapshotDirStats(dir, m.Height)
	if err != nil {
		return fmt.Errorf("checkpoint stats: %w", err)
	}
	if want := m.Stats(); got != want {
		return fmt.Errorf("checkpoint holds %s, the manifest records %s", formatStats(got), formatStats(want))
	}
	return nil
}

func formatStats(s flatkv.SnapshotStats) string {
	return fmt.Sprintf("%d rows, %d bytes, state hash %x", s.RowCount, s.Bytes, s.StateHash)
}

// readEntry reads the next entry, which must be a regular file named name of at most maxSize bytes.
func readEntry(tr *tar.Reader, name string, maxSize int64) ([]byte, error) {
	hdr, err := nextArchiveEntry(tr, name)
	if err != nil {
		return nil, err
	}
	if hdr.Size > maxSize {
		return nil, fmt.Errorf("%w: %s is %d bytes, more than %d", errEntryTooLarge, name, hdr.Size, maxSize)
	}
	data, err := io.ReadAll(tr)
	if err != nil {
		return nil, fmt.Errorf("archive entry %s: %w", name, err)
	}
	return data, nil
}

// nextArchiveEntry returns the header of the next entry, which must be a regular file named name.
func nextArchiveEntry(tr *tar.Reader, name string) (*tar.Header, error) {
	hdr, err := tr.Next()
	if errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: before entry %s", errArchiveEnded, name)
	}
	if err != nil {
		return nil, fmt.Errorf("archive entry %s: %w", name, err)
	}
	if hdr.Name != name {
		return nil, fmt.Errorf("%w: %q, want %q", errUnexpectedEntry, hdr.Name, name)
	}
	if hdr.Typeflag != tar.TypeReg {
		return nil, fmt.Errorf("%w: %s has type %q", errEntryType, name, hdr.Typeflag)
	}
	return hdr, nil
}

func createCheckpointDirs(dir string) error {
	if err := os.Mkdir(dir, 0o750); err != nil {
		return fmt.Errorf("create checkpoint dir: %w", err)
	}
	for _, database := range flatkv.DataDBDirs() {
		if err := os.Mkdir(filepath.Join(dir, database), 0o750); err != nil {
			return fmt.Errorf("create checkpoint dir: %w", err)
		}
	}
	return nil
}

func readFileEntry(tr *tar.Reader, f File, dir string) error {
	hdr, err := nextArchiveEntry(tr, f.Path)
	if err != nil {
		return err
	}
	if hdr.Size != f.Size {
		return fmt.Errorf("%w: %s is %d bytes, the manifest records %d", errEntrySize, f.Path, hdr.Size, f.Size)
	}
	database, err := parseFilePath(f.Path)
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, database, path.Base(f.Path))
	out, err := os.OpenFile(filepath.Clean(dst), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("archive entry %s: %w", f.Path, err)
	}
	if _, err := io.CopyN(out, tr, f.Size); err != nil {
		_ = out.Close()
		return fmt.Errorf("archive entry %s: %w", f.Path, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("archive entry %s: %w", f.Path, err)
	}
	return nil
}

// requireArchiveEnd returns an error unless the tar stream ends after the last file of m, and r
// ends there too, at the size that ArchiveSize gives for m.
func requireArchiveEnd(tr *tar.Reader, r *countingReader, m *Manifest, proofSize int64) error {
	hdr, err := tr.Next()
	if err == nil {
		return fmt.Errorf("%w: %q", errUnlistedEntry, hdr.Name)
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("archive end: %w", err)
	}
	if n, err := io.Copy(io.Discard, r); err != nil {
		return fmt.Errorf("archive end: %w", err)
	} else if n != 0 {
		return fmt.Errorf("%w: %d bytes", errTrailingData, n)
	}
	want, err := ArchiveSize(m, proofSize)
	if err != nil {
		return err
	}
	if r.n != want {
		return fmt.Errorf("%w: %d bytes, want %d", errArchiveSize, r.n, want)
	}
	return nil
}

// countingReader counts the bytes read from r.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
