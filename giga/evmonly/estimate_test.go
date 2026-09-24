package evmonly

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// estimateExecutor returns an executor over a memory store holding the given
// runtime code at contract, with sender funded.
func estimateExecutor(sender, contract common.Address, runtime []byte) (*Executor, *MemoryState) {
	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	if runtime != nil {
		state.SetCode(contract, runtime)
	}
	store := NewMemoryStore(state)
	return NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet)), state
}

// gasBurningCode returns runtime bytecode that writes rounds distinct
// storage slots, so that it needs roughly rounds*22k gas to succeed.
func gasBurningCode(rounds int) []byte {
	var code []byte
	for i := range rounds {
		code = append(code, 0x60, 0x01, 0x60, byte(i), 0x55) // PUSH1 1, PUSH1 i, SSTORE
	}
	return append(code, 0x00) // STOP
}

// bareRevertCode returns runtime bytecode that reverts with no data.
func bareRevertCode() []byte {
	return []byte{0x60, 0x00, 0x60, 0x00, 0xfd} // PUSH1 0, PUSH1 0, REVERT
}

// blockContextDigestCode returns runtime bytecode that leaves on the stack the
// keccak256 of NUMBER, TIMESTAMP, PREVRANDAO, GASLIMIT, BASEFEE, COINBASE and
// BLOCKHASH(NUMBER-1), each as a 32-byte word.
func blockContextDigestCode() []byte {
	var code []byte
	for i, op := range []byte{0x43, 0x42, 0x44, 0x45, 0x48, 0x41} {
		code = append(code, op, 0x60, byte(i*32), 0x52) // op, PUSH1 offset, MSTORE
	}
	code = append(code, 0x43, 0x60, 0x01, 0x90, 0x03, 0x40, 0x60, 0xc0, 0x52) // NUMBER, PUSH1 1, SWAP1, SUB, BLOCKHASH, PUSH1 192, MSTORE
	return append(code, 0x60, 0xe0, 0x60, 0x00, 0x20)                         // PUSH1 224, PUSH1 0, SHA3
}

// returnBlockContextDigestCode returns runtime bytecode that returns the
// block context digest.
func returnBlockContextDigestCode() []byte {
	code := blockContextDigestCode()
	return append(code, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3) // PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
}

// requireBlockContextDigestCode returns runtime bytecode that reverts unless
// the block context digest equals want.
func requireBlockContextDigestCode(want common.Hash) []byte {
	code := blockContextDigestCode()
	code = append(code, 0x7f) // PUSH32 want
	code = append(code, want.Bytes()...)
	code = append(code, 0x14)                                     // EQ
	dest := len(code) + 3 + 7                                     // after PUSH1 dest, JUMPI and the revert
	code = append(code, 0x60, byte(dest), 0x57)                   // PUSH1 dest, JUMPI
	code = append(code, 0x60, 0x00, 0x60, 0x00, 0xfd, 0x00, 0x00) // PUSH1 0, PUSH1 0, REVERT (padded)
	return append(code, 0x5b, 0x00)                               // JUMPDEST, STOP
}

func TestExecutorEstimateGasPlainTransferCostsTxGas(t *testing.T) {
	sender := testAddress(0xa1)
	target := testAddress(0xb2)
	executor, _ := estimateExecutor(sender, target, nil)
	msg := callMessage(sender, &target)
	msg.Value = big.NewInt(1)

	got, revert, err := executor.EstimateGas(t.Context(), blockContext(big.NewInt(testChainID)), msg, 10_000_000)

	require.NoError(t, err)
	require.Empty(t, revert)
	require.Equal(t, params.TxGas, got)
}

func TestExecutorEstimateGasConvergesOnMinimumGas(t *testing.T) {
	sender := testAddress(0xa1)
	contract := testAddress(0xc3)
	executor, _ := estimateExecutor(sender, contract, gasBurningCode(8))
	blockCtx := blockContext(big.NewInt(testChainID))
	msg := callMessage(sender, &contract)
	msg.GasLimit = 10_000_000

	got, revert, err := executor.EstimateGas(t.Context(), blockCtx, msg, 10_000_000)

	require.NoError(t, err)
	require.Empty(t, revert)
	// Verify: the estimate is the minimum to within the error ratio, by
	// executing at it and just below it.
	msg.GasLimit = got
	result, err := executor.Call(t.Context(), blockCtx, msg)
	require.NoError(t, err)
	require.False(t, result.Failed(), "estimate %d must suffice", got)
	msg.GasLimit = uint64(float64(got) * (1 - estimateGasErrorRatio))
	result, err = executor.Call(t.Context(), blockCtx, msg)
	require.NoError(t, err)
	require.True(t, result.Failed(), "%d, below the error ratio, must not suffice", msg.GasLimit)
}

func TestExecutorEstimateGasOmittedGasUsesBlockGasLimit(t *testing.T) {
	sender := testAddress(0xa1)
	contract := testAddress(0xc3)
	executor, _ := estimateExecutor(sender, contract, gasBurningCode(8))
	blockCtx := blockContext(big.NewInt(testChainID))
	blockCtx.GasLimit = 100_000
	msg := callMessage(sender, &contract)
	msg.GasLimit = 0

	_, _, err := executor.EstimateGas(t.Context(), blockCtx, msg, 10_000_000)

	require.EqualError(t, err, "gas required exceeds allowance (100000)")
}

func TestExecutorEstimateGasCapsByGasCap(t *testing.T) {
	sender := testAddress(0xa1)
	contract := testAddress(0xc3)
	executor, _ := estimateExecutor(sender, contract, gasBurningCode(8))
	msg := callMessage(sender, &contract)
	msg.GasLimit = 10_000_000

	_, _, err := executor.EstimateGas(t.Context(), blockContext(big.NewInt(testChainID)), msg, 100_000)

	require.EqualError(t, err, "gas required exceeds allowance (100000)")
}

func TestExecutorEstimateGasRejectsValueAboveBalance(t *testing.T) {
	sender := testAddress(0xa1)
	target := testAddress(0xb2)
	executor, state := estimateExecutor(sender, target, nil)
	state.SetBalance(sender, big.NewInt(1_000))
	msg := callMessage(sender, &target)
	msg.GasPrice = big.NewInt(1)
	msg.GasFeeCap = big.NewInt(1)
	msg.Value = big.NewInt(1_000)

	_, _, err := executor.EstimateGas(t.Context(), blockContext(big.NewInt(testChainID)), msg, 10_000_000)

	require.ErrorIs(t, err, core.ErrInsufficientFundsForTransfer)
}

func TestExecutorEstimateGasSurfacesRevertReason(t *testing.T) {
	sender := testAddress(0xa1)
	contract := testAddress(0xc3)
	executor, _ := estimateExecutor(sender, contract, revertReasonRuntime("nope"))

	got, revert, err := executor.EstimateGas(t.Context(), blockContext(big.NewInt(testChainID)), callMessage(sender, &contract), 10_000_000)

	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Zero(t, got)
	reason, unpackErr := abi.UnpackRevert(revert)
	require.NoError(t, unpackErr)
	require.Equal(t, "nope", reason)
}

func TestExecutorEstimateGasSurfacesBareRevert(t *testing.T) {
	sender := testAddress(0xa1)
	contract := testAddress(0xc3)
	executor, _ := estimateExecutor(sender, contract, bareRevertCode())

	got, revert, err := executor.EstimateGas(t.Context(), blockContext(big.NewInt(testChainID)), callMessage(sender, &contract), 10_000_000)

	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Zero(t, got)
	require.Empty(t, revert)
}

func TestExecutorEstimateGasRunsInCallBlockContext(t *testing.T) {
	sender := testAddress(0xa1)
	oracle := testAddress(0xc3)
	subject := testAddress(0xc4)
	executor, state := estimateExecutor(sender, oracle, returnBlockContextDigestCode())
	blockCtx := blockContext(big.NewInt(testChainID))
	blockCtx.Number = 7
	blockCtx.Time = 1_700_000_000
	blockCtx.BaseFee = big.NewInt(3)
	blockCtx.ParentHash = testHash(0x77)
	blockCtx.PrevRandao = testHash(0x88)

	// Like geth, the estimator zeroes BASEFEE for a zero gas price, so probe
	// with a priced message.
	msg := callMessage(sender, &oracle)
	msg.GasPrice = big.NewInt(1)
	result, err := executor.Call(t.Context(), blockCtx, msg)
	require.NoError(t, err)
	require.False(t, result.Failed())
	state.SetCode(subject, requireBlockContextDigestCode(common.BytesToHash(result.Return())))

	// Test: estimate against a contract that reverts unless it sees the same
	// block context Call did.
	msg = callMessage(sender, &subject)
	msg.GasPrice = big.NewInt(1)
	_, revert, err := executor.EstimateGas(t.Context(), blockCtx, msg, 10_000_000)

	require.NoError(t, err)
	require.Empty(t, revert)
}

func TestExecutorEstimateGasDoesNotMutateCommittedState(t *testing.T) {
	sender := testAddress(0xa1)
	contract := testAddress(0xc3)
	executor, state := estimateExecutor(sender, contract, gasBurningCode(2))
	msg := callMessage(sender, &contract)

	_, _, err := executor.EstimateGas(t.Context(), blockContext(big.NewInt(testChainID)), msg, 10_000_000)

	require.NoError(t, err)
	require.Equal(t, common.Hash{}, state.GetState(contract, common.Hash{}))
	require.Equal(t, common.Hash{}, state.GetState(contract, testHash(0x01)))
}

func TestExecutorEstimateGasTimesOutOnUnboundedExecution(t *testing.T) {
	original := callTimeout
	callTimeout = 20 * time.Millisecond
	defer func() { callTimeout = original }()

	sender := testAddress(0xa1)
	contract := testAddress(0xc3)
	executor, _ := estimateExecutor(sender, contract, infiniteLoopCode())
	blockCtx := blockContext(big.NewInt(testChainID))
	blockCtx.GasLimit = 1_000_000_000_000
	msg := callMessage(sender, &contract)
	msg.GasLimit = blockCtx.GasLimit

	_, _, err := executor.EstimateGas(t.Context(), blockCtx, msg, 0)

	require.ErrorContains(t, err, "timeout")
}

func TestExecutorEstimateGasRejectsMissingStateStore(t *testing.T) {
	executor := NewExecutor(Config{})

	_, _, err := executor.EstimateGas(t.Context(), blockContext(big.NewInt(testChainID)), callMessage(testAddress(0x01), nil), 10_000_000)

	require.ErrorIs(t, err, errMissingStateStore)
}

func TestExecutorEstimateGasHonorsCanceledContext(t *testing.T) {
	executor := NewExecutor(Config{}, withTestState(NewMemoryState()))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := executor.EstimateGas(ctx, blockContext(big.NewInt(testChainID)), callMessage(testAddress(0x01), nil), 10_000_000)

	require.ErrorIs(t, err, context.Canceled)
}
