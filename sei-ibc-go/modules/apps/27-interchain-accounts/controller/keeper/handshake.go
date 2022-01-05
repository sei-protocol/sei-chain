package keeper

import (
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

	connSequence, err := icatypes.ParseControllerConnSequence(portID)
	if err != nil {
		return sdkerrors.Wrapf(err, "expected format %s, got %s", icatypes.ControllerPortFormat, portID)
	}

	counterpartyConnSequence, err := icatypes.ParseHostConnSequence(portID)
	if err != nil {
		return sdkerrors.Wrapf(err, "expected format %s, got %s", icatypes.ControllerPortFormat, portID)
	}

	if err := k.validateControllerPortParams(ctx, connectionHops, connSequence, counterpartyConnSequence); err != nil {
		return sdkerrors.Wrapf(err, "failed to validate controller port %s", portID)
	}

	if counterparty.PortId != icatypes.PortID {
		return sdkerrors.Wrapf(porttypes.ErrInvalidPort, "expected %s, got %s", icatypes.PortID, counterparty.PortId)
	}

	if version != icatypes.VersionPrefix {
		return sdkerrors.Wrapf(icatypes.ErrInvalidVersion, "expected %s, got %s", icatypes.VersionPrefix, version)
	}

	activeChannelID, found := k.GetActiveChannelID(ctx, portID)
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
		return sdkerrors.Wrapf(porttypes.ErrInvalidPort, "portID cannot be host chain port ID: %s", icatypes.PortID)
	}

	if err := icatypes.ValidateVersion(counterpartyVersion); err != nil {
		return sdkerrors.Wrap(err, "counterparty version validation failed")
	}

	k.SetActiveChannelID(ctx, portID, channelID)

	accAddr, err := icatypes.ParseAddressFromVersion(counterpartyVersion)
	if err != nil {
		return sdkerrors.Wrapf(err, "expected format <app-version%saccount-address>, got %s", icatypes.Delimiter, counterpartyVersion)
	}

	k.SetInterchainAccountAddress(ctx, portID, accAddr)

	return nil
}

// OnChanCloseConfirm removes the active channel stored in state
func (k Keeper) OnChanCloseConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {

	k.DeleteActiveChannelID(ctx, portID)

	return nil
}

// validateControllerPortParams asserts the provided connection sequence and counterparty connection sequence
// match that of the associated connection stored in state
func (k Keeper) validateControllerPortParams(ctx sdk.Context, connectionHops []string, connectionSeq, counterpartyConnectionSeq uint64) error {
	connectionID := connectionHops[0]
	connection, err := k.channelKeeper.GetConnection(ctx, connectionID)
	if err != nil {
		return err
	}

	connSeq, err := connectiontypes.ParseConnectionSequence(connectionID)
	if err != nil {
		return sdkerrors.Wrapf(err, "failed to parse connection sequence %s", connectionID)
	}

	counterpartyConnSeq, err := connectiontypes.ParseConnectionSequence(connection.GetCounterparty().GetConnectionID())
	if err != nil {
		return sdkerrors.Wrapf(err, "failed to parse counterparty connection sequence %s", connection.GetCounterparty().GetConnectionID())
	}

	if connSeq != connectionSeq {
		return sdkerrors.Wrapf(connectiontypes.ErrInvalidConnection, "sequence mismatch, expected %d, got %d", connSeq, connectionSeq)
	}

	if counterpartyConnSeq != counterpartyConnectionSeq {
		return sdkerrors.Wrapf(connectiontypes.ErrInvalidConnection, "counterparty sequence mismatch, expected %d, got %d", counterpartyConnSeq, counterpartyConnectionSeq)
	}

	return nil
}
