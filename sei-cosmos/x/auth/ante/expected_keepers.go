package ante

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
)

// AccountKeeper defines the contract needed for AccountKeeper related APIs.
// Interface provides support to use non-sdk AccountKeeper for AnteHandler's decorators.
type AccountKeeper interface {
	GetParams(ctx sdk.Context) (params types.Params)
	GetAccount(ctx sdk.Context, addr sdk.AccAddress) types.AccountI
	SetAccount(ctx sdk.Context, acc types.AccountI)
	GetModuleAddress(moduleName string) sdk.AccAddress
}

// ParamKeeper defines the expected param keeper.
type ParamsKeeper interface {
	SetFeesParams(ctx sdk.Context, feesParams paramtypes.FeesParams)
	GetFeesParams(ctx sdk.Context) paramtypes.FeesParams
	SetCosmosGasParams(ctx sdk.Context, cosmosGasParams paramtypes.CosmosGasParams)
	GetCosmosGasParams(ctx sdk.Context) paramtypes.CosmosGasParams
}
