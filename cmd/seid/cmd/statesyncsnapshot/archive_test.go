package statesyncsnapshot

import (
	"archive/tar"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

func validManifest() *manifest {
	return &manifest{
		FormatVersion:    formatVersion,
		ChainID:          testChainID,
		CheckpointHeight: 7,
		CreatedAt:        time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		ProducerVersion:  "test",
		RowCount:         2,
		StateBytes:       int64(len("bank/a") + len("1") + len("bank/b") + len("2")),
		StateHash:        hexHash{0x01, 0x02, 0x03},
	}
}

func TestDecodeManifestAcceptsAValidManifest(t *testing.T) {
	data, err := json.Marshal(validManifest())
	require.NoError(t, err)
	m, err := decodeManifest(data)
	require.NoError(t, err)
	require.Equal(t, validManifest(), m)
}

func TestDecodeManifestRejects(t *testing.T) {
	edit := func(f func(m *manifest)) []byte {
		m := validManifest()
		f(m)
		data, err := json.Marshal(m)
		require.NoError(t, err)
		return data
	}
	valid, err := json.Marshal(validManifest())
	require.NoError(t, err)

	cases := map[string][]byte{
		"an unknown top field":   bytes.Replace(valid, []byte(`{"format_version"`), []byte(`{"extra":1,"format_version"`), 1),
		"trailing data":          append(append([]byte{}, valid...), []byte(`{}`)...),
		"a trailing brace":       append(append([]byte{}, valid...), '}'),
		"another format version": edit(func(m *manifest) { m.FormatVersion = 1 }),
		"an empty chain id":      edit(func(m *manifest) { m.ChainID = "" }),
		"height 0":               edit(func(m *manifest) { m.CheckpointHeight = 0 }),
		"a negative row count":   edit(func(m *manifest) { m.RowCount = -1 }),
		"a negative byte count":  edit(func(m *manifest) { m.StateBytes = -1 }),
		"a missing state hash":   edit(func(m *manifest) { m.StateHash = hexHash{} }),
		"a short state hash":     bytes.Replace(valid, []byte(`"state_hash":"01`), []byte(`"state_hash":"`), 1),
		"a non-hex state hash":   bytes.Replace(valid, []byte(`"state_hash":"01`), []byte(`"state_hash":"zz`), 1),
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
	content  []byte
	typeflag byte
}

func buildRawArchive(t *testing.T, entries []tarEntry, opts ...zstd.EOption) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf, opts...)
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
			hdr.Linkname = string(e.content)
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if typeflag == tar.TypeReg {
			_, err := tw.Write(e.content)
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
	return tarEntry{name: manifestName, content: data}
}

func proofEntry(height int64, data []byte) tarEntry {
	return tarEntry{name: proofArchivePath(height), content: data}
}

func rowChunkEntry(index int, rows ...rowKV) tarEntry {
	var data []byte
	for _, row := range rows {
		data = appendRecord(data, []byte(row.key), []byte(row.value))
	}
	return tarEntry{name: rowChunkPath(index), content: data}
}

type rowKV struct {
	key   string
	value string
}

func appendRecord(dst, key, value []byte) []byte {
	var buf [10]byte
	n := binary.PutUvarint(buf[:], uint64(len(key)))
	dst = append(dst, buf[:n]...)
	dst = append(dst, key...)
	n = binary.PutUvarint(buf[:], uint64(len(value)))
	dst = append(dst, buf[:n]...)
	dst = append(dst, value...)
	return dst
}

func readArchiveRows(t *testing.T, data []byte) ([]*sctypes.SnapshotNode, error) {
	t.Helper()
	a, m, err := openArchive(t.Context(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer a.Close()
	if _, err := a.readProof(m.CheckpointHeight); err != nil {
		return nil, err
	}
	importer := &recordingImporter{}
	if err := a.importRows(m, importer); err != nil {
		return nil, err
	}
	return importer.nodes, nil
}

func TestOpenArchiveRejectsAWindowLargerThanTheArchiveWindow(t *testing.T) {
	m := validManifest()
	// An entry larger than one zstd block makes the encoder write a window size into the frame.
	big := make([]byte, 1<<20)
	_, _ = rand.New(rand.NewSource(1)).Read(big) //nolint:gosec // test content.
	key := []byte("bank/big")
	m.RowCount = 1
	m.StateBytes = int64(len(key) + len(big))
	entries := []tarEntry{
		manifestEntry(t, m),
		proofEntry(m.CheckpointHeight, []byte("proof")),
		{name: rowChunkPath(0), content: appendRecord(nil, key, big)},
	}

	_, err := readArchiveRows(t, buildRawArchive(t, entries, zstd.WithWindowSize(archiveWindowSize)))
	require.NoError(t, err)
	_, err = readArchiveRows(t, buildRawArchive(t, entries, zstd.WithWindowSize(2*archiveWindowSize)))
	require.ErrorIs(t, err, zstd.ErrWindowSizeExceeded)
}

func TestArchiveReadsRows(t *testing.T) {
	m := validManifest()
	nodes, err := readArchiveRows(t, buildRawArchive(t, []tarEntry{
		manifestEntry(t, m),
		proofEntry(m.CheckpointHeight, []byte("proof")),
		rowChunkEntry(0, rowKV{"bank/a", "1"}, rowKV{"bank/b", "2"}),
	}))
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Equal(t, []byte("bank/a"), nodes[0].Key)
	require.Equal(t, []byte("1"), nodes[0].Value)
	require.Equal(t, int64(7), nodes[0].Version)
}

func TestArchiveRejects(t *testing.T) {
	m := validManifest()
	validRows := rowChunkEntry(0, rowKV{"bank/a", "1"}, rowKV{"bank/b", "2"})
	cases := map[string][]tarEntry{
		"a manifest that is not first":    {proofEntry(m.CheckpointHeight, []byte("proof")), manifestEntry(t, m), validRows},
		"a raw pebble entry":              {manifestEntry(t, m), proofEntry(m.CheckpointHeight, []byte("proof")), {name: "flatkv/snapshot-00000000000000000007/account/000001.sst", content: []byte("sst")}},
		"an unknown entry":                {manifestEntry(t, m), proofEntry(m.CheckpointHeight, []byte("proof")), {name: "other/x", content: []byte("x")}},
		"a missing chunk zero":            {manifestEntry(t, m), proofEntry(m.CheckpointHeight, []byte("proof")), {name: rowChunkPath(1), content: validRows.content}},
		"a symlink chunk":                 {manifestEntry(t, m), proofEntry(m.CheckpointHeight, []byte("proof")), {name: rowChunkPath(0), content: []byte("/etc/passwd"), typeflag: tar.TypeSymlink}},
		"a zero key length":               {manifestEntry(t, m), proofEntry(m.CheckpointHeight, []byte("proof")), {name: rowChunkPath(0), content: appendRecord(nil, nil, []byte("1"))}},
		"a zero value length":             {manifestEntry(t, m), proofEntry(m.CheckpointHeight, []byte("proof")), {name: rowChunkPath(0), content: appendRecord(nil, []byte("bank/a"), nil)}},
		"a row count mismatch":            {manifestEntry(t, editManifest(t, m, func(m *manifest) { m.RowCount = 3 })), proofEntry(m.CheckpointHeight, []byte("proof")), validRows},
		"a state byte count mismatch":     {manifestEntry(t, editManifest(t, m, func(m *manifest) { m.StateBytes++ })), proofEntry(m.CheckpointHeight, []byte("proof")), validRows},
		"a proof of another height":       {manifestEntry(t, m), proofEntry(m.CheckpointHeight+1, []byte("proof")), validRows},
		"a backslash path":                {manifestEntry(t, m), proofEntry(m.CheckpointHeight, []byte("proof")), {name: "flatkv\\rows\\00000000", content: validRows.content}},
		"a chunk with an oversized key":   {manifestEntry(t, editManifest(t, m, func(m *manifest) { m.RowCount = 1; m.StateBytes = int64(maxStateKeySize + 2) })), proofEntry(m.CheckpointHeight, []byte("proof")), {name: rowChunkPath(0), content: oversizedLength(maxStateKeySize + 1)}},
		"a chunk with an oversized value": {manifestEntry(t, editManifest(t, m, func(m *manifest) { m.RowCount = 1; m.StateBytes = int64(maxStateValueSize + 2) })), proofEntry(m.CheckpointHeight, []byte("proof")), {name: rowChunkPath(0), content: append(appendRecord(nil, []byte("bank/a"), []byte("1")), oversizedLength(maxStateValueSize+1)...)}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := readArchiveRows(t, buildRawArchive(t, entries))
			require.Error(t, err)
		})
	}

	t.Run("a truncated stream", func(t *testing.T) {
		data := buildRawArchive(t, []tarEntry{manifestEntry(t, m), proofEntry(m.CheckpointHeight, []byte("proof")), validRows})
		_, err := readArchiveRows(t, data[:len(data)/2])
		require.Error(t, err)
	})
	t.Run("an oversized manifest", func(t *testing.T) {
		_, err := readArchiveRows(t, buildRawArchive(t, []tarEntry{{name: manifestName, content: []byte(strings.Repeat(" ", maxManifestSize+1))}}))
		require.ErrorContains(t, err, "more than")
	})
}

func editManifest(t *testing.T, src *manifest, edit func(*manifest)) *manifest {
	t.Helper()
	cp := *src
	edit(&cp)
	return &cp
}

func oversizedLength(n int) []byte {
	var buf [10]byte
	size := binary.PutUvarint(buf[:], uint64(n))
	return buf[:size]
}

type recordingImporter struct {
	nodes []*sctypes.SnapshotNode
}

func (i *recordingImporter) AddModule(string) error { return nil }

func (i *recordingImporter) AddNode(node *sctypes.SnapshotNode) error {
	i.nodes = append(i.nodes, node)
	return nil
}

func (i *recordingImporter) Abort(reason error) error { return reason }

func (i *recordingImporter) Close() error { return nil }
