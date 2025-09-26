package seinet

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/sei-protocol/sei-chain/x/seinet/keeper"
	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

// ensure module interfaces
var _ module.AppModule = AppModule{}
var _ module.AppModuleBasic = AppModuleBasic{}

// AppModuleBasic defines basic application module used by the seinet module.
type AppModuleBasic struct{}

// Name returns module name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// DefaultGenesis returns default genesis state as raw bytes for the seinet module.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis performs genesis state validation for the seinet module.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var genesis types.GenesisState
	return cdc.UnmarshalJSON(bz, &genesis)
}

// AppModule implements module.AppModule.
type AppModule struct {
	AppModuleBasic
	keeper keeper.Keeper
}

// NewAppModule creates a new AppModule object.
func NewAppModule(k keeper.Keeper) AppModule {
	return AppModule{keeper: k}
}

// Name returns the module's name.
func (am AppModule) Name() string { return types.ModuleName }

// RegisterServices registers module services.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.keeper))
}

// InitGenesis performs genesis initialization for the seinet module.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, bz json.RawMessage) []abci.ValidatorUpdate {
	var genesis types.GenesisState
	cdc.MustUnmarshalJSON(bz, &genesis)
	// no-op initialization
	return []abci.ValidatorUpdate{}
}

// ExportGenesis returns the exported genesis state as raw bytes for the seinet module.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}
