package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

type BankKeeper interface {
	// Methods imported from bank should be defined here
	GetDenomMetaData(ctx sdk.Context, denom string) (banktypes.Metadata, bool)
	SetDenomMetaData(ctx sdk.Context, denomMetaData banktypes.Metadata)

	SetDenomAllowList(ctx sdk.Context, denom string, allowList banktypes.AllowList)
	GetDenomAllowList(ctx sdk.Context, denom string) banktypes.AllowList

	HasSupply(ctx sdk.Context, denom string) bool
	IterateTotalSupply(ctx sdk.Context, cb func(sdk.Coin) bool)

	SendCoinsFromModuleToAccount(ctx sdk.Context, senderModule string, recipientAddr seitypes.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx sdk.Context, senderAddr seitypes.AccAddress, recipientModule string, amt sdk.Coins) error

	DeferredSendCoinsFromAccountToModule(ctx sdk.Context, senderAddr seitypes.AccAddress, recipientModule string, amt sdk.Coins) error

	DelegateCoinsFromAccountToModule(ctx sdk.Context, senderAddr seitypes.AccAddress, recipientModule string, amt sdk.Coins) error
	UndelegateCoinsFromModuleToAccount(ctx sdk.Context, senderModule string, recipientAddr seitypes.AccAddress, amt sdk.Coins) error
	MintCoins(ctx sdk.Context, moduleName string, amt sdk.Coins) error
	BurnCoins(ctx sdk.Context, moduleName string, amt sdk.Coins) error

	SendCoins(ctx sdk.Context, fromAddr seitypes.AccAddress, toAddr seitypes.AccAddress, amt sdk.Coins) error
}

type AccountKeeper interface {
	GetModuleAddress(name string) seitypes.AccAddress
	SetModuleAccount(ctx sdk.Context, macc authtypes.ModuleAccountI)
	GetAccount(sdk.Context, seitypes.AccAddress) authtypes.AccountI
}

// DistrKeeper defines the contract needed to be fulfilled for distribution keeper.
type DistrKeeper interface {
	FundCommunityPool(ctx sdk.Context, amount sdk.Coins, sender seitypes.AccAddress) error
}
