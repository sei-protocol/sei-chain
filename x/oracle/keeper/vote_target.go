package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

func (k Keeper) IsVoteTarget(ctx sdk.Context, denom string) bool {
	_, err := k.GetVoteTarget(ctx, denom)
	return err == nil
}

func (k Keeper) GetVoteTargets(ctx sdk.Context) (voteTargets []string) {
	k.IterateVoteTargets(ctx, func(denom string, denomInfo types.Denom) bool {
		voteTargets = append(voteTargets, denom)
		return false
	})

	return voteTargets
}
