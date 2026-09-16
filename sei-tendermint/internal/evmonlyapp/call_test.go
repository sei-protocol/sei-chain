package evmonlyapp

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

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

func TestEVMOnlyApplicationEvmCallRequiresInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil)
	evmApp := app.(*evmOnlyApplication)

	_, err := evmApp.EvmCall(t.Context(), callMessage(common.Address{}, nil))

	require.Error(t, err)
}
