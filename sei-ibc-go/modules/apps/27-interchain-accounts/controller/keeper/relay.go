package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
)

// SendTx takes pre-built packet data containing messages to be executed on the host chain from an authentication module and attempts to send the packet.
// The packet sequence for the outgoing packet is returned as a result.
// If the base application has the capability to send on the provided portID. An appropriate
// absolute timeoutTimestamp must be provided. If the packet is timed out, the channel will be closed.
// In the case of channel closure, a new channel may be reopened to reconnect to the host chain.
func (k Keeper) SendTx(ctx sdk.Context, chanCap *capabilitytypes.Capability, connectionID, portID string, icaPacketData icatypes.InterchainAccountPacketData, timeoutTimestamp uint64) (uint64, error) {
	activeChannelID, found := k.GetOpenActiveChannel(ctx, connectionID, portID)
	if !found {
		return 0, sdkerrors.Wrapf(icatypes.ErrActiveChannelNotFound, "failed to retrieve active channel on connection %s for port %s", connectionID, portID)
	}

	sourceChannelEnd, found := k.channelKeeper.GetChannel(ctx, portID, activeChannelID)
	if !found {
		return 0, sdkerrors.Wrap(channeltypes.ErrChannelNotFound, activeChannelID)
	}

	destinationPort := sourceChannelEnd.GetCounterparty().GetPortID()
	destinationChannel := sourceChannelEnd.GetCounterparty().GetChannelID()

	if uint64(ctx.BlockTime().UnixNano()) >= timeoutTimestamp {
		return 0, icatypes.ErrInvalidTimeoutTimestamp
	}

	return k.createOutgoingPacket(ctx, portID, activeChannelID, destinationPort, destinationChannel, chanCap, icaPacketData, timeoutTimestamp)
}

func (k Keeper) createOutgoingPacket(
	ctx sdk.Context,
	sourcePort,
	sourceChannel,
	destinationPort,
	destinationChannel string,
	chanCap *capabilitytypes.Capability,
	icaPacketData icatypes.InterchainAccountPacketData,
	timeoutTimestamp uint64,
) (uint64, error) {
	if err := icaPacketData.ValidateBasic(); err != nil {
		return 0, sdkerrors.Wrap(err, "invalid interchain account packet data")
	}

	// get the next sequence
	sequence, found := k.channelKeeper.GetNextSequenceSend(ctx, sourcePort, sourceChannel)
	if !found {
		return 0, sdkerrors.Wrapf(channeltypes.ErrSequenceSendNotFound, "failed to retrieve next sequence send for channel %s on port %s", sourceChannel, sourcePort)
	}

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
	return nil
}
