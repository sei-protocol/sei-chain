package evmonlyapp

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func TestEVMOnlyApplicationEvmEstimateGasReadsCommittedContractCode(t *testing.T) {
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

	estimate, revert, err := evmApp.EvmEstimateGas(t.Context(), callMessage(sender, &contractAddr), 0)

	require.NoError(t, err)
	require.Empty(t, revert)
	require.Greater(t, estimate, uint64(params.TxGas))
}

func TestEVMOnlyApplicationEvmEstimateGasDoesNotMutateCommittedState(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	slot := common.BytesToHash([]byte{0x44})
	writtenValue := common.BytesToHash([]byte{0x55})
	// This contract unconditionally SSTOREs on every invocation; the search's
	// probes must never let that write reach committed state.
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

	_, _, err = evmApp.EvmEstimateGas(t.Context(), callMessage(sender, &contractAddr), 0)
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

	_, _, err = evmApp.EvmEstimateGas(t.Context(), callMessage(common.Address{}, nil), 0)

	require.Error(t, err)
}

func TestEVMOnlyApplicationEvmEstimateGasRequiresInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil)
	evmApp := app.(*evmOnlyApplication)

	_, _, err := evmApp.EvmEstimateGas(t.Context(), callMessage(common.Address{}, nil), 0)

	require.Error(t, err)
}
