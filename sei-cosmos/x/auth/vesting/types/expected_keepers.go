package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// BankKeeper defines the expected interface contract the vesting module requires
// for creating vesting accounts with funds.
type BankKeeper interface {
	IsSendEnabledCoins(ctx sdk.Context, coins ...sdk.Coin) error
	SendCoins(ctx sdk.Context, fromAddr seitypes.AccAddress, toAddr seitypes.AccAddress, amt sdk.Coins) error
	BlockedAddr(addr seitypes.AccAddress) bool
}
