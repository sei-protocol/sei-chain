package genesis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// SHA-256(genDoc)
var expectedGenesisDigests = map[string]string{
	"arctic-1":   "4ffad17abbd57a460f9228e07d7ada77255de1be92037f790f8816236a26a3a3",
	"atlantic-2": "15c0270b25f080851e8b5dcb81fab8582c4972c69a2ec5b48af0a37b1004c5a2",
	"pacific-1":  "2e5d7f8f16ec829073e3a6eadf4825f7dc56c2265b4dd2d05e1cf73b9fd13c0e",
}

func genesisDocDigest(chainID string) ([]byte, error) {
	genDoc, err := EmbeddedGenesisDoc(chainID)
	if err != nil {
		return nil, err
	}
	ser, err := json.Marshal(genDoc)
	if err != nil {
		return nil, fmt.Errorf("marshaling genesis doc: %w", err)
	}
	hash := sha256.Sum256(ser)
	return hash[:], nil
}

func TestPrintGenesisDigestsForUpdate(t *testing.T) {
	if os.Getenv("UPDATE_GENESIS_DIGESTS") == "" {
		t.Skip("run with UPDATE_GENESIS_DIGESTS=1 to print expected digests after adding or modifying chains/*.json")
	}
	ids, err := WellKnownChainIDs()
	require.NoError(t, err)
	fmt.Println("// Copy the following into expectedGenesisDigests:")
	fmt.Println("var expectedGenesisDigests = map[string]string{")
	for _, chainID := range ids {
		digest, err := genesisDocDigest(chainID)
		require.NoError(t, err)
		fmt.Printf("\t%q: %q,\n", chainID, hex.EncodeToString(digest))
	}
	fmt.Println("}")
}

func TestEmbeddedGenesisDigests(t *testing.T) {
	ids, err := WellKnownChainIDs()
	require.NoError(t, err)
	for _, chainID := range ids {
		t.Run(chainID, func(t *testing.T) {
			digest, err := genesisDocDigest(chainID)
			require.NoError(t, err)
			got := hex.EncodeToString(digest)
			require.Equal(t, expectedGenesisDigests[chainID], got, "embedded genesis %s.json was modified; update expected digest or restore file", chainID)
		})
	}
}

func TestWellKnownChainIDs(t *testing.T) {
	ids, err := WellKnownChainIDs()
	require.NoError(t, err)
	for chainID := range expectedGenesisDigests {
		require.Contains(t, ids, chainID)
	}
}

func TestEmbeddedGenesisDoc(t *testing.T) {
	ids, err := WellKnownChainIDs()
	require.NoError(t, err)
	for _, chainID := range ids {
		genDoc, err := EmbeddedGenesisDoc(chainID)
		require.NoError(t, err)
		require.NotNil(t, genDoc)
		require.Equal(t, chainID, genDoc.ChainID)
	}
}

func TestEmbeddedGenesisUnknownChain(t *testing.T) {
	_, err := EmbeddedGenesis("unknown")
	require.ErrorContains(t, err, "unknown chain-id")

	_, err = EmbeddedGenesisDoc("unknown")
	require.Error(t, err)
}
