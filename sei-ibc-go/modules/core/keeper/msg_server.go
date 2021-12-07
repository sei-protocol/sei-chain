package keeper

import (
	"context"

	"github.com/armon/go-metrics"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	clienttypes "github.com/cosmos/ibc-go/v2/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v2/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v2/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v2/modules/core/05-port/types"
	coretypes "github.com/cosmos/ibc-go/v2/modules/core/types"
)

var _ clienttypes.MsgServer = Keeper{}
var _ connectiontypes.MsgServer = Keeper{}
var _ channeltypes.MsgServer = Keeper{}

// CreateClient defines a rpc handler method for MsgCreateClient.
func (k Keeper) CreateClient(goCtx context.Context, msg *clienttypes.MsgCreateClient) (*clienttypes.MsgCreateClientResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	clientState, err := clienttypes.UnpackClientState(msg.ClientState)
	if err != nil {
		return nil, err
	}

	consensusState, err := clienttypes.UnpackConsensusState(msg.ConsensusState)
	if err != nil {
		return nil, err
	}

	clientID, err := k.ClientKeeper.CreateClient(ctx, clientState, consensusState)
	if err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			clienttypes.EventTypeCreateClient,
			sdk.NewAttribute(clienttypes.AttributeKeyClientID, clientID),
			sdk.NewAttribute(clienttypes.AttributeKeyClientType, clientState.ClientType()),
			sdk.NewAttribute(clienttypes.AttributeKeyConsensusHeight, clientState.GetLatestHeight().String()),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, clienttypes.AttributeValueCategory),
		),
	})

	return &clienttypes.MsgCreateClientResponse{}, nil
}

// UpdateClient defines a rpc handler method for MsgUpdateClient.
func (k Keeper) UpdateClient(goCtx context.Context, msg *clienttypes.MsgUpdateClient) (*clienttypes.MsgUpdateClientResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	header, err := clienttypes.UnpackHeader(msg.Header)
	if err != nil {
		return nil, err
	}

	if err = k.ClientKeeper.UpdateClient(ctx, msg.ClientId, header); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, clienttypes.AttributeValueCategory),
		),
	)

	return &clienttypes.MsgUpdateClientResponse{}, nil
}

// UpgradeClient defines a rpc handler method for MsgUpgradeClient.
func (k Keeper) UpgradeClient(goCtx context.Context, msg *clienttypes.MsgUpgradeClient) (*clienttypes.MsgUpgradeClientResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	upgradedClient, err := clienttypes.UnpackClientState(msg.ClientState)
	if err != nil {
		return nil, err
	}
	upgradedConsState, err := clienttypes.UnpackConsensusState(msg.ConsensusState)
	if err != nil {
		return nil, err
	}

	if err = k.ClientKeeper.UpgradeClient(ctx, msg.ClientId, upgradedClient, upgradedConsState,
		msg.ProofUpgradeClient, msg.ProofUpgradeConsensusState); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, clienttypes.AttributeValueCategory),
		),
	)

	return &clienttypes.MsgUpgradeClientResponse{}, nil
}

// SubmitMisbehaviour defines a rpc handler method for MsgSubmitMisbehaviour.
func (k Keeper) SubmitMisbehaviour(goCtx context.Context, msg *clienttypes.MsgSubmitMisbehaviour) (*clienttypes.MsgSubmitMisbehaviourResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	misbehaviour, err := clienttypes.UnpackMisbehaviour(msg.Misbehaviour)
	if err != nil {
		return nil, err
	}

	if err := k.ClientKeeper.CheckMisbehaviourAndUpdateState(ctx, misbehaviour); err != nil {
		return nil, sdkerrors.Wrap(err, "failed to process misbehaviour for IBC client")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			clienttypes.EventTypeSubmitMisbehaviour,
			sdk.NewAttribute(clienttypes.AttributeKeyClientID, msg.ClientId),
			sdk.NewAttribute(clienttypes.AttributeKeyClientType, misbehaviour.ClientType()),
		),
	)

	return &clienttypes.MsgSubmitMisbehaviourResponse{}, nil
}

// ConnectionOpenInit defines a rpc handler method for MsgConnectionOpenInit.
func (k Keeper) ConnectionOpenInit(goCtx context.Context, msg *connectiontypes.MsgConnectionOpenInit) (*connectiontypes.MsgConnectionOpenInitResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	connectionID, err := k.ConnectionKeeper.ConnOpenInit(ctx, msg.ClientId, msg.Counterparty, msg.Version, msg.DelayPeriod)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "connection handshake open init failed")
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			connectiontypes.EventTypeConnectionOpenInit,
			sdk.NewAttribute(connectiontypes.AttributeKeyConnectionID, connectionID),
			sdk.NewAttribute(connectiontypes.AttributeKeyClientID, msg.ClientId),
			sdk.NewAttribute(connectiontypes.AttributeKeyCounterpartyClientID, msg.Counterparty.ClientId),
			sdk.NewAttribute(connectiontypes.AttributeKeyCounterpartyConnectionID, msg.Counterparty.ConnectionId),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, connectiontypes.AttributeValueCategory),
		),
	})

	return &connectiontypes.MsgConnectionOpenInitResponse{}, nil
}

// ConnectionOpenTry defines a rpc handler method for MsgConnectionOpenTry.
func (k Keeper) ConnectionOpenTry(goCtx context.Context, msg *connectiontypes.MsgConnectionOpenTry) (*connectiontypes.MsgConnectionOpenTryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	targetClient, err := clienttypes.UnpackClientState(msg.ClientState)
	if err != nil {
		return nil, sdkerrors.Wrapf(err, "client in msg is not exported.ClientState. invalid client: %v.", targetClient)
	}

	connectionID, err := k.ConnectionKeeper.ConnOpenTry(
		ctx, msg.PreviousConnectionId, msg.Counterparty, msg.DelayPeriod, msg.ClientId, targetClient,
		connectiontypes.ProtoVersionsToExported(msg.CounterpartyVersions), msg.ProofInit, msg.ProofClient, msg.ProofConsensus,
		msg.ProofHeight, msg.ConsensusHeight,
	)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "connection handshake open try failed")
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			connectiontypes.EventTypeConnectionOpenTry,
			sdk.NewAttribute(connectiontypes.AttributeKeyConnectionID, connectionID),
			sdk.NewAttribute(connectiontypes.AttributeKeyClientID, msg.ClientId),
			sdk.NewAttribute(connectiontypes.AttributeKeyCounterpartyClientID, msg.Counterparty.ClientId),
			sdk.NewAttribute(connectiontypes.AttributeKeyCounterpartyConnectionID, msg.Counterparty.ConnectionId),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, connectiontypes.AttributeValueCategory),
		),
	})

	return &connectiontypes.MsgConnectionOpenTryResponse{}, nil
}

// ConnectionOpenAck defines a rpc handler method for MsgConnectionOpenAck.
func (k Keeper) ConnectionOpenAck(goCtx context.Context, msg *connectiontypes.MsgConnectionOpenAck) (*connectiontypes.MsgConnectionOpenAckResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	targetClient, err := clienttypes.UnpackClientState(msg.ClientState)
	if err != nil {
		return nil, sdkerrors.Wrapf(err, "client in msg is not exported.ClientState. invalid client: %v", targetClient)
	}

	if err := k.ConnectionKeeper.ConnOpenAck(
		ctx, msg.ConnectionId, targetClient, msg.Version, msg.CounterpartyConnectionId,
		msg.ProofTry, msg.ProofClient, msg.ProofConsensus,
		msg.ProofHeight, msg.ConsensusHeight,
	); err != nil {
		return nil, sdkerrors.Wrap(err, "connection handshake open ack failed")
	}

	connectionEnd, _ := k.ConnectionKeeper.GetConnection(ctx, msg.ConnectionId)

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			connectiontypes.EventTypeConnectionOpenAck,
			sdk.NewAttribute(connectiontypes.AttributeKeyConnectionID, msg.ConnectionId),
			sdk.NewAttribute(connectiontypes.AttributeKeyClientID, connectionEnd.ClientId),
			sdk.NewAttribute(connectiontypes.AttributeKeyCounterpartyClientID, connectionEnd.Counterparty.ClientId),
			sdk.NewAttribute(connectiontypes.AttributeKeyCounterpartyConnectionID, connectionEnd.Counterparty.ConnectionId),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, connectiontypes.AttributeValueCategory),
		),
	})

	return &connectiontypes.MsgConnectionOpenAckResponse{}, nil
}

// ConnectionOpenConfirm defines a rpc handler method for MsgConnectionOpenConfirm.
func (k Keeper) ConnectionOpenConfirm(goCtx context.Context, msg *connectiontypes.MsgConnectionOpenConfirm) (*connectiontypes.MsgConnectionOpenConfirmResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if err := k.ConnectionKeeper.ConnOpenConfirm(
		ctx, msg.ConnectionId, msg.ProofAck, msg.ProofHeight,
	); err != nil {
		return nil, sdkerrors.Wrap(err, "connection handshake open confirm failed")
	}

	connectionEnd, _ := k.ConnectionKeeper.GetConnection(ctx, msg.ConnectionId)

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			connectiontypes.EventTypeConnectionOpenConfirm,
			sdk.NewAttribute(connectiontypes.AttributeKeyConnectionID, msg.ConnectionId),
			sdk.NewAttribute(connectiontypes.AttributeKeyClientID, connectionEnd.ClientId),
			sdk.NewAttribute(connectiontypes.AttributeKeyCounterpartyClientID, connectionEnd.Counterparty.ClientId),
			sdk.NewAttribute(connectiontypes.AttributeKeyCounterpartyConnectionID, connectionEnd.Counterparty.ConnectionId),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, connectiontypes.AttributeValueCategory),
		),
	})

	return &connectiontypes.MsgConnectionOpenConfirmResponse{}, nil
}

// ChannelOpenInit defines a rpc handler method for MsgChannelOpenInit.
func (k Keeper) ChannelOpenInit(goCtx context.Context, msg *channeltypes.MsgChannelOpenInit) (*channeltypes.MsgChannelOpenInitResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Lookup module by port capability
	module, portCap, err := k.PortKeeper.LookupModuleByPort(ctx, msg.PortId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "could not retrieve module from port-id")
	}

	channelID, cap, err := k.ChannelKeeper.ChanOpenInit(
		ctx, msg.Channel.Ordering, msg.Channel.ConnectionHops, msg.PortId,
		portCap, msg.Channel.Counterparty, msg.Channel.Version,
	)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "channel handshake open init failed")
	}

	// Retrieve callbacks from router
	cbs, ok := k.Router.GetRoute(module)
	if !ok {
		return nil, sdkerrors.Wrapf(porttypes.ErrInvalidRoute, "route not found to module: %s", module)
	}

	if err = cbs.OnChanOpenInit(ctx, msg.Channel.Ordering, msg.Channel.ConnectionHops, msg.PortId, channelID, cap, msg.Channel.Counterparty, msg.Channel.Version); err != nil {
		return nil, sdkerrors.Wrap(err, "channel open init callback failed")
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, channeltypes.AttributeValueCategory),
		),
	})

	return &channeltypes.MsgChannelOpenInitResponse{}, nil
}

// ChannelOpenTry defines a rpc handler method for MsgChannelOpenTry.
func (k Keeper) ChannelOpenTry(goCtx context.Context, msg *channeltypes.MsgChannelOpenTry) (*channeltypes.MsgChannelOpenTryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	// Lookup module by port capability
	module, portCap, err := k.PortKeeper.LookupModuleByPort(ctx, msg.PortId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "could not retrieve module from port-id")
	}

	channelID, cap, err := k.ChannelKeeper.ChanOpenTry(ctx, msg.Channel.Ordering, msg.Channel.ConnectionHops, msg.PortId, msg.PreviousChannelId,
		portCap, msg.Channel.Counterparty, msg.Channel.Version, msg.CounterpartyVersion, msg.ProofInit, msg.ProofHeight,
	)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "channel handshake open try failed")
	}

	// Retrieve callbacks from router
	cbs, ok := k.Router.GetRoute(module)
	if !ok {
		return nil, sdkerrors.Wrapf(porttypes.ErrInvalidRoute, "route not found to module: %s", module)
	}

	if err = cbs.OnChanOpenTry(ctx, msg.Channel.Ordering, msg.Channel.ConnectionHops, msg.PortId, channelID, cap, msg.Channel.Counterparty, msg.Channel.Version, msg.CounterpartyVersion); err != nil {
		return nil, sdkerrors.Wrap(err, "channel open try callback failed")
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, channeltypes.AttributeValueCategory),
		),
	})

	return &channeltypes.MsgChannelOpenTryResponse{}, nil
}

// ChannelOpenAck defines a rpc handler method for MsgChannelOpenAck.
func (k Keeper) ChannelOpenAck(goCtx context.Context, msg *channeltypes.MsgChannelOpenAck) (*channeltypes.MsgChannelOpenAckResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Lookup module by channel capability
	module, cap, err := k.ChannelKeeper.LookupModuleByChannel(ctx, msg.PortId, msg.ChannelId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "could not retrieve module from port-id")
	}

	// Retrieve callbacks from router
	cbs, ok := k.Router.GetRoute(module)
	if !ok {
		return nil, sdkerrors.Wrapf(porttypes.ErrInvalidRoute, "route not found to module: %s", module)
	}

	err = k.ChannelKeeper.ChanOpenAck(
		ctx, msg.PortId, msg.ChannelId, cap, msg.CounterpartyVersion, msg.CounterpartyChannelId, msg.ProofTry, msg.ProofHeight,
	)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "channel handshake open ack failed")
	}

	if err = cbs.OnChanOpenAck(ctx, msg.PortId, msg.ChannelId, msg.CounterpartyVersion); err != nil {
		return nil, sdkerrors.Wrap(err, "channel open ack callback failed")
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, channeltypes.AttributeValueCategory),
		),
	})

	return &channeltypes.MsgChannelOpenAckResponse{}, nil
}

// ChannelOpenConfirm defines a rpc handler method for MsgChannelOpenConfirm.
func (k Keeper) ChannelOpenConfirm(goCtx context.Context, msg *channeltypes.MsgChannelOpenConfirm) (*channeltypes.MsgChannelOpenConfirmResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Lookup module by channel capability
	module, cap, err := k.ChannelKeeper.LookupModuleByChannel(ctx, msg.PortId, msg.ChannelId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "could not retrieve module from port-id")
	}

	// Retrieve callbacks from router
	cbs, ok := k.Router.GetRoute(module)
	if !ok {
		return nil, sdkerrors.Wrapf(porttypes.ErrInvalidRoute, "route not found to module: %s", module)
	}

	err = k.ChannelKeeper.ChanOpenConfirm(ctx, msg.PortId, msg.ChannelId, cap, msg.ProofAck, msg.ProofHeight)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "channel handshake open confirm failed")
	}

	if err = cbs.OnChanOpenConfirm(ctx, msg.PortId, msg.ChannelId); err != nil {
		return nil, sdkerrors.Wrap(err, "channel open confirm callback failed")
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, channeltypes.AttributeValueCategory),
		),
	})

	return &channeltypes.MsgChannelOpenConfirmResponse{}, nil
}

// ChannelCloseInit defines a rpc handler method for MsgChannelCloseInit.
func (k Keeper) ChannelCloseInit(goCtx context.Context, msg *channeltypes.MsgChannelCloseInit) (*channeltypes.MsgChannelCloseInitResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	// Lookup module by channel capability
	module, cap, err := k.ChannelKeeper.LookupModuleByChannel(ctx, msg.PortId, msg.ChannelId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "could not retrieve module from port-id")
	}

	// Retrieve callbacks from router
	cbs, ok := k.Router.GetRoute(module)
	if !ok {
		return nil, sdkerrors.Wrapf(porttypes.ErrInvalidRoute, "route not found to module: %s", module)
	}

	if err = cbs.OnChanCloseInit(ctx, msg.PortId, msg.ChannelId); err != nil {
		return nil, sdkerrors.Wrap(err, "channel close init callback failed")
	}

	err = k.ChannelKeeper.ChanCloseInit(ctx, msg.PortId, msg.ChannelId, cap)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "channel handshake close init failed")
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, channeltypes.AttributeValueCategory),
		),
	})

	return &channeltypes.MsgChannelCloseInitResponse{}, nil
}

// ChannelCloseConfirm defines a rpc handler method for MsgChannelCloseConfirm.
func (k Keeper) ChannelCloseConfirm(goCtx context.Context, msg *channeltypes.MsgChannelCloseConfirm) (*channeltypes.MsgChannelCloseConfirmResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Lookup module by channel capability
	module, cap, err := k.ChannelKeeper.LookupModuleByChannel(ctx, msg.PortId, msg.ChannelId)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "could not retrieve module from port-id")
	}

	// Retrieve callbacks from router
	cbs, ok := k.Router.GetRoute(module)
	if !ok {
		return nil, sdkerrors.Wrapf(porttypes.ErrInvalidRoute, "route not found to module: %s", module)
	}

	if err = cbs.OnChanCloseConfirm(ctx, msg.PortId, msg.ChannelId); err != nil {
		return nil, sdkerrors.Wrap(err, "channel close confirm callback failed")
	}

	err = k.ChannelKeeper.ChanCloseConfirm(ctx, msg.PortId, msg.ChannelId, cap, msg.ProofInit, msg.ProofHeight)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "channel handshake close confirm failed")
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, channeltypes.AttributeValueCategory),
		),
	})

	return &channeltypes.MsgChannelCloseConfirmResponse{}, nil
}

// RecvPacket defines a rpc handler method for MsgRecvPacket.
func (k Keeper) RecvPacket(goCtx context.Context, msg *channeltypes.MsgRecvPacket) (*channeltypes.MsgRecvPacketResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	relayer, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "Invalid address for msg Signer")
	}

	// Lookup module by channel capability
	module, cap, err := k.ChannelKeeper.LookupModuleByChannel(ctx, msg.Packet.DestinationPort, msg.Packet.DestinationChannel)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "could not retrieve module from port-id")
	}

	// Retrieve callbacks from router
	cbs, ok := k.Router.GetRoute(module)
	if !ok {
		return nil, sdkerrors.Wrapf(porttypes.ErrInvalidRoute, "route not found to module: %s", module)
	}

	// Perform TAO verification
	//
	// If the packet was already received, perform a no-op
	// Use a cached context to prevent accidental state changes
	cacheCtx, writeFn := ctx.CacheContext()
	err = k.ChannelKeeper.RecvPacket(cacheCtx, cap, msg.Packet, msg.ProofCommitment, msg.ProofHeight)

	// NOTE: The context returned by CacheContext() refers to a new EventManager, so it needs to explicitly set events to the original context.
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())

	switch err {
	case nil:
		writeFn()
	case channeltypes.ErrNoOpMsg:
		return &channeltypes.MsgRecvPacketResponse{}, nil // no-op
	default:
		return nil, sdkerrors.Wrap(err, "receive packet verification failed")
	}

	// Perform application logic callback
	//
	// Cache context so that we may discard state changes from callback if the acknowledgement is unsuccessful.
	cacheCtx, writeFn = ctx.CacheContext()
	ack := cbs.OnRecvPacket(cacheCtx, msg.Packet, relayer)
	// NOTE: The context returned by CacheContext() refers to a new EventManager, so it needs to explicitly set events to the original context.
	// Events from callback are emitted regardless of acknowledgement success
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
	if ack == nil || ack.Success() {
		// write application state changes for asynchronous and successful acknowledgements
		writeFn()
	}

	// Set packet acknowledgement only if the acknowledgement is not nil.
	// NOTE: IBC applications modules may call the WriteAcknowledgement asynchronously if the
	// acknowledgement is nil.
	if ack != nil {
		if err := k.ChannelKeeper.WriteAcknowledgement(ctx, cap, msg.Packet, ack.Acknowledgement()); err != nil {
			return nil, err
		}
	}

	defer func() {
		telemetry.IncrCounterWithLabels(
			[]string{"tx", "msg", "ibc", channeltypes.EventTypeRecvPacket},
			1,
			[]metrics.Label{
				telemetry.NewLabel(coretypes.LabelSourcePort, msg.Packet.SourcePort),
				telemetry.NewLabel(coretypes.LabelSourceChannel, msg.Packet.SourceChannel),
				telemetry.NewLabel(coretypes.LabelDestinationPort, msg.Packet.DestinationPort),
				telemetry.NewLabel(coretypes.LabelDestinationChannel, msg.Packet.DestinationChannel),
			},
		)
	}()

	return &channeltypes.MsgRecvPacketResponse{}, nil
}

// Timeout defines a rpc handler method for MsgTimeout.
func (k Keeper) Timeout(goCtx context.Context, msg *channeltypes.MsgTimeout) (*channeltypes.MsgTimeoutResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	relayer, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "Invalid address for msg Signer")
	}

	// Lookup module by channel capability
	module, cap, err := k.ChannelKeeper.LookupModuleByChannel(ctx, msg.Packet.SourcePort, msg.Packet.SourceChannel)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "could not retrieve module from port-id")
	}

	// Retrieve callbacks from router
	cbs, ok := k.Router.GetRoute(module)
	if !ok {
		return nil, sdkerrors.Wrapf(porttypes.ErrInvalidRoute, "route not found to module: %s", module)
	}

	// Perform TAO verification
	//
	// If the timeout was already received, perform a no-op
	// Use a cached context to prevent accidental state changes
	cacheCtx, writeFn := ctx.CacheContext()
	err = k.ChannelKeeper.TimeoutPacket(cacheCtx, msg.Packet, msg.ProofUnreceived, msg.ProofHeight, msg.NextSequenceRecv)

	// NOTE: The context returned by CacheContext() refers to a new EventManager, so it needs to explicitly set events to the original context.
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())

	switch err {
	case nil:
		writeFn()
	case channeltypes.ErrNoOpMsg:
		return &channeltypes.MsgTimeoutResponse{}, nil // no-op
	default:
		return nil, sdkerrors.Wrap(err, "timeout packet verification failed")
	}

	// Perform application logic callback
	err = cbs.OnTimeoutPacket(ctx, msg.Packet, relayer)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "timeout packet callback failed")
	}

	// Delete packet commitment
	if err = k.ChannelKeeper.TimeoutExecuted(ctx, cap, msg.Packet); err != nil {
		return nil, err
	}

	defer func() {
		telemetry.IncrCounterWithLabels(
			[]string{"ibc", "timeout", "packet"},
			1,
			[]metrics.Label{
				telemetry.NewLabel(coretypes.LabelSourcePort, msg.Packet.SourcePort),
				telemetry.NewLabel(coretypes.LabelSourceChannel, msg.Packet.SourceChannel),
				telemetry.NewLabel(coretypes.LabelDestinationPort, msg.Packet.DestinationPort),
				telemetry.NewLabel(coretypes.LabelDestinationChannel, msg.Packet.DestinationChannel),
				telemetry.NewLabel(coretypes.LabelTimeoutType, "height"),
			},
		)
	}()

	return &channeltypes.MsgTimeoutResponse{}, nil
}

// TimeoutOnClose defines a rpc handler method for MsgTimeoutOnClose.
func (k Keeper) TimeoutOnClose(goCtx context.Context, msg *channeltypes.MsgTimeoutOnClose) (*channeltypes.MsgTimeoutOnCloseResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	relayer, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "Invalid address for msg Signer")
	}

	// Lookup module by channel capability
	module, cap, err := k.ChannelKeeper.LookupModuleByChannel(ctx, msg.Packet.SourcePort, msg.Packet.SourceChannel)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "could not retrieve module from port-id")
	}

	// Retrieve callbacks from router
	cbs, ok := k.Router.GetRoute(module)
	if !ok {
		return nil, sdkerrors.Wrapf(porttypes.ErrInvalidRoute, "route not found to module: %s", module)
	}

	// Perform TAO verification
	//
	// If the timeout was already received, perform a no-op
	// Use a cached context to prevent accidental state changes
	cacheCtx, writeFn := ctx.CacheContext()
	err = k.ChannelKeeper.TimeoutOnClose(cacheCtx, cap, msg.Packet, msg.ProofUnreceived, msg.ProofClose, msg.ProofHeight, msg.NextSequenceRecv)

	// NOTE: The context returned by CacheContext() refers to a new EventManager, so it needs to explicitly set events to the original context.
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())

	switch err {
	case nil:
		writeFn()
	case channeltypes.ErrNoOpMsg:
		return &channeltypes.MsgTimeoutOnCloseResponse{}, nil // no-op
	default:
		return nil, sdkerrors.Wrap(err, "timeout on close packet verification failed")
	}

	// Perform application logic callback
	//
	// NOTE: MsgTimeout and MsgTimeoutOnClose use the same "OnTimeoutPacket"
	// application logic callback.
	err = cbs.OnTimeoutPacket(ctx, msg.Packet, relayer)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "timeout packet callback failed")
	}

	// Delete packet commitment
	if err = k.ChannelKeeper.TimeoutExecuted(ctx, cap, msg.Packet); err != nil {
		return nil, err
	}

	defer func() {
		telemetry.IncrCounterWithLabels(
			[]string{"ibc", "timeout", "packet"},
			1,
			[]metrics.Label{
				telemetry.NewLabel(coretypes.LabelSourcePort, msg.Packet.SourcePort),
				telemetry.NewLabel(coretypes.LabelSourceChannel, msg.Packet.SourceChannel),
				telemetry.NewLabel(coretypes.LabelDestinationPort, msg.Packet.DestinationPort),
				telemetry.NewLabel(coretypes.LabelDestinationChannel, msg.Packet.DestinationChannel),
				telemetry.NewLabel(coretypes.LabelTimeoutType, "channel-closed"),
			},
		)
	}()

	return &channeltypes.MsgTimeoutOnCloseResponse{}, nil
}

// Acknowledgement defines a rpc handler method for MsgAcknowledgement.
func (k Keeper) Acknowledgement(goCtx context.Context, msg *channeltypes.MsgAcknowledgement) (*channeltypes.MsgAcknowledgementResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	relayer, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "Invalid address for msg Signer")
	}

	// Lookup module by channel capability
	module, cap, err := k.ChannelKeeper.LookupModuleByChannel(ctx, msg.Packet.SourcePort, msg.Packet.SourceChannel)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "could not retrieve module from port-id")
	}

	// Retrieve callbacks from router
	cbs, ok := k.Router.GetRoute(module)
	if !ok {
		return nil, sdkerrors.Wrapf(porttypes.ErrInvalidRoute, "route not found to module: %s", module)
	}

	// Perform TAO verification
	//
	// If the acknowledgement was already received, perform a no-op
	// Use a cached context to prevent accidental state changes
	cacheCtx, writeFn := ctx.CacheContext()
	err = k.ChannelKeeper.AcknowledgePacket(cacheCtx, cap, msg.Packet, msg.Acknowledgement, msg.ProofAcked, msg.ProofHeight)

	// NOTE: The context returned by CacheContext() refers to a new EventManager, so it needs to explicitly set events to the original context.
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())

	switch err {
	case nil:
		writeFn()
	case channeltypes.ErrNoOpMsg:
		return &channeltypes.MsgAcknowledgementResponse{}, nil // no-op
	default:
		return nil, sdkerrors.Wrap(err, "acknowledge packet verification failed")
	}

	// Perform application logic callback
	err = cbs.OnAcknowledgementPacket(ctx, msg.Packet, msg.Acknowledgement, relayer)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "acknowledge packet callback failed")
	}

	defer func() {
		telemetry.IncrCounterWithLabels(
			[]string{"tx", "msg", "ibc", channeltypes.EventTypeAcknowledgePacket},
			1,
			[]metrics.Label{
				telemetry.NewLabel(coretypes.LabelSourcePort, msg.Packet.SourcePort),
				telemetry.NewLabel(coretypes.LabelSourceChannel, msg.Packet.SourceChannel),
				telemetry.NewLabel(coretypes.LabelDestinationPort, msg.Packet.DestinationPort),
				telemetry.NewLabel(coretypes.LabelDestinationChannel, msg.Packet.DestinationChannel),
			},
		)
	}()

	return &channeltypes.MsgAcknowledgementResponse{}, nil
}
