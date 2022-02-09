package keeper

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
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

	if err := icatypes.ValidateHostMetadata(ctx, k.channelKeeper, connectionHops, metadata); err != nil {
		return "", err
	}

	activeChannelID, found := k.GetActiveChannelID(ctx, connectionHops[0], counterparty.PortId)
	if found {
		channel, found := k.channelKeeper.GetChannel(ctx, portID, activeChannelID)
		if !found {
			panic(fmt.Sprintf("active channel mapping set for %s but channel does not exist in channel store", activeChannelID))
		}

		if channel.State == channeltypes.OPEN {
			return "", sdkerrors.Wrapf(icatypes.ErrActiveChannelAlreadySet, "existing active channel %s for portID %s is already OPEN", activeChannelID, portID)
		}

		if !icatypes.IsPreviousMetadataEqual(channel.Version, metadata) {
			return "", sdkerrors.Wrap(icatypes.ErrInvalidVersion, "previous active channel metadata does not match provided version")
		}
	}

	// On the host chain the capability may only be claimed during the OnChanOpenTry
	// The capability being claimed in OpenInit is for a controller chain (the port is different)
	if err := k.ClaimCapability(ctx, chanCap, host.ChannelCapabilityPath(portID, channelID)); err != nil {
		return "", sdkerrors.Wrapf(err, "failed to claim capability for channel %s on port %s", channelID, portID)
	}

	accAddress := icatypes.GenerateAddress(k.accountKeeper.GetModuleAddress(icatypes.ModuleName), metadata.HostConnectionId, counterparty.PortId)

	// Register interchain account if it does not already exist
	k.RegisterInterchainAccount(ctx, metadata.HostConnectionId, counterparty.PortId, accAddress)

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
	channel, found := k.channelKeeper.GetChannel(ctx, portID, channelID)
	if !found {
		return sdkerrors.Wrapf(channeltypes.ErrChannelNotFound, "failed to retrieve channel %s on port %s", channelID, portID)
	}

	// It is assumed the controller chain will not allow multiple active channels to be created for the same connectionID/portID
	// If the controller chain does allow multiple active channels to be created for the same connectionID/portID,
	// disallowing overwriting the current active channel guarantees the channel can no longer be used as the controller
	// and host will disagree on what the currently active channel is
	k.SetActiveChannelID(ctx, channel.ConnectionHops[0], channel.Counterparty.PortId, channelID)

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
