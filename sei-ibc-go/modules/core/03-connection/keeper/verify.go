package keeper

import (
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	clienttypes "github.com/cosmos/ibc-go/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/modules/core/exported"
)

// VerifyClientState verifies a proof of a client state of the running machine
// stored on the target machine
func (k Keeper) VerifyClientState(
	ctx sdk.Context,
	connection exported.ConnectionI,
	height exported.Height,
	proof []byte,
	clientState exported.ClientState,
) error {
	clientID := connection.GetClientID()
	clientStore := k.clientKeeper.ClientStore(ctx, clientID)

	targetClient, found := k.clientKeeper.GetClientState(ctx, clientID)
	if !found {
		return sdkerrors.Wrap(clienttypes.ErrClientNotFound, clientID)
	}

	if status := targetClient.Status(ctx, clientStore, k.cdc); status != exported.Active {
		return sdkerrors.Wrapf(clienttypes.ErrClientNotActive, "client (%s) status is %s", clientID, status)
	}

	if err := targetClient.VerifyClientState(
		clientStore, k.cdc, height,
		connection.GetCounterparty().GetPrefix(), connection.GetCounterparty().GetClientID(), proof, clientState); err != nil {
		return sdkerrors.Wrapf(err, "failed client state verification for target client: %s", clientID)
	}

	return nil
}

// VerifyClientConsensusState verifies a proof of the consensus state of the
// specified client stored on the target machine.
func (k Keeper) VerifyClientConsensusState(
	ctx sdk.Context,
	connection exported.ConnectionI,
	height exported.Height,
	consensusHeight exported.Height,
	proof []byte,
	consensusState exported.ConsensusState,
) error {
	clientID := connection.GetClientID()
	clientStore := k.clientKeeper.ClientStore(ctx, clientID)

	clientState, found := k.clientKeeper.GetClientState(ctx, clientID)
	if !found {
		return sdkerrors.Wrap(clienttypes.ErrClientNotFound, clientID)
	}

	if status := clientState.Status(ctx, clientStore, k.cdc); status != exported.Active {
		return sdkerrors.Wrapf(clienttypes.ErrClientNotActive, "client (%s) status is %s", clientID, status)
	}

	if err := clientState.VerifyClientConsensusState(
		clientStore, k.cdc, height,
		connection.GetCounterparty().GetClientID(), consensusHeight, connection.GetCounterparty().GetPrefix(), proof, consensusState,
	); err != nil {
		return sdkerrors.Wrapf(err, "failed consensus state verification for client (%s)", clientID)
	}

	return nil
}

// VerifyConnectionState verifies a proof of the connection state of the
// specified connection end stored on the target machine.
func (k Keeper) VerifyConnectionState(
	ctx sdk.Context,
	connection exported.ConnectionI,
	height exported.Height,
	proof []byte,
	connectionID string,
	connectionEnd exported.ConnectionI, // opposite connection
) error {
	clientID := connection.GetClientID()
	clientStore := k.clientKeeper.ClientStore(ctx, clientID)

	clientState, found := k.clientKeeper.GetClientState(ctx, clientID)
	if !found {
		return sdkerrors.Wrap(clienttypes.ErrClientNotFound, clientID)
	}

	if status := clientState.Status(ctx, clientStore, k.cdc); status != exported.Active {
		return sdkerrors.Wrapf(clienttypes.ErrClientNotActive, "client (%s) status is %s", clientID, status)
	}

	if err := clientState.VerifyConnectionState(
		clientStore, k.cdc, height,
		connection.GetCounterparty().GetPrefix(), proof, connectionID, connectionEnd,
	); err != nil {
		return sdkerrors.Wrapf(err, "failed connection state verification for client (%s)", clientID)
	}

	return nil
}

// VerifyChannelState verifies a proof of the channel state of the specified
// channel end, under the specified port, stored on the target machine.
func (k Keeper) VerifyChannelState(
	ctx sdk.Context,
	connection exported.ConnectionI,
	height exported.Height,
	proof []byte,
	portID,
	channelID string,
	channel exported.ChannelI,
) error {
	clientID := connection.GetClientID()
	clientStore := k.clientKeeper.ClientStore(ctx, clientID)

	clientState, found := k.clientKeeper.GetClientState(ctx, clientID)
	if !found {
		return sdkerrors.Wrap(clienttypes.ErrClientNotFound, clientID)
	}

	if status := clientState.Status(ctx, clientStore, k.cdc); status != exported.Active {
		return sdkerrors.Wrapf(clienttypes.ErrClientNotActive, "client (%s) status is %s", clientID, status)
	}

	if err := clientState.VerifyChannelState(
		clientStore, k.cdc, height,
		connection.GetCounterparty().GetPrefix(), proof,
		portID, channelID, channel,
	); err != nil {
		return sdkerrors.Wrapf(err, "failed channel state verification for client (%s)", clientID)
	}

	return nil
}

// VerifyPacketCommitment verifies a proof of an outgoing packet commitment at
// the specified port, specified channel, and specified sequence.
func (k Keeper) VerifyPacketCommitment(
	ctx sdk.Context,
	connection exported.ConnectionI,
	height exported.Height,
	proof []byte,
	portID,
	channelID string,
	sequence uint64,
	commitmentBytes []byte,
) error {
	clientID := connection.GetClientID()
	clientStore := k.clientKeeper.ClientStore(ctx, clientID)

	clientState, found := k.clientKeeper.GetClientState(ctx, clientID)
	if !found {
		return sdkerrors.Wrap(clienttypes.ErrClientNotFound, clientID)
	}

	if status := clientState.Status(ctx, clientStore, k.cdc); status != exported.Active {
		return sdkerrors.Wrapf(clienttypes.ErrClientNotActive, "client (%s) status is %s", clientID, status)
	}

	// get time and block delays
	timeDelay := connection.GetDelayPeriod()
	blockDelay := k.getBlockDelay(ctx, connection)

	if err := clientState.VerifyPacketCommitment(
		ctx, clientStore, k.cdc, height,
		timeDelay, blockDelay,
		connection.GetCounterparty().GetPrefix(), proof, portID, channelID,
		sequence, commitmentBytes,
	); err != nil {
		return sdkerrors.Wrapf(err, "failed packet commitment verification for client (%s)", clientID)
	}

	return nil
}

// VerifyPacketAcknowledgement verifies a proof of an incoming packet
// acknowledgement at the specified port, specified channel, and specified sequence.
func (k Keeper) VerifyPacketAcknowledgement(
	ctx sdk.Context,
	connection exported.ConnectionI,
	height exported.Height,
	proof []byte,
	portID,
	channelID string,
	sequence uint64,
	acknowledgement []byte,
) error {
	clientID := connection.GetClientID()
	clientStore := k.clientKeeper.ClientStore(ctx, clientID)

	clientState, found := k.clientKeeper.GetClientState(ctx, clientID)
	if !found {
		return sdkerrors.Wrap(clienttypes.ErrClientNotFound, clientID)
	}

	if status := clientState.Status(ctx, clientStore, k.cdc); status != exported.Active {
		return sdkerrors.Wrapf(clienttypes.ErrClientNotActive, "client (%s) status is %s", clientID, status)
	}

	// get time and block delays
	timeDelay := connection.GetDelayPeriod()
	blockDelay := k.getBlockDelay(ctx, connection)

	if err := clientState.VerifyPacketAcknowledgement(
		ctx, clientStore, k.cdc, height,
		timeDelay, blockDelay,
		connection.GetCounterparty().GetPrefix(), proof, portID, channelID,
		sequence, acknowledgement,
	); err != nil {
		return sdkerrors.Wrapf(err, "failed packet acknowledgement verification for client (%s)", clientID)
	}

	return nil
}

// VerifyPacketReceiptAbsence verifies a proof of the absence of an
// incoming packet receipt at the specified port, specified channel, and
// specified sequence.
func (k Keeper) VerifyPacketReceiptAbsence(
	ctx sdk.Context,
	connection exported.ConnectionI,
	height exported.Height,
	proof []byte,
	portID,
	channelID string,
	sequence uint64,
) error {
	clientID := connection.GetClientID()
	clientStore := k.clientKeeper.ClientStore(ctx, clientID)

	clientState, found := k.clientKeeper.GetClientState(ctx, clientID)
	if !found {
		return sdkerrors.Wrap(clienttypes.ErrClientNotFound, clientID)
	}

	if status := clientState.Status(ctx, clientStore, k.cdc); status != exported.Active {
		return sdkerrors.Wrapf(clienttypes.ErrClientNotActive, "client (%s) status is %s", clientID, status)
	}

	// get time and block delays
	timeDelay := connection.GetDelayPeriod()
	blockDelay := k.getBlockDelay(ctx, connection)

	if err := clientState.VerifyPacketReceiptAbsence(
		ctx, clientStore, k.cdc, height,
		timeDelay, blockDelay,
		connection.GetCounterparty().GetPrefix(), proof, portID, channelID,
		sequence,
	); err != nil {
		return sdkerrors.Wrapf(err, "failed packet receipt absence verification for client (%s)", clientID)
	}

	return nil
}

// VerifyNextSequenceRecv verifies a proof of the next sequence number to be
// received of the specified channel at the specified port.
func (k Keeper) VerifyNextSequenceRecv(
	ctx sdk.Context,
	connection exported.ConnectionI,
	height exported.Height,
	proof []byte,
	portID,
	channelID string,
	nextSequenceRecv uint64,
) error {
	clientID := connection.GetClientID()
	clientStore := k.clientKeeper.ClientStore(ctx, clientID)

	clientState, found := k.clientKeeper.GetClientState(ctx, clientID)
	if !found {
		return sdkerrors.Wrap(clienttypes.ErrClientNotFound, clientID)
	}

	if status := clientState.Status(ctx, clientStore, k.cdc); status != exported.Active {
		return sdkerrors.Wrapf(clienttypes.ErrClientNotActive, "client (%s) status is %s", clientID, status)
	}

	// get time and block delays
	timeDelay := connection.GetDelayPeriod()
	blockDelay := k.getBlockDelay(ctx, connection)

	if err := clientState.VerifyNextSequenceRecv(
		ctx, clientStore, k.cdc, height,
		timeDelay, blockDelay,
		connection.GetCounterparty().GetPrefix(), proof, portID, channelID,
		nextSequenceRecv,
	); err != nil {
		return sdkerrors.Wrapf(err, "failed next sequence receive verification for client (%s)", clientID)
	}

	return nil
}

// getBlockDelay calculates the block delay period from the time delay of the connection
// and the maximum expected time per block.
func (k Keeper) getBlockDelay(ctx sdk.Context, connection exported.ConnectionI) uint64 {
	// expectedTimePerBlock should never be zero, however if it is then return a 0 blcok delay for safety
	// as the expectedTimePerBlock parameter was not set.
	expectedTimePerBlock := k.GetMaxExpectedTimePerBlock(ctx)
	if expectedTimePerBlock == 0 {
		return 0
	}
	// calculate minimum block delay by dividing time delay period
	// by the expected time per block. Round up the block delay.
	timeDelay := connection.GetDelayPeriod()
	return uint64(math.Ceil(float64(timeDelay) / float64(expectedTimePerBlock)))
}
