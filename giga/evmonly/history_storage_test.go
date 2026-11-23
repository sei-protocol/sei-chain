package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// historySlot is the history storage slot that holds block number's hash.
func historySlot(number uint64) common.Hash {
	return common.BigToHash(new(big.Int).SetUint64(number % params.HistoryServeWindow))
}

// executeHistoryBlock executes an empty block number with parent hash parent and applies its
// changes to state.
func executeHistoryBlock(t *testing.T, executor *Executor, state *MemoryState, number uint64, parent common.Hash) StateChangeSet {
	t.Helper()
	ctx := blockContext(big.NewInt(testChainID))
	ctx.Number = number
	ctx.ParentHash = parent
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: ctx})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	return result.ChangeSet
}

func TestHistoryStorageIsInstalledAtTheInitialHeight(t *testing.T) {
	state := NewMemoryState()
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(state), WithGenesisAlloc(5, nil))

	executeHistoryBlock(t, executor, state, 5, common.Hash{})

	require.Equal(t, params.HistoryStorageCode, state.GetCode(params.HistoryStorageAddress))
	require.Equal(t, uint64(1), state.GetNonce(params.HistoryStorageAddress))
}

func TestHistoryStorageHoldsEachParentHash(t *testing.T) {
	state := NewMemoryState()
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(state), WithGenesisAlloc(1, nil))
	executeHistoryBlock(t, executor, state, 1, common.Hash{})

	for number := uint64(2); number <= 4; number++ {
		parent := common.Hash{0xbb, byte(number)}
		changes := executeHistoryBlock(t, executor, state, number, parent)
		require.Empty(t, changes.Accounts, "block %d", number)
		require.Equal(t, []StorageChange{{Address: params.HistoryStorageAddress, Key: historySlot(number - 1), Value: parent}}, changes.Storage, "block %d", number)
	}
}

func TestHistoryStorageWrapsAtItsServeWindow(t *testing.T) {
	state := NewMemoryState()
	state.SetCode(params.HistoryStorageAddress, params.HistoryStorageCode)
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(state))
	parent := common.Hash{0xcc}

	executeHistoryBlock(t, executor, state, params.HistoryServeWindow+2, parent)

	require.Equal(t, parent, state.GetState(params.HistoryStorageAddress, common.BigToHash(big.NewInt(1))))
}

func TestHistoryStorageGetReturnsAStoredParentHash(t *testing.T) {
	state := NewMemoryState()
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(state), WithGenesisAlloc(1, nil))
	executeHistoryBlock(t, executor, state, 1, common.Hash{})
	parent := common.Hash{0xdd}
	executeHistoryBlock(t, executor, state, 2, parent)

	ctx := blockContext(big.NewInt(testChainID))
	ctx.Number = 3
	msg := callMessage(common.Address{0xf1}, &params.HistoryStorageAddress)
	msg.Data = common.BigToHash(big.NewInt(1)).Bytes()
	result, err := executor.Call(t.Context(), ctx, msg)
	require.NoError(t, err)
	require.False(t, result.Failed(), "%v", result.Err)
	require.Equal(t, parent.Bytes(), result.ReturnData)

	msg.Data = common.BigToHash(big.NewInt(3)).Bytes()
	result, err = executor.Call(t.Context(), ctx, msg)
	require.NoError(t, err)
	require.True(t, result.Failed(), "the current block has no stored hash")
}

func TestHistoryStorageLeavesTheSystemAddressAloneUnderMissingAccountState(t *testing.T) {
	state := NewMemoryState()
	funded := NewMemoryState()
	funded.SetBalance(params.SystemAddress, big.NewInt(1_000))
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(state), WithMissingAccountState(funded), WithGenesisAlloc(1, nil))
	executeHistoryBlock(t, executor, state, 1, common.Hash{})

	changes := executeHistoryBlock(t, executor, state, 2, common.Hash{0xee})

	require.Empty(t, changes.Accounts)
	require.Len(t, changes.Storage, 1)
}

func TestHistoryStorageWithoutItsCodeChangesNothing(t *testing.T) {
	state := NewMemoryState()
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(state))

	changes := executeHistoryBlock(t, executor, state, 2, common.Hash{0xee})

	require.Empty(t, changes.Accounts)
	require.Empty(t, changes.Storage)
}
