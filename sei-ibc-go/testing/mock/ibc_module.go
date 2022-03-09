package mock

import (
	"bytes"
	"fmt"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
)

// IBCModule implements the ICS26 callbacks for testing/mock.
type IBCModule struct {
	appModule *AppModule
	IBCApp    *MockIBCApp // base application of an IBC middleware stack
}

// NewIBCModule creates a new IBCModule given the underlying mock IBC application and scopedKeeper.
func NewIBCModule(appModule *AppModule, app *MockIBCApp) IBCModule {
	appModule.ibcApps = append(appModule.ibcApps, app)
	return IBCModule{
		appModule: appModule,
		IBCApp:    app,
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
	if err := im.IBCApp.ScopedKeeper.ClaimCapability(ctx, chanCap, host.ChannelCapabilityPath(portID, channelID)); err != nil {
		return err
	}

	return nil
}

// OnChanOpenTry implements the IBCModule interface.
func (im IBCModule) OnChanOpenTry(
	ctx sdk.Context, order channeltypes.Order, connectionHops []string, portID string,
	channelID string, chanCap *capabilitytypes.Capability, counterparty channeltypes.Counterparty, counterpartyVersion string,
) (version string, err error) {
	if im.IBCApp.OnChanOpenTry != nil {
		return im.IBCApp.OnChanOpenTry(ctx, order, connectionHops, portID, channelID, chanCap, counterparty, counterpartyVersion)
	}

	// Claim channel capability passed back by IBC module
	if err := im.IBCApp.ScopedKeeper.ClaimCapability(ctx, chanCap, host.ChannelCapabilityPath(portID, channelID)); err != nil {
		return "", err
	}

	return Version, nil
}

// OnChanOpenAck implements the IBCModule interface.
func (im IBCModule) OnChanOpenAck(ctx sdk.Context, portID string, channelID string, counterpartyChannelID string, counterpartyVersion string) error {
	if im.IBCApp.OnChanOpenAck != nil {
		return im.IBCApp.OnChanOpenAck(ctx, portID, channelID, counterpartyChannelID, counterpartyVersion)
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
	capName := GetMockRecvCanaryCapabilityName(packet)
	if _, err := im.IBCApp.ScopedKeeper.NewCapability(ctx, capName); err != nil {
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

	capName := GetMockAckCanaryCapabilityName(packet)
	if _, err := im.IBCApp.ScopedKeeper.NewCapability(ctx, capName); err != nil {
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

	capName := GetMockTimeoutCanaryCapabilityName(packet)
	if _, err := im.IBCApp.ScopedKeeper.NewCapability(ctx, capName); err != nil {
		// application callback called twice on same packet sequence
		// must never occur
		panic(err)
	}

	return nil
}

// GetMockRecvCanaryCapabilityName generates a capability name for testing OnRecvPacket functionality.
func GetMockRecvCanaryCapabilityName(packet channeltypes.Packet) string {
	return fmt.Sprintf("%s%s%s%s", MockRecvCanaryCapabilityName, packet.GetDestPort(), packet.GetDestChannel(), strconv.Itoa(int(packet.GetSequence())))
}

// GetMockAckCanaryCapabilityName generates a capability name for OnAcknowledgementPacket functionality.
func GetMockAckCanaryCapabilityName(packet channeltypes.Packet) string {
	return fmt.Sprintf("%s%s%s%s", MockAckCanaryCapabilityName, packet.GetSourcePort(), packet.GetSourceChannel(), strconv.Itoa(int(packet.GetSequence())))
}

// GetMockTimeoutCanaryCapabilityName generates a capability name for OnTimeoutacket functionality.
func GetMockTimeoutCanaryCapabilityName(packet channeltypes.Packet) string {
	return fmt.Sprintf("%s%s%s%s", MockTimeoutCanaryCapabilityName, packet.GetSourcePort(), packet.GetSourceChannel(), strconv.Itoa(int(packet.GetSequence())))
}
