package keeper

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("x", "epoch", "keeper")

func (k Keeper) BeginBlock(ctx sdk.Context) {
	start := time.Now()
	defer func() {
		epochMetrics.beginBlockerDuration.Record(ctx.Context(), time.Since(start).Seconds())
		// TODO(PLT-336): remove once epoch_begin_blocker_duration_seconds verified
		telemetry.ModuleMeasureSince(types.ModuleName, start, telemetry.MetricKeyBeginBlocker)
	}()
	lastEpoch := k.GetEpoch(ctx)
	logger.Debug(" Block time", "current", ctx.BlockTime(), "last", lastEpoch.CurrentEpochStartTime, "epoch-duration", lastEpoch.EpochDuration)

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

		epochMetrics.epochNew.Record(ctx.Context(), int64(newEpoch.CurrentEpoch)) //nolint:gosec
		// TODO(PLT-336): remove once epoch_new verified
		metrics.SetEpochNew(newEpoch.CurrentEpoch)
	}
}
