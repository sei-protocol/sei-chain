package evmonlyapp

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/export"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEVMOnlyApplicationSimulatesAgainstCommittedState(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmApp := app.(*evmOnlyApplication)
	slot := common.BytesToHash([]byte{0x11})
	runtime := storeCode(slot, common.BytesToHash([]byte{0x22}))
	deployRaw, sender, contractAddr := signedEVMOnlyCreateTx(t, evmOnlyTestChainID, initCode(runtime), 300_000)
	_, err := app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Txs:    [][]byte{deployRaw},
		Hash:   crypto.Keccak256([]byte("block-1")),
		Header: &proxy.Header{Height: 1, Time: time.Unix(1_700_000_001, 0)},
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
		alone, _, err := evmApp.EvmEstimateGas(t.Context(), callMessage(sender, &contractAddr), 0, evmonly.CallOverrides{})
		require.NoError(t, err)

		// A prior call has already written the slot, so the same write costs less.
		afterWrite, _, err := evmApp.EvmEstimateGasAfterCalls(t.Context(),
			[]*ethcore.Message{callMessage(sender, &contractAddr)}, callMessage(sender, &contractAddr), 0, evmonly.CallOverrides{})

		require.NoError(t, err)
		require.Less(t, afterWrite, alone)
	})
}

func TestEVMOnlyApplicationSimulationRequiresInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil)
	evmApp := app.(*evmOnlyApplication)

	_, _, err := evmApp.EvmCreateAccessList(t.Context(), callMessage(common.Address{}, nil))
	require.Error(t, err)
	_, _, err = evmApp.EvmEstimateGasAfterCalls(t.Context(), nil, callMessage(common.Address{}, nil), 0, evmonly.CallOverrides{})
	require.Error(t, err)
}

// An access list or an estimate after calls at a past block runs against the state after that
// block, with any state override applied before the calls; a state the node does not hold is
// refused.
func TestEVMOnlyApplicationSimulatesAgainstAPastBlock(t *testing.T) {
	app := newUndoLogEVMOnlyTestApp(t)
	slot := common.BytesToHash([]byte{0x11})
	runtime := storeCode(slot, common.BytesToHash([]byte{0x22}))
	deployRaw, sender, contract := signedEVMOnlyCreateTx(t, evmOnlyTestChainID, initCode(runtime), 300_000)
	first := evmOnlyTestBlock(1)
	finalizeAndCommitEVMOnlyTestBlock(t, app, first)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2, deployRaw))
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(3))
	// The undo log applies blocks behind the commit; until it has, their history is not held.
	app.storage.StateDB().UndoSS().WaitForPendingWrites()
	beforeDeploy, deployed := traceTestHeader(1, nil), traceTestHeader(2, first.Hash)
	msg := func() *ethcore.Message { return callMessage(sender, &contract) }

	t.Run("access list", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			header *ethtypes.Header
			want   ethtypes.AccessList
		}{
			{name: "before the deploy", header: beforeDeploy, want: ethtypes.AccessList{}},
			{name: "the deploy block", header: deployed, want: ethtypes.AccessList{{Address: contract, StorageKeys: []common.Hash{slot}}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				accessList, result, err := app.EvmCreateAccessListAt(t.Context(), msg(), tc.header)

				require.NoError(t, err)
				require.NoError(t, result.Err)
				require.Equal(t, tc.want, accessList)
			})
		}
	})

	code := hexutil.Bytes(runtime)
	withCode := evmonly.CallOverrides{State: export.StateOverride{contract: {Code: &code}}}
	for _, tc := range []struct {
		name      string
		header    *ethtypes.Header
		overrides evmonly.CallOverrides
	}{
		{name: "estimate gas after calls", header: deployed},
		// The contract exists only in the override, so the prior call writes the slot only if the
		// override is applied before it.
		{name: "state override before the calls", header: beforeDeploy, overrides: withCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alone, _, err := app.EvmEstimateGasAt(t.Context(), msg(), 0, tc.header, tc.overrides)
			require.NoError(t, err)

			// A prior call has already written the slot, so the same write costs less.
			afterWrite, _, err := app.EvmEstimateGasAfterCallsAt(t.Context(), []*ethcore.Message{msg()}, msg(), 0, tc.header, tc.overrides)

			require.NoError(t, err)
			require.Less(t, afterWrite, alone)
		})
	}

	t.Run("a state the node does not hold", func(t *testing.T) {
		_, _, err := app.EvmCreateAccessListAt(t.Context(), msg(), traceTestHeader(9, nil))
		require.Error(t, err)
		_, _, err = app.EvmEstimateGasAfterCallsAt(t.Context(), []*ethcore.Message{msg()}, msg(), 0, traceTestHeader(9, nil), evmonly.CallOverrides{})
		require.Error(t, err)
	})
}
