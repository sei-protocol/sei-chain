package bud

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
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
	StateProofs []string `json:"stateProofs"`
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
	require.Contains(t, paths, budGoldenFilePath(budVersion, budProofVersion, budStateProofVersion),
		"no golden file for the current versions")

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			file := readBUDGoldenFile(t, path)
			versionsPath := budGoldenFilePath(
				file.BUDVersion, file.BUDProofVersion, file.BUDStateProofVersion)
			require.Equal(t, versionsPath, path,
				"golden file names different versions")
			require.Equal(t, budVersion, file.BUDVersion, "this build does not produce that BUD version")
			require.Equal(t, budProofVersion, file.BUDProofVersion,
				"this build does not produce that BUD proof version")
			require.Equal(t, budStateProofVersion, file.BUDStateProofVersion,
				"this build does not produce that BUD state proof version")
			require.NotEmpty(t, file.Records)

			for i, record := range file.Records {
				verifyBUDGoldenRecord(t, i, record)
			}
			require.NotEmpty(t, file.StateProofs)
			for i, stateProof := range file.StateProofs {
				verifyBUDGoldenStateProof(t, i, stateProof)
			}
		})
	}
}

// verifyBUDGoldenRecord requires this build to reproduce the recorded BUD and BUD proofs, and every recorded BUD
// proof to verify.
func verifyBUDGoldenRecord(t *testing.T, index int, record budGoldenRecord) {
	t.Helper()

	budlets := make([]*Budlet, 0, len(record.Budlets))
	for i, recorded := range record.Budlets {
		budlets = append(budlets, decodeBUDGoldenBudlet(t, index, i, recorded))
	}

	tree, err := NewBUDTree(budlets)
	require.NoError(t, err, "record %d", index)
	require.Equal(t, record.Tree, hex.EncodeToString(tree.Serialize()), "record %d tree", index)
	bud := tree.BUD()
	require.Equal(t, record.BUD, hex.EncodeToString(bud[:]), "record %d BUD", index)

	serializedTree, err := hex.DecodeString(record.Tree)
	require.NoError(t, err, "record %d tree", index)
	deserializedTree, err := DeserializeBUDTree(serializedTree)
	require.NoError(t, err, "record %d tree", index)
	require.Equal(t, bud, deserializedTree.BUD(), "record %d deserialized tree", index)

	require.Len(t, record.Proofs, len(budlets), "record %d", index)
	for i, budlet := range budlets {
		proof, found := tree.BuildBUDProof(budlet.Key())
		require.True(t, found, "record %d budlet %d", index, i)
		require.Equal(t, record.Proofs[i], hex.EncodeToString(proof.Serialize()),
			"record %d budlet %d", index, i)

		serialized, err := hex.DecodeString(record.Proofs[i])
		require.NoError(t, err, "record %d budlet %d", index, i)
		deserialized, err := DeserializeBUDProof(serialized)
		require.NoError(t, err, "record %d budlet %d", index, i)
		require.Equal(t, budlet, deserialized.Budlet(), "record %d budlet %d", index, i)
		require.Equal(t, bud, deserialized.ComputeBUD(), "record %d budlet %d", index, i)
	}
}

// verifyBUDGoldenStateProof requires a recorded BUD state proof to deserialize and reserialize to the same bytes.
func verifyBUDGoldenStateProof(t *testing.T, index int, recorded string) {
	t.Helper()

	serialized, err := hex.DecodeString(recorded)
	require.NoError(t, err, "state proof %d", index)
	stateProof, err := DeserializeBUDStateProof(serialized)
	require.NoError(t, err, "state proof %d", index)
	require.Equal(t, recorded, hex.EncodeToString(stateProof.Serialize()), "state proof %d", index)
}

// decodeBUDGoldenBudlet decodes one recorded budlet, failing the test if a field does not parse.
func decodeBUDGoldenBudlet(t *testing.T, recordIndex int, budletIndex int, recorded budGoldenBudlet) *Budlet {
	t.Helper()

	key, err := hex.DecodeString(recorded.Key)
	require.NoError(t, err, "record %d budlet %d key", recordIndex, budletIndex)
	value, err := hex.DecodeString(recorded.Value)
	require.NoError(t, err, "record %d budlet %d value", recordIndex, budletIndex)
	previousHeight, err := strconv.ParseUint(recorded.PreviousHeight, 10, 64)
	require.NoError(t, err, "record %d budlet %d previous height", recordIndex, budletIndex)

	switch {
	case recorded.Deleted:
		require.Empty(t, value, "record %d budlet %d deletion value", recordIndex, budletIndex)
		value = nil
	case value == nil:
		// A recorded write of the empty value must not become a deletion.
		value = []byte{}
	}
	budlet, err := NewBudlet(key, value, previousHeight)
	require.NoError(t, err, "record %d budlet %d", recordIndex, budletIndex)
	return budlet
}

// budGoldenFilePath returns the path of the golden file for a combination of BUD, BUD proof, and BUD state proof
// versions.
func budGoldenFilePath(budVersion uint8, budProofVersion uint8, budStateProofVersion uint8) string {
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
	file.StateProofs = []string{hex.EncodeToString(single.Serialize()), hex.EncodeToString(pair.Serialize())}
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
