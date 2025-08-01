package types_test

import (
	"sort"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestEVMAddressToSeiAddressKey(t *testing.T) {
	evmAddr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	expectedPrefix := types.EVMAddressToSeiAddressKeyPrefix
	key := types.EVMAddressToSeiAddressKey(evmAddr)

	require.Equal(t, expectedPrefix[0], key[0], "Key prefix for evm address to sei address key is incorrect")
	require.Equal(t, append(expectedPrefix, evmAddr.Bytes()...), key, "Generated key format is incorrect")
}

func TestSeiAddressToEVMAddressKey(t *testing.T) {
	seiAddr := sdk.AccAddress("sei1234567890abcdef1234567890abcdef12345678")
	expectedPrefix := types.SeiAddressToEVMAddressKeyPrefix
	key := types.SeiAddressToEVMAddressKey(seiAddr)

	require.Equal(t, expectedPrefix[0], key[0], "Key prefix for sei address to evm address key is incorrect")
	require.Equal(t, append(expectedPrefix, seiAddr...), key, "Generated key format is incorrect")
}

func TestStateKey(t *testing.T) {
	evmAddr := common.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	expectedPrefix := types.StateKeyPrefix
	key := types.StateKey(evmAddr)

	require.Equal(t, expectedPrefix[0], key[0], "Key prefix for state key is incorrect")
	require.Equal(t, append(expectedPrefix, evmAddr.Bytes()...), key, "Generated key format is incorrect")
}

func TestBlockBloomKey(t *testing.T) {
	height := int64(123456)
	key := types.BlockBloomKey(height)

	require.Equal(t, types.BlockBloomPrefix[0], key[0], "Key prefix for block bloom key is incorrect")
}

func TestTransientReceiptKeyTransactionHashExtraction(t *testing.T) {
	trk := types.NewTransientReceiptKey(10, common.HexToHash("0x1"))
	require.Equal(t, common.HexToHash("0x1"), trk.TransactionHash())
}

func TestTransientReceiptKeyTransactionIndexSorting(t *testing.T) {
	keys := []types.TransientReceiptKey{
		types.NewTransientReceiptKey(100, common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")),
		types.NewTransientReceiptKey(5, common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")),
		types.NewTransientReceiptKey(50, common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")),
		types.NewTransientReceiptKey(1, common.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444")),
		types.NewTransientReceiptKey(25, common.HexToHash("0x5555555555555555555555555555555555555555555555555555555555555555")),
	}

	expectedIndices := map[string]uint64{
		string(keys[0]): 100,
		string(keys[1]): 5,
		string(keys[2]): 50,
		string(keys[3]): 1,
		string(keys[4]): 25,
	}

	sort.Slice(keys, func(i, j int) bool {
		return string(keys[i]) < string(keys[j])
	})

	expectedOrder := []uint64{1, 5, 25, 50, 100}
	for i, key := range keys {
		expectedIndex := expectedOrder[i]
		actualIndex := expectedIndices[string(key)]
		require.Equal(t, expectedIndex, actualIndex,
			"Key at position %d should have transaction index %d, but got %d",
			i, expectedIndex, actualIndex)
	}
}
