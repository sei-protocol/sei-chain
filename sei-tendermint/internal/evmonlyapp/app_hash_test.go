package evmonlyapp

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

var (
	testCursorPrevious  = common.HexToHash("0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	testCursorBlockHash = common.HexToHash("0xf0e0d0c0b0a090807060504030201000ffeeddccbbaa99887766554433221100")
)

// TestHashEVMOnlyResultGoldenVector pins the cursor hash, which is committed with every block's state.
func TestHashEVMOnlyResultGoldenVector(t *testing.T) {
	got := hashEVMOnlyResult(testCursorPrevious, 4_321, testCursorBlockHash, 21_000)
	require.Equal(t, "0x59659fa84f55ae1ffa25b0695d9e90a86b0f99721183a5507bd41194222dc789", got.Hex())
}

func TestHashEVMOnlyResultDependsOnEveryInput(t *testing.T) {
	base := hashEVMOnlyResult(testCursorPrevious, 4_321, testCursorBlockHash, 21_000)
	for _, other := range []common.Hash{
		hashEVMOnlyResult(common.Hash{}, 4_321, testCursorBlockHash, 21_000),
		hashEVMOnlyResult(testCursorPrevious, 4_322, testCursorBlockHash, 21_000),
		hashEVMOnlyResult(testCursorPrevious, 4_321, common.Hash{}, 21_000),
		hashEVMOnlyResult(testCursorPrevious, 4_321, testCursorBlockHash, 21_001),
	} {
		require.NotEqual(t, base, other)
	}
}
