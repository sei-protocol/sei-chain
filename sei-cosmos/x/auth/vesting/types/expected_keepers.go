package types

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// BankKeeper defines the expected interface contract the vesting module requires
// for creating vesting accounts with funds.
type BankKeeper interface {
	IsSendEnabledCoins(ctx sdk.Context, coins ...sdk.Coin) error
	SendCoins(ctx sdk.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error
	BlockedAddr(addr sdk.AccAddress) bool
}

// UpgradeKeeper defines the expected interface contract the vesting module
// requires for checking whether the deprecation upgrade has executed.
type UpgradeKeeper interface {
	IsUpgradeActiveAtHeight(ctx sdk.Context, name string, height int64) bool
}
