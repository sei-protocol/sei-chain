package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
)

// RegisterInterchainAccount attempts to create a new account using the provided address and stores it in state keyed by the provided port identifier
// If an account for the provided address already exists this function returns early (no-op)
func (k Keeper) RegisterInterchainAccount(ctx sdk.Context, accAddr sdk.AccAddress, controllerPortID string) {
	if acc := k.accountKeeper.GetAccount(ctx, accAddr); acc != nil {
		return
	}

	interchainAccount := icatypes.NewInterchainAccount(
		authtypes.NewBaseAccountWithAddress(accAddr),
		controllerPortID,
	)

	k.accountKeeper.NewAccount(ctx, interchainAccount)
	k.accountKeeper.SetAccount(ctx, interchainAccount)

	k.SetInterchainAccountAddress(ctx, controllerPortID, interchainAccount.Address)
}
