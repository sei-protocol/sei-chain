package evmonlyapp

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// storeCode returns runtime bytecode that unconditionally SSTOREs value at
// key on every invocation.
func storeCode(key, value common.Hash) []byte {
	code := append([]byte{0x7f}, value.Bytes()...) // PUSH32 value
	code = append(code, 0x7f)                      // PUSH32 key
	code = append(code, key.Bytes()...)
	return append(code, 0x55, 0x00) // SSTORE, STOP
}

// initCode wraps runtime bytecode in the standard CODECOPY+RETURN preamble a
// contract-creation transaction executes to install it.
func initCode(runtime []byte) []byte {
	if len(runtime) > 255 {
		panic("test runtime too large")
	}
	runtimeLen := byte(len(runtime)) //nolint:gosec // bounded by the check above.
	code := []byte{
		0x60, runtimeLen,
		0x60, 0x0c,
		0x60, 0x00,
		0x39,
		0x60, runtimeLen,
		0x60, 0x00,
		0xf3,
	}
	return append(code, runtime...)
}

func signedEVMOnlyCreateTx(t *testing.T, chainID uint64, data []byte, gas uint64) (raw []byte, sender, contractAddr common.Address) {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender = crypto.PubkeyToAddress(key.PublicKey)
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(evmOnlyMinGasPrice),
		Gas:      gas,
		Value:    big.NewInt(0),
		Data:     data,
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), key)
	require.NoError(t, err)
	raw, err = signed.MarshalBinary()
	require.NoError(t, err)
	return raw, sender, crypto.CreateAddress(sender, 0)
}

func callMessage(from common.Address, to *common.Address) *ethcore.Message {
	return (&ethcore.Message{
		From:      from,
		To:        to,
		GasLimit:  100_000,
		GasPrice:  new(uint256.Int),
		GasFeeCap: new(uint256.Int),
		GasTipCap: new(uint256.Int),
		Value:     new(uint256.Int),
	}).SkipNonceCheck().SkipEOACheck().SkipGasLimitCapCheck()
}

func TestEVMOnlyApplicationEvmCallReadsCommittedContractCode(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	slot := common.BytesToHash([]byte{0x11})
	value := common.BytesToHash([]byte{0x22})
	runtime := storeCode(slot, value)
	deployRaw, sender, contractAddr := signedEVMOnlyCreateTx(t, evmOnlyTestChainID, initCode(runtime), 300_000)

	_, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{deployRaw})),
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)})
	require.NoError(t, err)

	result, err := evmApp.EvmCall(t.Context(), callMessage(sender, &contractAddr), abevm.CallOverrides{})

	require.NoError(t, err)
	require.False(t, result.Failed())
}

func TestEVMOnlyApplicationEvmCallDoesNotMutateCommittedState(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	slot := common.BytesToHash([]byte{0x44})
	writtenValue := common.BytesToHash([]byte{0x55})
	// This contract unconditionally SSTOREs on every invocation; EvmCall must
	// never let that write reach committed state.
	runtime := storeCode(slot, writtenValue)
	deployRaw, sender, contractAddr := signedEVMOnlyCreateTx(t, evmOnlyTestChainID, initCode(runtime), 300_000)

	_, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{deployRaw})),
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)})
	require.NoError(t, err)

	before := evmApp.storage.StateDB().OpenView()
	beforeValue := before.GetStorage(contractAddr, slot)
	before.Close()
	require.Equal(t, common.Hash{}, beforeValue)

	result, err := evmApp.EvmCall(t.Context(), callMessage(sender, &contractAddr), abevm.CallOverrides{})
	require.NoError(t, err)
	require.False(t, result.Failed())

	after := evmApp.storage.StateDB().OpenView()
	defer after.Close()
	require.Equal(t, common.Hash{}, after.GetStorage(contractAddr, slot))
}

func TestEVMOnlyApplicationEvmCallAfterFinalization(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	deploy, sender, contract := signedEVMOnlyCreateTx(t, evmOnlyTestChainID, initCode(blockInfoCode), 300_000)

	_, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{deploy})),
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)})
	require.NoError(t, err)

	result, err := evmApp.EvmCall(t.Context(), callMessage(sender, &contract), abevm.CallOverrides{})

	require.NoError(t, err)
	require.NoError(t, result.Err)
	want := append(common.BigToHash(big.NewInt(1)).Bytes(), common.BigToHash(big.NewInt(1_700_000_001)).Bytes()...)
	require.Equal(t, want, result.Return())
}

func TestEVMOnlyApplicationExposesChainMetadata(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)

	// Test: the chain-id / config / fee / gas-limit accessors used by RPC.
	chainID := evmApp.EvmChainID()
	chainConfig := evmApp.EvmChainConfig()
	baseFee := evmApp.EvmBaseFee()
	gasLimit := evmApp.EvmGasLimit()

	// Verify: InitChain values, not the BaseApplication zeros.
	require.Equal(t, evmOnlyTestChainID, chainID)
	require.NotNil(t, chainConfig)
	require.Equal(t, new(big.Int).SetUint64(evmOnlyTestChainID), chainConfig.ChainID)
	require.Zero(t, big.NewInt(evmOnlyGenesisMinFeePerGas).Cmp(baseFee), "a chain starts at its minimum fee")
	require.Equal(t, uint64(30_000_000), gasLimit)
}

// blockInfoCode is runtime bytecode that returns NUMBER and TIMESTAMP as two 32-byte words.
var blockInfoCode = []byte{
	0x43, 0x60, 0x00, 0x52, // NUMBER, PUSH1 0, MSTORE
	0x42, 0x60, 0x20, 0x52, // TIMESTAMP, PUSH1 32, MSTORE
	0x60, 0x40, 0x60, 0x00, 0xf3, // PUSH1 64, PUSH1 0, RETURN
}

// A call or gas estimate at a past block runs against the state after that block, in the context it
// executed at; a state the node does not hold is refused.
func TestEVMOnlyApplicationExecutesAgainstAPastBlock(t *testing.T) {
	app := newUndoLogEVMOnlyTestApp(t)
	deployRaw, sender, contract := signedEVMOnlyCreateTx(t, evmOnlyTestChainID, initCode(blockInfoCode), 300_000)
	first := evmOnlyTestBlock(app, 1)
	second := evmOnlyTestBlock(app, 2, deployRaw)
	finalizeEVMOnlyTestBlock(t, app, first)
	finalizeEVMOnlyTestBlock(t, app, second)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 3))
	// The undo log applies blocks behind the commit; until it has, their history is not held.
	app.storage.StateDB().HistoricalStateDB().WaitForPendingWrites()

	for _, tc := range []struct {
		name       string
		header     abevm.BlockRef
		wantReturn []byte
		wantCode   bool
	}{
		{name: "before the deploy", header: traceTestHeader(1, common.Hash{})},
		{
			name:       "the deploy block",
			header:     traceTestHeader(2, first.Hash),
			wantReturn: append(common.BigToHash(big.NewInt(2)).Bytes(), common.BigToHash(big.NewInt(1_700_000_002)).Bytes()...),
			wantCode:   true,
		},
		{
			name:       "after the deploy",
			header:     traceTestHeader(3, second.Hash),
			wantReturn: append(common.BigToHash(big.NewInt(3)).Bytes(), common.BigToHash(big.NewInt(1_700_000_003)).Bytes()...),
			wantCode:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := app.EvmCallAt(t.Context(), callMessage(sender, &contract), tc.header, abevm.CallOverrides{})
			require.NoError(t, err)
			require.NoError(t, result.Err)
			require.Equal(t, tc.wantReturn, result.Return())

			estimate, _, err := app.EvmEstimateGasAt(t.Context(), callMessage(sender, &contract), 0, tc.header, abevm.CallOverrides{})
			require.NoError(t, err)
			// Only a call that runs code costs more than a plain transfer.
			require.Equal(t, tc.wantCode, estimate > params.TxGas)
		})
	}

	t.Run("a state the node does not hold", func(t *testing.T) {
		_, err := app.EvmCallAt(t.Context(), callMessage(sender, &contract), traceTestHeader(9, common.Hash{}), abevm.CallOverrides{})
		require.Error(t, err)
		_, _, err = app.EvmEstimateGasAt(t.Context(), callMessage(sender, &contract), 0, traceTestHeader(9, common.Hash{}), abevm.CallOverrides{})
		require.Error(t, err)
	})
}

// A call or gas estimate, at the latest block or a past one, runs with the overrides it is given.
func TestEVMOnlyApplicationAppliesCallOverrides(t *testing.T) {
	app := newUndoLogEVMOnlyTestApp(t)
	first := evmOnlyTestBlock(app, 1)
	finalizeEVMOnlyTestBlock(t, app, first)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2))
	app.storage.StateDB().HistoricalStateDB().WaitForPendingWrites()
	contract := common.HexToAddress("0x2000000000000000000000000000000000000002")
	overrides := codeOverride(contract, blockInfoCode)
	overrides.Block = utils.Some(export.BlockOverrides{Number: (*hexutil.Big)(big.NewInt(0x1234))})
	wantNumber := common.BigToHash(big.NewInt(0x1234)).Bytes()

	for _, tc := range []struct {
		name     string
		call     func() (*ethcore.ExecutionResult, error)
		estimate func() (uint64, []byte, error)
	}{
		{
			name: "latest",
			call: func() (*ethcore.ExecutionResult, error) {
				return app.EvmCall(t.Context(), callMessage(common.Address{}, &contract), overrides)
			},
			estimate: func() (uint64, []byte, error) {
				return app.EvmEstimateGas(t.Context(), callMessage(common.Address{}, &contract), 0, overrides)
			},
		},
		{
			name: "past block",
			call: func() (*ethcore.ExecutionResult, error) {
				return app.EvmCallAt(t.Context(), callMessage(common.Address{}, &contract), traceTestHeader(1, common.Hash{}), overrides)
			},
			estimate: func() (uint64, []byte, error) {
				return app.EvmEstimateGasAt(t.Context(), callMessage(common.Address{}, &contract), 0, traceTestHeader(1, common.Hash{}), overrides)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.call()
			require.NoError(t, err)
			require.NoError(t, result.Err)
			// The code returns NUMBER and TIMESTAMP.
			require.True(t, bytes.HasPrefix(result.Return(), wantNumber))

			estimate, _, err := tc.estimate()
			require.NoError(t, err)
			// The contract exists only in the override, so only the override makes the call run code.
			require.True(t, estimate > params.TxGas)
		})
	}
}

// A validator keeps no history, so it executes only against the latest block's state.
func TestEVMOnlyApplicationValidatorExecutesOnlyAgainstTheLatestBlock(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
	first := evmOnlyTestBlock(app, 1)
	finalizeEVMOnlyTestBlock(t, app, first)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2))
	to := common.Address{1}

	_, err := app.EvmCallAt(t.Context(), callMessage(common.Address{}, &to), traceTestHeader(2, first.Hash), abevm.CallOverrides{})
	require.NoError(t, err)
	_, _, err = app.EvmEstimateGasAt(t.Context(), callMessage(common.Address{}, &to), 0, traceTestHeader(2, first.Hash), abevm.CallOverrides{})
	require.NoError(t, err)

	_, err = app.EvmCallAt(t.Context(), callMessage(common.Address{}, &to), traceTestHeader(1, common.Hash{}), abevm.CallOverrides{})
	require.Error(t, err)
	_, _, err = app.EvmEstimateGasAt(t.Context(), callMessage(common.Address{}, &to), 0, traceTestHeader(1, common.Hash{}), abevm.CallOverrides{})
	require.Error(t, err)
}

func TestEVMOnlyApplicationEvmCallRequiresInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil)
	evmApp := app.(*evmOnlyApplication)

	_, err := evmApp.EvmCall(t.Context(), callMessage(common.Address{}, nil), abevm.CallOverrides{})

	require.Error(t, err)
}

// prevRandaoCode is runtime bytecode that returns PREVRANDAO as a 32-byte word.
var prevRandaoCode = []byte{
	0x44, 0x60, 0x00, 0x52, // PREVRANDAO, PUSH1 0, MSTORE
	0x60, 0x20, 0x60, 0x00, 0xf3, // PUSH1 32, PUSH1 0, RETURN
}

// prevRandaoRecorderInitCode is creation bytecode that stores the PREVRANDAO its transaction
// executes at in slot 0, then installs prevRandaoCode.
func prevRandaoRecorderInitCode() []byte {
	runtimeLen := byte(len(prevRandaoCode))
	code := []byte{
		0x44, 0x60, 0x00, 0x55, // PREVRANDAO, PUSH1 0, SSTORE
		0x60, runtimeLen, 0x60, 0x10, 0x60, 0x00, 0x39, // CODECOPY(0, 16, len)
		0x60, runtimeLen, 0x60, 0x00, 0xf3, // RETURN(0, len)
	}
	return append(code, prevRandaoCode...)
}

// prevRandaoCheckCode returns runtime bytecode that stops when PREVRANDAO is want and reverts
// otherwise.
func prevRandaoCheckCode(want common.Hash) []byte {
	code := append([]byte{0x44, 0x7f}, want.Bytes()...) // PREVRANDAO, PUSH32 want
	return append(code,
		0x14, 0x60, 0x2a, 0x57, // EQ, PUSH1 42, JUMPI
		0x60, 0x00, 0x80, 0xfd, // PUSH1 0, DUP1, REVERT
		0x5b, 0x00, // JUMPDEST, STOP
	)
}

// codeOverride returns call overrides that install code at address.
func codeOverride(address common.Address, code []byte) abevm.CallOverrides {
	bz := hexutil.Bytes(code)
	return abevm.CallOverrides{State: export.StateOverride{address: {Code: &bz}}}
}

// A block's PREVRANDAO is derived from its full time, so two blocks in the same second differ, and the
// block's transactions, a call or gas estimate at the latest block or at a past one, and a past block's
// rebuilt context all see the value the block executed at.
func TestEVMOnlyApplicationDerivesPrevRandaoFromTheFullBlockTime(t *testing.T) {
	app := newUndoLogEVMOnlyTestApp(t)
	deployRaw, sender, recorder := signedEVMOnlyCreateTx(t, evmOnlyTestChainID, prevRandaoRecorderInitCode(), 300_000)
	first := evmOnlyTestBlock(app, 1, deployRaw)
	first.Time = time.Unix(1_700_000_001, 250_000_000)
	second := evmOnlyTestBlock(app, 2)
	second.Time = time.Unix(1_700_000_001, 750_000_000)
	finalizeEVMOnlyTestBlock(t, app, first)
	finalizeEVMOnlyTestBlock(t, app, second)
	app.storage.StateDB().HistoricalStateDB().WaitForPendingWrites()
	firstPrevRandao := evmonly.PrevRandao(first.Time)
	secondPrevRandao := evmonly.PrevRandao(second.Time)
	require.NotEqual(t, firstPrevRandao, secondPrevRandao, "blocks in the same second must differ")
	checker := common.HexToAddress("0x2000000000000000000000000000000000000002")

	// Verify: the deploy transaction executed at the first block's PREVRANDAO.
	recorded, err := app.EvmStorageAt(recorder, common.Hash{}, 1)
	require.NoError(t, err)
	require.Equal(t, firstPrevRandao, recorded)

	t.Run("latest block", func(t *testing.T) {
		result, err := app.EvmCall(t.Context(), callMessage(sender, &recorder), abevm.CallOverrides{})
		require.NoError(t, err)
		require.NoError(t, result.Err)
		require.Equal(t, secondPrevRandao.Bytes(), result.Return())

		_, _, err = app.EvmEstimateGas(t.Context(), callMessage(sender, &checker), 0, codeOverride(checker, prevRandaoCheckCode(secondPrevRandao)))
		require.NoError(t, err)
		_, _, err = app.EvmEstimateGas(t.Context(), callMessage(sender, &checker), 0, codeOverride(checker, prevRandaoCheckCode(firstPrevRandao)))
		require.ErrorIs(t, err, vm.ErrExecutionReverted, "the check code reverts at any other PREVRANDAO")
	})

	t.Run("past block", func(t *testing.T) {
		// The reference the RPC builds for the first block, carrying its base fee and PREVRANDAO.
		block := abevm.BlockRef{
			Height:     1,
			Time:       uint64(first.Time.Unix()), //nolint:gosec // G115: test times are positive.
			BaseFee:    big.NewInt(evmOnlyGenesisMinFeePerGas),
			PrevRandao: firstPrevRandao,
		}
		result, err := app.EvmCallAt(t.Context(), callMessage(sender, &recorder), block, abevm.CallOverrides{})
		require.NoError(t, err)
		require.NoError(t, result.Err)
		require.Equal(t, firstPrevRandao.Bytes(), result.Return())

		_, _, err = app.EvmEstimateGasAt(t.Context(), callMessage(sender, &checker), 0, block, codeOverride(checker, prevRandaoCheckCode(firstPrevRandao)))
		require.NoError(t, err)

		blockCtx, err := app.EvmBlockContext(block)
		require.NoError(t, err)
		require.Equal(t, firstPrevRandao, *blockCtx.Random)
		require.Equal(t, uint64(1_700_000_001), blockCtx.Time, "TIMESTAMP stays in whole seconds")
	})
}
