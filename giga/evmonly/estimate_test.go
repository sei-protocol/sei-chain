package evmonly

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

func TestExecutorEstimateGasPlainTransferReturnsIntrinsicGas(t *testing.T) {
	chainID := big.NewInt(testChainID)
	sender := testAddress(0xa1)
	target := testAddress(0xb2)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	store := NewMemoryStore(state)
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

	msg := callMessage(sender, &target)
	msg.Value = big.NewInt(1)

	estimate, revert, err := executor.EstimateGas(t.Context(), blockContext(chainID), msg, 0)

	require.NoError(t, err)
	require.Empty(t, revert)
	require.Equal(t, uint64(params.TxGas), estimate)
}

func TestExecutorEstimateGasSurfacesRevertReason(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	runtime := revertReasonRuntime("insufficient balance")
	contractAddr := crypto.CreateAddress(sender, 0)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	store := NewMemoryStore(state)
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

	deploy := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(runtime), 300_000)
	_, err = executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{deploy},
	})
	require.NoError(t, err)

	_, revert, err := executor.EstimateGas(t.Context(), blockContext(chainID), callMessage(sender, &contractAddr), 0)

	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	reason, unpackErr := abi.UnpackRevert(revert)
	require.NoError(t, unpackErr)
	require.Equal(t, "insufficient balance", reason)
}

func TestExecutorEstimateGasDoesNotMutateCommittedState(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	slot := testHash(0x44)
	writtenValue := testHash(0x55)
	// Unconditionally SSTOREs; a probe must never let this reach committed state.
	runtime := storeCode(slot, writtenValue)
	contractAddr := crypto.CreateAddress(sender, 0)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	store := NewMemoryStore(state)
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

	deploy := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(runtime), 300_000)
	_, err = executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{deploy},
	})
	require.NoError(t, err)

	_, _, err = executor.EstimateGas(t.Context(), blockContext(chainID), callMessage(sender, &contractAddr), 0)
	require.NoError(t, err)

	afterView := store.OpenView()
	defer afterView.Close()
	require.Equal(t, common.Hash{}, afterView.GetStorage(contractAddr, slot))
}

func TestExecutorEstimateGasUsesBlockGasLimitAsUpperBound(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	runtime := storeCode(testHash(0x11), testHash(0x22))
	contractAddr := crypto.CreateAddress(sender, 0)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	store := NewMemoryStore(state)
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

	deploy := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(runtime), 300_000)
	_, err = executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{deploy},
	})
	require.NoError(t, err)

	// A cold SSTORE needs more gas than this block's limit allows.
	tightBlock := blockContext(chainID)
	tightBlock.GasLimit = 22_000
	msg := callMessage(sender, &contractAddr)
	msg.GasLimit = 0

	_, _, err = executor.EstimateGas(t.Context(), tightBlock, msg, 0)

	require.ErrorContains(t, err, "gas required exceeds allowance")
}

func TestExecutorEstimateGasHonorsGasCap(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	runtime := storeCode(testHash(0x11), testHash(0x22))
	contractAddr := crypto.CreateAddress(sender, 0)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	store := NewMemoryStore(state)
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

	deploy := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(runtime), 300_000)
	_, err = executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{deploy},
	})
	require.NoError(t, err)

	msg := callMessage(sender, &contractAddr)
	msg.GasLimit = 0

	_, _, err = executor.EstimateGas(t.Context(), blockContext(chainID), msg, 22_000)

	require.ErrorContains(t, err, "gas required exceeds allowance")
}

func TestExecutorEstimateGasTimesOutOnUnboundedExecution(t *testing.T) {
	original := callTimeout
	callTimeout = 20 * time.Millisecond
	defer func() { callTimeout = original }()

	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	contractAddr := crypto.CreateAddress(sender, 0)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	store := NewMemoryStore(state)
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

	deploy := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(infiniteLoopCode()), 300_000)
	_, err = executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{deploy},
	})
	require.NoError(t, err)

	msg := callMessage(sender, &contractAddr)
	msg.GasLimit = 1_000_000_000_000 // far more gas than the shrunk timeout allows spending

	_, _, err = executor.EstimateGas(t.Context(), blockContext(chainID), msg, 0)

	require.ErrorContains(t, err, "timeout")
}

func TestExecutorEstimateGasReportsCancellationDistinctFromTimeout(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	contractAddr := crypto.CreateAddress(sender, 0)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	store := NewMemoryStore(state)
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

	deploy := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(infiniteLoopCode()), 300_000)
	_, err = executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{deploy},
	})
	require.NoError(t, err)

	msg := callMessage(sender, &contractAddr)
	msg.GasLimit = 1_000_000_000_000

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, _, err = executor.EstimateGas(ctx, blockContext(chainID), msg, 0)

	require.ErrorIs(t, err, context.Canceled)
}

// contextOpcodeReturnSuffix stores the top stack word to memory and returns
// it: PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN.
var contextOpcodeReturnSuffix = []byte{0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}

// pushReturns returns runtime bytecode that runs op with no stack input and
// returns whatever it pushed.
func pushReturns(op vm.OpCode) []byte {
	return append([]byte{byte(op)}, contextOpcodeReturnSuffix...)
}

// TestExecutorEstimateGasContextOpcodesDoNotPanic covers every 0x30-0x4a
// opcode that reads block/transaction context rather than call data.
func TestExecutorEstimateGasContextOpcodesDoNotPanic(t *testing.T) {
	cases := []struct {
		name    string
		runtime []byte
	}{
		{"ORIGIN", pushReturns(vm.ORIGIN)},
		{"CALLER", pushReturns(vm.CALLER)},
		{"CALLVALUE", pushReturns(vm.CALLVALUE)},
		{"CALLDATASIZE", pushReturns(vm.CALLDATASIZE)},
		{"CODESIZE", pushReturns(vm.CODESIZE)},
		// PUSH1 0 (size), PUSH1 0 (offset), PUSH1 0 (destOffset), CODECOPY, STOP.
		{"CODECOPY", []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, byte(vm.CODECOPY), 0x00}},
		{"GASPRICE", pushReturns(vm.GASPRICE)},
		{"RETURNDATASIZE", pushReturns(vm.RETURNDATASIZE)},
		// PUSH1 0 (block 1's parent), then BLOCKHASH.
		{"BLOCKHASH(0)", append([]byte{0x60, 0x00, byte(vm.BLOCKHASH)}, contextOpcodeReturnSuffix...)},
		{"COINBASE", pushReturns(vm.COINBASE)},
		{"TIMESTAMP", pushReturns(vm.TIMESTAMP)},
		{"NUMBER", pushReturns(vm.NUMBER)},
		{"PREVRANDAO", pushReturns(vm.PREVRANDAO)},
		{"GASLIMIT", pushReturns(vm.GASLIMIT)},
		{"CHAINID", pushReturns(vm.CHAINID)},
		{"SELFBALANCE", pushReturns(vm.SELFBALANCE)},
		{"BASEFEE", pushReturns(vm.BASEFEE)},
		// PUSH1 0 (blob index), then BLOBHASH.
		{"BLOBHASH(0)", append([]byte{0x60, 0x00, byte(vm.BLOBHASH)}, contextOpcodeReturnSuffix...)},
		{"BLOBBASEFEE", pushReturns(vm.BLOBBASEFEE)},
	}

	chainID := big.NewInt(testChainID)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, err := crypto.GenerateKey()
			require.NoError(t, err)
			sender := crypto.PubkeyToAddress(key.PublicKey)
			contractAddr := crypto.CreateAddress(sender, 0)

			state := NewMemoryState()
			state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
			store := NewMemoryStore(state)
			executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

			deploy := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(tc.runtime), 300_000)
			_, err = executor.ExecuteBlock(t.Context(), BlockRequest{
				Context: blockContext(chainID),
				Txs:     [][]byte{deploy},
			})
			require.NoError(t, err)

			_, _, err = executor.EstimateGas(t.Context(), blockContext(chainID), callMessage(sender, &contractAddr), 0)

			require.NoError(t, err)
		})
	}
}

func TestExecutorEstimateGasRejectsMissingStateStore(t *testing.T) {
	executor := NewExecutor(Config{})

	_, _, err := executor.EstimateGas(t.Context(), blockContext(big.NewInt(testChainID)), callMessage(testAddress(0x01), nil), 0)

	require.ErrorIs(t, err, errMissingStateStore)
}

func TestExecutorEstimateGasHonorsCanceledContext(t *testing.T) {
	executor := NewExecutor(Config{}, withTestState(NewMemoryState()))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := executor.EstimateGas(ctx, blockContext(big.NewInt(testChainID)), callMessage(testAddress(0x01), nil), 0)

	require.ErrorIs(t, err, context.Canceled)
}
