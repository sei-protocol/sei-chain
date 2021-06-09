package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	channeltypes "github.com/cosmos/ibc-go/modules/core/04-channel/types"
	"github.com/cosmos/ibc-go/modules/core/exported"
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
	) (*sdk.Result, error)

	OnTimeoutPacket(
		ctx sdk.Context,
		packet channeltypes.Packet,
		relayer sdk.AccAddress,
	) (*sdk.Result, error)
}
