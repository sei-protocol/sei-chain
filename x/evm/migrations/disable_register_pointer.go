package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

func MigrateDisableRegisterPointer(ctx sdk.Context, k *keeper.Keeper) error {
	params := k.GetParams(ctx)
	params.RegisterPointerDisabled = true
	k.SetParams(ctx, params)
	return nil
}
