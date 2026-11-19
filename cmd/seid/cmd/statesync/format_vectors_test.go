package statesync

import (
	"archive/tar"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// formatVectorsFile holds the archive test vectors of formatVersion that FORMAT.md specifies.
var formatVectorsFile = filepath.Join("testdata", fmt.Sprintf("format_v%d_vectors.json", formatVersion))

// recordFormatVectors writes formatVectorsFile from buildFormatVectors instead of checking it. Off by
// default, refused on CI, and refused when the file exists.
var recordFormatVectors = flag.Bool("format-vectors-record", false,
	"write the vectors file of this format version from this build instead of checking it")

// vectorHeight is the checkpoint height of every vector archive.
const vectorHeight = 7

// formatVectors is the content of formatVectorsFile.
type formatVectors struct {
	FormatVersion int            `json:"format_version"`
	Valid         []formatVector `json:"valid"`
	Invalid       []formatVector `json:"invalid"`
}

// formatVector is one archive, given as its tar entries before compression.
type formatVector struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Entries     []vectorEntry `json:"entries"`
	// Rows are the rows that a reader imports from a valid archive, in order.
	Rows []vectorRow `json:"rows,omitempty"`
	// ErrorContains is part of the error this reader returns for an invalid archive. Another reader only
	// has to refuse the archive.
	ErrorContains string `json:"error_contains,omitempty"`
}

// vectorEntry is one tar entry of a vector archive.
type vectorEntry struct {
	Name string `json:"name"`
	// Symlink is the target of a symbolic link entry. An entry without it is a regular file.
	Symlink string `json:"symlink,omitempty"`
	// JSON is the content of a JSON file.
	JSON json.RawMessage `json:"json,omitempty"`
	// Hex is the content of any other regular file.
	Hex string `json:"hex,omitempty"`
}

// vectorRow is one FlatKV row, in hex.
type vectorRow struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func TestFormatVectors(t *testing.T) {
	if *recordFormatVectors {
		recordFormatVectorsFile(t)
	}
	vectors := readFormatVectors(t)
	require.Equal(t, formatVersion, vectors.FormatVersion)

	for _, v := range vectors.Valid {
		t.Run(v.Name, func(t *testing.T) {
			checkpoint, m, err := checkFormatVector(t, v)
			require.NoError(t, err)
			require.Equal(t, v.Rows, exportedVectorRows(t, checkpoint))
			requireWriterReproduces(t, v, m, checkpoint)
		})
	}
	for _, v := range vectors.Invalid {
		t.Run(v.Name, func(t *testing.T) {
			require.NotEmpty(t, v.ErrorContains)
			_, _, err := checkFormatVector(t, v)
			require.ErrorContains(t, err, v.ErrorContains)
		})
	}
}

// The rows of the first valid vector are the rows that the FlatKV key and value encoders produce.
func TestFormatVectorRowsUseTheFlatKVEncoders(t *testing.T) {
	require.Equal(t, formatVectorRows(), readFormatVectors(t).Valid[0].Rows,
		"the FlatKV encoders no longer produce the rows of the v%d vectors. These bytes are in the state hash, "+
			"so this needs a chain upgrade and a new format_version. Do not re-record the vectors.", formatVersion)
}

func TestFormatUvarintExamples(t *testing.T) {
	for length, encoded := range map[uint64]string{1: "01", 127: "7f", 128: "8001", 300: "ac02"} {
		w := &rowChunkWriter{}
		w.appendUvarint(length)
		require.Equal(t, encoded, hex.EncodeToString(w.chunk), "length %d", length)
	}
}

func readFormatVectors(t *testing.T) formatVectors {
	t.Helper()
	data, err := os.ReadFile(formatVectorsFile)
	require.NoError(t, err)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var vectors formatVectors
	require.NoError(t, dec.Decode(&vectors))
	return vectors
}

// checkFormatVector imports the archive of v the way import does before it verifies the proof, and returns
// the imported checkpoint and the manifest. It does not decode the proof entry.
func checkFormatVector(t *testing.T, v formatVector) (string, *manifest, error) {
	t.Helper()
	checkpoint, m, err := importFormatVector(t, v)
	if err != nil {
		return "", nil, err
	}
	if err := requireImportedStats(checkpoint, m); err != nil {
		return "", nil, err
	}
	return checkpoint, m, nil
}

// importFormatVector is checkFormatVector without the comparison of the imported stats with the manifest.
func importFormatVector(t *testing.T, v formatVector) (string, *manifest, error) {
	t.Helper()
	a, m, err := openArchive(t.Context(), bytes.NewReader(buildRawArchive(t, v.tarEntries(t))))
	if err != nil {
		return "", nil, err
	}
	defer a.Close()
	if _, err := a.readProof(m.CheckpointHeight); err != nil {
		return "", nil, err
	}
	root := filepath.Join(t.TempDir(), flatkvPrefix)
	if _, err := importArchivedRows(t.Context(), a, m, root); err != nil {
		return "", nil, err
	}
	checkpoint := filepath.Join(root, flatkv.SnapshotDirName(m.CheckpointHeight))
	if err := flatkv.VerifySnapshotDir(checkpoint, m.CheckpointHeight); err != nil {
		return "", nil, err
	}
	return checkpoint, m, nil
}

func (v formatVector) tarEntries(t *testing.T) []tarEntry {
	t.Helper()
	entries := make([]tarEntry, 0, len(v.Entries))
	for _, e := range v.Entries {
		switch {
		case e.Symlink != "":
			entries = append(entries, tarEntry{name: e.Name, content: []byte(e.Symlink), typeflag: tar.TypeSymlink})
		case e.JSON != nil:
			entries = append(entries, tarEntry{name: e.Name, content: e.JSON})
		default:
			content, err := hex.DecodeString(e.Hex)
			require.NoError(t, err)
			entries = append(entries, tarEntry{name: e.Name, content: content})
		}
	}
	return entries
}

func exportedVectorRows(t *testing.T, checkpoint string) []vectorRow {
	t.Helper()
	var rows []vectorRow
	_, err := flatkv.ExportSnapshotDir(t.Context(), checkpoint, vectorHeight, func(key, value []byte) error {
		rows = append(rows, vectorRow{Key: hex.EncodeToString(key), Value: hex.EncodeToString(value)})
		return nil
	})
	require.NoError(t, err)
	return rows
}

// requireWriterReproduces packs checkpoint with the manifest and the proof of v, and requires the entries of
// v: the same names in the same order, the same proof and row chunk bytes, and an equal manifest.
func requireWriterReproduces(t *testing.T, v formatVector, m *manifest, checkpoint string) {
	t.Helper()
	want := v.tarEntries(t)
	var buf bytes.Buffer
	require.NoError(t, writeArchive(t.Context(), &buf, m, want[1].content, checkpoint))
	got := readTarEntries(t, buf.Bytes())
	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].name, got[i].name)
		if want[i].name == manifestName {
			wantManifest, err := decodeManifest(want[i].content)
			require.NoError(t, err)
			gotManifest, err := decodeManifest(got[i].content)
			require.NoError(t, err)
			require.Equal(t, wantManifest, gotManifest)
			continue
		}
		require.Equal(t, hex.EncodeToString(want[i].content), hex.EncodeToString(got[i].content), want[i].name)
	}
}

func readTarEntries(t *testing.T, archive []byte) []tarEntry {
	t.Helper()
	zr, err := zstd.NewReader(bytes.NewReader(archive))
	require.NoError(t, err)
	defer zr.Close()
	tr := tar.NewReader(zr)
	var entries []tarEntry
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}
		require.NoError(t, err)
		content, err := io.ReadAll(tr)
		require.NoError(t, err)
		entries = append(entries, tarEntry{name: hdr.Name, content: content, typeflag: hdr.Typeflag})
	}
}

// formatVectorRows returns the rows of the first valid vector, in key order: a bank row, an EVM misc row (an
// EVM-to-Sei address mapping), an EVM storage row, an EVM code row and an EVM account row, all written at
// block 5.
func formatVectorRows() []vectorRow {
	var addr ktype.Address
	for i := range addr {
		addr[i] = 0x11
	}
	slot := ktype.Slot{31: 0x02}
	slotValue := [32]byte{31: 0x01}
	balance := vtype.Balance{31: 0x64}
	seiAddr := bytes.Repeat([]byte{0x22}, len(addr))
	row := func(key, value []byte) vectorRow {
		return vectorRow{Key: hex.EncodeToString(key), Value: hex.EncodeToString(value)}
	}
	return []vectorRow{
		row(ktype.BuildPhysicalKey("bank", []byte{0x01}), vtype.SerializeMisc(5, []byte("v"))),
		row(ktype.BuildPhysicalKey(keys.EVMStoreKey, append([]byte{0x01}, addr[:]...)), vtype.SerializeMisc(5, seiAddr)),
		row(ktype.BuildPhysicalEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
			vtype.NewStorageData().SetBlockHeight(5).SetValue(&slotValue).Serialize()),
		row(ktype.BuildPhysicalEVMKey(keys.EVMKeyCode, addr[:]),
			vtype.NewCodeData().SetBlockHeight(5).SetBytecode([]byte{0x60, 0x00, 0x60, 0x00, 0xf3}).Serialize()),
		row(ktype.BuildPhysicalEVMKey(ktype.EVMKeyAccount, addr[:]),
			vtype.NewAccountData().SetBlockHeight(5).SetBalance(&balance).SetNonce(1).Serialize()),
	}
}

func recordFormatVectorsFile(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatal("refusing to re-record the archive format vectors on CI: they are the expected values, so a " +
			"build that rewrites them verifies nothing")
	}
	data, err := json.MarshalIndent(buildFormatVectors(t), "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(formatVectorsFile), 0o750))
	f, err := os.OpenFile(formatVectorsFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err, "%s exists: increase formatVersion, or delete the file if this version was never released",
		formatVectorsFile)
	_, err = f.Write(append(data, '\n'))
	require.NoError(t, err)
	require.NoError(t, f.Close())
	t.Logf("recorded %s; review the diff before committing", formatVectorsFile)
}

// buildFormatVectors returns the valid vectors and one invalid vector for each rule that a reader checks
// from the archive alone. Each invalid vector breaks one rule of the first valid one.
func buildFormatVectors(t *testing.T) formatVectors {
	t.Helper()
	rows := formatVectorRows()
	decoded := decodeVectorRows(t, rows)
	m := vectorManifest(t, decoded)
	empty := vectorManifest(t, nil)

	proof := []byte("placeholder state sync proof")
	chunk := recordChunk(decoded...)
	manifestEntry := manifestVectorEntry(t, m)
	proofEntry := fileVectorEntry(proofArchivePath(vectorHeight), proof)
	chunkEntry := fileVectorEntry(rowChunkPath(0), chunk)
	edited := func(edit func(*manifest)) vectorEntry {
		cp := *m
		edit(&cp)
		return manifestVectorEntry(t, &cp)
	}
	withRows := func(extra ...rowKV) (vectorEntry, vectorEntry) {
		all := append(append([]rowKV{}, extra...), decoded...)
		slices.SortFunc(all, func(a, b rowKV) int { return strings.Compare(a.key, b.key) })
		return edited(func(e *manifest) { e.RowCount = int64(len(all)); e.StateBytes = rowBytes(all) }),
			fileVectorEntry(rowChunkPath(0), recordChunk(all...))
	}
	invalid := func(name, description, errorContains string, entries ...vectorEntry) formatVector {
		return formatVector{Name: name, Description: description, Entries: entries, ErrorContains: errorContains}
	}

	unknownField := bytes.Replace(manifestEntry.JSON, []byte(`{"format_version"`), []byte(`{"extra":1,"format_version"`), 1)
	duplicateManifest, duplicateChunk := withRows(decoded[0])
	metaManifest, metaChunk := withRows(rowKV{key: string(ktype.MetaVersionKey), value: string(vtype.SerializeMisc(5, []byte("x")))})
	noModuleManifest, noModuleChunk := withRows(rowKV{key: "nomodule", value: string(vtype.SerializeMisc(5, []byte("x")))})
	unordered := recordChunk(append([]rowKV{decoded[1], decoded[0]}, decoded[2:]...)...)
	zeroValue := append(append([]byte{byte(len(decoded[0].key))}, decoded[0].key...), 0x00)

	return formatVectors{
		FormatVersion: formatVersion,
		Valid: []formatVector{
			{
				Name: "valid",
				Description: "Five rows in one chunk. The proof entry is a placeholder: the container reader checks only " +
					"its name and size, and the statesyncproof package specifies its content.",
				Entries: []vectorEntry{manifestEntry, proofEntry, chunkEntry},
				Rows:    rows,
			},
			{
				Name:        "no-rows",
				Description: "A state with no row. The archive has no row chunk.",
				Entries:     []vectorEntry{manifestVectorEntry(t, empty), proofEntry},
			},
		},
		Invalid: []formatVector{
			invalid("manifest-not-first", "The proof entry comes before the manifest.",
				"the first archive entry is", proofEntry, manifestEntry, chunkEntry),
			invalid("another-format-version", "The manifest has format_version 2.",
				"format_version 2, want 1", edited(func(e *manifest) { e.FormatVersion = 2 }), proofEntry, chunkEntry),
			invalid("unknown-manifest-field", "The manifest has a field that the format does not define.",
				"unknown field", vectorEntry{Name: manifestName, JSON: unknownField}, proofEntry, chunkEntry),
			invalid("empty-chain-id", "The manifest chain_id is empty.",
				"chain_id is empty", edited(func(e *manifest) { e.ChainID = "" }), proofEntry, chunkEntry),
			invalid("zero-state-hash", "The manifest state_hash is all zero.",
				"state_hash is missing", edited(func(e *manifest) { e.StateHash = hexHash{} }), proofEntry, chunkEntry),
			invalid("proof-of-another-height", "The proof entry is named for height 8.",
				fmt.Sprintf("want %q", proofArchivePath(vectorHeight)),
				manifestEntry, fileVectorEntry(proofArchivePath(vectorHeight+1), proof), chunkEntry),
			invalid("row-chunk-index-gap", "The first row chunk has index 1.",
				fmt.Sprintf("want %q", rowChunkPath(0)), manifestEntry, proofEntry, fileVectorEntry(rowChunkPath(1), chunk)),
			invalid("entry-after-the-last-chunk", "An entry that is not a row chunk follows the last row chunk.",
				fmt.Sprintf("want %q", rowChunkPath(1)), manifestEntry, proofEntry, chunkEntry, fileVectorEntry("other/x", []byte("x"))),
			invalid("raw-pebble-file", "A Pebble file takes the place of the row chunk.",
				fmt.Sprintf("want %q", rowChunkPath(0)), manifestEntry, proofEntry,
				fileVectorEntry("flatkv/snapshot-00000000000000000007/account/000001.sst", []byte("sst"))),
			invalid("non-canonical-path", "The row chunk path holds a \".\" element.",
				"is not canonical", manifestEntry, proofEntry, fileVectorEntry("flatkv/./rows/00000000", chunk)),
			invalid("symlink-row-chunk", "The row chunk is a symbolic link.",
				"want a regular file", manifestEntry, proofEntry, vectorEntry{Name: rowChunkPath(0), Symlink: "/etc/passwd"}),
			invalid("zero-key-length", "The first record has key length 0.",
				"key length is 0", manifestEntry, proofEntry, fileVectorEntry(rowChunkPath(0), append([]byte{0x00}, chunk...))),
			invalid("zero-value-length", "The first record has value length 0.",
				"value length is 0", manifestEntry, proofEntry, fileVectorEntry(rowChunkPath(0), zeroValue)),
			invalid("non-minimal-length", "The first key length is 6 encoded in two bytes, 86 00.",
				"not the minimal", manifestEntry, proofEntry, fileVectorEntry(rowChunkPath(0), append([]byte{0x86, 0x00}, chunk[1:]...))),
			invalid("truncated-record", "The row chunk ends one byte before the end of its last record.",
				"exceeds the remaining chunk size", manifestEntry, proofEntry, fileVectorEntry(rowChunkPath(0), chunk[:len(chunk)-1])),
			invalid("keys-not-ascending", "The EVM misc row comes before the bank row.",
				"keys must be in ascending order", manifestEntry, proofEntry, fileVectorEntry(rowChunkPath(0), unordered)),
			invalid("duplicate-key", "The bank row appears twice.",
				"duplicate physical key", duplicateManifest, proofEntry, duplicateChunk),
			invalid("metadata-key", "A row has the key _meta/version.",
				"metadata namespace", metaManifest, proofEntry, metaChunk),
			invalid("key-without-module", "A row key has no \"/\" separator.",
				"missing module prefix separator", noModuleManifest, proofEntry, noModuleChunk),
			invalid("row-count-too-high", "The manifest row_count is one more than the rows.",
				fmt.Sprintf("manifest records %d rows", m.RowCount+1), edited(func(e *manifest) { e.RowCount++ }), proofEntry, chunkEntry),
			invalid("row-count-too-low", "The manifest row_count is one less than the rows.",
				"exceeds manifest", edited(func(e *manifest) { e.RowCount-- }), proofEntry, chunkEntry),
			invalid("state-bytes-mismatch", "The manifest state_bytes is one more than the rows hold.",
				fmt.Sprintf("manifest records %d rows and %d bytes", m.RowCount, m.StateBytes+1),
				edited(func(e *manifest) { e.StateBytes++ }), proofEntry, chunkEntry),
			invalid("state-hash-mismatch", "The manifest state_hash differs from the state hash of the rows in one bit.",
				"the imported checkpoint holds", edited(func(e *manifest) { e.StateHash[0] ^= 1 }), proofEntry, chunkEntry),
		},
	}
}

// vectorManifest returns the manifest of a vector archive that holds rows.
func vectorManifest(t *testing.T, rows []rowKV) *manifest {
	t.Helper()
	m := &manifest{
		FormatVersion:    formatVersion,
		ChainID:          "vector-chain",
		CheckpointHeight: vectorHeight,
		CreatedAt:        time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC),
		ProducerVersion:  "format-vectors",
		RowCount:         int64(len(rows)),
		StateBytes:       rowBytes(rows),
	}
	m.StateHash = vectorStateHash(t, m, rows)
	return m
}

// vectorStateHash returns the state hash of rows, as an import of them computes it.
func vectorStateHash(t *testing.T, m *manifest, rows []rowKV) hexHash {
	t.Helper()
	draft := *m
	draft.StateHash = hexHash{1}
	entries := []vectorEntry{
		manifestVectorEntry(t, &draft),
		fileVectorEntry(proofArchivePath(vectorHeight), []byte("proof")),
	}
	if len(rows) > 0 {
		entries = append(entries, fileVectorEntry(rowChunkPath(0), recordChunk(rows...)))
	}
	checkpoint, _, err := importFormatVector(t, formatVector{Entries: entries})
	require.NoError(t, err)
	stats, err := flatkv.SnapshotDirStats(checkpoint, vectorHeight)
	require.NoError(t, err)
	return hexHash(stats.StateHash)
}

func manifestVectorEntry(t *testing.T, m *manifest) vectorEntry {
	t.Helper()
	data, err := json.Marshal(m)
	require.NoError(t, err)
	return vectorEntry{Name: manifestName, JSON: data}
}

func fileVectorEntry(name string, content []byte) vectorEntry {
	return vectorEntry{Name: name, Hex: hex.EncodeToString(content)}
}

func decodeVectorRows(t *testing.T, rows []vectorRow) []rowKV {
	t.Helper()
	decoded := make([]rowKV, 0, len(rows))
	for _, row := range rows {
		key, err := hex.DecodeString(row.Key)
		require.NoError(t, err)
		value, err := hex.DecodeString(row.Value)
		require.NoError(t, err)
		decoded = append(decoded, rowKV{key: string(key), value: string(value)})
	}
	return decoded
}

func recordChunk(rows ...rowKV) []byte {
	var chunk []byte
	for _, row := range rows {
		chunk = appendRecord(chunk, []byte(row.key), []byte(row.value))
	}
	return chunk
}

func rowBytes(rows []rowKV) int64 {
	var n int64
	for _, row := range rows {
		n += int64(len(row.key) + len(row.value))
	}
	return n
}
