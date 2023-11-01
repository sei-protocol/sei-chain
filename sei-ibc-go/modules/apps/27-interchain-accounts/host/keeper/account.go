package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
)

// RegisterInterchainAccount attempts to create a new account using the provided address and
// stores it in state keyed by the provided connection and port identifiers
// If an account for the provided address already exists this function returns early (no-op)
// NOTE: This function is deprecated!
func (k Keeper) RegisterInterchainAccount(ctx sdk.Context, connectionID, controllerPortID string, accAddress sdk.AccAddress) {
	if acc := k.accountKeeper.GetAccount(ctx, accAddress); acc != nil {
		return
	}

	interchainAccount := icatypes.NewInterchainAccount(
		authtypes.NewBaseAccountWithAddress(accAddress),
		controllerPortID,
	)

	k.accountKeeper.NewAccount(ctx, interchainAccount)
	k.accountKeeper.SetAccount(ctx, interchainAccount)

	k.SetInterchainAccountAddress(ctx, connectionID, controllerPortID, interchainAccount.Address)
}

// createInterchainAccount creates a new interchain account. An address is generated using the host connectionID, the controller portID,
// and block dependent information. An error is returned if an account already exists for the generated account.
// An interchain account type is set in the account keeper and the interchain account address mapping is updated.
func (k Keeper) createInterchainAccount(ctx sdk.Context, connectionID, controllerPortID string) (sdk.AccAddress, error) {
	accAddress := icatypes.GenerateUniqueAddress(ctx, connectionID, controllerPortID)

	if acc := k.accountKeeper.GetAccount(ctx, accAddress); acc != nil {
		return nil, sdkerrors.Wrapf(icatypes.ErrAccountAlreadyExist, "existing account for newly generated interchain account address %s", accAddress)
	}

	interchainAccount := icatypes.NewInterchainAccount(
		authtypes.NewBaseAccountWithAddress(accAddress),
		controllerPortID,
	)

	k.accountKeeper.NewAccount(ctx, interchainAccount)
	k.accountKeeper.SetAccount(ctx, interchainAccount)

	k.SetInterchainAccountAddress(ctx, connectionID, controllerPortID, interchainAccount.Address)

	return accAddress, nil
}
