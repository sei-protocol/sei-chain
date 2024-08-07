package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func V11ToV12(ctx sdk.Context, dexkeeper keeper.Keeper) error {
	defaultParams := types.DefaultParams()
	dexkeeper.SetParams(ctx, defaultParams)
	return nil
}
