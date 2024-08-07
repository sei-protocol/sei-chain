package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func V14ToV15(ctx sdk.Context, dexkeeper keeper.Keeper) error {
	// This isn't the cleanest migration since it could potentially revert any dex params we have changed
	// but we haven't, so we'll just do this.
	defaultParams := types.DefaultParams()
	dexkeeper.SetParams(ctx, defaultParams)
	return nil
}
