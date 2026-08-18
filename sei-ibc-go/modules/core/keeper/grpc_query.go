package keeper

import (
	"context"

	clienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	connectiontypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/types"
	channeltypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
)

// ClientState implements the IBC QueryServer interface.
func (Keeper) ClientState(context.Context, *clienttypes.QueryClientStateRequest) (*clienttypes.QueryClientStateResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ClientStates implements the IBC QueryServer interface.
func (Keeper) ClientStates(context.Context, *clienttypes.QueryClientStatesRequest) (*clienttypes.QueryClientStatesResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ConsensusState implements the IBC QueryServer interface.
func (Keeper) ConsensusState(context.Context, *clienttypes.QueryConsensusStateRequest) (*clienttypes.QueryConsensusStateResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ConsensusStates implements the IBC QueryServer interface.
func (Keeper) ConsensusStates(context.Context, *clienttypes.QueryConsensusStatesRequest) (*clienttypes.QueryConsensusStatesResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ConsensusStateHeights implements the IBC QueryServer interface.
func (Keeper) ConsensusStateHeights(context.Context, *clienttypes.QueryConsensusStateHeightsRequest) (*clienttypes.QueryConsensusStateHeightsResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ClientStatus implements the IBC QueryServer interface.
func (Keeper) ClientStatus(context.Context, *clienttypes.QueryClientStatusRequest) (*clienttypes.QueryClientStatusResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ClientParams implements the IBC QueryServer interface.
func (Keeper) ClientParams(context.Context, *clienttypes.QueryClientParamsRequest) (*clienttypes.QueryClientParamsResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// UpgradedClientState implements the IBC QueryServer interface.
func (Keeper) UpgradedClientState(context.Context, *clienttypes.QueryUpgradedClientStateRequest) (*clienttypes.QueryUpgradedClientStateResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// UpgradedConsensusState implements the IBC QueryServer interface.
func (Keeper) UpgradedConsensusState(context.Context, *clienttypes.QueryUpgradedConsensusStateRequest) (*clienttypes.QueryUpgradedConsensusStateResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// Connection implements the IBC QueryServer interface.
func (Keeper) Connection(context.Context, *connectiontypes.QueryConnectionRequest) (*connectiontypes.QueryConnectionResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// Connections implements the IBC QueryServer interface.
func (Keeper) Connections(context.Context, *connectiontypes.QueryConnectionsRequest) (*connectiontypes.QueryConnectionsResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ClientConnections implements the IBC QueryServer interface.
func (Keeper) ClientConnections(context.Context, *connectiontypes.QueryClientConnectionsRequest) (*connectiontypes.QueryClientConnectionsResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ConnectionClientState implements the IBC QueryServer interface.
func (Keeper) ConnectionClientState(context.Context, *connectiontypes.QueryConnectionClientStateRequest) (*connectiontypes.QueryConnectionClientStateResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ConnectionConsensusState implements the IBC QueryServer interface.
func (Keeper) ConnectionConsensusState(context.Context, *connectiontypes.QueryConnectionConsensusStateRequest) (*connectiontypes.QueryConnectionConsensusStateResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// Channel implements the IBC QueryServer interface.
func (Keeper) Channel(context.Context, *channeltypes.QueryChannelRequest) (*channeltypes.QueryChannelResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// Channels implements the IBC QueryServer interface.
func (Keeper) Channels(context.Context, *channeltypes.QueryChannelsRequest) (*channeltypes.QueryChannelsResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ConnectionChannels implements the IBC QueryServer interface.
func (Keeper) ConnectionChannels(context.Context, *channeltypes.QueryConnectionChannelsRequest) (*channeltypes.QueryConnectionChannelsResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ChannelClientState implements the IBC QueryServer interface.
func (Keeper) ChannelClientState(context.Context, *channeltypes.QueryChannelClientStateRequest) (*channeltypes.QueryChannelClientStateResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// ChannelConsensusState implements the IBC QueryServer interface.
func (Keeper) ChannelConsensusState(context.Context, *channeltypes.QueryChannelConsensusStateRequest) (*channeltypes.QueryChannelConsensusStateResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// PacketCommitment implements the IBC QueryServer interface.
func (Keeper) PacketCommitment(context.Context, *channeltypes.QueryPacketCommitmentRequest) (*channeltypes.QueryPacketCommitmentResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// PacketCommitments implements the IBC QueryServer interface.
func (Keeper) PacketCommitments(context.Context, *channeltypes.QueryPacketCommitmentsRequest) (*channeltypes.QueryPacketCommitmentsResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// PacketReceipt implements the IBC QueryServer interface.
func (Keeper) PacketReceipt(context.Context, *channeltypes.QueryPacketReceiptRequest) (*channeltypes.QueryPacketReceiptResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// PacketAcknowledgement implements the IBC QueryServer interface.
func (Keeper) PacketAcknowledgement(context.Context, *channeltypes.QueryPacketAcknowledgementRequest) (*channeltypes.QueryPacketAcknowledgementResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// PacketAcknowledgements implements the IBC QueryServer interface.
func (Keeper) PacketAcknowledgements(context.Context, *channeltypes.QueryPacketAcknowledgementsRequest) (*channeltypes.QueryPacketAcknowledgementsResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// UnreceivedPackets implements the IBC QueryServer interface.
func (Keeper) UnreceivedPackets(context.Context, *channeltypes.QueryUnreceivedPacketsRequest) (*channeltypes.QueryUnreceivedPacketsResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// UnreceivedAcks implements the IBC QueryServer interface.
func (Keeper) UnreceivedAcks(context.Context, *channeltypes.QueryUnreceivedAcksRequest) (*channeltypes.QueryUnreceivedAcksResponse, error) {
	return nil, types.ErrIBCDeprecated
}

// NextSequenceReceive implements the IBC QueryServer interface.
func (Keeper) NextSequenceReceive(context.Context, *channeltypes.QueryNextSequenceReceiveRequest) (*channeltypes.QueryNextSequenceReceiveResponse, error) {
	return nil, types.ErrIBCDeprecated
}
