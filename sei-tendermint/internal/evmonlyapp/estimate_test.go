package evmonlyapp

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func deployRuntime(t *testing.T, app abci.Application, runtime []byte) (sender, contractAddr common.Address) {
	t.Helper()
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
	return sender, contractAddr
}

func TestEVMOnlyApplicationEvmEstimateGasAgainstCommittedContract(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	sender, contractAddr := deployRuntime(t, app, storeCode(common.BytesToHash([]byte{0x11}), common.BytesToHash([]byte{0x22})))
	msg := callMessage(sender, &contractAddr)
	msg.GasLimit = 0

	estimate, revert, err := evmApp.EvmEstimateGas(t.Context(), msg, 10_000_000)

	require.NoError(t, err)
	require.Empty(t, revert)
	require.Greater(t, estimate, params.TxGas)
	result, err := evmApp.EvmCall(t.Context(), callMessage(sender, &contractAddr))
	require.NoError(t, err)
	require.False(t, result.Failed())
	require.GreaterOrEqual(t, estimate, result.UsedGas)
}

func TestEVMOnlyApplicationEvmEstimateGasSurfacesRevert(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	sender, contractAddr := deployRuntime(t, app, []byte{0x60, 0x00, 0x60, 0x00, 0xfd}) // PUSH1 0, PUSH1 0, REVERT

	_, revert, err := evmApp.EvmEstimateGas(t.Context(), callMessage(sender, &contractAddr), 10_000_000)

	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Empty(t, revert)
}

func TestEVMOnlyApplicationEvmEstimateGasDoesNotMutateCommittedState(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	slot := common.BytesToHash([]byte{0x44})
	sender, contractAddr := deployRuntime(t, app, storeCode(slot, common.BytesToHash([]byte{0x55})))

	_, _, err := evmApp.EvmEstimateGas(t.Context(), callMessage(sender, &contractAddr), 10_000_000)
	require.NoError(t, err)

	after := evmApp.storage.StateDB().OpenView()
	defer after.Close()
	require.Equal(t, common.Hash{}, after.GetStorage(contractAddr, slot))
}

func TestEVMOnlyApplicationEvmEstimateGasRefusesDuringPendingCommit(t *testing.T) {
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

	_, _, err = evmApp.EvmEstimateGas(t.Context(), callMessage(common.Address{}, nil), 10_000_000)

	require.Error(t, err)
}

func TestEVMOnlyApplicationEvmEstimateGasRequiresInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil)
	evmApp := app.(*evmOnlyApplication)

	_, _, err := evmApp.EvmEstimateGas(t.Context(), callMessage(common.Address{}, nil), 10_000_000)

	require.Error(t, err)
}
