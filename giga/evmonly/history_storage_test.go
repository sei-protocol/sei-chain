package evmonly

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

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

// blockHashRecorderCode stores BLOCKHASH(n) at slot n for the n its calldata holds.
var blockHashRecorderCode = []byte{0x5f, 0x35, 0x80, 0x40, 0x90, 0x55, 0x00}

// blockHashReaderCode returns BLOCKHASH(n) for the n its calldata holds.
var blockHashReaderCode = []byte{0x5f, 0x35, 0x40, 0x5f, 0x52, 0x60, 0x20, 0x5f, 0xf3}

// historyTestHash is the hash the history tests give block n.
func historyTestHash(n uint64) common.Hash {
	return common.Hash{0xab, byte(n >> 8), byte(n)}
}

// executeHistoryChain executes empty blocks 1 through last of a chain that installs the history
// storage contract at height 1, applying their changes to state.
func executeHistoryChain(t *testing.T, executor *Executor, state *MemoryState, last uint64) {
	t.Helper()
	executeHistoryBlock(t, executor, state, 1, common.Hash{})
	for n := uint64(2); n <= last; n++ {
		executeHistoryBlock(t, executor, state, n, historyTestHash(n-1))
	}
}

func TestBlockHashAnswersFromTheParentAndHistoryStorage(t *testing.T) {
	const number = params.HistoryServeWindow + 809
	history := NewMemoryState()
	for _, n := range []uint64{number - 2, number - params.HistoryServeWindow, number - params.HistoryServeWindow - 1} {
		history.SetState(params.HistoryStorageAddress, historySlot(n), historyTestHash(n))
	}
	ctx := BlockContext{Number: number, ParentHash: historyTestHash(number - 1)}

	for _, tc := range []struct {
		name string
		n    uint64
		want common.Hash
	}{
		{"parent", number - 1, historyTestHash(number - 1)},
		{"stored", number - 2, historyTestHash(number - 2)},
		{"oldest served", number - params.HistoryServeWindow, historyTestHash(number - params.HistoryServeWindow)},
		{"past the serve window", number - params.HistoryServeWindow - 1, common.Hash{}},
		{"current", number, common.Hash{}},
		{"future", number + 1, common.Hash{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, blockHash(ctx, history, tc.n))
		})
	}
}

func TestBlockHashOpcodeReadsHistoryStorageInABlock(t *testing.T) {
	forEachExecutionPath(t, func(t *testing.T, cfg Config) {
		chainID := big.NewInt(testChainID)
		recorder := testAddress(0xb1)
		state := NewMemoryState()
		state.SetCode(recorder, blockHashRecorderCode)
		queries := []uint64{9, 8, 1, 10}
		keys := make([]*ecdsa.PrivateKey, len(queries))
		for i := range keys {
			key, err := crypto.GenerateKey()
			require.NoError(t, err)
			keys[i] = key
			state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(testFundedBalanceWei))
		}
		cfg.MinGasPrice = big.NewInt(0)
		executor := NewExecutor(cfg, withTestState(state), WithGenesisAlloc(1, nil))
		defer executor.Close()
		executeHistoryChain(t, executor, state, 9)

		ctx := blockContext(chainID)
		ctx.Number = 10
		ctx.ParentHash = historyTestHash(9)
		txs := make([][]byte, len(queries))
		for i, n := range queries {
			txs[i] = signLegacyTx(t, keys[i], chainID, 0, &recorder, big.NewInt(0), common.BigToHash(new(big.Int).SetUint64(n)).Bytes())
		}
		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: ctx, Txs: txs})
		require.NoError(t, err)
		defer result.Release()
		requireOCCRan(t, cfg, result)
		state.ApplyChangeSet(result.ChangeSet)

		for _, n := range []uint64{9, 8, 1} {
			require.Equal(t, historyTestHash(n), state.GetState(recorder, common.BigToHash(new(big.Int).SetUint64(n))), "BLOCKHASH(%d)", n)
		}
		require.Equal(t, common.Hash{}, state.GetState(recorder, common.BigToHash(big.NewInt(10))), "BLOCKHASH of the current block")
	})
}

func TestBlockHashOpcodeReadsHistoryStorageInACall(t *testing.T) {
	reader := testAddress(0xb2)
	state := NewMemoryState()
	state.SetCode(reader, blockHashReaderCode)
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(state), WithGenesisAlloc(1, nil))
	defer executor.Close()
	executeHistoryChain(t, executor, state, 9)

	ctx := blockContext(big.NewInt(testChainID))
	ctx.Number = 10
	ctx.ParentHash = historyTestHash(9)
	for _, n := range []uint64{9, 5, 10} {
		msg := callMessage(common.Address{0xf1}, &reader)
		msg.Data = common.BigToHash(new(big.Int).SetUint64(n)).Bytes()
		result, err := executor.Call(t.Context(), ctx, msg)
		require.NoError(t, err)
		require.False(t, result.Failed(), "%v", result.Err)
		want := historyTestHash(n)
		if n == 10 {
			want = common.Hash{}
		}
		require.Equal(t, want.Bytes(), result.ReturnData, "BLOCKHASH(%d)", n)
	}
}
