package capture

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/parity"
)

func TestMergeAccountKeepsFirstSeenState(t *testing.T) {
	address := common.HexToAddress("0xabc")
	slotA, slotB := common.Hash{0x0a}, common.Hash{0x0b}
	prestate := map[common.Address]parity.Account{}

	mergeAccount(prestate, address, &prestateAccount{
		Balance: (*hexutil.Big)(big.NewInt(100)),
		Nonce:   3,
		Storage: map[common.Hash]common.Hash{slotA: {0x01}},
	})
	// A later transaction sees the state the earlier one left, and touches a new slot.
	mergeAccount(prestate, address, &prestateAccount{
		Balance: (*hexutil.Big)(big.NewInt(40)),
		Nonce:   4,
		Storage: map[common.Hash]common.Hash{slotA: {0x02}, slotB: {0x03}},
	})

	account := prestate[address]
	require.Equal(t, big.NewInt(100), account.Balance.ToInt())
	require.Equal(t, hexutil.Uint64(3), account.Nonce)
	require.Equal(t, map[common.Hash]common.Hash{slotA: {0x01}, slotB: {0x03}}, account.Storage)
}

func TestMergeAccountWithoutBalance(t *testing.T) {
	prestate := map[common.Address]parity.Account{}
	mergeAccount(prestate, common.HexToAddress("0xdef"), &prestateAccount{})
	require.Equal(t, 0, prestate[common.HexToAddress("0xdef")].Balance.ToInt().Sign())
}

func TestIsCustomPrecompile(t *testing.T) {
	require.False(t, isCustomPrecompile(common.HexToAddress("0x0000000000000000000000000000000000000001")))
	require.False(t, isCustomPrecompile(common.HexToAddress("0x0000000000000000000000000000000000001000")))
	require.True(t, isCustomPrecompile(common.HexToAddress("0x0000000000000000000000000000000000001001")))
	require.True(t, isCustomPrecompile(common.HexToAddress("0x00000000000000000000000000000000000010ff")))
	require.False(t, isCustomPrecompile(common.HexToAddress("0x0000000000000000000000000000000000011001")))
}
