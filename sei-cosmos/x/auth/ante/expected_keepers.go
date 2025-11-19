package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// AccountKeeper defines the contract needed for AccountKeeper related APIs.
// Interface provides support to use non-sdk AccountKeeper for AnteHandler's decorators.
type AccountKeeper interface {
	GetParams(ctx sdk.Context) (params types.Params)
	GetAccount(ctx sdk.Context, addr seitypes.AccAddress) types.AccountI
	SetAccount(ctx sdk.Context, acc types.AccountI)
	GetModuleAddress(moduleName string) seitypes.AccAddress
}

// FeegrantKeeper defines the expected feegrant keeper.
type FeegrantKeeper interface {
	UseGrantedFees(ctx sdk.Context, granter, grantee seitypes.AccAddress, fee sdk.Coins, msgs []seitypes.Msg) error
}

// ParamKeeper defines the expected param keeper.
type ParamsKeeper interface {
	SetFeesParams(ctx sdk.Context, feesParams paramtypes.FeesParams)
	GetFeesParams(ctx sdk.Context) paramtypes.FeesParams
	SetCosmosGasParams(ctx sdk.Context, cosmosGasParams paramtypes.CosmosGasParams)
	GetCosmosGasParams(ctx sdk.Context) paramtypes.CosmosGasParams
}
