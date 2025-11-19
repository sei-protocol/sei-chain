package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// AccountKeeper defines the account contract that must be fulfilled when
// creating a x/bank keeper.
type AccountKeeper interface {
	NewAccount(sdk.Context, types.AccountI) types.AccountI
	NewAccountWithAddress(ctx sdk.Context, addr seitypes.AccAddress) types.AccountI

	GetAccount(ctx sdk.Context, addr seitypes.AccAddress) types.AccountI
	GetAllAccounts(ctx sdk.Context) []types.AccountI
	HasAccount(ctx sdk.Context, addr seitypes.AccAddress) bool
	SetAccount(ctx sdk.Context, acc types.AccountI)

	IterateAccounts(ctx sdk.Context, process func(types.AccountI) bool)

	ValidatePermissions(macc types.ModuleAccountI) error

	GetModuleAddress(moduleName string) seitypes.AccAddress
	GetModuleAddressAndPermissions(moduleName string) (addr seitypes.AccAddress, permissions []string)
	GetModuleAccountAndPermissions(ctx sdk.Context, moduleName string) (types.ModuleAccountI, []string)
	GetModuleAccount(ctx sdk.Context, moduleName string) types.ModuleAccountI
	SetModuleAccount(ctx sdk.Context, macc types.ModuleAccountI)
}
