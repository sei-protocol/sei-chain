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
	minter := k.GetMinter(ctx)

	// If the current yyyy-mm-dd of the epoch timestamp matches any of the scheduled token release then proceed to mint
	lastRelaseDate := k.GetLastTokenReleaseDate(ctx)
	scheduledTokenRelease := types.GetScheduledTokenRelease(epoch, lastRelaseDate, params.GetTokenReleaseSchedule())
	if scheduledTokenRelease == nil {
		ctx.Logger().Debug(fmt.Sprintf("No release at epoch time %s; last release %s", epoch.GetCurrentEpochStartTime().String(), lastRelaseDate.Format(types.TokenReleaseDateFormat)))
		return
	}

	minter.EpochProvisions = sdk.NewDec(scheduledTokenRelease.GetTokenReleaseAmount())
	k.SetMinter(ctx, minter)
	k.SetLastTokenReleaseDate(ctx, scheduledTokenRelease.GetDate())

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
		mintedCoins := float32(mintedCoin.Amount.Int64())
		ctx.Logger().Info(fmt.Sprintf("Minted %f at block time %s", mintedCoins, epoch.CurrentEpochStartTime.String()))
		defer telemetry.ModuleSetGauge(types.ModuleName, mintedCoins, "minted_tokens")
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
