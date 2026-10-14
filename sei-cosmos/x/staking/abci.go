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

// backfillDelegationByValIndex indexes the next batch of delegations while the
// delegation-by-validator index is backfilled. It runs after every transaction in the
// block, so no transaction iterates the staking store while the batch's writes are
// pending in it. A delegation that cannot be decoded halts the chain, as it would at
// any other read.
func backfillDelegationByValIndex(ctx sdk.Context, k keeper.Keeper) {
	// The batch is block-level work, so it is not charged to any gas meter.
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1))
	if _, err := k.BackfillDelegationByValIndex(ctx, keeper.DelegationByValIndexBackfillBatchSize); err != nil {
		panic(err)
	}
}

// Called every block, update validator set. It also advances the
// delegation-by-validator index backfill while one is in progress.
func EndBlocker(ctx sdk.Context, k keeper.Keeper) []abci.ValidatorUpdate {
	endBlockerStart := time.Now()
	defer func() {
		stakingMetrics.endBlockerDuration.Record(ctx.Context(), time.Since(endBlockerStart).Seconds())
	}()

	updates := k.BlockValidatorUpdates(ctx)
	wasReady := k.DelegationByValIndexReady(ctx)
	backfillDelegationByValIndex(ctx, k)
	auditDelegationByValIndex(ctx, k, wasReady)
	return updates
}

// auditDelegationByValIndex audits the delegation-by-validator index on the block it
// becomes ready and every DelegationByValIndexAuditInterval blocks after. TEST ONLY.
func auditDelegationByValIndex(ctx sdk.Context, k keeper.Keeper, wasReady bool) {
	if !k.DelegationByValIndexReady(ctx) {
		return
	}
	if wasReady && ctx.BlockHeight()%keeper.DelegationByValIndexAuditInterval != 0 {
		return
	}
	k.LogDelegationByValIndexAudit(ctx)
}
