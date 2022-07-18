package keeper

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	epochTypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/sei-protocol/sei-chain/x/mint/types"
)

func (k Keeper) BeforeEpochStart(ctx sdk.Context, epoch epochTypes.Epoch) {
}

func (k Keeper) AfterEpochEnd(ctx sdk.Context, epoch epochTypes.Epoch) {
	params := k.GetParams(ctx)
	// Check if we have hit an epoch where we update the inflation parameter.
	// Since epochs only update based on BFT time data, it is safe to store the "halvening period time"
	// in terms of the number of epochs that have transpired
	// If it's not time to mint coins, exit
	if epoch.GetCurrentEpoch() < k.GetLastHalvenEpochNum(ctx)+uint64(params.ReductionPeriodInEpochs) {
		return
	}
	// Halven the reward per halven period
	minter := k.GetMinter(ctx)
	minter.EpochProvisions = minter.NextEpochProvisions(params)
	k.SetMinter(ctx, minter)
	k.SetLastHalvenEpochNum(ctx, epoch.GetCurrentEpoch())
	// mint coins, update supply
	mintedCoin := minter.EpochProvision(params)
	mintedCoins := sdk.NewCoins(mintedCoin)
	if err := k.MintCoins(ctx, mintedCoins); err != nil {
		panic(err)
	}
	// send the minted coins to the fee collector account

	if err := k.AddCollectedFees(ctx, mintedCoins); err != nil {
		panic(err)
	}

	if mintedCoin.Amount.IsInt64() {
		defer telemetry.ModuleSetGauge(types.ModuleName, float32(mintedCoin.Amount.Int64()), "minted_tokens")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeMint,
			sdk.NewAttribute(types.AttributeEpochNumber, fmt.Sprintf("%d", epoch.GetCurrentEpoch())),
			sdk.NewAttribute(types.AttributeKeyEpochProvisions, minter.EpochProvisions.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, mintedCoin.Amount.String()),
		),
	)
}

type Hooks struct {
	k Keeper
}

var _ epochTypes.EpochHooks = Hooks{}

// Return the wrapper struct.
func (k Keeper) Hooks() Hooks {
	return Hooks{k}
}

// epochs hooks.
func (h Hooks) BeforeEpochStart(ctx sdk.Context, epoch epochTypes.Epoch) {
	h.k.BeforeEpochStart(ctx, epoch)
}

func (h Hooks) AfterEpochEnd(ctx sdk.Context, epoch epochTypes.Epoch) {
	h.k.AfterEpochEnd(ctx, epoch)
}
