package apphash

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

// recordGolden rewrites the current version's golden file from this build instead of verifying against it. Off by
// default, and refused on CI: see recordGoldenFile().
var recordGolden = flag.Bool("apphash-golden-record", false,
	"rewrite the current version's app hash golden file from this build instead of verifying against it")

const (
	// goldenSeed drives the random workflow that builds the golden chain.
	goldenSeed = 0x0a99_4a54_600d_5eed

	// goldenChainLength is the number of chained records in the golden chain.
	goldenChainLength = 8

	// goldenDir holds one golden file per schema version, named v<version>.json.
	goldenDir = "testdata/golden"
)

// goldenFile is the contents of one version's golden file.
type goldenFile struct {
	// The schema version every record in this file was produced under.
	Version uint8 `json:"version"`

	// The recorded app hash data, in chain order.
	Records []goldenRecord `json:"records"`
}

// goldenRecord is one recorded app hash: its inputs, its serialization, and its hash. Byte fields are lowercase hex.
type goldenRecord struct {
	// The EVM chain ID, as a decimal string so readers without exact 64-bit JSON numbers keep every digit.
	ChainID string `json:"chainID"`

	// The block height, as a decimal string so readers without exact 64-bit JSON numbers keep every digit.
	BlockHeight string `json:"blockHeight"`

	// The hash of the block.
	BlockHash string `json:"blockHash"`

	// The hash of the state lattice hash.
	StateHash string `json:"stateHash"`

	// The Block Update Digest.
	BUD string `json:"bud"`

	// The hash of the transaction receipts.
	ReceiptHash string `json:"receiptHash"`

	// The app hash of the previous block.
	PreviousAppHash string `json:"previousAppHash"`

	// The canonical serialization of the record.
	Serialization string `json:"serialization"`

	// The app hash of the record.
	AppHash string `json:"appHash"`
}

// TestAppHashGolden requires every committed golden file, of every schema version, to verify against this build.
func TestAppHashGolden(t *testing.T) {
	if *recordGolden {
		recordGoldenFile(t)
		return
	}

	paths, err := filepath.Glob(filepath.Join(goldenDir, "v*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no golden files in %s", goldenDir)

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			file := readGoldenFile(t, path)
			require.Equal(t, goldenFilePath(file.Version), path, "golden file names a different version")
			require.NotEmpty(t, file.Records)

			for i, record := range file.Records {
				verifyGoldenRecord(t, i, file.Version, record)
			}
		})
	}
}

// TestCurrentVersionHasGoldenFile requires a golden file for the schema version this build produces.
func TestCurrentVersionHasGoldenFile(t *testing.T) {
	path := goldenFilePath(appHashVersion)
	_, err := os.Stat(path)
	require.NoError(t, err, "no golden file for app hash version %d; record %s with -apphash-golden-record",
		appHashVersion, path)
}

// verifyGoldenRecord requires record to deserialize to its recorded inputs and hash, and to serialize back to its
// recorded bytes. A record of the current version must also be reproduced by NewAppHashData().
func verifyGoldenRecord(t *testing.T, index int, version uint8, record goldenRecord) {
	t.Helper()

	chainID, err := strconv.ParseUint(record.ChainID, 10, 64)
	require.NoError(t, err, "record %d chainID", index)
	blockHeight, err := strconv.ParseUint(record.BlockHeight, 10, 64)
	require.NoError(t, err, "record %d blockHeight", index)
	blockHash := decodeGoldenHash(t, index, "blockHash", record.BlockHash)
	stateHash := decodeGoldenHash(t, index, "stateHash", record.StateHash)
	bud := decodeGoldenHash(t, index, "bud", record.BUD)
	receiptHash := decodeGoldenHash(t, index, "receiptHash", record.ReceiptHash)
	previousAppHash := decodeGoldenHash(t, index, "previousAppHash", record.PreviousAppHash)
	appHash := decodeGoldenHash(t, index, "appHash", record.AppHash)
	serialization, err := hex.DecodeString(record.Serialization)
	require.NoError(t, err, "record %d serialization", index)

	decoded, err := Deserialize(serialization)
	require.NoError(t, err, "record %d", index)
	require.Equal(t, version, decoded.Version(), "record %d", index)
	require.Equal(t, chainID, decoded.ChainID(), "record %d", index)
	require.Equal(t, blockHeight, decoded.BlockHeight(), "record %d", index)
	require.Equal(t, blockHash, decoded.BlockHash(), "record %d", index)
	require.Equal(t, stateHash, decoded.StateHash(), "record %d", index)
	require.Equal(t, bud, decoded.BUD(), "record %d", index)
	require.Equal(t, receiptHash, decoded.ReceiptHash(), "record %d", index)
	require.Equal(t, previousAppHash, decoded.PreviousAppHash(), "record %d", index)
	require.Equal(t, serialization, decoded.Serialize(), "record %d", index)
	require.Equal(t, appHash, decoded.AppHash(), "record %d", index)

	if version == appHashVersion {
		constructed := NewAppHashData(
			chainID, blockHeight, blockHash, stateHash, bud, receiptHash, previousAppHash)
		require.Equal(t, serialization, constructed.Serialize(), "record %d", index)
		require.Equal(t, appHash, constructed.AppHash(), "record %d", index)
	}
}

// decodeGoldenHash decodes a recorded hex hash, failing the test unless it is exactly 32 bytes.
func decodeGoldenHash(t *testing.T, index int, field string, encoded string) [32]byte {
	t.Helper()

	decoded, err := hex.DecodeString(encoded)
	require.NoError(t, err, "record %d %s", index, field)
	require.Len(t, decoded, 32, "record %d %s", index, field)
	return [32]byte(decoded)
}

// goldenFilePath returns the path of the golden file for a schema version.
func goldenFilePath(version uint8) string {
	return filepath.Join(goldenDir, fmt.Sprintf("v%d.json", version))
}

// readGoldenFile parses a committed golden file.
func readGoldenFile(t *testing.T, path string) goldenFile {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	var file goldenFile
	require.NoError(t, json.Unmarshal(content, &file), "parsing %s", path)
	return file
}

// recordGoldenFile rewrites the current version's golden file from the golden chain. It refuses to run on CI.
func recordGoldenFile(t *testing.T) {
	t.Helper()

	if os.Getenv("CI") != "" {
		t.Fatal("refusing to re-record the app hash golden file on CI: " +
			"the recorded values are the expected values, so a build that rewrites them verifies nothing")
	}

	chain := buildGoldenChain()
	file := goldenFile{Version: appHashVersion, Records: make([]goldenRecord, 0, len(chain))}
	for _, ahd := range chain {
		file.Records = append(file.Records, newGoldenRecord(ahd))
	}
	content, err := json.MarshalIndent(file, "", "  ")
	require.NoError(t, err)

	path := goldenFilePath(appHashVersion)
	require.NoError(t, os.MkdirAll(goldenDir, 0o755))
	require.NoError(t, os.WriteFile(path, append(content, '\n'), 0o644))
	t.Logf("recorded %d records into %s; review the diff before committing", len(chain), path)
}

// buildGoldenChain returns goldenChainLength records of one random chain ID at consecutive heights with random
// hashes, each chained to the one before it through previousAppHash. The first record's previousAppHash is all
// zeros.
func buildGoldenChain() []*AppHashData {
	rng := rand.New(rand.NewSource(goldenSeed))
	randomHash := func() [32]byte {
		var h [32]byte
		_, _ = rng.Read(h[:])
		return h
	}

	chainID := rng.Uint64()
	startHeight := rng.Uint64() >> 1
	chain := make([]*AppHashData, 0, goldenChainLength)
	var previousAppHash [32]byte
	for i := uint64(0); i < goldenChainLength; i++ {
		blockHash := randomHash()
		stateHash := randomHash()
		bud := randomHash()
		receiptHash := randomHash()

		ahd := NewAppHashData(chainID, startHeight+i, blockHash, stateHash, bud, receiptHash, previousAppHash)
		chain = append(chain, ahd)
		previousAppHash = ahd.AppHash()
	}
	return chain
}

// newGoldenRecord returns the golden record of ahd.
func newGoldenRecord(ahd *AppHashData) goldenRecord {
	blockHash := ahd.BlockHash()
	stateHash := ahd.StateHash()
	bud := ahd.BUD()
	receiptHash := ahd.ReceiptHash()
	previousAppHash := ahd.PreviousAppHash()
	appHash := ahd.AppHash()
	return goldenRecord{
		ChainID:         strconv.FormatUint(ahd.ChainID(), 10),
		BlockHeight:     strconv.FormatUint(ahd.BlockHeight(), 10),
		BlockHash:       hex.EncodeToString(blockHash[:]),
		StateHash:       hex.EncodeToString(stateHash[:]),
		BUD:             hex.EncodeToString(bud[:]),
		ReceiptHash:     hex.EncodeToString(receiptHash[:]),
		PreviousAppHash: hex.EncodeToString(previousAppHash[:]),
		Serialization:   hex.EncodeToString(ahd.Serialize()),
		AppHash:         hex.EncodeToString(appHash[:]),
	}
}
