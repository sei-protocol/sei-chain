package mock

import (
	"encoding/json"
	"fmt"

	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	capabilitytypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/capability/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/spf13/cobra"

	channeltypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
	porttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/05-port/types"
	host "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/24-host"
)

const (
	ModuleName = "mock"

	PortID  = ModuleName
	Version = "mock-version"
)

var (
	MockAcknowledgement             = channeltypes.NewResultAcknowledgement([]byte("mock acknowledgement"))
	MockFailAcknowledgement         = channeltypes.NewErrorAcknowledgement("mock failed acknowledgement")
	MockPacketData                  = []byte("mock packet data")
	MockFailPacketData              = []byte("mock failed packet data")
	MockAsyncPacketData             = []byte("mock async packet data")
	MockRecvCanaryCapabilityName    = "mock receive canary capability name"
	MockAckCanaryCapabilityName     = "mock acknowledgement canary capability name"
	MockTimeoutCanaryCapabilityName = "mock timeout canary capability name"
)

var _ porttypes.IBCModule = IBCModule{}

// Expected Interface
// PortKeeper defines the expected IBC port keeper
type PortKeeper interface {
	BindPort(ctx sdk.Context, portID string) *capabilitytypes.Capability
	IsBound(ctx sdk.Context, portID string) bool
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

// ValidateGenesisStream implements the AppModuleBasic interface.
func (am AppModuleBasic) ValidateGenesisStream(cdc codec.JSONCodec, config client.TxEncodingConfig, genesisCh <-chan json.RawMessage) error {
	for genesis := range genesisCh {
		err := am.ValidateGenesis(cdc, config, genesis)
		if err != nil {
			return err
		}
	}
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
	ibcApps    []*MockIBCApp
	portKeeper PortKeeper
}

// NewAppModule returns a mock AppModule instance.
func NewAppModule(pk PortKeeper) AppModule {
	return AppModule{
		portKeeper: pk,
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
	for _, ibcApp := range am.ibcApps {
		if ibcApp.PortID != "" && !am.portKeeper.IsBound(ctx, ibcApp.PortID) {
			// bind mock portID
			cap := am.portKeeper.BindPort(ctx, ibcApp.PortID)
			err := ibcApp.ScopedKeeper.ClaimCapability(ctx, cap, host.PortPath(ibcApp.PortID))
			if err != nil {
				panic(fmt.Sprintf("failed to claim capability: %v", err))
			}
		}
	}

	return []abci.ValidatorUpdate{}
}

// ExportGenesis implements the AppModule interface.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	return nil
}

// ExportGenesisStream implements the AppModule interface.
func (am AppModule) ExportGenesisStream(ctx sdk.Context, cdc codec.JSONCodec) <-chan json.RawMessage {
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
