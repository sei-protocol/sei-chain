package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

func (k Keeper) BeforeEpochStart(ctx sdk.Context, epoch types.Epoch) {
}

func (k Keeper) AfterEpochEnd(ctx sdk.Context, epoch types.Epoch) {
	params := k.GetParams(ctx)
	// Check if we have hit an epoch where we update the inflation parameter.
	// Since epochs only update based on BFT time data, it is safe to store the "halvening period time"
	// in terms of the number of epochs that have transpired
	if epoch.GetCurrentEpoch() >= uint64(params.ReductionPeriodInEpochs)+k.GetLastHalvenEpochNum(ctx) {
		minter := k.GetMinter(ctx)
		// Halven the reward per halven period
		minter.EpochProvisions = minter.NextEpochProvisions(params)

	}

}
