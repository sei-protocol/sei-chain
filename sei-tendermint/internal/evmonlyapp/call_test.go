package evmonlyapp

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
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
	return &ethcore.Message{
		From:             from,
		To:               to,
		GasLimit:         100_000,
		GasPrice:         new(big.Int),
		GasFeeCap:        new(big.Int),
		GasTipCap:        new(big.Int),
		Value:            new(big.Int),
		SkipNonceChecks:  true,
		SkipFromEOACheck: true,
	}
}

func TestEVMOnlyApplicationEvmCallReadsCommittedContractCode(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	slot := common.BytesToHash([]byte{0x11})
	value := common.BytesToHash([]byte{0x22})
	runtime := storeCode(slot, value)
	deployRaw, sender, contractAddr := signedEVMOnlyCreateTx(t, evmOnlyTestChainID, initCode(runtime), 300_000)

	_, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:  [][]byte{deployRaw},
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	})
	require.NoError(t, err)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)

	result, err := evmApp.EvmCall(t.Context(), callMessage(sender, &contractAddr))

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

	_, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:  [][]byte{deployRaw},
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	})
	require.NoError(t, err)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)

	before := evmApp.storage.StateDB().OpenView()
	beforeValue := before.GetStorage(contractAddr, slot)
	before.Close()
	require.Equal(t, common.Hash{}, beforeValue)

	result, err := evmApp.EvmCall(t.Context(), callMessage(sender, &contractAddr))
	require.NoError(t, err)
	require.False(t, result.Failed())

	after := evmApp.storage.StateDB().OpenView()
	defer after.Close()
	require.Equal(t, common.Hash{}, after.GetStorage(contractAddr, slot))
}

func TestEVMOnlyApplicationEvmCallRefusesDuringPendingCommit(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)

	_, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	})
	require.NoError(t, err)

	_, err = evmApp.EvmCall(t.Context(), callMessage(common.Address{}, nil))

	require.Error(t, err)
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
	require.Equal(t, big.NewInt(0), baseFee)
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
	first := evmOnlyTestBlock(1)
	second := evmOnlyTestBlock(2, deployRaw)
	finalizeAndCommitEVMOnlyTestBlock(t, app, first)
	finalizeAndCommitEVMOnlyTestBlock(t, app, second)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(3))
	// The undo log applies blocks behind the commit; until it has, their history is not held.
	app.storage.StateDB().UndoSS().WaitForPendingWrites()

	for _, tc := range []struct {
		name       string
		header     *ethtypes.Header
		wantReturn []byte
		wantCode   bool
	}{
		{name: "before the deploy", header: traceTestHeader(1, nil)},
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
			result, err := app.EvmCallAt(t.Context(), callMessage(sender, &contract), tc.header)
			require.NoError(t, err)
			require.NoError(t, result.Err)
			require.Equal(t, tc.wantReturn, result.Return())

			estimate, _, err := app.EvmEstimateGasAt(t.Context(), callMessage(sender, &contract), 0, tc.header)
			require.NoError(t, err)
			// Only a call that runs code costs more than a plain transfer.
			require.Equal(t, tc.wantCode, estimate > params.TxGas)
		})
	}

	t.Run("a state the node does not hold", func(t *testing.T) {
		_, err := app.EvmCallAt(t.Context(), callMessage(sender, &contract), traceTestHeader(9, nil))
		require.Error(t, err)
		_, _, err = app.EvmEstimateGasAt(t.Context(), callMessage(sender, &contract), 0, traceTestHeader(9, nil))
		require.Error(t, err)
	})
}

// A validator keeps no history, so it executes only against the latest block's state.
func TestEVMOnlyApplicationValidatorExecutesOnlyAgainstTheLatestBlock(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
	first := evmOnlyTestBlock(1)
	finalizeAndCommitEVMOnlyTestBlock(t, app, first)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2))
	to := common.Address{1}

	_, err := app.EvmCallAt(t.Context(), callMessage(common.Address{}, &to), traceTestHeader(2, first.Hash))
	require.NoError(t, err)
	_, _, err = app.EvmEstimateGasAt(t.Context(), callMessage(common.Address{}, &to), 0, traceTestHeader(2, first.Hash))
	require.NoError(t, err)

	_, err = app.EvmCallAt(t.Context(), callMessage(common.Address{}, &to), traceTestHeader(1, nil))
	require.Error(t, err)
	_, _, err = app.EvmEstimateGasAt(t.Context(), callMessage(common.Address{}, &to), 0, traceTestHeader(1, nil))
	require.Error(t, err)
}

func TestEVMOnlyApplicationEvmCallRequiresInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil)
	evmApp := app.(*evmOnlyApplication)

	_, err := evmApp.EvmCall(t.Context(), callMessage(common.Address{}, nil))

	require.Error(t, err)
}
