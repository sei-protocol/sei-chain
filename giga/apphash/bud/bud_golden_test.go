package bud

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordBUDGolden rewrites the current versions' golden file from this build instead of verifying against it.
// Refused on CI: see writeBUDGoldenFile().
var recordBUDGolden = flag.Bool("bud-golden-record", false,
	"rewrite the current versions' BUD golden file from this build instead of verifying against it")

// budGoldenCounts are the budlet counts of the recorded blocks, in record order.
var budGoldenCounts = []int{0, 1, 2, 3, 5, 8}

const (
	// budGoldenSeed drives the random budlets of the recorded blocks.
	budGoldenSeed = 0x0b0d_600d_5eed

	// budGoldenDir holds one golden file per combination of BUD, BUD proof, and BUD state proof versions.
	budGoldenDir = "testdata/golden"
)

// budGoldenFile is the contents of one golden file.
type budGoldenFile struct {
	// The BUD version every record in this file was produced under.
	BUDVersion uint8 `json:"budVersion"`

	// The BUD proof version every record in this file was produced under.
	BUDProofVersion uint8 `json:"budProofVersion"`

	// The BUD state proof version every state proof in this file was produced under.
	BUDStateProofVersion uint8 `json:"budStateProofVersion"`

	// The recorded blocks.
	Records []budGoldenRecord `json:"records"`

	// Serialized BUD state proofs, each over one or two recorded writes of a key.
	StateProofs []budGoldenStateProof `json:"stateProofs"`
}

// budGoldenStateProof is one recorded BUD state proof and the answers it gives. Byte fields are lowercase hex.
type budGoldenStateProof struct {
	// The serialized BUD state proof.
	Proof string `json:"proof"`

	// The key the proof is about.
	Key string `json:"key"`

	// The EVM chain ID of the proof's blocks, as a decimal string.
	ChainID string `json:"chainID"`

	// The lowest block height the proof covers, as a decimal string.
	StartHeight string `json:"startHeight"`

	// The highest block height the proof covers, as a decimal string.
	EndHeight string `json:"endHeight"`

	// The app hash of each of the proof's blocks, in height order.
	AppHashes []string `json:"appHashes"`

	// The value the proof gives at the start height and at every height below the end height.
	StartValue budGoldenValue `json:"startValue"`

	// The value the proof gives at the end height.
	EndValue budGoldenValue `json:"endValue"`
}

// budGoldenValue is one recorded value of a key. Byte fields are lowercase hex.
type budGoldenValue struct {
	// The value's bytes, empty for a deletion.
	Value string `json:"value"`

	// Whether the value is a deletion.
	Deleted bool `json:"deleted"`
}

// budGoldenRecord is one recorded block: its budlets, its serialized BUD tree, its BUD, and the BUD proof of each
// budlet. Byte fields are lowercase hex.
type budGoldenRecord struct {
	// The block's budlets, sorted by key.
	Budlets []budGoldenBudlet `json:"budlets"`

	// The block's serialized BUD tree.
	Tree string `json:"tree"`

	// The block's BUD.
	BUD string `json:"bud"`

	// The serialized BUD proof of each budlet, in budlet order.
	Proofs []string `json:"proofs"`
}

// budGoldenBudlet is one recorded budlet. Byte fields are lowercase hex.
type budGoldenBudlet struct {
	// The budlet's key.
	Key string `json:"key"`

	// The budlet's value.
	Value string `json:"value"`

	// Whether the budlet is a deletion.
	Deleted bool `json:"deleted"`

	// The budlet's previous height, as a decimal string so readers without exact 64-bit JSON numbers keep every
	// digit.
	PreviousHeight string `json:"previousHeight"`
}

// TestBUDGolden requires every committed golden file to verify against this build.
func TestBUDGolden(t *testing.T) {
	if *recordBUDGolden {
		recordBUDGoldenFile(t)
		return
	}

	paths, err := filepath.Glob(filepath.Join(budGoldenDir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no golden files in %s", budGoldenDir)

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			file := readBUDGoldenFile(t, path)
			versionsPath := budGoldenFilePath(
				file.BUDVersion, file.BUDProofVersion, file.BUDStateProofVersion)
			require.Equal(t, versionsPath, path,
				"golden file names different versions")
			current := file.BUDVersion == budVersion && file.BUDProofVersion == budProofVersion &&
				file.BUDStateProofVersion == budStateProofVersion
			require.NotEmpty(t, file.Records)

			for i, record := range file.Records {
				verifyBUDGoldenRecord(t, i, current, record)
			}
			require.NotEmpty(t, file.StateProofs)
			for i, stateProof := range file.StateProofs {
				verifyBUDGoldenStateProof(t, i, current, stateProof)
			}
		})
	}
}

// TestCurrentVersionsHaveGoldenFile requires a golden file for the BUD, BUD proof, and BUD state proof versions this
// build produces.
func TestCurrentVersionsHaveGoldenFile(t *testing.T) {
	path := budGoldenFilePath(budVersion, budProofVersion, budStateProofVersion)
	_, err := os.Stat(path)
	require.NoError(t, err, "no golden file for the current versions; record %s with -bud-golden-record", path)
}

// verifyBUDGoldenRecord requires the recorded BUD tree and BUD proofs to deserialize to the recorded budlets and
// compute the recorded BUD. A record of the current versions must also be reproduced by NewBUDTree() and
// BuildBUDProof().
func verifyBUDGoldenRecord(
	t *testing.T,
	// The position of the record in its golden file.
	index int,
	// Whether the record's golden file is of the versions this build produces.
	current bool,
	// The record to verify.
	record budGoldenRecord,
) {
	t.Helper()

	budlets := make([]*Budlet, 0, len(record.Budlets))
	for i, recorded := range record.Budlets {
		budlets = append(budlets, decodeBUDGoldenBudlet(t, index, i, recorded))
	}

	serializedTree, err := hex.DecodeString(record.Tree)
	require.NoError(t, err, "record %d tree", index)
	deserializedTree, err := DeserializeBUDTree(serializedTree)
	require.NoError(t, err, "record %d tree", index)
	require.Equal(t, budlets, deserializedTree.Budlets(), "record %d deserialized tree", index)
	bud := deserializedTree.BUD()
	require.Equal(t, record.BUD, hex.EncodeToString(bud[:]), "record %d BUD", index)

	require.Len(t, record.Proofs, len(budlets), "record %d", index)
	for i, budlet := range budlets {
		serialized, err := hex.DecodeString(record.Proofs[i])
		require.NoError(t, err, "record %d budlet %d", index, i)
		deserialized, err := DeserializeBUDProof(serialized)
		require.NoError(t, err, "record %d budlet %d", index, i)
		require.Equal(t, budlet, deserialized.Budlet(), "record %d budlet %d", index, i)
		require.Equal(t, bud, deserialized.ComputeBUD(), "record %d budlet %d", index, i)
	}

	if !current {
		return
	}
	tree, err := NewBUDTree(budlets)
	require.NoError(t, err, "record %d", index)
	require.Equal(t, record.Tree, hex.EncodeToString(tree.Serialize()), "record %d tree", index)
	require.Equal(t, bud, tree.BUD(), "record %d BUD", index)
	for i, budlet := range budlets {
		proof, found := tree.BuildBUDProof(budlet.Key())
		require.True(t, found, "record %d budlet %d", index, i)
		require.Equal(t, record.Proofs[i], hex.EncodeToString(proof.Serialize()),
			"record %d budlet %d", index, i)
	}
}

// verifyBUDGoldenStateProof requires a recorded BUD state proof to deserialize and give the recorded key, chain ID,
// heights, app hashes, and values. A state proof of the current versions must also serialize back to the recorded
// bytes.
func verifyBUDGoldenStateProof(
	t *testing.T,
	// The position of the state proof in its golden file.
	index int,
	// Whether the state proof's golden file is of the versions this build produces.
	current bool,
	// The recorded state proof.
	recorded budGoldenStateProof,
) {
	t.Helper()

	serialized, err := hex.DecodeString(recorded.Proof)
	require.NoError(t, err, "state proof %d", index)
	stateProof, err := DeserializeBUDStateProof(serialized)
	require.NoError(t, err, "state proof %d", index)
	if current {
		require.Equal(t, recorded.Proof, hex.EncodeToString(stateProof.Serialize()), "state proof %d", index)
	}

	require.Equal(t, recorded.Key, hex.EncodeToString(stateProof.Key()), "state proof %d key", index)
	require.Equal(t, recorded.ChainID, strconv.FormatUint(stateProof.ChainID(), 10), "state proof %d", index)
	require.Equal(t, recorded.StartHeight, strconv.FormatUint(stateProof.StartHeight(), 10),
		"state proof %d", index)
	require.Equal(t, recorded.EndHeight, strconv.FormatUint(stateProof.EndHeight(), 10), "state proof %d", index)
	appHashes := make([]string, 0, len(stateProof.AppHashes()))
	for _, appHash := range stateProof.AppHashes() {
		appHashes = append(appHashes, hex.EncodeToString(appHash[:]))
	}
	require.Equal(t, recorded.AppHashes, appHashes, "state proof %d app hashes", index)

	startValue := decodeBUDGoldenValue(t, recorded.StartValue)
	endValue := decodeBUDGoldenValue(t, recorded.EndValue)
	requireBUDGoldenValueAt(t, stateProof, stateProof.StartHeight(), startValue)
	for height := stateProof.StartHeight(); height < stateProof.EndHeight(); height++ {
		requireBUDGoldenValueAt(t, stateProof, height, startValue)
	}
	requireBUDGoldenValueAt(t, stateProof, stateProof.EndHeight(), endValue)
	if stateProof.StartHeight() > 0 {
		_, covered := stateProof.ValueAt(stateProof.StartHeight() - 1)
		require.False(t, covered, "state proof %d below its start height", index)
	}
	if stateProof.EndHeight() < math.MaxUint64 {
		_, covered := stateProof.ValueAt(stateProof.EndHeight() + 1)
		require.False(t, covered, "state proof %d above its end height", index)
	}
}

// requireBUDGoldenValueAt requires a BUD state proof to give want at height, telling a deletion apart from a write
// of the empty value.
func requireBUDGoldenValueAt(
	t *testing.T,
	// The state proof to ask.
	stateProof *BUDStateProof,
	// The height to ask about.
	height uint64,
	// The value the state proof must give, or nil for a deletion.
	want []byte,
) {
	t.Helper()

	value, covered := stateProof.ValueAt(height)
	require.True(t, covered, "height %d", height)
	require.Equal(t, want == nil, value == nil, "height %d deletion", height)
	require.Equal(t, want, value, "height %d", height)
}

// decodeBUDGoldenValue decodes one recorded value, returning nil for a deletion and a non-nil slice for any write,
// including a write of the empty value.
func decodeBUDGoldenValue(t *testing.T, recorded budGoldenValue) []byte {
	t.Helper()

	value, err := hex.DecodeString(recorded.Value)
	require.NoError(t, err, "value %q", recorded.Value)
	switch {
	case recorded.Deleted:
		require.Empty(t, value, "deletion value")
		return nil
	case value == nil:
		// A recorded write of the empty value must not become a deletion.
		return []byte{}
	}
	return value
}

// newBUDGoldenValue returns the golden record of a value, nil for a deletion.
func newBUDGoldenValue(value []byte) budGoldenValue {
	return budGoldenValue{Value: hex.EncodeToString(value), Deleted: value == nil}
}

// newBUDGoldenStateProof returns the golden record of a BUD state proof and the answers it gives.
func newBUDGoldenStateProof(t *testing.T, stateProof *BUDStateProof) budGoldenStateProof {
	t.Helper()

	startValue, covered := stateProof.ValueAt(stateProof.StartHeight())
	require.True(t, covered)
	endValue, covered := stateProof.ValueAt(stateProof.EndHeight())
	require.True(t, covered)
	recorded := budGoldenStateProof{
		Proof:       hex.EncodeToString(stateProof.Serialize()),
		Key:         hex.EncodeToString(stateProof.Key()),
		ChainID:     strconv.FormatUint(stateProof.ChainID(), 10),
		StartHeight: strconv.FormatUint(stateProof.StartHeight(), 10),
		EndHeight:   strconv.FormatUint(stateProof.EndHeight(), 10),
		AppHashes:   make([]string, 0, len(stateProof.AppHashes())),
		StartValue:  newBUDGoldenValue(startValue),
		EndValue:    newBUDGoldenValue(endValue),
	}
	for _, appHash := range stateProof.AppHashes() {
		recorded.AppHashes = append(recorded.AppHashes, hex.EncodeToString(appHash[:]))
	}
	return recorded
}

// decodeBUDGoldenBudlet decodes one recorded budlet, failing the test if a field does not parse.
func decodeBUDGoldenBudlet(t *testing.T, recordIndex int, budletIndex int, recorded budGoldenBudlet) *Budlet {
	t.Helper()

	key, err := hex.DecodeString(recorded.Key)
	require.NoError(t, err, "record %d budlet %d key", recordIndex, budletIndex)
	value := decodeBUDGoldenValue(t, budGoldenValue{Value: recorded.Value, Deleted: recorded.Deleted})
	previousHeight, err := strconv.ParseUint(recorded.PreviousHeight, 10, 64)
	require.NoError(t, err, "record %d budlet %d previous height", recordIndex, budletIndex)

	budlet, err := NewBudlet(key, value, previousHeight)
	require.NoError(t, err, "record %d budlet %d", recordIndex, budletIndex)
	return budlet
}

// budGoldenFilePath returns the path of the golden file for a combination of BUD, BUD proof, and BUD state proof
// versions.
func budGoldenFilePath(
	// The BUD version of the golden file.
	budVersion uint8,
	// The BUD proof version of the golden file.
	budProofVersion uint8,
	// The BUD state proof version of the golden file.
	budStateProofVersion uint8,
) string {
	name := fmt.Sprintf("bud-v%d-proof-v%d-state-proof-v%d.json", budVersion, budProofVersion, budStateProofVersion)
	return filepath.Join(budGoldenDir, name)
}

// readBUDGoldenFile parses a committed golden file.
func readBUDGoldenFile(t *testing.T, path string) budGoldenFile {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	var file budGoldenFile
	require.NoError(t, json.Unmarshal(content, &file), "parsing %s", path)
	return file
}

// recordBUDGoldenFile rewrites the current versions' golden file from seeded random blocks.
func recordBUDGoldenFile(t *testing.T) {
	t.Helper()

	rng := rand.New(rand.NewSource(budGoldenSeed))
	file := budGoldenFile{
		BUDVersion:           budVersion,
		BUDProofVersion:      budProofVersion,
		BUDStateProofVersion: budStateProofVersion,
		Records:              make([]budGoldenRecord, 0, len(budGoldenCounts)),
	}
	for _, count := range budGoldenCounts {
		file.Records = append(file.Records, newBUDGoldenRecord(t, randomBudlets(t, rng, count)))
	}
	writes := newTestConsecutiveWrites(t, rng, []byte("second value"))
	single, err := NewBUDStateProof(writes.appHashData[:1], writes.budProofs[:1])
	require.NoError(t, err)
	pair, err := NewBUDStateProof(writes.appHashData, writes.budProofs)
	require.NoError(t, err)
	file.StateProofs = []budGoldenStateProof{newBUDGoldenStateProof(t, single), newBUDGoldenStateProof(t, pair)}
	writeBUDGoldenFile(t, budGoldenFilePath(budVersion, budProofVersion, budStateProofVersion), file)
}

// newBUDGoldenRecord returns the golden record of one block's budlets.
func newBUDGoldenRecord(t *testing.T, budlets []*Budlet) budGoldenRecord {
	t.Helper()

	tree, err := NewBUDTree(budlets)
	require.NoError(t, err)
	bud := tree.BUD()
	record := budGoldenRecord{
		Budlets: make([]budGoldenBudlet, 0, len(budlets)),
		Tree:    hex.EncodeToString(tree.Serialize()),
		BUD:     hex.EncodeToString(bud[:]),
		Proofs:  make([]string, 0, len(budlets)),
	}
	for _, budlet := range budlets {
		record.Budlets = append(record.Budlets, budGoldenBudlet{
			Key:            hex.EncodeToString(budlet.Key()),
			Value:          hex.EncodeToString(budlet.Value()),
			Deleted:        budlet.Value() == nil,
			PreviousHeight: strconv.FormatUint(budlet.PreviousHeight(), 10),
		})
		proof, found := tree.BuildBUDProof(budlet.Key())
		require.True(t, found)
		record.Proofs = append(record.Proofs, hex.EncodeToString(proof.Serialize()))
	}
	return record
}

// writeBUDGoldenFile writes a golden file. It refuses to run on CI.
func writeBUDGoldenFile(t *testing.T, path string, file budGoldenFile) {
	t.Helper()

	if os.Getenv("CI") != "" {
		t.Fatal("refusing to rewrite a BUD golden file on CI: " +
			"the recorded values are the expected values, so a build that rewrites them verifies nothing")
	}
	content, err := json.MarshalIndent(file, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, append(content, '\n'), 0o644))
	t.Logf("recorded %d records into %s; review the diff before committing", len(file.Records), path)
}
