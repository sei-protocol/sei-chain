package mock

import (
	"bytes"
	"errors"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitykeeper "github.com/cosmos/cosmos-sdk/x/capability/keeper"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	channeltypes "github.com/cosmos/ibc-go/v2/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v2/modules/core/24-host"
	"github.com/cosmos/ibc-go/v2/modules/core/exported"
)

// IBCModule implements the ICS26 callbacks for testing/mock.
type IBCModule struct {
	IBCApp       *MockIBCApp // base application of an IBC middleware stack
	scopedKeeper capabilitykeeper.ScopedKeeper
}

// NewIBCModule creates a new IBCModule given the underlying mock IBC application and scopedKeeper.
func NewIBCModule(app *MockIBCApp, scopedKeeper capabilitykeeper.ScopedKeeper) IBCModule {
	return IBCModule{
		IBCApp:       app,
		scopedKeeper: scopedKeeper,
	}
}

// OnChanOpenInit implements the IBCModule interface.
func (im IBCModule) OnChanOpenInit(
	ctx sdk.Context, order channeltypes.Order, connectionHops []string, portID string,
	channelID string, chanCap *capabilitytypes.Capability, counterparty channeltypes.Counterparty, version string,
) error {
	if im.IBCApp.OnChanOpenInit != nil {
		return im.IBCApp.OnChanOpenInit(ctx, order, connectionHops, portID, channelID, chanCap, counterparty, version)

	}

	// Claim channel capability passed back by IBC module
	if err := im.scopedKeeper.ClaimCapability(ctx, chanCap, host.ChannelCapabilityPath(portID, channelID)); err != nil {
		return err
	}

	return nil
}

// OnChanOpenTry implements the IBCModule interface.
func (im IBCModule) OnChanOpenTry(
	ctx sdk.Context, order channeltypes.Order, connectionHops []string, portID string,
	channelID string, chanCap *capabilitytypes.Capability, counterparty channeltypes.Counterparty, version, counterpartyVersion string,
) error {
	if im.IBCApp.OnChanOpenTry != nil {
		return im.IBCApp.OnChanOpenTry(ctx, order, connectionHops, portID, channelID, chanCap, counterparty, version, counterpartyVersion)

	}
	// Claim channel capability passed back by IBC module
	if err := im.scopedKeeper.ClaimCapability(ctx, chanCap, host.ChannelCapabilityPath(portID, channelID)); err != nil {
		return err
	}

	return nil
}

// OnChanOpenAck implements the IBCModule interface.
func (im IBCModule) OnChanOpenAck(ctx sdk.Context, portID string, channelID string, counterpartyVersion string) error {
	if im.IBCApp.OnChanOpenAck != nil {
		return im.IBCApp.OnChanOpenAck(ctx, portID, channelID, counterpartyVersion)
	}

	return nil
}

// OnChanOpenConfirm implements the IBCModule interface.
func (im IBCModule) OnChanOpenConfirm(ctx sdk.Context, portID, channelID string) error {
	if im.IBCApp.OnChanOpenConfirm != nil {
		return im.IBCApp.OnChanOpenConfirm(ctx, portID, channelID)
	}

	return nil
}

// OnChanCloseInit implements the IBCModule interface.
func (im IBCModule) OnChanCloseInit(ctx sdk.Context, portID, channelID string) error {
	if im.IBCApp.OnChanCloseInit != nil {
		return im.IBCApp.OnChanCloseInit(ctx, portID, channelID)
	}

	return nil
}

// OnChanCloseConfirm implements the IBCModule interface.
func (im IBCModule) OnChanCloseConfirm(ctx sdk.Context, portID, channelID string) error {
	if im.IBCApp.OnChanCloseConfirm != nil {
		return im.IBCApp.OnChanCloseConfirm(ctx, portID, channelID)
	}

	return nil
}

// OnRecvPacket implements the IBCModule interface.
func (im IBCModule) OnRecvPacket(ctx sdk.Context, packet channeltypes.Packet, relayer sdk.AccAddress) exported.Acknowledgement {
	if im.IBCApp.OnRecvPacket != nil {
		return im.IBCApp.OnRecvPacket(ctx, packet, relayer)
	}

	// set state by claiming capability to check if revert happens return
	_, err := im.scopedKeeper.NewCapability(ctx, MockRecvCanaryCapabilityName+strconv.Itoa(int(packet.GetSequence())))
	if err != nil {
		// application callback called twice on same packet sequence
		// must never occur
		panic(err)
	}
	if bytes.Equal(MockPacketData, packet.GetData()) {
		return MockAcknowledgement
	} else if bytes.Equal(MockAsyncPacketData, packet.GetData()) {
		return nil
	}

	return MockFailAcknowledgement
}

// OnAcknowledgementPacket implements the IBCModule interface.
func (im IBCModule) OnAcknowledgementPacket(ctx sdk.Context, packet channeltypes.Packet, acknowledgement []byte, relayer sdk.AccAddress) error {
	if im.IBCApp.OnAcknowledgementPacket != nil {
		return im.IBCApp.OnAcknowledgementPacket(ctx, packet, acknowledgement, relayer)
	}

	_, err := im.scopedKeeper.NewCapability(ctx, MockAckCanaryCapabilityName+strconv.Itoa(int(packet.GetSequence())))
	if err != nil {
		// application callback called twice on same packet sequence
		// must never occur
		panic(err)
	}

	return nil
}

// OnTimeoutPacket implements the IBCModule interface.
func (im IBCModule) OnTimeoutPacket(ctx sdk.Context, packet channeltypes.Packet, relayer sdk.AccAddress) error {
	if im.IBCApp.OnTimeoutPacket != nil {
		return im.IBCApp.OnTimeoutPacket(ctx, packet, relayer)
	}

	_, err := im.scopedKeeper.NewCapability(ctx, MockTimeoutCanaryCapabilityName+strconv.Itoa(int(packet.GetSequence())))
	if err != nil {
		// application callback called twice on same packet sequence
		// must never occur
		panic(err)
	}

	return nil
}

// NegotiateAppVersion implements the IBCModule interface.
func (im IBCModule) NegotiateAppVersion(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionID string,
	portID string,
	counterparty channeltypes.Counterparty,
	proposedVersion string,
) (string, error) {
	if im.IBCApp.NegotiateAppVersion != nil {
		return im.IBCApp.NegotiateAppVersion(ctx, order, connectionID, portID, counterparty, proposedVersion)
	}

	if proposedVersion != Version { // allow testing of error scenarios
		return "", errors.New("failed to negotiate app version")
	}

	return Version, nil
}
