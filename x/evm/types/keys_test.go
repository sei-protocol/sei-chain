package types_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"testing"

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
