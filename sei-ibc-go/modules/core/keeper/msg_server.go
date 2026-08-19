package keeper

import (
	"context"

	clienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	connectiontypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/types"
	channeltypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
	coretypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
)

var (
	_ clienttypes.MsgServer     = Keeper{}
	_ connectiontypes.MsgServer = Keeper{}
	_ channeltypes.MsgServer    = Keeper{}
)

// CreateClient defines an RPC handler for MsgCreateClient.
func (Keeper) CreateClient(context.Context, *clienttypes.MsgCreateClient) (*clienttypes.MsgCreateClientResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// UpdateClient defines an RPC handler for MsgUpdateClient.
func (Keeper) UpdateClient(context.Context, *clienttypes.MsgUpdateClient) (*clienttypes.MsgUpdateClientResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// UpgradeClient defines an RPC handler for MsgUpgradeClient.
func (Keeper) UpgradeClient(context.Context, *clienttypes.MsgUpgradeClient) (*clienttypes.MsgUpgradeClientResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// SubmitMisbehaviour defines an RPC handler for MsgSubmitMisbehaviour.
func (Keeper) SubmitMisbehaviour(context.Context, *clienttypes.MsgSubmitMisbehaviour) (*clienttypes.MsgSubmitMisbehaviourResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// ConnectionOpenInit defines an RPC handler for MsgConnectionOpenInit.
func (Keeper) ConnectionOpenInit(context.Context, *connectiontypes.MsgConnectionOpenInit) (*connectiontypes.MsgConnectionOpenInitResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// ConnectionOpenTry defines an RPC handler for MsgConnectionOpenTry.
func (Keeper) ConnectionOpenTry(context.Context, *connectiontypes.MsgConnectionOpenTry) (*connectiontypes.MsgConnectionOpenTryResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// ConnectionOpenAck defines an RPC handler for MsgConnectionOpenAck.
func (Keeper) ConnectionOpenAck(context.Context, *connectiontypes.MsgConnectionOpenAck) (*connectiontypes.MsgConnectionOpenAckResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// ConnectionOpenConfirm defines an RPC handler for MsgConnectionOpenConfirm.
func (Keeper) ConnectionOpenConfirm(context.Context, *connectiontypes.MsgConnectionOpenConfirm) (*connectiontypes.MsgConnectionOpenConfirmResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// ChannelOpenInit defines an RPC handler for MsgChannelOpenInit.
func (Keeper) ChannelOpenInit(context.Context, *channeltypes.MsgChannelOpenInit) (*channeltypes.MsgChannelOpenInitResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// ChannelOpenTry defines an RPC handler for MsgChannelOpenTry.
func (Keeper) ChannelOpenTry(context.Context, *channeltypes.MsgChannelOpenTry) (*channeltypes.MsgChannelOpenTryResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// ChannelOpenAck defines an RPC handler for MsgChannelOpenAck.
func (Keeper) ChannelOpenAck(context.Context, *channeltypes.MsgChannelOpenAck) (*channeltypes.MsgChannelOpenAckResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// ChannelOpenConfirm defines an RPC handler for MsgChannelOpenConfirm.
func (Keeper) ChannelOpenConfirm(context.Context, *channeltypes.MsgChannelOpenConfirm) (*channeltypes.MsgChannelOpenConfirmResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// ChannelCloseInit defines an RPC handler for MsgChannelCloseInit.
func (Keeper) ChannelCloseInit(context.Context, *channeltypes.MsgChannelCloseInit) (*channeltypes.MsgChannelCloseInitResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// ChannelCloseConfirm defines an RPC handler for MsgChannelCloseConfirm.
func (Keeper) ChannelCloseConfirm(context.Context, *channeltypes.MsgChannelCloseConfirm) (*channeltypes.MsgChannelCloseConfirmResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// RecvPacket defines an RPC handler for MsgRecvPacket.
func (Keeper) RecvPacket(context.Context, *channeltypes.MsgRecvPacket) (*channeltypes.MsgRecvPacketResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// Timeout defines an RPC handler for MsgTimeout.
func (Keeper) Timeout(context.Context, *channeltypes.MsgTimeout) (*channeltypes.MsgTimeoutResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// TimeoutOnClose defines an RPC handler for MsgTimeoutOnClose.
func (Keeper) TimeoutOnClose(context.Context, *channeltypes.MsgTimeoutOnClose) (*channeltypes.MsgTimeoutOnCloseResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}

// Acknowledgement defines an RPC handler for MsgAcknowledgement.
func (Keeper) Acknowledgement(context.Context, *channeltypes.MsgAcknowledgement) (*channeltypes.MsgAcknowledgementResponse, error) {
	return nil, coretypes.ErrIBCDeprecated
}
