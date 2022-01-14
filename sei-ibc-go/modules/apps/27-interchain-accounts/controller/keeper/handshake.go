package keeper

import (
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v3/modules/core/05-port/types"
)

// OnChanOpenInit performs basic validation of channel initialization.
// The channel order must be ORDERED, the counterparty port identifier
// must be the host chain representation as defined in the types package,
// the channel version must be equal to the version in the types package,
// there must not be an active channel for the specfied port identifier,
// and the interchain accounts module must be able to claim the channel
// capability.
func (k Keeper) OnChanOpenInit(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID string,
	channelID string,
	chanCap *capabilitytypes.Capability,
	counterparty channeltypes.Counterparty,
	version string,
) error {
	if order != channeltypes.ORDERED {
		return sdkerrors.Wrapf(channeltypes.ErrInvalidChannelOrdering, "expected %s channel, got %s", channeltypes.ORDERED, order)
	}

	if !strings.HasPrefix(portID, icatypes.PortPrefix) {
		return sdkerrors.Wrapf(icatypes.ErrInvalidControllerPort, "expected %s{owner-account-address}, got %s", icatypes.PortPrefix, portID)
	}

	if counterparty.PortId != icatypes.PortID {
		return sdkerrors.Wrapf(icatypes.ErrInvalidHostPort, "expected %s, got %s", icatypes.PortID, counterparty.PortId)
	}

	var metadata icatypes.Metadata
	if err := icatypes.ModuleCdc.UnmarshalJSON([]byte(version), &metadata); err != nil {
		return sdkerrors.Wrapf(icatypes.ErrUnknownDataType, "cannot unmarshal ICS-27 interchain accounts metadata")
	}

	if err := k.validateConnectionParams(ctx, connectionHops, metadata.ControllerConnectionId, metadata.HostConnectionId); err != nil {
		return err
	}

	if metadata.Version != icatypes.Version {
		return sdkerrors.Wrapf(icatypes.ErrInvalidVersion, "expected %s, got %s", icatypes.Version, metadata.Version)
	}

	activeChannelID, found := k.GetOpenActiveChannel(ctx, portID)
	if found {
		return sdkerrors.Wrapf(porttypes.ErrInvalidPort, "existing active channel %s for portID %s", activeChannelID, portID)
	}

	return nil
}

// OnChanOpenAck sets the active channel for the interchain account/owner pair
// and stores the associated interchain account address in state keyed by it's corresponding port identifier
func (k Keeper) OnChanOpenAck(
	ctx sdk.Context,
	portID,
	channelID string,
	counterpartyVersion string,
) error {
	if portID == icatypes.PortID {
		return sdkerrors.Wrapf(icatypes.ErrInvalidControllerPort, "portID cannot be host chain port ID: %s", icatypes.PortID)
	}

	if !strings.HasPrefix(portID, icatypes.PortPrefix) {
		return sdkerrors.Wrapf(icatypes.ErrInvalidControllerPort, "expected %s{owner-account-address}, got %s", icatypes.PortPrefix, portID)
	}

	var metadata icatypes.Metadata
	if err := icatypes.ModuleCdc.UnmarshalJSON([]byte(counterpartyVersion), &metadata); err != nil {
		return sdkerrors.Wrapf(icatypes.ErrUnknownDataType, "cannot unmarshal ICS-27 interchain accounts metadata")
	}

	if err := icatypes.ValidateAccountAddress(metadata.Address); err != nil {
		return err
	}

	if metadata.Version != icatypes.Version {
		return sdkerrors.Wrapf(icatypes.ErrInvalidVersion, "expected %s, got %s", icatypes.Version, metadata.Version)
	}

	k.SetActiveChannelID(ctx, portID, channelID)
	k.SetInterchainAccountAddress(ctx, portID, metadata.Address)

	return nil
}

// OnChanCloseConfirm removes the active channel stored in state
func (k Keeper) OnChanCloseConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {

	return nil
}

// validateConnectionParams asserts the provided controller and host connection identifiers match that of the associated connection stored in state
func (k Keeper) validateConnectionParams(ctx sdk.Context, connectionHops []string, controllerConnectionID, hostConnectionID string) error {
	connectionID := connectionHops[0]
	connection, err := k.channelKeeper.GetConnection(ctx, connectionID)
	if err != nil {
		return err
	}

	if controllerConnectionID != connectionID {
		return sdkerrors.Wrapf(connectiontypes.ErrInvalidConnection, "expected %s, got %s", connectionID, controllerConnectionID)
	}

	if hostConnectionID != connection.GetCounterparty().GetConnectionID() {
		return sdkerrors.Wrapf(connectiontypes.ErrInvalidConnection, "expected %s, got %s", connection.GetCounterparty().GetConnectionID(), hostConnectionID)
	}

	return nil
}
