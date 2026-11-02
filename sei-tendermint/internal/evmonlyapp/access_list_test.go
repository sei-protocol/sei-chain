package evmonlyapp

import (
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

func TestEVMOnlyApplicationSimulatesAgainstCommittedState(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	slot := common.BytesToHash([]byte{0x11})
	runtime := storeCode(slot, common.BytesToHash([]byte{0x22}))
	deployRaw, sender, contractAddr := signedEVMOnlyCreateTx(t, evmOnlyTestChainID, initCode(runtime), 300_000)
	_, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:    [][]byte{deployRaw},
		Hash:   crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{Height: 1, Time: time.Unix(1_700_000_001, 0)},
	})
	require.NoError(t, err)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)

	t.Run("access list", func(t *testing.T) {
		accessList, result, err := evmApp.EvmCreateAccessList(t.Context(), callMessage(sender, &contractAddr))

		require.NoError(t, err)
		require.NoError(t, result.Err)
		require.Equal(t, ethtypes.AccessList{{Address: contractAddr, StorageKeys: []common.Hash{slot}}}, accessList)
	})

	t.Run("estimate gas after calls", func(t *testing.T) {
		alone, _, err := evmApp.EvmEstimateGas(t.Context(), callMessage(sender, &contractAddr), 0)
		require.NoError(t, err)

		// A prior call has already written the slot, so the same write costs less.
		afterWrite, _, err := evmApp.EvmEstimateGasAfterCalls(t.Context(),
			[]*ethcore.Message{callMessage(sender, &contractAddr)}, callMessage(sender, &contractAddr), 0)

		require.NoError(t, err)
		require.Less(t, afterWrite, alone)
	})
}

func TestEVMOnlyApplicationSimulationRequiresInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil)
	evmApp := app.(*evmOnlyApplication)

	_, _, err := evmApp.EvmCreateAccessList(t.Context(), callMessage(common.Address{}, nil))
	require.Error(t, err)
	_, _, err = evmApp.EvmEstimateGasAfterCalls(t.Context(), nil, callMessage(common.Address{}, nil), 0)
	require.Error(t, err)
}
