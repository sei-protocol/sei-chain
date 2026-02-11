package genesis

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// SHA-256
var expectedGenesisDigests = map[string]string{
	"arctic-1":   "a8b60161e345d8afb0d9b0c524b6541e6135f5eda4092fae287d8496e14554d8",
	"atlantic-2": "3c135db9177a428893353d7889149ca2ed9c075d6846be07af60354022b81318",
	"pacific-1":  "4304cf1c7f46d153b79f1195b2d334f7f7cf02f26e02a3bb77c544a4987c1432",
}

func TestEmbeddedGenesisDigests(t *testing.T) {
	for _, chainID := range WellKnownChainIDs() {
		t.Run(chainID, func(t *testing.T) {
			data, err := EmbeddedGenesis(chainID)
			require.NoError(t, err)
			digest := sha256.Sum256(data)
			got := hex.EncodeToString(digest[:])
			require.Equal(t, expectedGenesisDigests[chainID], got, "embedded genesis %s.json was modified; update expected digest or restore file", chainID)
		})
	}
}

func TestWellKnownChainIDs(t *testing.T) {
	ids := WellKnownChainIDs()
	require.EqualValues(t, wellKnownChainIDs, ids)
}

func TestEmbeddedGenesisDoc(t *testing.T) {
	for _, chainID := range WellKnownChainIDs() {
		genDoc, err := EmbeddedGenesisDoc(chainID)
		require.NoError(t, err)
		require.NotNil(t, genDoc)
		require.Equal(t, chainID, genDoc.ChainID)
	}
}

func TestEmbeddedGenesisUnknownChain(t *testing.T) {
	_, err := EmbeddedGenesis("unknown")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown chain-id")

	_, err = EmbeddedGenesisDoc("unknown")
	require.Error(t, err)
}
