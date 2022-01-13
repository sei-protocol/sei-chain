package keeper

import (
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
)

// OnChanOpenTry performs basic validation of the ICA channel
// and registers a new interchain account (if it doesn't exist).
// The version returned will include the registered interchain
// account address.
func (k Keeper) OnChanOpenTry(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID,
	channelID string,
	chanCap *capabilitytypes.Capability,
	counterparty channeltypes.Counterparty,
	counterpartyVersion string,
) (string, error) {
	if order != channeltypes.ORDERED {
		return "", sdkerrors.Wrapf(channeltypes.ErrInvalidChannelOrdering, "expected %s channel, got %s", channeltypes.ORDERED, order)
	}

	if portID != icatypes.PortID {
		return "", sdkerrors.Wrapf(icatypes.ErrInvalidHostPort, "expected %s, got %s", icatypes.PortID, portID)
	}

	if !strings.HasPrefix(counterparty.PortId, icatypes.PortPrefix) {
		return "", sdkerrors.Wrapf(icatypes.ErrInvalidControllerPort, "expected %s{owner-account-address}, got %s", icatypes.PortPrefix, counterparty.PortId)
	}

	var metadata icatypes.Metadata
	if err := icatypes.ModuleCdc.UnmarshalJSON([]byte(counterpartyVersion), &metadata); err != nil {
		return "", sdkerrors.Wrapf(icatypes.ErrUnknownDataType, "cannot unmarshal ICS-27 interchain accounts metadata")
	}

	if err := k.validateConnectionParams(ctx, connectionHops, metadata.ControllerConnectionId, metadata.HostConnectionId); err != nil {
		return "", err
	}

	if metadata.Version != icatypes.Version {
		return "", sdkerrors.Wrapf(icatypes.ErrInvalidVersion, "expected %s, got %s", icatypes.Version, metadata.Version)
	}

	// On the host chain the capability may only be claimed during the OnChanOpenTry
	// The capability being claimed in OpenInit is for a controller chain (the port is different)
	if err := k.ClaimCapability(ctx, chanCap, host.ChannelCapabilityPath(portID, channelID)); err != nil {
		return "", sdkerrors.Wrapf(err, "failed to claim capability for channel %s on port %s", channelID, portID)
	}

	accAddress := icatypes.GenerateAddress(k.accountKeeper.GetModuleAddress(icatypes.ModuleName), counterparty.PortId)

	// Register interchain account if it does not already exist
	k.RegisterInterchainAccount(ctx, accAddress, counterparty.PortId)

	metadata.Address = accAddress.String()
	versionBytes, err := icatypes.ModuleCdc.MarshalJSON(&metadata)
	if err != nil {
		return "", err
	}

	return string(versionBytes), nil
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

// validateConnectionParams asserts the provided controller and host connection identifiers match that of the associated connection stored in state
func (k Keeper) validateConnectionParams(ctx sdk.Context, connectionHops []string, controllerConnectionID, hostConnectionID string) error {
	connectionID := connectionHops[0]
	connection, err := k.channelKeeper.GetConnection(ctx, connectionID)
	if err != nil {
		return err
	}

	if hostConnectionID != connectionID {
		return sdkerrors.Wrapf(connectiontypes.ErrInvalidConnection, "expected %s, got %s", connectionID, controllerConnectionID)
	}

	if controllerConnectionID != connection.GetCounterparty().GetConnectionID() {
		return sdkerrors.Wrapf(connectiontypes.ErrInvalidConnection, "expected %s, got %s", connection.GetCounterparty().GetConnectionID(), hostConnectionID)
	}

	return nil
}
