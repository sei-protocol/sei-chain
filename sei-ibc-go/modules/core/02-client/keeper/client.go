package keeper

import (
	"encoding/hex"

	"github.com/armon/go-metrics"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/exported"
)

var logger = seilog.NewLogger("ibc-go", "modules", "core", "02-client", "keeper")

// UpdateClient updates the consensus state and the state root from a provided header.
func (k Keeper) UpdateClient(ctx sdk.Context, clientID string, header exported.Header) error {
	clientState, found := k.GetClientState(ctx, clientID)
	if !found {
		return sdkerrors.Wrapf(types.ErrClientNotFound, "cannot update client with ID %s", clientID)
	}

	clientStore := k.ClientStore(ctx, clientID)

	if status := clientState.Status(ctx, clientStore, k.cdc); status != exported.Active {
		return sdkerrors.Wrapf(types.ErrClientNotActive, "cannot update client (%s) with status %s", clientID, status)
	}

	// Any writes made in CheckHeaderAndUpdateState are persisted on both valid updates and misbehaviour updates.
	// Light client implementations are responsible for writing the correct metadata (if any) in either case.
	newClientState, newConsensusState, err := clientState.CheckHeaderAndUpdateState(ctx, k.cdc, clientStore, header)
	if err != nil {
		return sdkerrors.Wrapf(err, "cannot update client with ID %s", clientID)
	}

	// emit the full header in events
	var (
		headerStr       string
		consensusHeight exported.Height
	)
	if header != nil {
		// Marshal the Header as an Any and encode the resulting bytes to hex.
		// This prevents the event value from containing invalid UTF-8 characters
		// which may cause data to be lost when JSON encoding/decoding.
		headerStr = hex.EncodeToString(types.MustMarshalHeader(k.cdc, header))
		// set default consensus height with header height
		consensusHeight = header.GetHeight()
	}

	// set new client state regardless of if update is valid update or misbehaviour
	k.SetClientState(ctx, clientID, newClientState)
	// If client state is not frozen after clientState CheckHeaderAndUpdateState,
	// then update was valid. Write the update state changes, and set new consensus state.
	// Else the update was proof of misbehaviour and we must emit appropriate misbehaviour events.
	if status := newClientState.Status(ctx, clientStore, k.cdc); status != exported.Frozen {
		// if update is not misbehaviour then update the consensus state
		// we don't set consensus state for localhost client
		if header != nil && clientID != exported.Localhost {
			k.SetClientConsensusState(ctx, clientID, header.GetHeight(), newConsensusState)
		} else {
			consensusHeight = types.GetSelfHeight(ctx)
		}

		logger.Info("client state updated", "client-id", clientID, "height", consensusHeight.String())

		defer func() {
			ibcClientMetrics.ibcClientUpdate.Add(ctx.Context(), 1, otelmetric.WithAttributes(
				attribute.String(types.LabelClientType, clientState.ClientType()),
				attribute.String(types.LabelClientID, clientID),
				attribute.String(types.LabelUpdateType, "msg"),
			))
			// TODO(PLT-428): remove once ibc_client_update verified
			telemetry.IncrCounterWithLabels(
				[]string{"ibc", "client", "update"},
				1,
				[]metrics.Label{
					telemetry.NewLabel(types.LabelClientType, clientState.ClientType()),
					telemetry.NewLabel(types.LabelClientID, clientID),
					telemetry.NewLabel(types.LabelUpdateType, "msg"),
				},
			)
		}()

		// emitting events in the keeper emits for both begin block and handler client updates
		EmitUpdateClientEvent(ctx, clientID, newClientState, consensusHeight, headerStr)
	} else {
		logger.Info("client frozen due to misbehaviour", "client-id", clientID)

		defer func() {
			ibcClientMetrics.ibcClientMisbehaviour.Add(ctx.Context(), 1, otelmetric.WithAttributes(
				attribute.String(types.LabelClientType, clientState.ClientType()),
				attribute.String(types.LabelClientID, clientID),
				attribute.String(types.LabelMsgType, "update"),
			))
			// TODO(PLT-428): remove once ibc_client_misbehaviour verified
			telemetry.IncrCounterWithLabels(
				[]string{"ibc", "client", "misbehaviour"},
				1,
				[]metrics.Label{
					telemetry.NewLabel(types.LabelClientType, clientState.ClientType()),
					telemetry.NewLabel(types.LabelClientID, clientID),
					telemetry.NewLabel(types.LabelMsgType, "update"),
				},
			)
		}()

		EmitSubmitMisbehaviourEventOnUpdate(ctx, clientID, newClientState, consensusHeight, headerStr)
	}

	return nil
}
