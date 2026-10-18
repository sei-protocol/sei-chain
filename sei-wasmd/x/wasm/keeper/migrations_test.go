package keeper

import (
	"testing"

	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

// TestMigrate1to2 migrates state written before the msgExecuteContractDisabled param existed,
// for which governance cannot submit a change until the param is stored.
func TestMigrate1to2(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, "")
	deleteWasmParam(t, ctx, keepers, types.ParamStoreKeyMsgExecuteContractDisabled)
	disable := &proposal.ParameterChangeProposal{
		Title:       "Disable MsgExecuteContract",
		Description: "Disable MsgExecuteContract",
		Changes: []proposal.ParamChange{{
			Subspace: types.ModuleName,
			Key:      string(types.ParamStoreKeyMsgExecuteContractDisabled),
			Value:    "true",
		}},
	}
	_, err := keepers.GovKeeper.SubmitProposal(ctx, disable)
	require.ErrorIs(t, err, govtypes.ErrInvalidProposalContent)

	require.NoError(t, NewMigrator(*keepers.WasmKeeper).Migrate1to2(ctx))

	require.False(t, keepers.WasmKeeper.msgExecuteContractDisabled(ctx))
	_, err = keepers.GovKeeper.SubmitProposal(ctx, disable)
	require.NoError(t, err)
}
