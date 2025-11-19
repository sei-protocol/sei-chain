package feegrant

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	auth "github.com/cosmos/cosmos-sdk/x/auth/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// AccountKeeper defines the expected auth Account Keeper (noalias)
type AccountKeeper interface {
	GetModuleAddress(moduleName string) seitypes.AccAddress
	GetModuleAccount(ctx sdk.Context, moduleName string) auth.ModuleAccountI

	NewAccountWithAddress(ctx sdk.Context, addr seitypes.AccAddress) auth.AccountI
	GetAccount(ctx sdk.Context, addr seitypes.AccAddress) auth.AccountI
	SetAccount(ctx sdk.Context, acc auth.AccountI)
}

// BankKeeper defines the expected supply Keeper (noalias)
type BankKeeper interface {
	SpendableCoins(ctx sdk.Context, addr seitypes.AccAddress) sdk.Coins
	SendCoinsFromAccountToModule(ctx sdk.Context, senderAddr seitypes.AccAddress, recipientModule string, amt sdk.Coins) error
}
