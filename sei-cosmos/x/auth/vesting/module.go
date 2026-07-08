package vesting

import (
	"encoding/json"

	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/client/cli"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// AppModuleBasic defines the basic application module used by the sub-vesting
// module. The module itself contain no special logic or state other than message
// handling.
//
// The vesting module is deprecated: once the deprecation gate is active (the
// DeprecationUpgradeName upgrade on chains with pre-deprecation history,
// genesis everywhere else), its message handlers reject all messages, so new
// vesting accounts can no longer be created. The module must remain wired into
// the app so its codec and interface registrations stay in place: they are
// required to decode existing vesting accounts in the auth store and
// historical transactions.
type AppModuleBasic struct{}

// Name returns the module's name.
func (AppModuleBasic) Name() string {
	return types.ModuleName
}

// RegisterCodec registers the module's types with the given codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers the module's interfaces and implementations with
// the given interface registry.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns the module's default genesis state as raw bytes.
func (AppModuleBasic) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	return []byte("{}")
}

// ValidateGenesis performs genesis state validation. Currently, this is a no-op.
func (AppModuleBasic) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	return nil
}

func (am AppModuleBasic) ValidateGenesisStream(cdc codec.JSONCodec, config client.TxEncodingConfig, genesisCh <-chan json.RawMessage) error {
	for genesis := range genesisCh {
		err := am.ValidateGenesis(cdc, config, genesis)
		if err != nil {
			return err
		}
	}
	return nil
}

// RegisterRESTRoutes registers module's REST handlers. Currently, this is a no-op.
func (AppModuleBasic) RegisterRESTRoutes(_ client.Context, _ *mux.Router) {}

// RegisterGRPCGatewayRoutes registers the module's gRPC Gateway routes. Currently, this
// is a no-op.
func (a AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *runtime.ServeMux) {}

// GetTxCmd returns the root tx command for the auth module.
func (AppModuleBasic) GetTxCmd() *cobra.Command {
	return cli.GetTxCmd()
}

// GetQueryCmd returns the module's root query command. Currently, this is a no-op.
func (AppModuleBasic) GetQueryCmd() *cobra.Command {
	return nil
}

// AppModule extends the AppModuleBasic implementation by implementing the
// AppModule interface.
//
// The vesting module is deprecated; see AppModuleBasic. Once the deprecation
// gate is active, all message handlers return types.ErrVestingDeprecated.
type AppModule struct {
	AppModuleBasic

	accountKeeper keeper.AccountKeeper
	bankKeeper    types.BankKeeper
	upgradeKeeper types.UpgradeKeeper
}

func NewAppModule(ak keeper.AccountKeeper, bk types.BankKeeper, uk types.UpgradeKeeper) AppModule {
	return AppModule{
		AppModuleBasic: AppModuleBasic{},
		accountKeeper:  ak,
		bankKeeper:     bk,
		upgradeKeeper:  uk,
	}
}

// RegisterInvariants performs a no-op; there are no invariants to enforce.
func (AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// Route returns the module's message router and handler.
func (am AppModule) Route() sdk.Route {
	return sdk.NewRoute(types.RouterKey, NewHandler(am.accountKeeper, am.bankKeeper, am.upgradeKeeper))
}

// QuerierRoute returns an empty string as the module contains no query
// functionality.
func (AppModule) QuerierRoute() string { return "" }

// RegisterServices registers module services.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), NewMsgServerImpl(am.accountKeeper, am.bankKeeper, am.upgradeKeeper))
}

// LegacyQuerierHandler performs a no-op.
func (am AppModule) LegacyQuerierHandler(_ *codec.LegacyAmino) sdk.Querier {
	return nil
}

// InitGenesis performs a no-op.
func (am AppModule) InitGenesis(_ sdk.Context, _ codec.JSONCodec, _ json.RawMessage) []abci.ValidatorUpdate {
	return []abci.ValidatorUpdate{}
}

// ExportGenesis is always empty, as InitGenesis does nothing either.
func (am AppModule) ExportGenesis(_ sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	return am.DefaultGenesis(cdc)
}

func (am AppModule) ExportGenesisStream(ctx sdk.Context, cdc codec.JSONCodec) <-chan json.RawMessage {
	ch := make(chan json.RawMessage)
	go func() {
		ch <- am.ExportGenesis(ctx, cdc)
		close(ch)
	}()
	return ch
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 1 }
