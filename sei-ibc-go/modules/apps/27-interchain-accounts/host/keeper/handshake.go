package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v3/modules/core/05-port/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
)

// OnChanOpenTry performs basic validation of the ICA channel
// and registers a new interchain account (if it doesn't exist).
func (k Keeper) OnChanOpenTry(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID,
	channelID string,
	chanCap *capabilitytypes.Capability,
	counterparty channeltypes.Counterparty,
	version,
	counterpartyVersion string,
) error {
	if order != channeltypes.ORDERED {
		return sdkerrors.Wrapf(channeltypes.ErrInvalidChannelOrdering, "expected %s channel, got %s", channeltypes.ORDERED, order)
	}

	if portID != icatypes.PortID {
		return sdkerrors.Wrapf(porttypes.ErrInvalidPort, "expected %s, got %s", icatypes.PortID, portID)
	}

	connSequence, err := icatypes.ParseHostConnSequence(counterparty.PortId)
	if err != nil {
		return sdkerrors.Wrapf(err, "expected format %s, got %s", icatypes.ControllerPortFormat, counterparty.PortId)
	}

	counterpartyConnSequence, err := icatypes.ParseControllerConnSequence(counterparty.PortId)
	if err != nil {
		return sdkerrors.Wrapf(err, "expected format %s, got %s", icatypes.ControllerPortFormat, counterparty.PortId)
	}

	if err := k.validateControllerPortParams(ctx, channelID, portID, connSequence, counterpartyConnSequence); err != nil {
		return sdkerrors.Wrapf(err, "failed to validate controller port %s", counterparty.PortId)
	}

	if err := icatypes.ValidateVersion(version); err != nil {
		return sdkerrors.Wrap(err, "version validation failed")
	}

	if counterpartyVersion != icatypes.VersionPrefix {
		return sdkerrors.Wrapf(icatypes.ErrInvalidVersion, "expected %s, got %s", icatypes.VersionPrefix, version)
	}

	// On the host chain the capability may only be claimed during the OnChanOpenTry
	// The capability being claimed in OpenInit is for a controller chain (the port is different)
	if err := k.ClaimCapability(ctx, chanCap, host.ChannelCapabilityPath(portID, channelID)); err != nil {
		return sdkerrors.Wrapf(err, "failed to claim capability for channel %s on port %s", channelID, portID)
	}

	// Check to ensure that the version string contains the expected address generated from the Counterparty portID
	accAddr := icatypes.GenerateAddress(k.accountKeeper.GetModuleAddress(icatypes.ModuleName), counterparty.PortId)
	parsedAddr, err := icatypes.ParseAddressFromVersion(version)
	if err != nil {
		return sdkerrors.Wrapf(err, "expected format <app-version%saccount-address>, got %s", icatypes.Delimiter, version)
	}

	if parsedAddr != accAddr.String() {
		return sdkerrors.Wrapf(icatypes.ErrInvalidVersion, "version contains invalid account address: expected %s, got %s", parsedAddr, accAddr)
	}

	// Register interchain account if it does not already exist
	k.RegisterInterchainAccount(ctx, accAddr, counterparty.PortId)

	return nil
}

// OnChanOpenConfirm completes the handshake process by setting the active channel in state on the host chain
func (k Keeper) OnChanOpenConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {

	k.SetActiveChannelID(ctx, portID, channelID)

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
func (k Keeper) validateControllerPortParams(ctx sdk.Context, channelID, portID string, connectionSeq, counterpartyConnectionSeq uint64) error {
	connectionID, connection, err := k.channelKeeper.GetChannelConnection(ctx, portID, channelID)
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
