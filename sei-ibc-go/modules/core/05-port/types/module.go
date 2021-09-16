package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	channeltypes "github.com/cosmos/ibc-go/v2/modules/core/04-channel/types"
	"github.com/cosmos/ibc-go/v2/modules/core/exported"
)

// IBCModule defines an interface that implements all the callbacks
// that modules must define as specified in ICS-26
type IBCModule interface {
	OnChanOpenInit(
		ctx sdk.Context,
		order channeltypes.Order,
		connectionHops []string,
		portID string,
		channelID string,
		channelCap *capabilitytypes.Capability,
		counterparty channeltypes.Counterparty,
		version string,
	) error

	OnChanOpenTry(
		ctx sdk.Context,
		order channeltypes.Order,
		connectionHops []string,
		portID,
		channelID string,
		channelCap *capabilitytypes.Capability,
		counterparty channeltypes.Counterparty,
		version,
		counterpartyVersion string,
	) error

	OnChanOpenAck(
		ctx sdk.Context,
		portID,
		channelID string,
		counterpartyVersion string,
	) error

	OnChanOpenConfirm(
		ctx sdk.Context,
		portID,
		channelID string,
	) error

	OnChanCloseInit(
		ctx sdk.Context,
		portID,
		channelID string,
	) error

	OnChanCloseConfirm(
		ctx sdk.Context,
		portID,
		channelID string,
	) error

	// OnRecvPacket must return an acknowledgement that implements the Acknowledgement interface.
	// In the case of an asynchronous acknowledgement, nil should be returned.
	// If the acknowledgement returned is successful, the state changes on callback are written,
	// otherwise the application state changes are discarded. In either case the packet is received
	// and the acknowledgement is written (in synchronous cases).
	OnRecvPacket(
		ctx sdk.Context,
		packet channeltypes.Packet,
		relayer sdk.AccAddress,
	) exported.Acknowledgement

	OnAcknowledgementPacket(
		ctx sdk.Context,
		packet channeltypes.Packet,
		acknowledgement []byte,
		relayer sdk.AccAddress,
	) error

	OnTimeoutPacket(
		ctx sdk.Context,
		packet channeltypes.Packet,
		relayer sdk.AccAddress,
	) error

	// NegotiateAppVersion performs application version negotiation given the provided channel ordering, connectionID, portID, counterparty and proposed version.
	// An error is returned if version negotiation cannot be performed. For example, an application module implementing this interface
	// may decide to return an error in the event of the proposed version being incompatible with it's own
	NegotiateAppVersion(
		ctx sdk.Context,
		order channeltypes.Order,
		connectionID string,
		portID string,
		counterparty channeltypes.Counterparty,
		proposedVersion string,
	) (version string, err error)
}
