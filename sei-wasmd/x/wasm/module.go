package wasm

import (
	"context"
	"encoding/json"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	simKeeper "github.com/cosmos/cosmos-sdk/x/simulation"
	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/CosmWasm/wasmd/x/wasm/client/cli"
	"github.com/CosmWasm/wasmd/x/wasm/client/rest"
	"github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/CosmWasm/wasmd/x/wasm/simulation"
	"github.com/CosmWasm/wasmd/x/wasm/types"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// Module init related flags
const (
	flagWasmMemoryCacheSize    = "wasm.memory_cache_size"
	flagWasmQueryGasLimit      = "wasm.query_gas_limit"
	flagWasmSimulationGasLimit = "wasm.simulation_gas_limit"
)

// AppModuleBasic defines the basic application module used by the wasm module.
type AppModuleBasic struct{}

func (b AppModuleBasic) RegisterLegacyAminoCodec(amino *codec.LegacyAmino) { //nolint:staticcheck
	RegisterCodec(amino)
}

func (b AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, serveMux *runtime.ServeMux) {
	err := types.RegisterQueryHandlerClient(context.Background(), serveMux, types.NewQueryClient(clientCtx))
	if err != nil {
		panic(err)
	}
}

// Name returns the wasm module's name.
func (AppModuleBasic) Name() string {
	return ModuleName
}

// DefaultGenesis returns default genesis state as raw bytes for the wasm
// module.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(&GenesisState{
		Params: DefaultParams(),
	})
}

// ValidateGenesis performs genesis state validation for the wasm module.
func (b AppModuleBasic) ValidateGenesis(marshaler codec.JSONCodec, config client.TxEncodingConfig, message json.RawMessage) error {
	var data GenesisState
	err := marshaler.UnmarshalJSON(message, &data)
	if err != nil {
		return err
	}
	return ValidateGenesis(data)
}

func (am AppModuleBasic) ValidateGenesisStream(cdc codec.JSONCodec, config client.TxEncodingConfig, genesisCh <-chan json.RawMessage) error {
	var err error
	doneCh := make(chan struct{})
	genesisStateCh := make(chan GenesisState)
	go func() {
		err = types.ValidateGenesisStream(genesisStateCh)
		doneCh <- struct{}{}
	}()
	go func() {
		defer close(genesisStateCh)
		for genesis := range genesisCh {
			var data GenesisState
			err_ := cdc.UnmarshalJSON(genesis, &data)
			if err_ != nil {
				err = err_
				doneCh <- struct{}{}
				return
			}
			genesisStateCh <- data
		}
	}()
	<-doneCh
	return err
}

// RegisterRESTRoutes registers the REST routes for the wasm module.
func (AppModuleBasic) RegisterRESTRoutes(cliCtx client.Context, rtr *mux.Router) {
	rest.RegisterRoutes(cliCtx, rtr)
}

// GetTxCmd returns the root tx command for the wasm module.
func (b AppModuleBasic) GetTxCmd() *cobra.Command {
	return cli.GetTxCmd()
}

// GetQueryCmd returns no root query command for the wasm module.
func (b AppModuleBasic) GetQueryCmd() *cobra.Command {
	return cli.GetQueryCmd()
}

// RegisterInterfaces implements InterfaceModule
func (b AppModuleBasic) RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// ____________________________________________________________________________

// AppModule implements an application module for the wasm module.
type AppModule struct {
	AppModuleBasic
	cdc                codec.Codec
	keeper             *Keeper
	validatorSetSource keeper.ValidatorSetSource
	accountKeeper      types.AccountKeeper // for simulation
	bankKeeper         simKeeper.BankKeeper
}

// ConsensusVersion is a sequence number for state-breaking change of the
// module. It should be incremented on each consensus-breaking change
// introduced by the module. To avoid wrong/empty versions, the initial version
// should be set to 1.
func (AppModule) ConsensusVersion() uint64 { return 1 }

// NewAppModule creates a new AppModule object
func NewAppModule(
	cdc codec.Codec,
	keeper *Keeper,
	validatorSetSource keeper.ValidatorSetSource,
	ak types.AccountKeeper,
	bk simKeeper.BankKeeper,
) AppModule {
	return AppModule{
		AppModuleBasic:     AppModuleBasic{},
		cdc:                cdc,
		keeper:             keeper,
		validatorSetSource: validatorSetSource,
		accountKeeper:      ak,
		bankKeeper:         bk,
	}
}

func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(keeper.NewDefaultPermissionKeeper(am.keeper)))
	types.RegisterQueryServer(cfg.QueryServer(), NewQuerier(am.keeper))
}

func (am AppModule) LegacyQuerierHandler(amino *codec.LegacyAmino) sdk.Querier { //nolint:staticcheck
	return keeper.NewLegacyQuerier(am.keeper, am.keeper.QueryGasLimit())
}

// RegisterInvariants registers the wasm module invariants.
func (am AppModule) RegisterInvariants(ir sdk.InvariantRegistry) {}

// Route returns the message routing key for the wasm module.
func (am AppModule) Route() sdk.Route {
	return sdk.NewRoute(RouterKey, NewHandler(keeper.NewDefaultPermissionKeeper(am.keeper)))
}

// QuerierRoute returns the wasm module's querier route name.
func (AppModule) QuerierRoute() string {
	return QuerierRoute
}

// InitGenesis performs genesis initialization for the wasm module. It returns
// no validator updates.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) []abci.ValidatorUpdate {
	var genesisState GenesisState
	cdc.MustUnmarshalJSON(data, &genesisState)
	validators, err := InitGenesis(ctx, am.keeper, genesisState, am.validatorSetSource, am.Route().Handler())
	if err != nil {
		panic(err)
	}
	return validators
}

// ExportGenesis returns the exported genesis state as raw bytes for the wasm
// module.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	gs := ExportGenesis(ctx, am.keeper)
	marshalled := cdc.MustMarshalJSON(gs)
	return marshalled
}

func (am AppModule) ExportGenesisStream(ctx sdk.Context, cdc codec.JSONCodec) <-chan json.RawMessage {
	ch := ExportGenesisStream(ctx, am.keeper)
	chRaw := make(chan json.RawMessage)
	go func() {
		for genState := range ch {
			chRaw <- cdc.MustMarshalJSON(genState)
		}
		close(chRaw)
	}()
	return chRaw
}

// BeginBlock returns the begin blocker for the wasm module.
func (am AppModule) BeginBlock(_ sdk.Context, _ abci.RequestBeginBlock) {}

// EndBlock returns the end blocker for the wasm module. It returns no validator
// updates.
func (AppModule) EndBlock(_ sdk.Context, _ abci.RequestEndBlock) []abci.ValidatorUpdate {
	return []abci.ValidatorUpdate{}
}

// ____________________________________________________________________________

// AppModuleSimulation functions

// GenerateGenesisState creates a randomized GenState of the bank module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomizedGenState(simState)
}

// ProposalContents doesn't return any content functions for governance proposals.
func (AppModule) ProposalContents(simState module.SimulationState) []simtypes.WeightedProposalContent {
	return nil
}

// RandomizedParams creates randomized bank param changes for the simulator.
func (am AppModule) RandomizedParams(r *rand.Rand) []simtypes.ParamChange {
	return simulation.ParamChanges(r, am.cdc)
}

// RegisterStoreDecoder registers a decoder for supply module's types
func (am AppModule) RegisterStoreDecoder(sdr sdk.StoreDecoderRegistry) {
}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	return simulation.WeightedOperations(&simState, am.accountKeeper, am.bankKeeper, am.keeper)
}

// ____________________________________________________________________________

// AddModuleInitFlags implements servertypes.ModuleInitFlags interface.
func AddModuleInitFlags(startCmd *cobra.Command) {
	defaults := DefaultWasmConfig()
	startCmd.Flags().Uint32(flagWasmMemoryCacheSize, defaults.MemoryCacheSize, "Sets the size in MiB (NOT bytes) of an in-memory cache for Wasm modules. Set to 0 to disable.")
	startCmd.Flags().Uint64(flagWasmQueryGasLimit, defaults.SmartQueryGasLimit, "Set the max gas that can be spent on executing a query with a Wasm contract")
	startCmd.Flags().String(flagWasmSimulationGasLimit, "", "Set the max gas that can be spent when executing a simulation TX")
}

// ReadWasmConfig reads the wasm specifig configuration
func ReadWasmConfig(opts servertypes.AppOptions) (types.WasmConfig, error) {
	cfg := types.DefaultWasmConfig()
	var err error
	if v := opts.Get(flagWasmMemoryCacheSize); v != nil {
		if cfg.MemoryCacheSize, err = cast.ToUint32E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagWasmQueryGasLimit); v != nil {
		if cfg.SmartQueryGasLimit, err = cast.ToUint64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagWasmSimulationGasLimit); v != nil {
		if raw, ok := v.(string); ok && raw != "" {
			limit, err := cast.ToUint64E(v) // non empty string set
			if err != nil {
				return cfg, err
			}
			cfg.SimulationGasLimit = &limit
		}
	}
	// attach contract debugging to global "trace" flag
	if v := opts.Get(server.FlagTrace); v != nil {
		if cfg.ContractDebugMode, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
