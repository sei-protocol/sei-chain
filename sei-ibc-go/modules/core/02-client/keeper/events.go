package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/exported"
)

// EmitUpdateClientEvent emits an update client event
func EmitUpdateClientEvent(ctx sdk.Context, clientID string, clientState exported.ClientState, consensusHeight exported.Height, headerStr string) {
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeUpdateClient,
			sdk.NewAttribute(types.AttributeKeyClientID, clientID),
			sdk.NewAttribute(types.AttributeKeyClientType, clientState.ClientType()),
			sdk.NewAttribute(types.AttributeKeyConsensusHeight, consensusHeight.String()),
			sdk.NewAttribute(types.AttributeKeyHeader, headerStr),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
		),
	})
}

// EmitSubmitMisbehaviourEventOnUpdate emits a client misbehaviour event on a client update event
func EmitSubmitMisbehaviourEventOnUpdate(ctx sdk.Context, clientID string, clientState exported.ClientState, consensusHeight exported.Height, headerStr string) {
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSubmitMisbehaviour,
			sdk.NewAttribute(types.AttributeKeyClientID, clientID),
			sdk.NewAttribute(types.AttributeKeyClientType, clientState.ClientType()),
			sdk.NewAttribute(types.AttributeKeyConsensusHeight, consensusHeight.String()),
			sdk.NewAttribute(types.AttributeKeyHeader, headerStr),
		),
	)
}
