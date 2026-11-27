package evmonlyapp

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEVMOnlyApplicationEvmEstimateGasReadsCommittedContractCode(t *testing.T) {
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

	estimate, revert, err := evmApp.EvmEstimateGas(t.Context(), callMessage(sender, &contractAddr), 0, abevm.CallOverrides{})

	require.NoError(t, err)
	require.Empty(t, revert)
	require.Greater(t, estimate, uint64(params.TxGas))
}

func TestEVMOnlyApplicationEvmEstimateGasDoesNotMutateCommittedState(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	slot := common.BytesToHash([]byte{0x44})
	writtenValue := common.BytesToHash([]byte{0x55})
	// Unconditionally SSTOREs; a probe must never let this reach committed state.
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

	_, _, err = evmApp.EvmEstimateGas(t.Context(), callMessage(sender, &contractAddr), 0, abevm.CallOverrides{})
	require.NoError(t, err)

	after := evmApp.storage.StateDB().OpenView()
	defer after.Close()
	require.Equal(t, common.Hash{}, after.GetStorage(contractAddr, slot))
}

func TestEVMOnlyApplicationEvmEstimateGasAfterFinalization(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)

	_, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)})
	require.NoError(t, err)

	_, _, err = evmApp.EvmEstimateGas(t.Context(), callMessage(common.Address{}, nil), 0, abevm.CallOverrides{})

	require.NoError(t, err)
}

func TestEVMOnlyApplicationEvmEstimateGasRequiresInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil)
	evmApp := app.(*evmOnlyApplication)

	_, _, err := evmApp.EvmEstimateGas(t.Context(), callMessage(common.Address{}, nil), 0, abevm.CallOverrides{})

	require.Error(t, err)
}

// A transaction writing a storage slot from zero to non-zero is charged evmonly.SstoreSetGas for the
// write by a call, a gas estimate and block execution alike, and executes within its estimate.
func TestEVMOnlyApplicationChargesSstoreSetGasForANewSlot(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	runtime := storeCode(common.BytesToHash([]byte{0x11}), common.BytesToHash([]byte{0x22}))
	deployRaw, sender, contract := signedEVMOnlyCreateTx(t, evmOnlyTestChainID, initCode(runtime), 300_000)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1, deployRaw))
	// Intrinsic gas, runtime's two PUSH32s, the cold slot access, and the write to a new slot.
	want := params.TxGas + 2*3 + params.ColdSloadCostEIP2929 + evmonly.SstoreSetGas

	require.Equal(t, evmonly.SstoreSetGas, evmApp.EvmChainConfig().SstoreSetGasEIP2200())

	result, err := evmApp.EvmCall(t.Context(), callMessage(sender, &contract), abevm.CallOverrides{})
	require.NoError(t, err)
	require.NoError(t, result.Err)
	require.Equal(t, want, result.UsedGas)

	estimate, revert, err := evmApp.EvmEstimateGas(t.Context(), callMessage(sender, &contract), 0, abevm.CallOverrides{})
	require.NoError(t, err)
	require.Empty(t, revert)
	// The estimate's binary search stops within a tolerance above the gas used.
	require.GreaterOrEqual(t, estimate, want)

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		GasPrice: big.NewInt(evmOnlyMinGasPrice),
		Gas:      estimate,
		To:       &contract,
		Value:    big.NewInt(0),
	}), ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(evmOnlyTestChainID)), key)
	require.NoError(t, err)
	response, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(app, 2, utils.OrPanic1(signed.MarshalBinary())))
	require.NoError(t, err)
	require.Equal(t, want, response.GasUsed)
}
