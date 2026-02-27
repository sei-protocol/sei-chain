package keeper

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

func (k Keeper) BeginBlock(ctx sdk.Context) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)
	lastEpoch := k.GetEpoch(ctx)
	ctx.Logger().Info(fmt.Sprintf("Current block time %s, last %s; duration %d", ctx.BlockTime().String(), lastEpoch.CurrentEpochStartTime.String(), lastEpoch.EpochDuration))

	if ctx.BlockTime().Sub(lastEpoch.CurrentEpochStartTime) > lastEpoch.EpochDuration {
		k.AfterEpochEnd(ctx, lastEpoch)

		newEpoch := types.Epoch{
			GenesisTime:           lastEpoch.GenesisTime,
			EpochDuration:         lastEpoch.EpochDuration,
			CurrentEpoch:          lastEpoch.CurrentEpoch + 1,
			CurrentEpochStartTime: ctx.BlockTime(),
			CurrentEpochHeight:    ctx.BlockHeight(),
		}
		k.SetEpoch(ctx, newEpoch)
		k.BeforeEpochStart(ctx, newEpoch)

		ctx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeNewEpoch,
				sdk.NewAttribute(types.AttributeEpochNumber, fmt.Sprint(newEpoch.CurrentEpoch)),
				sdk.NewAttribute(types.AttributeEpochTime, newEpoch.CurrentEpochStartTime.String()),
				sdk.NewAttribute(types.AttributeEpochHeight, fmt.Sprint(newEpoch.CurrentEpochHeight)),
			),
		)

		metrics.SetEpochNew(newEpoch.CurrentEpoch)
	}
}
