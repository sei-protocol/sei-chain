package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper defines the contract needed for supply related APIs (noalias)
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	DeferredSendCoinsFromAccountToModule(ctx sdk.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	DeferredSendCoinsFromModuleToAccount(ctx sdk.Context, senderModule string, recipientAddr sdk.AccAddress, amount sdk.Coins) error
	DeferredMintCoins(ctx sdk.Context, moduleName string, amt sdk.Coins) error
	DeferredBurnCoins(ctx sdk.Context, moduleName string, amt sdk.Coins) error
}
