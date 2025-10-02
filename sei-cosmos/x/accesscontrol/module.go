package accesscontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"

	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	cli "github.com/cosmos/cosmos-sdk/x/accesscontrol/client/cli"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/constants"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/migrations"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

var (
	_ module.AppModule           = AppModule{}
	_ module.AppModuleBasic      = AppModuleBasic{}
	_ module.AppModuleSimulation = AppModule{}
)

// AppModuleBasic defines the basic application module used by the accesscontrol module.
type AppModuleBasic struct {
	cdc codec.Codec
}

var _ module.AppModuleBasic = AppModuleBasic{}

// Name returns the accesscontrol module's name.
func (AppModuleBasic) Name() string {
	return types.ModuleName
}

// RegisterLegacyAminoCodec registers the accesscontrol module's types on the given LegacyAmino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterCodec(cdc)
}

// RegisterInterfaces registers the module's interface types
func (b AppModuleBasic) RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// DefaultGenesis returns default genesis state as raw bytes for the accesscontrol
// module.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// ValidateGenesis performs genesis state validation for the accesscontrol module.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, config client.TxEncodingConfig, bz json.RawMessage) error {
	var data types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}

	return data.Params.Validate()
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

// RegisterRESTRoutes registers the REST routes for the accesscontrol module.
func (AppModuleBasic) RegisterRESTRoutes(clientCtx client.Context, rtr *mux.Router) {}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the accesscontrol module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx))
}

// GetTxCmd returns no root tx command for the accesscontrol module.
func (AppModuleBasic) GetTxCmd() *cobra.Command { return cli.GetTxCmd() }

// GetQueryCmd returns the root query command for the accesscontrol module.
func (AppModuleBasic) GetQueryCmd() *cobra.Command { return cli.GetQueryCmd() }

// AppModule implements an application module for the accesscontrol module.
type AppModule struct {
	AppModuleBasic

	keeper keeper.Keeper
}

// NewAppModule creates a new AppModule object
func NewAppModule(cdc codec.Codec, keeper keeper.Keeper) AppModule {
	return AppModule{
		AppModuleBasic: AppModuleBasic{cdc: cdc},
		keeper:         keeper,
	}
}

// Name returns the accesscontrol module's name.
func (AppModule) Name() string {
	return types.ModuleName
}

// RegisterInvariants registers the accesscontrol module invariants.
func (am AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// Route returns the message routing key for the accesscontrol module.
func (am AppModule) Route() sdk.Route {
	return sdk.NewRoute(types.RouterKey, NewHandler(am.keeper))
}

// QuerierRoute returns the accesscontrol module's querier route name.
func (AppModule) QuerierRoute() string {
	return types.QuerierRoute
}

// LegacyQuerierHandler returns the x/accesscontrol module's sdk.Querier.
func (am AppModule) LegacyQuerierHandler(legacyQuerierCdc *codec.LegacyAmino) sdk.Querier {
	return func(sdk.Context, []string, abci.RequestQuery) ([]byte, error) {
		return nil, fmt.Errorf("legacy querier not supported for the x/%s module", types.ModuleName)
	}
}

// RegisterServices registers a gRPC query service to respond to the
// module-specific gRPC queries.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), am.keeper)

	_ = cfg.RegisterMigration(types.ModuleName, 1, func(ctx sdk.Context) error {
		return migrations.V1ToV2(ctx, am.keeper.GetStoreKey())
	})
}

// InitGenesis performs genesis initialization for the accesscontrol module. It returns
// no validator updates.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) []abci.ValidatorUpdate {
	var genState types.GenesisState
	cdc.MustUnmarshalJSON(data, &genState)

	am.keeper.InitGenesis(ctx, genState)

	return []abci.ValidatorUpdate{}
}

// ExportGenesis returns the exported genesis state as raw bytes for the mint
// module.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	genState := am.keeper.ExportGenesis(ctx)
	return cdc.MustMarshalJSON(genState)
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
func (AppModule) ConsensusVersion() uint64 { return 2 }

// BeginBlock returns the begin blocker for the accesscontrol module.
func (am AppModule) BeginBlock(ctx sdk.Context, _ abci.RequestBeginBlock) {}

// EndBlock returns the end blocker for the accesscontrol module. It returns no validator
// updates.
func (am AppModule) EndBlock(ctx sdk.Context, _ abci.RequestEndBlock) []abci.ValidatorUpdate {
	badWasmDependencyAddresses := ctx.Context().Value(constants.BadWasmDependencyAddressesKey)
	if badWasmDependencyAddresses != nil {
		typedBadWasmDependencyAddresses, ok := badWasmDependencyAddresses.([]sdk.AccAddress)
		if ok && typedBadWasmDependencyAddresses != nil {
			for _, addr := range typedBadWasmDependencyAddresses {
				am.keeper.ResetWasmDependencyMapping(ctx, addr, constants.ResetReasonBadWasmDependency)
			}
		}
	}
	return []abci.ValidatorUpdate{}
}

// GenerateGenesisState creates a randomized GenState of the accesscontrol module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// ProposalContents doesn't return any content functions for accesscontrol proposals.
func (AppModule) ProposalContents(simState module.SimulationState) []simtypes.WeightedProposalContent {
	return nil
}

// RandomizedParams creates randomized accesscontrol param changes for the simulator.
func (AppModule) RandomizedParams(r *rand.Rand) []simtypes.ParamChange {
	return nil
}

// RegisterStoreDecoder registers a decoder for accesscontrol module's types.
func (am AppModule) RegisterStoreDecoder(sdr sdk.StoreDecoderRegistry) {}

// WeightedOperations doesn't return any accesscontrol module operation.
func (AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
