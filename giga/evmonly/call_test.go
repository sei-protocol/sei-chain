package evmonly

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func callMessage(from common.Address, to *common.Address) *core.Message {
	return &core.Message{
		From:             from,
		To:               to,
		GasLimit:         200_000,
		GasPrice:         new(big.Int),
		GasFeeCap:        new(big.Int),
		GasTipCap:        new(big.Int),
		Value:            new(big.Int),
		SkipNonceChecks:  true,
		SkipFromEOACheck: true,
	}
}

// sloadReturnCode returns runtime bytecode that reads storage slot key and
// returns its 32-byte value, mirroring a view function such as ERC20
// balanceOf.
func sloadReturnCode(key common.Hash) []byte {
	code := []byte{0x7f} // PUSH32 key
	code = append(code, key.Bytes()...)
	code = append(code, 0x54)                         // SLOAD
	code = append(code, 0x60, 0x00, 0x52)             // PUSH1 0, MSTORE
	code = append(code, 0x60, 0x20, 0x60, 0x00, 0xf3) // PUSH1 32, PUSH1 0, RETURN
	return code
}

// revertReasonRuntime returns runtime bytecode that always reverts with the
// ABI-encoded Error(string) selector and reason, matching a Solidity
// `require(false, reason)`.
func revertReasonRuntime(reason string) []byte {
	selector := crypto.Keccak256([]byte("Error(string)"))[:4]
	payload := append(append([]byte{}, selector...), abiEncodeString(reason)...)
	return revertCodeForPayload(payload)
}

func abiEncodeString(s string) []byte {
	data := []byte(s)
	offset := make([]byte, 32)
	offset[31] = 32
	length := make([]byte, 32)
	new(big.Int).SetUint64(uint64(len(data))).FillBytes(length)
	padded := make([]byte, ((len(data)+31)/32)*32)
	copy(padded, data)
	out := append(append([]byte{}, offset...), length...)
	return append(out, padded...)
}

// revertCodeForPayload returns runtime bytecode that copies payload out of its
// own code (via CODECOPY) and REVERTs with it.
func revertCodeForPayload(payload []byte) []byte {
	const preambleLen = 14
	if len(payload) > 0xffff {
		panic("payload too large for test helper")
	}
	hi := byte(len(payload) >> 8)   //nolint:gosec // bounded by the check above.
	lo := byte(len(payload) & 0xff) //nolint:gosec // bounded by the check above.
	code := []byte{
		0x61, hi, lo, // PUSH2 len(payload)
		0x60, preambleLen, // PUSH1 offset (start of payload in this code)
		0x60, 0x00, // PUSH1 0 (destination memory offset)
		0x39,         // CODECOPY
		0x61, hi, lo, // PUSH2 len(payload)
		0x60, 0x00, // PUSH1 0
		0xfd, // REVERT
	}
	if len(code) != preambleLen {
		panic("preamble length mismatch")
	}
	return append(code, payload...)
}

func TestExecutorCallReturnsViewFunctionResult(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	slot := testHash(0x11)
	value := testHash(0x22)
	readRuntime := sloadReturnCode(slot)
	contractAddr := crypto.CreateAddress(sender, 0)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	store := NewMemoryStore(state)
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

	deployRead := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(readRuntime), 300_000)
	_, err = executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{deployRead},
	})
	require.NoError(t, err)
	// Seed the slot directly, standing in for a prior committed transaction's
	// SSTORE; the view function under test only reads it back.
	state.SetState(contractAddr, slot, value)

	result, err := executor.Call(t.Context(), blockContext(chainID), callMessage(sender, &contractAddr))

	require.NoError(t, err)
	require.False(t, result.Failed())
	require.Equal(t, value.Bytes(), result.Return())
}

func TestExecutorCallSurfacesRevertReason(t *testing.T) {
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

	result, err := executor.Call(t.Context(), blockContext(chainID), callMessage(sender, &contractAddr))

	require.NoError(t, err)
	require.ErrorIs(t, result.Err, vm.ErrExecutionReverted)
	reason, unpackErr := abi.UnpackRevert(result.Revert())
	require.NoError(t, unpackErr)
	require.Equal(t, "insufficient balance", reason)
}

func TestExecutorCallToNonexistentContractSucceedsWithEmptyReturnData(t *testing.T) {
	chainID := big.NewInt(testChainID)
	sender := testAddress(0xa1)
	target := testAddress(0xb2)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	store := NewMemoryStore(state)
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))

	result, err := executor.Call(t.Context(), blockContext(chainID), callMessage(sender, &target))

	require.NoError(t, err)
	require.False(t, result.Failed())
	require.Empty(t, result.Return())
}

func TestExecutorCallDoesNotMutateCommittedState(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	slot := testHash(0x44)
	writtenValue := testHash(0x55)
	// This contract unconditionally SSTOREs on every invocation; a call must
	// never let that write reach committed state.
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

	beforeView := store.OpenView()
	before := beforeView.GetStorage(contractAddr, slot)
	beforeView.Close()
	require.Equal(t, common.Hash{}, before)

	result, err := executor.Call(t.Context(), blockContext(chainID), callMessage(sender, &contractAddr))
	require.NoError(t, err)
	require.False(t, result.Failed())

	afterView := store.OpenView()
	defer afterView.Close()
	require.Equal(t, common.Hash{}, afterView.GetStorage(contractAddr, slot),
		"eth_call-style execution must never persist a state change")
}

// infiniteLoopCode returns runtime bytecode that loops forever
// (JUMPDEST, PUSH1 0, JUMP), for a call whose gas alone would never stop it.
func infiniteLoopCode() []byte {
	return []byte{0x5b, 0x60, 0x00, 0x56}
}

func TestExecutorCallTimesOutOnUnboundedExecution(t *testing.T) {
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

	_, err = executor.Call(t.Context(), blockContext(chainID), msg)

	require.ErrorContains(t, err, "timeout")
}

// CallAt and EstimateGasAt read the view they are given rather than the store's current state.
func TestExecutorExecutesAgainstTheGivenView(t *testing.T) {
	contract := common.HexToAddress("0x2000000000000000000000000000000000000002")
	slot := testHash(0x01)
	value := testHash(0x2a)
	view := newMemoryGigaSnapshot(7)
	view.code[contract] = sloadReturnCode(slot)
	view.storage[gigaStorageKey{address: contract, key: slot}] = value
	// The contract does not exist in the current state.
	executor := NewExecutor(Config{}, withTestState(NewMemoryState()))
	blockCtx := blockContext(big.NewInt(testChainID))
	msg := callMessage(testAddress(0x01), &contract)

	current, err := executor.Call(t.Context(), blockCtx, msg)
	require.NoError(t, err)
	require.Empty(t, current.Return())

	result, err := executor.CallAt(t.Context(), view, blockCtx, msg, CallOverrides{})
	require.NoError(t, err)
	require.Equal(t, value.Bytes(), result.Return())

	estimate, _, err := executor.EstimateGasAt(t.Context(), view, blockCtx, msg, 0, CallOverrides{})
	require.NoError(t, err)
	require.Greater(t, estimate, params.TxGas)
}

// numberReturnCode is runtime bytecode that returns NUMBER as one 32-byte word.
var numberReturnCode = []byte{0x43, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}

// CallAt and EstimateGasAt run with the accounts and block fields the overrides replace, leaving the
// state beneath unchanged.
func TestExecutorAppliesCallOverrides(t *testing.T) {
	contract := common.HexToAddress("0x2000000000000000000000000000000000000002")
	moved := common.HexToAddress("0x3000000000000000000000000000000000000003")
	identity := common.BytesToAddress([]byte{0x04})
	slot := testHash(0x01)
	stored := testHash(0x2a)
	replaced := testHash(0x2b)
	code := hexutil.Bytes(sloadReturnCode(slot))
	number := hexutil.Uint64(0x1234)
	for _, tc := range []struct {
		name       string
		to         common.Address
		data       []byte
		overrides  CallOverrides
		wantReturn []byte
		wantErr    bool
	}{
		{name: "no overrides", to: contract, wantReturn: stored.Bytes()},
		{
			name:       "state diff",
			to:         contract,
			overrides:  CallOverrides{State: export.StateOverride{contract: {StateDiff: map[common.Hash]common.Hash{slot: replaced}}}},
			wantReturn: replaced.Bytes(),
		},
		{
			name:       "replaced storage",
			to:         contract,
			overrides:  CallOverrides{State: export.StateOverride{contract: {State: map[common.Hash]common.Hash{}}}},
			wantReturn: common.Hash{}.Bytes(),
		},
		{
			name:       "code",
			to:         moved,
			overrides:  CallOverrides{State: export.StateOverride{moved: {Code: &code}}},
			wantReturn: common.Hash{}.Bytes(),
		},
		{
			name: "block number",
			to:   moved,
			overrides: CallOverrides{
				State: export.StateOverride{moved: {Code: (*hexutil.Bytes)(&numberReturnCode)}},
				Block: utils.Some(export.BlockOverrides{Number: (*hexutil.Big)(new(big.Int).SetUint64(uint64(number)))}),
			},
			wantReturn: common.BigToHash(new(big.Int).SetUint64(uint64(number))).Bytes(),
		},
		{
			name:       "moved precompile",
			to:         moved,
			data:       []byte{0xca, 0xfe},
			overrides:  CallOverrides{State: export.StateOverride{identity: {MovePrecompileTo: &moved}}},
			wantReturn: []byte{0xca, 0xfe},
		},
		{
			name:      "state and state diff together",
			to:        contract,
			overrides: CallOverrides{State: export.StateOverride{contract: {State: map[common.Hash]common.Hash{}, StateDiff: map[common.Hash]common.Hash{}}}},
			wantErr:   true,
		},
		{
			name:      "unsupported block field",
			to:        contract,
			overrides: CallOverrides{Block: utils.Some(export.BlockOverrides{BeaconRoot: &common.Hash{}})},
			wantErr:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := newMemoryGigaSnapshot(7)
			view.code[contract] = code
			view.storage[gigaStorageKey{address: contract, key: slot}] = stored
			executor := NewExecutor(Config{}, withTestState(NewMemoryState()))
			msg := callMessage(testAddress(0x01), &tc.to)
			msg.Data = tc.data

			result, err := executor.CallAt(t.Context(), view, blockContext(big.NewInt(testChainID)), msg, tc.overrides)

			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NoError(t, result.Err)
			require.Equal(t, tc.wantReturn, result.Return())
			require.Equal(t, stored, view.storage[gigaStorageKey{address: contract, key: slot}])
		})
	}

	t.Run("estimate", func(t *testing.T) {
		executor := NewExecutor(Config{}, withTestState(NewMemoryState()))
		msg := callMessage(testAddress(0x01), &moved)
		blockCtx := blockContext(big.NewInt(testChainID))

		plain, _, err := executor.EstimateGasAt(t.Context(), newMemoryGigaSnapshot(7), blockCtx, msg, 0, CallOverrides{})
		require.NoError(t, err)
		withCode, _, err := executor.EstimateGasAt(t.Context(), newMemoryGigaSnapshot(7), blockCtx, msg, 0,
			CallOverrides{State: export.StateOverride{moved: {Code: &code}}})
		require.NoError(t, err)
		require.Equal(t, params.TxGas, plain)
		require.Greater(t, withCode, params.TxGas)

		_, _, err = executor.EstimateGasAt(t.Context(), newMemoryGigaSnapshot(7), blockCtx, msg, 0,
			CallOverrides{Block: utils.Some(export.BlockOverrides{BeaconRoot: &common.Hash{}})})
		require.Error(t, err)
	})
}

func TestExecutorCallRejectsMissingStateStore(t *testing.T) {
	executor := NewExecutor(Config{})

	_, err := executor.Call(t.Context(), blockContext(big.NewInt(testChainID)), callMessage(testAddress(0x01), nil))

	require.ErrorIs(t, err, errMissingStateStore)
}

func TestExecutorCallHonorsCanceledContext(t *testing.T) {
	executor := NewExecutor(Config{}, withTestState(NewMemoryState()))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Test: Call with an already-canceled context.
	_, err := executor.Call(ctx, blockContext(big.NewInt(testChainID)), callMessage(testAddress(0x01), nil))

	// Verify: the canceled context is returned before a snapshot is opened.
	require.ErrorIs(t, err, context.Canceled)
}

func TestExecutorCallRejectsNilSnapshot(t *testing.T) {
	executor := NewExecutor(Config{}, withTestStores(&recordingGigaStore{}, NewMemoryReceiptStore(), func(StateChangeSet) ([]*proto.NamedChangeSet, error) {
		return nil, nil
	}))

	// Test: the store's OpenView returns nil.
	_, err := executor.Call(t.Context(), blockContext(big.NewInt(testChainID)), callMessage(testAddress(0x01), nil))

	// Verify: that is an error, not a panic on the snapshot.
	require.EqualError(t, err, "giga store returned a nil snapshot")
}

func TestExecutorCallRejectsZeroGasLimit(t *testing.T) {
	executor := NewExecutor(Config{}, withTestState(NewMemoryState()))
	ctx := blockContext(big.NewInt(testChainID))
	ctx.GasLimit = 0

	// Test: Call with a zero block gas limit.
	_, err := executor.Call(t.Context(), ctx, callMessage(testAddress(0x01), nil))

	// Verify: rejected before a snapshot is opened.
	require.ErrorIs(t, err, errInvalidBlockGasLimit)
}
