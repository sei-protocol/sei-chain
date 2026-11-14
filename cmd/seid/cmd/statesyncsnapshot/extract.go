package statesyncsnapshot

import (
	"archive/tar"
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"

	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// chunkReadBufferSize is the read buffer of a row chunk decoder.
const chunkReadBufferSize = 1 << 20

// archiveReader reads an archive stream once, in order.
type archiveReader struct {
	zr *zstd.Decoder
	tr *tar.Reader
}

// openArchive reads the manifest, which must be the first entry of the archive in r.
func openArchive(ctx context.Context, r io.Reader) (*archiveReader, *manifest, error) {
	zr, err := zstd.NewReader(ctxReader{ctx: ctx, r: r},
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxWindow(archiveWindowSize),
		zstd.WithDecoderMaxMemory(maxRowChunkSize+archiveWindowSize),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("open zstd: %w", err)
	}
	a := &archiveReader{zr: zr, tr: tar.NewReader(zr)}
	m, err := a.readManifest()
	if err != nil {
		a.Close()
		return nil, nil, err
	}
	return a, m, nil
}

// Close releases the decoder.
func (a *archiveReader) Close() {
	a.zr.Close()
}

func (a *archiveReader) readManifest() (*manifest, error) {
	hdr, err := a.tr.Next()
	if err != nil {
		return nil, fmt.Errorf("read the first archive entry: %w", err)
	}
	if hdr.Name != manifestName || hdr.Typeflag != tar.TypeReg {
		return nil, fmt.Errorf("the first archive entry is %q, want %s", hdr.Name, manifestName)
	}
	if hdr.Size > maxManifestSize {
		return nil, fmt.Errorf("the manifest is %d bytes, more than %d", hdr.Size, maxManifestSize)
	}
	data, err := io.ReadAll(a.tr)
	if err != nil {
		return nil, fmt.Errorf("read the manifest: %w", err)
	}
	return decodeManifest(data)
}

// readProof reads the proof entry of height, which must follow the manifest.
func (a *archiveReader) readProof(height int64) ([]byte, error) {
	hdr, err := a.tr.Next()
	if err != nil {
		return nil, fmt.Errorf("read the proof archive entry: %w", err)
	}
	want := proofArchivePath(height)
	if err := validateArchiveEntry(hdr, want, maxProofSize); err != nil {
		return nil, err
	}
	data := make([]byte, hdr.Size)
	if _, err := io.ReadFull(a.tr, data); err != nil {
		return nil, fmt.Errorf("read %s: %w", want, err)
	}
	return data, nil
}

// importRows decodes the row chunks that follow the proof into importer.
func (a *archiveReader) importRows(m *manifest, importer sctypes.Importer) error {
	var rows, bytesSeen int64
	for chunkIndex := 0; ; chunkIndex++ {
		hdr, err := a.tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read row chunk %d: %w", chunkIndex, err)
		}
		want := rowChunkPath(chunkIndex)
		if err := validateArchiveEntry(hdr, want, maxRowChunkSize); err != nil {
			return err
		}
		if err := a.importRowChunk(hdr, m, importer, &rows, &bytesSeen); err != nil {
			return err
		}
	}
	if rows != m.RowCount || bytesSeen != m.StateBytes {
		return fmt.Errorf("imported %d rows and %d bytes, manifest records %d rows and %d bytes",
			rows, bytesSeen, m.RowCount, m.StateBytes)
	}
	return nil
}

func (a *archiveReader) importRowChunk(
	hdr *tar.Header,
	m *manifest,
	importer sctypes.Importer,
	rows *int64,
	bytesSeen *int64,
) error {
	r := &chunkReader{r: bufio.NewReaderSize(io.LimitReader(a.tr, hdr.Size), chunkReadBufferSize), remaining: hdr.Size}
	for r.remaining > 0 {
		key, value, err := r.readRecord()
		if err != nil {
			return fmt.Errorf("%s: %w", hdr.Name, err)
		}
		(*rows)++
		*bytesSeen += int64(len(key)) + int64(len(value))
		if *rows > m.RowCount || *bytesSeen > m.StateBytes {
			return fmt.Errorf("archive row stream exceeds manifest: now at %d rows and %d bytes, manifest records %d rows and %d bytes",
				*rows, *bytesSeen, m.RowCount, m.StateBytes)
		}
		if err := importer.AddNode(&sctypes.SnapshotNode{
			Key:     key,
			Value:   value,
			Version: m.CheckpointHeight,
			Height:  0,
		}); err != nil {
			return fmt.Errorf("import FlatKV row %d: %w", *rows, err)
		}
	}
	return nil
}

// chunkReader reads the row records of one row chunk from the archive stream.
type chunkReader struct {
	r *bufio.Reader
	// remaining is the number of chunk bytes not yet read.
	remaining int64
}

func (c *chunkReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err != nil {
		return 0, err
	}
	c.remaining--
	return b, nil
}

func (c *chunkReader) readRecord() ([]byte, []byte, error) {
	key, err := c.readField("key", maxStateKeySize)
	if err != nil {
		return nil, nil, err
	}
	value, err := c.readField("value", maxStateValueSize)
	if err != nil {
		return nil, nil, err
	}
	return key, value, nil
}

// readField reads one length-prefixed field, checking its length before it allocates the field.
func (c *chunkReader) readField(name string, max int) ([]byte, error) {
	n, err := binary.ReadUvarint(c)
	if err != nil {
		return nil, fmt.Errorf("read %s length: %w", name, err)
	}
	if n == 0 {
		return nil, fmt.Errorf("%s length is 0", name)
	}
	if n > uint64(max) { //nolint:gosec // max is a positive archive limit constant.
		return nil, fmt.Errorf("%s length %d is more than %d", name, n, max)
	}
	if n > uint64(c.remaining) { //nolint:gosec // remaining is not negative.
		return nil, fmt.Errorf("%s length %d exceeds the remaining chunk size %d", name, n, c.remaining)
	}
	field := make([]byte, n)
	if _, err := io.ReadFull(c.r, field); err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	c.remaining -= int64(n) //nolint:gosec // n is bounded by maxStateKeySize or maxStateValueSize.
	return field, nil
}

func validateArchiveEntry(hdr *tar.Header, want string, maxSize int64) error {
	if err := validateArchivePath(hdr.Name); err != nil {
		return err
	}
	if hdr.Name != want {
		return fmt.Errorf("archive entry %q, want %q", hdr.Name, want)
	}
	if hdr.Typeflag != tar.TypeReg {
		return fmt.Errorf("archive entry %q has type %q, want a regular file", hdr.Name, hdr.Typeflag)
	}
	if hdr.Size < 0 || hdr.Size > maxSize {
		return fmt.Errorf("archive entry %q has size %d, want 0..%d", hdr.Name, hdr.Size, maxSize)
	}
	return nil
}

// syncPath fsyncs the file or directory at p.
func syncPath(p string) error {
	f, err := os.Open(p) //nolint:gosec // p is a path under the node home.
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("fsync %s: %w", p, err)
	}
	return f.Close()
}
