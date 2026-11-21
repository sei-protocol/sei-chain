package statesync

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// archiveEntry is one entry of a test archive.
type archiveEntry struct {
	name     string
	data     []byte
	typeflag byte
}

// validEntries returns the entries of the archive of the checkpoint in dir at height, and its
// manifest.
func validEntries(t *testing.T, dir string, height int64) ([]archiveEntry, *Manifest) {
	t.Helper()
	m, err := BuildManifest(testChainID, height, dir)
	require.NoError(t, err)
	manifest, err := EncodeManifest(m)
	require.NoError(t, err)
	entries := []archiveEntry{
		{name: manifestEntry, data: manifest},
		{name: proofEntry, data: []byte("proof")},
	}
	for _, f := range m.Files {
		data, err := os.ReadFile(filepath.Join(dir, strings.TrimPrefix(f.Path, flatKVEntryDir+"/")))
		require.NoError(t, err)
		entries = append(entries, archiveEntry{name: f.Path, data: data})
	}
	return entries, m
}

func buildArchive(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	for _, e := range entries {
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		hdr := entryHeader(e.name, int64(len(e.data)))
		hdr.Typeflag = typeflag
		if typeflag != tar.TypeReg {
			hdr.Size = 0
			hdr.Linkname = "elsewhere"
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if typeflag == tar.TypeReg {
			_, err := tw.Write(e.data)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	return out.Bytes()
}

func acceptAll(*Manifest) error { return nil }

func TestReadArchiveRoundTrip(t *testing.T) {
	dir, height := stagedCheckpoint(t)
	m, err := BuildManifest(testChainID, height, dir)
	require.NoError(t, err)
	var archive bytes.Buffer
	require.NoError(t, WriteArchive(t.Context(), &archive, m, []byte("proof"), dir))
	out := filepath.Join(t.TempDir(), "download")

	got, proof, err := ReadArchive(t.Context(), &archive, out, acceptAll)
	require.NoError(t, err)

	require.Equal(t, m, got)
	require.Equal(t, []byte("proof"), proof)
	for _, f := range m.Files {
		rel := strings.TrimPrefix(f.Path, flatKVEntryDir+"/")
		want, err := os.ReadFile(filepath.Join(dir, rel))
		require.NoError(t, err)
		gotData, err := os.ReadFile(filepath.Join(out, rel))
		require.NoError(t, err)
		require.Equal(t, want, gotData, f.Path)
	}
	require.NoError(t, VerifyCheckpoint(out, got))
}

func TestReadArchiveStopsWhenAcceptRefuses(t *testing.T) {
	dir, height := stagedCheckpoint(t)
	entries, _ := validEntries(t, dir, height)
	out := filepath.Join(t.TempDir(), "download")
	refused := errors.New("wrong chain")

	_, _, err := ReadArchive(t.Context(), bytes.NewReader(buildArchive(t, entries)), out, func(*Manifest) error { return refused })

	require.ErrorIs(t, err, refused)
	require.NoDirExists(t, out)
}

func TestReadArchiveRejects(t *testing.T) {
	dir, height := stagedCheckpoint(t)
	for name, tc := range map[string]struct {
		change func(t *testing.T, entries []archiveEntry) []byte
		want   error
	}{
		"extra file": {func(t *testing.T, entries []archiveEntry) []byte {
			return buildArchive(t, append(entries, archiveEntry{name: "flatkv/account/extra", data: []byte("x")}))
		}, errUnlistedEntry},
		"missing file": {func(t *testing.T, entries []archiveEntry) []byte {
			return buildArchive(t, entries[:len(entries)-1])
		}, errArchiveEnded},
		"file of another size": {func(t *testing.T, entries []archiveEntry) []byte {
			entries[2].data = append(entries[2].data, 0)
			return buildArchive(t, entries)
		}, errEntrySize},
		"files out of order": {func(t *testing.T, entries []archiveEntry) []byte {
			entries[2], entries[3] = entries[3], entries[2]
			return buildArchive(t, entries)
		}, errUnexpectedEntry},
		"symlink entry": {func(t *testing.T, entries []archiveEntry) []byte {
			entries[2].typeflag = tar.TypeSymlink
			return buildArchive(t, entries)
		}, errEntryType},
		"proof first": {func(t *testing.T, entries []archiveEntry) []byte {
			entries[0], entries[1] = entries[1], entries[0]
			return buildArchive(t, entries)
		}, errUnexpectedEntry},
		"truncated": {func(t *testing.T, entries []archiveEntry) []byte {
			archive := buildArchive(t, entries)
			return archive[:len(archive)-2048]
		}, errArchiveEnded},
		"truncated end": {func(t *testing.T, entries []archiveEntry) []byte {
			archive := buildArchive(t, entries)
			return archive[:len(archive)-1024]
		}, errArchiveSize},
		"trailing data": {func(t *testing.T, entries []archiveEntry) []byte {
			return append(buildArchive(t, entries), 1)
		}, errTrailingData},
		"unsupported pebble format": {func(t *testing.T, entries []archiveEntry) []byte {
			m, err := DecodeManifest(entries[0].data)
			require.NoError(t, err)
			m.PebbleFormatMajorVersion++
			data, err := json.Marshal(m)
			require.NoError(t, err)
			entries[0].data = data
			return buildArchive(t, entries)
		}, errUnsupportedPebbleFormat},
	} {
		t.Run(name, func(t *testing.T) {
			entries, _ := validEntries(t, dir, height)
			archive := tc.change(t, entries)
			out := filepath.Join(t.TempDir(), "download")

			_, _, err := ReadArchive(t.Context(), bytes.NewReader(archive), out, acceptAll)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestVerifyCheckpointRejectsOtherStats(t *testing.T) {
	dir, height := stagedCheckpoint(t)
	m, err := BuildManifest(testChainID, height, dir)
	require.NoError(t, err)
	m.RowCount++

	require.ErrorContains(t, VerifyCheckpoint(dir, m), "manifest records")
}
