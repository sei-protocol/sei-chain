package keeper

import (
	"testing"

	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

// TestMigrate1to2 migrates state written before the msgExecuteContractDisabled and
// contractExecutionDisabled params existed, for which governance cannot submit a change
// until the param is stored.
func TestMigrate1to2(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, "")
	keys := [][]byte{types.ParamStoreKeyMsgExecuteContractDisabled, types.ParamStoreKeyContractExecutionDisabled}
	for _, key := range keys {
		deleteWasmParam(t, ctx, keepers, key)
		_, err := keepers.GovKeeper.SubmitProposal(ctx, enableFlagProposal(key))
		require.ErrorIs(t, err, govtypes.ErrInvalidProposalContent, string(key))
	}

	require.NoError(t, NewMigrator(*keepers.WasmKeeper).Migrate1to2(ctx))

	require.False(t, keepers.WasmKeeper.msgExecuteContractDisabled(ctx))
	require.False(t, keepers.WasmKeeper.contractExecutionDisabled(ctx))
	for _, key := range keys {
		_, err := keepers.GovKeeper.SubmitProposal(ctx, enableFlagProposal(key))
		require.NoError(t, err, string(key))
	}
}

// enableFlagProposal is a param-change proposal that sets the wasm bool param key to true.
func enableFlagProposal(key []byte) *proposal.ParameterChangeProposal {
	return &proposal.ParameterChangeProposal{
		Title:       "Set " + string(key),
		Description: "Set " + string(key),
		Changes: []proposal.ParamChange{{
			Subspace: types.ModuleName,
			Key:      string(key),
			Value:    "true",
		}},
	}
}
