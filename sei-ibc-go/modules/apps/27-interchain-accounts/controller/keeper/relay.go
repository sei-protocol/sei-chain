package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
)

// TrySendTx takes in a transaction from an authentication module and attempts to send the packet
// if the base application has the capability to send on the provided portID
func (k Keeper) TrySendTx(ctx sdk.Context, chanCap *capabilitytypes.Capability, portID string, icaPacketData icatypes.InterchainAccountPacketData) (uint64, error) {
	// Check for the active channel
	activeChannelID, found := k.GetActiveChannelID(ctx, portID)
	if !found {
		return 0, sdkerrors.Wrapf(icatypes.ErrActiveChannelNotFound, "failed to retrieve active channel for port %s", portID)
	}

	sourceChannelEnd, found := k.channelKeeper.GetChannel(ctx, portID, activeChannelID)
	if !found {
		return 0, sdkerrors.Wrap(channeltypes.ErrChannelNotFound, activeChannelID)
	}

	destinationPort := sourceChannelEnd.GetCounterparty().GetPortID()
	destinationChannel := sourceChannelEnd.GetCounterparty().GetChannelID()

	return k.createOutgoingPacket(ctx, portID, activeChannelID, destinationPort, destinationChannel, chanCap, icaPacketData)
}

func (k Keeper) createOutgoingPacket(
	ctx sdk.Context,
	sourcePort,
	sourceChannel,
	destinationPort,
	destinationChannel string,
	chanCap *capabilitytypes.Capability,
	icaPacketData icatypes.InterchainAccountPacketData,
) (uint64, error) {
	if err := icaPacketData.ValidateBasic(); err != nil {
		return 0, sdkerrors.Wrap(err, "invalid interchain account packet data")
	}

	// get the next sequence
	sequence, found := k.channelKeeper.GetNextSequenceSend(ctx, sourcePort, sourceChannel)
	if !found {
		return 0, sdkerrors.Wrapf(channeltypes.ErrSequenceSendNotFound, "failed to retrieve next sequence send for channel %s on port %s", sourceChannel, sourcePort)
	}

	// timeoutTimestamp is set to be a max number here so that we never recieve a timeout
	// ics-27-1 uses ordered channels which can close upon recieving a timeout, which is an undesired effect
	const timeoutTimestamp = ^uint64(0) >> 1 // Shift the unsigned bit to satisfy hermes relayer timestamp conversion

	packet := channeltypes.NewPacket(
		icaPacketData.GetBytes(),
		sequence,
		sourcePort,
		sourceChannel,
		destinationPort,
		destinationChannel,
		clienttypes.ZeroHeight(),
		timeoutTimestamp,
	)

	if err := k.ics4Wrapper.SendPacket(ctx, chanCap, packet); err != nil {
		return 0, err
	}

	return packet.Sequence, nil
}

// OnTimeoutPacket removes the active channel associated with the provided packet, the underlying channel end is closed
// due to the semantics of ORDERED channels
func (k Keeper) OnTimeoutPacket(ctx sdk.Context, packet channeltypes.Packet) error {
	k.DeleteActiveChannelID(ctx, packet.SourcePort)

	return nil
}
