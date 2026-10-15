package statesyncsnapshot

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

func validManifest() *manifest {
	dir := flatkv.SnapshotDirName(7)
	return &manifest{
		FormatVersion:    formatVersion,
		ChainID:          testChainID,
		CheckpointHeight: 7,
		CheckpointDir:    dir,
		CreatedAt:        time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		ProducerVersion:  "test",
		Files: []manifestFile{
			fileOf("epochs/registry_a.pb", "a"),
			fileOf("flatkv/"+dir+"/account/000001.sst", "sst"),
		},
	}
}

func fileOf(path, content string) manifestFile {
	sum := sha256.Sum256([]byte(content))
	return manifestFile{Path: path, Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:])}
}

func TestDecodeManifestAcceptsAValidManifest(t *testing.T) {
	data, err := json.Marshal(validManifest())
	require.NoError(t, err)
	m, err := decodeManifest(data)
	require.NoError(t, err)
	require.Equal(t, validManifest(), m)
}

func TestDecodeManifestRejects(t *testing.T) {
	dir := flatkv.SnapshotDirName(7)
	edit := func(f func(m *manifest)) []byte {
		m := validManifest()
		f(m)
		data, err := json.Marshal(m)
		require.NoError(t, err)
		return data
	}
	withFile := func(p string) []byte {
		return edit(func(m *manifest) { m.Files = append(m.Files, fileOf(p, "x")) })
	}
	valid, err := json.Marshal(validManifest())
	require.NoError(t, err)

	cases := map[string][]byte{
		"a mode field":              bytes.Replace(valid, []byte(`"size":1,`), []byte(`"size":1,"mode":511,`), 1),
		"an unknown top field":      bytes.Replace(valid, []byte(`{"format_version"`), []byte(`{"extra":1,"format_version"`), 1),
		"trailing data":             append(append([]byte{}, valid...), []byte(`{}`)...),
		"another format version":    edit(func(m *manifest) { m.FormatVersion = 2 }),
		"an empty chain id":         edit(func(m *manifest) { m.ChainID = "" }),
		"height 0":                  edit(func(m *manifest) { m.CheckpointHeight = 0; m.CheckpointDir = flatkv.SnapshotDirName(0) }),
		"a checkpoint dir mismatch": edit(func(m *manifest) { m.CheckpointDir = flatkv.SnapshotDirName(8) }),
		"a duplicate path":          edit(func(m *manifest) { m.Files = append(m.Files, m.Files[0]) }),
		"a negative size":           edit(func(m *manifest) { m.Files[0].Size = -1 }),
		"a bad sha256":              edit(func(m *manifest) { m.Files[0].SHA256 = "abc" }),
		"a parent path":             withFile("flatkv/" + dir + "/../x"),
		"an absolute path":          withFile("/epochs/x"),
		"a nested epoch path":       withFile("epochs/a/b"),
		"another checkpoint dir":    withFile("flatkv/" + flatkv.SnapshotDirName(8) + "/account/1.sst"),
		"a file at the flatkv top":  withFile("flatkv/" + dir),
		"a backslash":               withFile("epochs/a\\b"),
		"a path outside the tree":   withFile("other/x"),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := decodeManifest(data)
			require.Error(t, err)
		})
	}
}

// tarEntry is one entry of a hand-built archive.
type tarEntry struct {
	name     string
	content  string
	typeflag byte
}

func buildRawArchive(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	require.NoError(t, err)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Mode: 0o600, Typeflag: typeflag}
		if typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.content))
		} else {
			hdr.Linkname = e.content
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if typeflag == tar.TypeReg {
			_, err := tw.Write([]byte(e.content))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func manifestEntry(t *testing.T, m *manifest) tarEntry {
	t.Helper()
	data, err := json.Marshal(m)
	require.NoError(t, err)
	return tarEntry{name: manifestName, content: string(data)}
}

func extractInto(t *testing.T, data []byte) (string, error) {
	t.Helper()
	staging := t.TempDir()
	a, m, err := openArchive(t.Context(), bytes.NewReader(data))
	if err != nil {
		return staging, err
	}
	defer a.Close()
	return staging, a.extract(m, staging)
}

func TestExtractWritesEveryListedFile(t *testing.T) {
	m := validManifest()
	staging, err := extractInto(t, buildRawArchive(t, []tarEntry{
		manifestEntry(t, m),
		{name: m.Files[0].Path, content: "a"},
		{name: m.Files[1].Path, content: "sst"},
	}))
	require.NoError(t, err)
	for _, f := range m.Files {
		p := filepath.Join(staging, filepath.FromSlash(f.Path))
		info, err := os.Stat(p)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	info, err := os.Stat(filepath.Join(staging, "flatkv"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o750), info.Mode().Perm())
}

func TestExtractRejects(t *testing.T) {
	m := validManifest()
	epoch, sst := m.Files[0].Path, m.Files[1].Path
	cases := map[string][]tarEntry{
		"a manifest that is not first": {{name: epoch, content: "a"}, manifestEntry(t, m), {name: sst, content: "sst"}},
		"an unlisted entry":            {manifestEntry(t, m), {name: "epochs/unlisted.pb", content: "u"}, {name: epoch, content: "a"}, {name: sst, content: "sst"}},
		"a repeated entry":             {manifestEntry(t, m), {name: epoch, content: "a"}, {name: epoch, content: "a"}, {name: sst, content: "sst"}},
		"a size mismatch":              {manifestEntry(t, m), {name: epoch, content: "ab"}, {name: sst, content: "sst"}},
		"a sha256 mismatch":            {manifestEntry(t, m), {name: epoch, content: "b"}, {name: sst, content: "sst"}},
		"a missing file":               {manifestEntry(t, m), {name: epoch, content: "a"}},
		"a symlink entry":              {manifestEntry(t, m), {name: epoch, content: "/etc/passwd", typeflag: tar.TypeSymlink}, {name: sst, content: "sst"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			staging, err := extractInto(t, buildRawArchive(t, entries))
			require.Error(t, err)
			require.NoFileExists(t, filepath.Join(staging, "epochs", "unlisted.pb"))
		})
	}

	t.Run("a truncated stream", func(t *testing.T) {
		data := buildRawArchive(t, []tarEntry{manifestEntry(t, m), {name: epoch, content: "a"}, {name: sst, content: "sst"}})
		_, err := extractInto(t, data[:len(data)/2])
		require.Error(t, err)
	})
	t.Run("an oversized manifest", func(t *testing.T) {
		_, err := extractInto(t, buildRawArchive(t, []tarEntry{{name: manifestName, content: strings.Repeat(" ", maxManifestSize+1)}}))
		require.ErrorContains(t, err, "more than")
	})
}
