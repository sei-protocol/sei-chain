package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// SupplyKeeper defines the expected supply keeper (noalias)
type SupplyKeeper interface {
	SendCoinsFromAccountToModule(ctx sdk.Context, senderAddr seitypes.AccAddress, recipientModule string, amt sdk.Coins) error
}
