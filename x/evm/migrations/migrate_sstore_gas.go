package migrations

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// MigrateSstoreGas updates the SeiSstoreSetGasEip2200 parameter to the default value.
func MigrateSstoreGas(ctx sdk.Context, k *keeper.Keeper) error {
	params := k.GetParams(ctx)
	params.SeiSstoreSetGasEip2200 = types.DefaultSeiSstoreSetGasEIP2200
	k.SetParams(ctx, params)
	return nil
}
