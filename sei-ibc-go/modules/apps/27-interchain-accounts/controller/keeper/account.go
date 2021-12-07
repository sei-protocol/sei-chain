package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	icatypes "github.com/cosmos/ibc-go/v2/modules/apps/27-interchain-accounts/types"
	channeltypes "github.com/cosmos/ibc-go/v2/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v2/modules/core/24-host"
)

// InitInterchainAccount is the entry point to registering an interchain account.
// It generates a new port identifier using the owner address, connection identifier,
// and counterparty connection identifier. It will bind to the port identifier and
// call 04-channel 'ChanOpenInit'. An error is returned if the port identifier is
// already in use. Gaining access to interchain accounts whose channels have closed
// cannot be done with this function. A regular MsgChanOpenInit must be used.
func (k Keeper) InitInterchainAccount(ctx sdk.Context, connectionID, counterpartyConnectionID, owner string) error {
	portID, err := icatypes.GeneratePortID(owner, connectionID, counterpartyConnectionID)
	if err != nil {
		return err
	}

	if k.portKeeper.IsBound(ctx, portID) {
		return sdkerrors.Wrap(icatypes.ErrPortAlreadyBound, portID)
	}

	cap := k.BindPort(ctx, portID)
	if err := k.ClaimCapability(ctx, cap, host.PortPath(portID)); err != nil {
		return sdkerrors.Wrap(err, "unable to bind to newly generated portID")
	}

	msg := channeltypes.NewMsgChannelOpenInit(portID, icatypes.VersionPrefix, channeltypes.ORDERED, []string{connectionID}, icatypes.PortID, icatypes.ModuleName)
	handler := k.msgRouter.Handler(msg)
	if _, err := handler(ctx, msg); err != nil {
		return err
	}

	return nil
}
