package distribution

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// BeginBlocker sets the proposer for determining distribution during endblock
// and distribute rewards for the previous block
func BeginBlocker(ctx sdk.Context, votes []abci.VoteInfo, k keeper.Keeper) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	// determine the total power signing the block
	var previousTotalPower, sumPreviousPrecommitPower int64
	for _, voteInfo := range votes {
		previousTotalPower += voteInfo.Validator.Power
		if voteInfo.SignedLastBlock {
			sumPreviousPrecommitPower += voteInfo.Validator.Power
		}
	}

	// TODO this is Tendermint-dependent
	// ref https://github.com/cosmos/cosmos-sdk/issues/3095
	if ctx.BlockHeight() > 1 {
		previousProposer := k.GetPreviousProposerConsAddr(ctx)
		k.AllocateTokens(ctx, sumPreviousPrecommitPower, previousTotalPower, previousProposer, votes)
	}

	// record the proposer for when we payout on the next block
	consAddr := sdk.ConsAddress(ctx.BlockHeader().ProposerAddress)
	k.SetPreviousProposerConsAddr(ctx, consAddr)
}
