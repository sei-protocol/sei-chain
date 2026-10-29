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
	"arctic-1":   "de878e00262cf11eae35bdcc19787448fd0163db4512b57c497489e7eb065d7a",
	"atlantic-2": "f779c564fe250ac2a53003b24a6fb00f61275cdfe6cfbd3de0b55fd831552fe5",
	"pacific-1":  "c0064d0a6b131e80d546e407161b699ac94e2cb8ce99322431ae8b4cdc4fba65",
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
