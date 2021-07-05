package mock

import (
	"bytes"
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	capabilitykeeper "github.com/cosmos/cosmos-sdk/x/capability/keeper"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"
	abci "github.com/tendermint/tendermint/abci/types"

	channeltypes "github.com/cosmos/ibc-go/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/modules/core/05-port/types"
	host "github.com/cosmos/ibc-go/modules/core/24-host"
	"github.com/cosmos/ibc-go/modules/core/exported"
)

const (
	ModuleName = "mock"
)

var (
	MockAcknowledgement      = channeltypes.NewResultAcknowledgement([]byte("mock acknowledgement"))
	MockFailAcknowledgement  = channeltypes.NewErrorAcknowledgement("mock failed acknowledgement")
	MockPacketData           = []byte("mock packet data")
	MockFailPacketData       = []byte("mock failed packet data")
	MockAsyncPacketData      = []byte("mock async packet data")
	MockCanaryCapabilityName = "mock canary capability name"
)

var _ porttypes.IBCModule = AppModule{}

// Expected Interface
// PortKeeper defines the expected IBC port keeper
type PortKeeper interface {
	BindPort(ctx sdk.Context, portID string) *capabilitytypes.Capability
}

// AppModuleBasic is the mock AppModuleBasic.
type AppModuleBasic struct{}

// Name implements AppModuleBasic interface.
func (AppModuleBasic) Name() string {
	return ModuleName
}

// RegisterLegacyAminoCodec implements AppModuleBasic interface.
func (AppModuleBasic) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

// RegisterInterfaces implements AppModuleBasic interface.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {}

// DefaultGenesis implements AppModuleBasic interface.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return nil
}

// ValidateGenesis implements the AppModuleBasic interface.
func (AppModuleBasic) ValidateGenesis(codec.JSONCodec, client.TxEncodingConfig, json.RawMessage) error {
	return nil
}

// RegisterRESTRoutes implements AppModuleBasic interface.
func (AppModuleBasic) RegisterRESTRoutes(clientCtx client.Context, rtr *mux.Router) {}

// RegisterGRPCGatewayRoutes implements AppModuleBasic interface.
func (a AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *runtime.ServeMux) {}

// GetTxCmd implements AppModuleBasic interface.
func (AppModuleBasic) GetTxCmd() *cobra.Command {
	return nil
}

// GetQueryCmd implements AppModuleBasic interface.
func (AppModuleBasic) GetQueryCmd() *cobra.Command {
	return nil
}

// AppModule represents the AppModule for the mock module.
type AppModule struct {
	AppModuleBasic
	scopedKeeper capabilitykeeper.ScopedKeeper
	portKeeper   PortKeeper
}

// NewAppModule returns a mock AppModule instance.
func NewAppModule(sk capabilitykeeper.ScopedKeeper, pk PortKeeper) AppModule {
	return AppModule{
		scopedKeeper: sk,
		portKeeper:   pk,
	}
}

// RegisterInvariants implements the AppModule interface.
func (AppModule) RegisterInvariants(ir sdk.InvariantRegistry) {}

// Route implements the AppModule interface.
func (am AppModule) Route() sdk.Route {
	return sdk.NewRoute(ModuleName, nil)
}

// QuerierRoute implements the AppModule interface.
func (AppModule) QuerierRoute() string {
	return ""
}

// LegacyQuerierHandler implements the AppModule interface.
func (am AppModule) LegacyQuerierHandler(*codec.LegacyAmino) sdk.Querier {
	return nil
}

// RegisterServices implements the AppModule interface.
func (am AppModule) RegisterServices(module.Configurator) {}

// InitGenesis implements the AppModule interface.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) []abci.ValidatorUpdate {
	// bind mock port ID
	cap := am.portKeeper.BindPort(ctx, ModuleName)
	am.scopedKeeper.ClaimCapability(ctx, cap, host.PortPath(ModuleName))

	return []abci.ValidatorUpdate{}
}

// ExportGenesis implements the AppModule interface.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	return nil
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 1 }

// BeginBlock implements the AppModule interface
func (am AppModule) BeginBlock(ctx sdk.Context, req abci.RequestBeginBlock) {
}

// EndBlock implements the AppModule interface
func (am AppModule) EndBlock(ctx sdk.Context, req abci.RequestEndBlock) []abci.ValidatorUpdate {
	return []abci.ValidatorUpdate{}
}

// OnChanOpenInit implements the IBCModule interface.
func (am AppModule) OnChanOpenInit(
	ctx sdk.Context, _ channeltypes.Order, _ []string, portID string,
	channelID string, chanCap *capabilitytypes.Capability, _ channeltypes.Counterparty, _ string,
) error {
	// Claim channel capability passed back by IBC module
	if err := am.scopedKeeper.ClaimCapability(ctx, chanCap, host.ChannelCapabilityPath(portID, channelID)); err != nil {
		return err
	}

	return nil
}

// OnChanOpenTry implements the IBCModule interface.
func (am AppModule) OnChanOpenTry(
	ctx sdk.Context, _ channeltypes.Order, _ []string, portID string,
	channelID string, chanCap *capabilitytypes.Capability, _ channeltypes.Counterparty, _, _ string,
) error {
	// Claim channel capability passed back by IBC module
	if err := am.scopedKeeper.ClaimCapability(ctx, chanCap, host.ChannelCapabilityPath(portID, channelID)); err != nil {
		return err
	}

	return nil
}

// OnChanOpenAck implements the IBCModule interface.
func (am AppModule) OnChanOpenAck(sdk.Context, string, string, string) error {
	return nil
}

// OnChanOpenConfirm implements the IBCModule interface.
func (am AppModule) OnChanOpenConfirm(sdk.Context, string, string) error {
	return nil
}

// OnChanCloseInit implements the IBCModule interface.
func (am AppModule) OnChanCloseInit(sdk.Context, string, string) error {
	return nil
}

// OnChanCloseConfirm implements the IBCModule interface.
func (am AppModule) OnChanCloseConfirm(sdk.Context, string, string) error {
	return nil
}

// OnRecvPacket implements the IBCModule interface.
func (am AppModule) OnRecvPacket(ctx sdk.Context, packet channeltypes.Packet, relayer sdk.AccAddress) exported.Acknowledgement {
	// set state by claiming capability to check if revert happens return
	am.scopedKeeper.NewCapability(ctx, MockCanaryCapabilityName)
	if bytes.Equal(MockPacketData, packet.GetData()) {
		return MockAcknowledgement
	} else if bytes.Equal(MockAsyncPacketData, packet.GetData()) {
		return nil
	}

	return MockFailAcknowledgement
}

// OnAcknowledgementPacket implements the IBCModule interface.
func (am AppModule) OnAcknowledgementPacket(sdk.Context, channeltypes.Packet, []byte, sdk.AccAddress) error {
	return nil
}

// OnTimeoutPacket implements the IBCModule interface.
func (am AppModule) OnTimeoutPacket(sdk.Context, channeltypes.Packet, sdk.AccAddress) error {
	return nil
}
