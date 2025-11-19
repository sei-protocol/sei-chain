package authz

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// AccountKeeper defines the expected account keeper (noalias)
type AccountKeeper interface {
	GetAccount(ctx sdk.Context, addr seitypes.AccAddress) authtypes.AccountI
}

// BankKeeper defines the expected interface needed to retrieve account balances.
type BankKeeper interface {
	SpendableCoins(ctx sdk.Context, addr seitypes.AccAddress) sdk.Coins
	IsSendEnabledCoins(ctx sdk.Context, coins ...sdk.Coin) error
}
