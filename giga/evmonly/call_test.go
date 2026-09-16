package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
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
// own code (via CODECOPY) and REVERTs with it, for constructing EVM code that
// reverts with an arbitrary ABI-encoded reason.
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

func TestExecutorCallRejectsMissingStateStore(t *testing.T) {
	executor := NewExecutor(Config{})

	_, err := executor.Call(t.Context(), blockContext(big.NewInt(testChainID)), callMessage(testAddress(0x01), nil))

	require.ErrorIs(t, err, errMissingStateStore)
}
