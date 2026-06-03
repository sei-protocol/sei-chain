package staking

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// BeginBlocker will persist the current header and validator set as a historical entry
// and prune the oldest entry based on the HistoricalEntries parameter
func BeginBlocker(ctx sdk.Context, k keeper.Keeper) {
	beginBlockerStart := time.Now()
	defer func() {
		stakingMetrics.beginBlockerDuration.Record(ctx.Context(), time.Since(beginBlockerStart).Seconds())
		// TODO(PLT-414): remove once staking_begin_blocker_duration verified
		telemetry.ModuleMeasureSince(types.ModuleName, beginBlockerStart, telemetry.MetricKeyBeginBlocker)
	}()

	k.TrackHistoricalInfo(ctx)
}

// Called every block, update validator set
func EndBlocker(ctx sdk.Context, k keeper.Keeper) []abci.ValidatorUpdate {
	endBlockerStart := time.Now()
	defer func() {
		stakingMetrics.endBlockerDuration.Record(ctx.Context(), time.Since(endBlockerStart).Seconds())
		// TODO(PLT-414): remove once staking_end_blocker_duration verified
		telemetry.ModuleMeasureSince(types.ModuleName, endBlockerStart, telemetry.MetricKeyEndBlocker)
	}()

	return k.BlockValidatorUpdates(ctx)
}
