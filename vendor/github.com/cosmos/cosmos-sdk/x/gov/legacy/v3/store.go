package v3

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/gov/types"
)

// If expedited, the deposit to enter voting period will be increased to 5000 usei.
// The expedited proposal will have 24 hours to achieve
// a two-thirds quorum of all voting power participation
// a two-thirds majority of all staked voting power voting YES.
var (
	MinExpeditedDeposit   = sdk.NewCoins(sdk.NewCoin("usei", types.DefaultMinExpeditedDepositTokens))
	ExpeditedVotingPeriod = types.DefaultExpeditedPeriod
	ExpeditedQuorum       = types.DefaultExpeditedQuorum
	ExpeditedThreshold    = types.DefaultExpeditedThreshold
)

// MigrateStore performs in-place store migrations for consensus version 4 in the gov module.
// The migration includes: Setting the expedited proposals params in the paramstore.
func MigrateStore(ctx sdk.Context, paramstore types.ParamSubspace) error {
	migrateParamsStore(ctx, paramstore)
	println("Finished expedited gov parameter migration")
	return nil
}

func migrateParamsStore(ctx sdk.Context, paramstore types.ParamSubspace) {
	var (
		depositParams types.DepositParams
		votingParams  types.VotingParams
		tallyParams   types.TallyParams
	)

	// Set depositParams
	paramstore.Get(ctx, types.ParamStoreKeyDepositParams, &depositParams)
	depositParams.MinExpeditedDeposit = MinExpeditedDeposit
	paramstore.Set(ctx, types.ParamStoreKeyDepositParams, depositParams)

	// Set votingParams
	paramstore.Get(ctx, types.ParamStoreKeyVotingParams, &votingParams)
	votingParams.ExpeditedVotingPeriod = ExpeditedVotingPeriod
	paramstore.Set(ctx, types.ParamStoreKeyVotingParams, votingParams)

	// Set tallyParams
	paramstore.Get(ctx, types.ParamStoreKeyTallyParams, &tallyParams)
	tallyParams.ExpeditedQuorum = ExpeditedQuorum
	tallyParams.ExpeditedThreshold = ExpeditedThreshold
	paramstore.Set(ctx, types.ParamStoreKeyTallyParams, tallyParams)
}
