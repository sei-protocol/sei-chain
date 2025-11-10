package wasmtesting

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v3/modules/core/exported"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

type MockChannelKeeper struct {
	GetChannelFn          func(ctx sdk.Context, srcPort, srcChan string) (channel channeltypes.Channel, found bool)
	GetNextSequenceSendFn func(ctx sdk.Context, portID, channelID string) (uint64, bool)
	SendPacketFn          func(ctx sdk.Context, channelCap *capabilitytypes.Capability, packet ibcexported.PacketI) error
	ChanCloseInitFn       func(ctx sdk.Context, portID, channelID string, chanCap *capabilitytypes.Capability) error
	GetAllChannelsFn      func(ctx sdk.Context) []channeltypes.IdentifiedChannel
	IterateChannelsFn     func(ctx sdk.Context, cb func(channeltypes.IdentifiedChannel) bool)
	SetChannelFn          func(ctx sdk.Context, portID, channelID string, channel channeltypes.Channel)
}

func (m *MockChannelKeeper) GetChannel(ctx sdk.Context, srcPort, srcChan string) (channel channeltypes.Channel, found bool) {
	if m.GetChannelFn == nil {
		panic("not supposed to be called!")
	}
	return m.GetChannelFn(ctx, srcPort, srcChan)
}

func (m *MockChannelKeeper) GetAllChannels(ctx sdk.Context) []channeltypes.IdentifiedChannel {
	if m.GetAllChannelsFn == nil {
		panic("not supposed to be called!")
	}
	return m.GetAllChannelsFn(ctx)
}

func (m *MockChannelKeeper) GetNextSequenceSend(ctx sdk.Context, portID, channelID string) (uint64, bool) {
	if m.GetNextSequenceSendFn == nil {
		panic("not supposed to be called!")
	}
	return m.GetNextSequenceSendFn(ctx, portID, channelID)
}

func (m *MockChannelKeeper) SendPacket(ctx sdk.Context, channelCap *capabilitytypes.Capability, packet ibcexported.PacketI) error {
	if m.SendPacketFn == nil {
		panic("not supposed to be called!")
	}
	return m.SendPacketFn(ctx, channelCap, packet)
}

func (m *MockChannelKeeper) ChanCloseInit(ctx sdk.Context, portID, channelID string, chanCap *capabilitytypes.Capability) error {
	if m.ChanCloseInitFn == nil {
		panic("not supposed to be called!")
	}
	return m.ChanCloseInitFn(ctx, portID, channelID, chanCap)
}

func (m *MockChannelKeeper) IterateChannels(ctx sdk.Context, cb func(channeltypes.IdentifiedChannel) bool) {
	if m.IterateChannelsFn == nil {
		panic("not expected to be called")
	}
	m.IterateChannelsFn(ctx, cb)
}

func (m *MockChannelKeeper) SetChannel(ctx sdk.Context, portID, channelID string, channel channeltypes.Channel) {
	if m.GetChannelFn == nil {
		panic("not supposed to be called!")
	}
	m.SetChannelFn(ctx, portID, channelID, channel)
}

func MockChannelKeeperIterator(s []channeltypes.IdentifiedChannel) func(ctx sdk.Context, cb func(channeltypes.IdentifiedChannel) bool) {
	return func(ctx sdk.Context, cb func(channeltypes.IdentifiedChannel) bool) {
		for _, channel := range s {
			stop := cb(channel)
			if stop {
				break
			}
		}
	}
}

type MockCapabilityKeeper struct {
	GetCapabilityFn          func(ctx sdk.Context, name string) (*capabilitytypes.Capability, bool)
	ClaimCapabilityFn        func(ctx sdk.Context, cap *capabilitytypes.Capability, name string) error
	AuthenticateCapabilityFn func(ctx sdk.Context, capability *capabilitytypes.Capability, name string) bool
}

func (m MockCapabilityKeeper) GetCapability(ctx sdk.Context, name string) (*capabilitytypes.Capability, bool) {
	if m.GetCapabilityFn == nil {
		panic("not supposed to be called!")
	}
	return m.GetCapabilityFn(ctx, name)
}

func (m MockCapabilityKeeper) ClaimCapability(ctx sdk.Context, cap *capabilitytypes.Capability, name string) error {
	if m.ClaimCapabilityFn == nil {
		panic("not supposed to be called!")
	}
	return m.ClaimCapabilityFn(ctx, cap, name)
}

func (m MockCapabilityKeeper) AuthenticateCapability(ctx sdk.Context, capability *capabilitytypes.Capability, name string) bool {
	if m.AuthenticateCapabilityFn == nil {
		panic("not supposed to be called!")
	}
	return m.AuthenticateCapabilityFn(ctx, capability, name)
}

var _ types.ICS20TransferPortSource = &MockIBCTransferKeeper{}

type MockIBCTransferKeeper struct {
	GetPortFn func(ctx sdk.Context) string
}

func (m MockIBCTransferKeeper) GetPort(ctx sdk.Context) string {
	if m.GetPortFn == nil {
		panic("not expected to be called")
	}
	return m.GetPortFn(ctx)
}
