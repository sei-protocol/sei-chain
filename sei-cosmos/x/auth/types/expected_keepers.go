package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// BankKeeper defines the contract needed for supply related APIs (noalias)
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx sdk.Context, senderAddr seitypes.AccAddress, recipientModule string, amt sdk.Coins) error
	DeferredSendCoinsFromAccountToModule(ctx sdk.Context, senderAddr seitypes.AccAddress, recipientModule string, amt sdk.Coins) error
}
