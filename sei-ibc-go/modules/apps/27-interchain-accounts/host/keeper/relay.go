package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
)

// AuthenticateTx ensures the provided msgs contain the correct interchain account signer address retrieved
// from state using the provided controller port identifier
func (k Keeper) AuthenticateTx(ctx sdk.Context, msgs []sdk.Msg, portID string) error {
	interchainAccountAddr, found := k.GetInterchainAccountAddress(ctx, portID)
	if !found {
		return sdkerrors.Wrapf(icatypes.ErrInterchainAccountNotFound, "failed to retrieve interchain account on port %s", portID)
	}

	allowMsgs := k.GetAllowMessages(ctx)
	for _, msg := range msgs {
		if !types.ContainsMsgType(allowMsgs, msg) {
			return sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "message type not allowed: %s", sdk.MsgTypeURL(msg))
		}

		for _, signer := range msg.GetSigners() {
			if interchainAccountAddr != signer.String() {
				return sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "unexpected signer address: expected %s, got %s", interchainAccountAddr, signer.String())
			}
		}
	}

	return nil
}

func (k Keeper) executeTx(ctx sdk.Context, sourcePort, destPort, destChannel string, msgs []sdk.Msg) error {
	if err := k.AuthenticateTx(ctx, msgs, sourcePort); err != nil {
		return err
	}

	// CacheContext returns a new context with the multi-store branched into a cached storage object
	// writeCache is called only if all msgs succeed, performing state transitions atomically
	cacheCtx, writeCache := ctx.CacheContext()
	for _, msg := range msgs {
		if err := msg.ValidateBasic(); err != nil {
			return err
		}

		if err := k.executeMsg(cacheCtx, msg); err != nil {
			return err
		}
	}

	// NOTE: The context returned by CacheContext() creates a new EventManager, so events must be correctly propagated back to the current context
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
	writeCache()

	return nil
}

// Attempts to get the message handler from the router and if found will then execute the message
func (k Keeper) executeMsg(ctx sdk.Context, msg sdk.Msg) error {
	handler := k.msgRouter.Handler(msg)
	if handler == nil {
		return icatypes.ErrInvalidRoute
	}

	res, err := handler(ctx, msg)
	if err != nil {
		return err
	}

	// NOTE: The sdk msg handler creates a new EventManager, so events must be correctly propagated back to the current context
	ctx.EventManager().EmitEvents(res.GetEvents())

	return nil
}

// OnRecvPacket handles a given interchain accounts packet on a destination host chain
func (k Keeper) OnRecvPacket(ctx sdk.Context, packet channeltypes.Packet) error {
	var data icatypes.InterchainAccountPacketData

	if err := icatypes.ModuleCdc.UnmarshalJSON(packet.GetData(), &data); err != nil {
		// UnmarshalJSON errors are indeterminate and therefore are not wrapped and included in failed acks
		return sdkerrors.Wrapf(icatypes.ErrUnknownDataType, "cannot unmarshal ICS-27 interchain account packet data")
	}

	switch data.Type {
	case icatypes.EXECUTE_TX:
		msgs, err := icatypes.DeserializeCosmosTx(k.cdc, data.Data)
		if err != nil {
			return err
		}

		if err = k.executeTx(ctx, packet.SourcePort, packet.DestinationPort, packet.DestinationChannel, msgs); err != nil {
			return err
		}

		return nil
	default:
		return icatypes.ErrUnknownDataType
	}
}
