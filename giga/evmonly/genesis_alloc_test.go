package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestGenesisAllocCreditsOnlyItsHeight(t *testing.T) {
	a, b := common.HexToAddress("0xa110c000000000000000000000000000000000aa"), common.HexToAddress("0xa110c000000000000000000000000000000000bb")
	state := NewMemoryState()
	state.SetBalance(b, big.NewInt(2))
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(state),
		WithGenesisAlloc(1, map[common.Address]*uint256.Int{a: uint256.NewInt(5), b: uint256.NewInt(7)}))

	for number := uint64(1); number <= 2; number++ {
		ctx := blockContext(big.NewInt(testChainID))
		ctx.Number = number
		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: ctx})
		require.NoError(t, err)
		if number == 1 {
			require.Len(t, result.ChangeSet.Accounts, 2)
		} else {
			require.Empty(t, result.ChangeSet.Accounts, "block %d credits nothing", number)
		}
		state.ApplyChangeSet(result.ChangeSet)
		require.Equal(t, big.NewInt(5), state.GetBalance(a), "a after block %d", number)
		require.Equal(t, big.NewInt(9), state.GetBalance(b), "b after block %d", number)
	}
}

func TestGenesisAllocRefusesMissingAccountState(t *testing.T) {
	alloc := WithGenesisAlloc(1, map[common.Address]*uint256.Int{{1}: uint256.NewInt(5)})
	require.Panics(t, func() {
		NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(NewMemoryState()), WithMissingAccountState(NewMemoryState()), alloc)
	})
}

func TestGenesisAllocFundsItsBlocksTransactions(t *testing.T) {
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	to := common.HexToAddress("0xa110c000000000000000000000000000000000cc")
	tx := signLegacyTxWithGasPrice(t, key, big.NewInt(testChainID), 0, &to, big.NewInt(3), nil, 21_000, big.NewInt(1))
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(NewMemoryState()),
		WithGenesisAlloc(1, map[common.Address]*uint256.Int{sender: uint256.NewInt(behaviorSenderFunds)}))

	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(big.NewInt(testChainID)), Txs: [][]byte{tx}})
	require.NoError(t, err)
	require.Equal(t, uint64(1), result.Txs[0].Status, result.Txs[0].Err)
}
