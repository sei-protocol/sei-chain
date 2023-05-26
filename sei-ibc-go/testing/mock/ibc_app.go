package mock

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitykeeper "github.com/cosmos/cosmos-sdk/x/capability/keeper"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
)

// MockIBCApp contains IBC application module callbacks as defined in 05-port.
type MockIBCApp struct {
	PortID       string
	ScopedKeeper capabilitykeeper.ScopedKeeper

	OnChanOpenInit func(
		ctx sdk.Context,
		order channeltypes.Order,
		connectionHops []string,
		portID string,
		channelID string,
		channelCap *capabilitytypes.Capability,
		counterparty channeltypes.Counterparty,
		version string,
	) error

	OnChanOpenTry func(
		ctx sdk.Context,
		order channeltypes.Order,
		connectionHops []string,
		portID,
		channelID string,
		channelCap *capabilitytypes.Capability,
		counterparty channeltypes.Counterparty,
		counterpartyVersion string,
	) (version string, err error)

	OnChanOpenAck func(
		ctx sdk.Context,
		portID,
		channelID string,
		counterpartyChannelID string,
		counterpartyVersion string,
	) error

	OnChanOpenConfirm func(
		ctx sdk.Context,
		portID,
		channelID string,
	) error

	OnChanCloseInit func(
		ctx sdk.Context,
		portID,
		channelID string,
	) error

	OnChanCloseConfirm func(
		ctx sdk.Context,
		portID,
		channelID string,
	) error

	// OnRecvPacket must return an acknowledgement that implements the Acknowledgement interface.
	// In the case of an asynchronous acknowledgement, nil should be returned.
	// If the acknowledgement returned is successful, the state changes on callback are written,
	// otherwise the application state changes are discarded. In either case the packet is received
	// and the acknowledgement is written (in synchronous cases).
	OnRecvPacket func(
		ctx sdk.Context,
		packet channeltypes.Packet,
		relayer sdk.AccAddress,
	) exported.Acknowledgement

	OnAcknowledgementPacket func(
		ctx sdk.Context,
		packet channeltypes.Packet,
		acknowledgement []byte,
		relayer sdk.AccAddress,
	) error

	OnTimeoutPacket func(
		ctx sdk.Context,
		packet channeltypes.Packet,
		relayer sdk.AccAddress,
	) error
}

// NewMockIBCApp returns a MockIBCApp. An empty PortID indicates the mock app doesn't bind/claim ports.
func NewMockIBCApp(portID string, scopedKeeper capabilitykeeper.ScopedKeeper) *MockIBCApp {
	return &MockIBCApp{
		PortID:       portID,
		ScopedKeeper: scopedKeeper,
	}
}
