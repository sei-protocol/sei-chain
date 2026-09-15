package staking

import (
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// BeginBlocker will persist the current header and validator set as a historical entry
// and prune the oldest entry based on the HistoricalEntries parameter
func BeginBlocker(ctx sdk.Context, k keeper.Keeper) {
	beginBlockerStart := time.Now()
	defer func() {
		stakingMetrics.beginBlockerDuration.Record(ctx.Context(), time.Since(beginBlockerStart).Seconds())
	}()

	k.TrackHistoricalInfo(ctx)
}

// Called every block, update validator set
func EndBlocker(ctx sdk.Context, k keeper.Keeper) []abci.ValidatorUpdate {
	endBlockerStart := time.Now()
	defer func() {
		stakingMetrics.endBlockerDuration.Record(ctx.Context(), time.Since(endBlockerStart).Seconds())
	}()

	return k.BlockValidatorUpdates(ctx)
}
