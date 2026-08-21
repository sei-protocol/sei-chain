package keeper

import (
	"fmt"
	"strings"

	"github.com/armon/go-metrics"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
	clienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	channeltypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
	host "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/24-host"
	coretypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
)

// SendTransfer handles transfer sending logic. There are 2 possible cases:
//
// 1. Sender chain is acting as the source zone. The coins are transferred
// to an escrow address (i.e locked) on the sender chain and then transferred
// to the receiving chain through IBC TAO logic. It is expected that the
// receiving chain will mint vouchers to the receiving address.
//
// 2. Sender chain is acting as the sink zone. The coins (vouchers) are burned
// on the sender chain and then transferred to the receiving chain though IBC
// TAO logic. It is expected that the receiving chain, which had previously
// sent the original denomination, will unescrow the fungible token and send
// it to the receiving address.
//
// Another way of thinking of source and sink zones is through the token's
// timeline. Each send to any chain other than the one it was previously
// received from is a movement forwards in the token's timeline. This causes
// trace to be added to the token's history and the destination port and
// destination channel to be prefixed to the denomination. In these instances
// the sender chain is acting as the source zone. When the token is sent back
// to the chain it previously received from, the prefix is removed. This is
// a backwards movement in the token's timeline and the sender chain
// is acting as the sink zone.
//
// Example:
// These steps of transfer occur: A -> B -> C -> A -> C -> B -> A
//
// 1. A -> B : sender chain is source zone. Denom upon receiving: 'B/denom'
// 2. B -> C : sender chain is source zone. Denom upon receiving: 'C/B/denom'
// 3. C -> A : sender chain is source zone. Denom upon receiving: 'A/C/B/denom'
// 4. A -> C : sender chain is sink zone. Denom upon receiving: 'C/B/denom'
// 5. C -> B : sender chain is sink zone. Denom upon receiving: 'B/denom'
// 6. B -> A : sender chain is sink zone. Denom upon receiving: 'denom'
//
// Note: An IBC Transfer must be initiated using a MsgTransfer via the Transfer rpc handler
func (k Keeper) SendTransfer(
	ctx sdk.Context,
	sourcePort,
	sourceChannel string,
	token sdk.Coin,
	sender sdk.AccAddress,
	receiver string,
	timeoutHeight clienttypes.Height,
	timeoutTimestamp uint64,
) error {
	_, err := k.sendTransfer(
		ctx,
		sourcePort,
		sourceChannel,
		token,
		sender,
		receiver,
		timeoutHeight,
		timeoutTimestamp,
		"",
	)
	return err
}

// sendTransfer handles transfer sending logic.
func (k Keeper) sendTransfer(
	ctx sdk.Context,
	sourcePort,
	sourceChannel string,
	token sdk.Coin,
	sender sdk.AccAddress,
	receiver string,
	timeoutHeight clienttypes.Height,
	timeoutTimestamp uint64,
	memo string,
) (uint64, error) {
	if !k.GetSendEnabled(ctx) {
		return 0, types.ErrSendDisabled
	}

	if !k.bankKeeper.IsSendEnabledCoin(ctx, token) {
		return 0, sdkerrors.Wrapf(types.ErrSendDisabled, "%s transfers are currently disabled", token.Denom)
	}

	if k.bankKeeper.BlockedAddr(sender) {
		return 0, sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "%s is not allowed to send funds", sender)
	}

	sourceChannelEnd, found := k.channelKeeper.GetChannel(ctx, sourcePort, sourceChannel)
	if !found {
		return 0, sdkerrors.Wrapf(channeltypes.ErrChannelNotFound, "port ID (%s) channel ID (%s)", sourcePort, sourceChannel)
	}

	destinationPort := sourceChannelEnd.GetCounterparty().GetPortID()
	destinationChannel := sourceChannelEnd.GetCounterparty().GetChannelID()

	// get the next sequence
	sequence, found := k.channelKeeper.GetNextSequenceSend(ctx, sourcePort, sourceChannel)
	if !found {
		return 0, sdkerrors.Wrapf(
			channeltypes.ErrSequenceSendNotFound,
			"source port: %s, source channel: %s", sourcePort, sourceChannel,
		)
	}

	// begin createOutgoingPacket logic
	// See spec for this logic: https://github.com/cosmos/ibc/tree/master/spec/app/ics-020-fungible-token-transfer#packet-relay
	channelCap, ok := k.scopedKeeper.GetCapability(ctx, host.ChannelCapabilityPath(sourcePort, sourceChannel))
	if !ok {
		return 0, sdkerrors.Wrap(channeltypes.ErrChannelCapabilityNotFound, "module does not own channel capability")
	}

	// NOTE: denomination and hex hash correctness checked during msg.ValidateBasic
	fullDenomPath := token.Denom

	var err error

	// deconstruct the token denomination into the denomination trace info
	// to determine if the sender is the source chain
	if strings.HasPrefix(token.Denom, "ibc/") {
		fullDenomPath, err = k.DenomPathFromHash(ctx, token.Denom)
		if err != nil {
			return 0, err
		}
	}

	labels := []metrics.Label{
		telemetry.NewLabel(coretypes.LabelDestinationPort, destinationPort),
		telemetry.NewLabel(coretypes.LabelDestinationChannel, destinationChannel),
	}

	// NOTE: SendTransfer simply sends the denomination as it exists on its own
	// chain inside the packet data. The receiving chain will perform denom
	// prefixing as necessary.

	var isSource bool
	if types.SenderChainIsSource(sourcePort, sourceChannel, fullDenomPath) {
		isSource = true
		labels = append(labels, telemetry.NewLabel(coretypes.LabelSource, "true"))

		// create the escrow address for the tokens
		escrowAddress := types.GetEscrowAddress(sourcePort, sourceChannel)

		// escrow source tokens. It fails if balance insufficient.
		if err := k.bankKeeper.SendCoins(
			ctx, sender, escrowAddress, sdk.NewCoins(token),
		); err != nil {
			return 0, err
		}

	} else {
		labels = append(labels, telemetry.NewLabel(coretypes.LabelSource, "false"))

		// transfer the coins to the module account and burn them
		if err := k.bankKeeper.SendCoinsFromAccountToModule(
			ctx, sender, types.ModuleName, sdk.NewCoins(token),
		); err != nil {
			return 0, err
		}

		if err := k.bankKeeper.BurnCoins(
			ctx, types.ModuleName, sdk.NewCoins(token),
		); err != nil {
			// NOTE: should not happen as the module account was
			// retrieved on the step above and it has enough balace
			// to burn.
			panic(fmt.Sprintf("cannot burn coins after a successful send to a module account: %v", err))
		}
	}

	packetData := types.NewFungibleTokenPacketData(
		fullDenomPath, token.Amount.String(), sender.String(), receiver,
	)
	packetData.Memo = memo

	packet := channeltypes.NewPacket(
		packetData.GetBytes(),
		sequence,
		sourcePort,
		sourceChannel,
		destinationPort,
		destinationChannel,
		timeoutHeight,
		timeoutTimestamp,
	)

	if err := k.ics4Wrapper.SendPacket(ctx, channelCap, packet); err != nil {
		return 0, err
	}

	defer func() {
		if token.Amount.IsInt64() {
			ibcTransferMetrics.txMsgIbcTransfer.Record(ctx.Context(), token.Amount.Int64(), otelmetric.WithAttributes(attribute.String("denom_class", telemetry.DenomClass(fullDenomPath))))
			// TODO(PLT-428): remove once tx_msg_ibc_transfer verified
			telemetry.SetGaugeWithLabels(
				[]string{"tx", "msg", "ibc", "transfer"},
				float32(token.Amount.Int64()),
				[]metrics.Label{telemetry.NewLabel(coretypes.LabelDenom, fullDenomPath)},
			)
		}

		ibcTransferMetrics.ibcTransferSend.Add(ctx.Context(), 1, otelmetric.WithAttributes(
			attribute.String(coretypes.LabelDestinationPort, destinationPort),
			attribute.String(coretypes.LabelDestinationChannel, destinationChannel),
			attribute.Bool(coretypes.LabelSource, isSource),
		))
		// TODO(PLT-428): remove once ibc_transfer_send verified
		telemetry.IncrCounterWithLabels(
			[]string{"ibc", types.ModuleName, "send"},
			1,
			labels,
		)
	}()

	return sequence, nil
}

// DenomPathFromHash returns the full denomination path prefix from an ibc denom with a hash
// component.
func (k Keeper) DenomPathFromHash(ctx sdk.Context, denom string) (string, error) {
	// trim the denomination prefix, by default "ibc/"
	hexHash := denom[len(types.DenomPrefix+"/"):]

	hash, err := types.ParseHexHash(hexHash)
	if err != nil {
		return "", sdkerrors.Wrap(types.ErrInvalidDenomForTransfer, err.Error())
	}

	denomTrace, found := k.GetDenomTrace(ctx, hash)
	if !found {
		return "", sdkerrors.Wrap(types.ErrTraceNotFound, hexHash)
	}

	fullDenomPath := denomTrace.GetFullDenomPath()
	return fullDenomPath, nil
}
