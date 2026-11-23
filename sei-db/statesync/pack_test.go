package statesync

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

func TestBuildManifest(t *testing.T) {
	dir, height := stagedCheckpoint(t)

	m, err := BuildManifest(testChainID, height, dir)
	require.NoError(t, err)

	stats, err := flatkv.SnapshotDirStats(dir, height)
	require.NoError(t, err)
	require.Equal(t, stats, m.Stats())
	require.Positive(t, m.RowCount)
	require.Equal(t, testChainID, m.ChainID)
	require.Equal(t, height, m.Height)
	require.Equal(t, pebbledb.FormatMajorVersion, m.PebbleFormatMajorVersion)
	for _, f := range m.Files {
		require.NotEqual(t, lockFileName, filepath.Base(f.Path))
		info, err := os.Stat(filepath.Join(dir, strings.TrimPrefix(f.Path, flatKVEntryDir+"/")))
		require.NoError(t, err)
		require.Equal(t, info.Size(), f.Size, f.Path)
	}
}

func TestBuildManifestRejectsAnUnknownEntry(t *testing.T) {
	dir, height := stagedCheckpoint(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra"), []byte("x"), 0o600))

	_, err := BuildManifest(testChainID, height, dir)
	require.ErrorContains(t, err, "extra")
}

func TestWriteArchiveRoundTrip(t *testing.T) {
	dir, height := stagedCheckpoint(t)
	m, err := BuildManifest(testChainID, height, dir)
	require.NoError(t, err)
	proof := []byte("proof bytes")

	var out bytes.Buffer
	require.NoError(t, WriteArchive(t.Context(), &out, m, proof, dir))

	size, err := ArchiveSize(m, int64(len(proof)))
	require.NoError(t, err)
	require.Equal(t, size, int64(out.Len()))

	tr := tar.NewReader(&out)
	manifest := nextEntry(t, tr, manifestEntry)
	got, err := DecodeManifest(manifest)
	require.NoError(t, err)
	require.Equal(t, m, got)
	require.Equal(t, proof, nextEntry(t, tr, proofEntry))
	for _, f := range m.Files {
		want, err := os.ReadFile(filepath.Join(dir, strings.TrimPrefix(f.Path, flatKVEntryDir+"/")))
		require.NoError(t, err)
		require.Equal(t, want, nextEntry(t, tr, f.Path))
	}
	_, err = tr.Next()
	require.ErrorIs(t, err, io.EOF)
}

func TestWriteArchiveIsReproducible(t *testing.T) {
	dir, height := stagedCheckpoint(t)
	m, err := BuildManifest(testChainID, height, dir)
	require.NoError(t, err)

	var first, second bytes.Buffer
	require.NoError(t, WriteArchive(t.Context(), &first, m, []byte("p"), dir))
	require.NoError(t, WriteArchive(t.Context(), &second, m, []byte("p"), dir))
	require.Equal(t, first.Bytes(), second.Bytes())
}

func TestWriteArchiveRejectsAChangedFile(t *testing.T) {
	dir, height := stagedCheckpoint(t)
	m, err := BuildManifest(testChainID, height, dir)
	require.NoError(t, err)
	changed := filepath.Join(dir, strings.TrimPrefix(m.Files[0].Path, flatKVEntryDir+"/"))
	require.NoError(t, os.Remove(changed))
	require.NoError(t, os.WriteFile(changed, []byte("different size"), 0o600))

	err = WriteArchive(t.Context(), io.Discard, m, nil, dir)
	require.ErrorContains(t, err, m.Files[0].Path)
}

func TestWriteArchiveStopsWhenCanceled(t *testing.T) {
	dir, height := stagedCheckpoint(t)
	m, err := BuildManifest(testChainID, height, dir)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, WriteArchive(ctx, io.Discard, m, nil, dir), context.Canceled)
}

func TestArchiveSizeRejectsALargeProof(t *testing.T) {
	dir, height := stagedCheckpoint(t)
	m, err := BuildManifest(testChainID, height, dir)
	require.NoError(t, err)

	_, err = ArchiveSize(m, maxProofSize+1)
	require.Error(t, err)
}

func TestEntrySize(t *testing.T) {
	require.Equal(t, int64(512), entrySize(0))
	require.Equal(t, int64(1024), entrySize(1))
	require.Equal(t, int64(1024), entrySize(512))
	require.Equal(t, int64(1536), entrySize(513))
}

func nextEntry(t *testing.T, tr *tar.Reader, name string) []byte {
	t.Helper()
	hdr, err := tr.Next()
	require.NoError(t, err)
	require.Equal(t, name, hdr.Name)
	require.Equal(t, byte(tar.TypeReg), hdr.Typeflag)
	data, err := io.ReadAll(tr)
	require.NoError(t, err)
	return data
}
