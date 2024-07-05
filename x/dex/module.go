package dex

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/utils/tracing"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/client/cli/query"
	"github.com/sei-protocol/sei-chain/x/dex/client/cli/tx"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	dexkeeperabci "github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	dexkeeperquery "github.com/sei-protocol/sei-chain/x/dex/keeper/query"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	"github.com/sei-protocol/sei-chain/x/store"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// ----------------------------------------------------------------------------
// AppModuleBasic
// ----------------------------------------------------------------------------

// AppModuleBasic implements the AppModuleBasic interface for the capability module.
type AppModuleBasic struct {
	cdc codec.BinaryCodec
}

func NewAppModuleBasic(cdc codec.BinaryCodec) AppModuleBasic {
	return AppModuleBasic{cdc: cdc}
}

// Name returns the capability module's name.
func (AppModuleBasic) Name() string {
	return types.ModuleName
}

func (AppModuleBasic) RegisterCodec(cdc *codec.LegacyAmino) {
	types.RegisterCodec(cdc)
}

func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterCodec(cdc)
}

// RegisterInterfaces registers the module's interface types
func (a AppModuleBasic) RegisterInterfaces(reg cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(reg)
}

// DefaultGenesis returns the capability module's default genesis state.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis performs genesis state validation for the capability module.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var genState types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &genState); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return genState.Validate()
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

// RegisterRESTRoutes registers the capability module's REST service handlers.
func (AppModuleBasic) RegisterRESTRoutes(_ client.Context, _ *mux.Router) {
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)) //nolint:errcheck // this is inside a module, and the method doesn't return error.  Leave it alone.
}

// GetTxCmd returns the capability module's root tx command.
func (a AppModuleBasic) GetTxCmd() *cobra.Command {
	return tx.GetTxCmd()
}

// GetQueryCmd returns the capability module's root query command.
func (AppModuleBasic) GetQueryCmd() *cobra.Command {
	return query.GetQueryCmd()
}

// ----------------------------------------------------------------------------
// AppModule
// ----------------------------------------------------------------------------

// AppModule implements the AppModule interface for the capability module.
type AppModule struct {
	AppModuleBasic

	keeper        keeper.Keeper
	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	wasmKeeper    wasm.Keeper

	abciWrapper dexkeeperabci.KeeperWrapper

	tracingInfo *tracing.Info
}

func NewAppModule(
	cdc codec.Codec,
	keeper keeper.Keeper,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	wasmKeeper wasm.Keeper,
	tracingInfo *tracing.Info,
) AppModule {
	return AppModule{
		AppModuleBasic: NewAppModuleBasic(cdc),
		keeper:         keeper,
		accountKeeper:  accountKeeper,
		bankKeeper:     bankKeeper,
		wasmKeeper:     wasmKeeper,
		abciWrapper:    dexkeeperabci.KeeperWrapper{Keeper: &keeper},
		tracingInfo:    tracingInfo,
	}
}

// Name returns the capability module's name.
func (am AppModule) Name() string {
	return am.AppModuleBasic.Name()
}

// Route returns the capability module's message routing key.
func (am AppModule) Route() sdk.Route {
	return sdk.NewRoute(types.RouterKey, NewHandler(am.keeper))
}

// QuerierRoute returns the capability module's query routing key.
func (AppModule) QuerierRoute() string { return types.QuerierRoute }

// LegacyQuerierHandler returns the capability module's Querier.
func (am AppModule) LegacyQuerierHandler(_ *codec.LegacyAmino) sdk.Querier {
	return nil
}

// RegisterServices registers a GRPC query service to respond to the
// module-specific GRPC queries.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), msgserver.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), dexkeeperquery.KeeperWrapper{Keeper: &am.keeper})

	_ = cfg.RegisterMigration(types.ModuleName, 1, func(ctx sdk.Context) error { return nil })
	_ = cfg.RegisterMigration(types.ModuleName, 2, func(ctx sdk.Context) error {
		return migrations.DataTypeUpdate(ctx, am.keeper.GetStoreKey(), am.keeper.Cdc)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 3, func(ctx sdk.Context) error {
		return migrations.PriceSnapshotUpdate(ctx, am.keeper.Paramstore)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 4, func(ctx sdk.Context) error {
		return migrations.V4ToV5(ctx, am.keeper.GetStoreKey(), am.keeper.Paramstore)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 5, func(ctx sdk.Context) error {
		return migrations.V5ToV6(ctx, am.keeper.GetStoreKey(), am.keeper.Cdc)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 6, func(ctx sdk.Context) error {
		return migrations.V6ToV7(ctx, am.keeper.GetStoreKey())
	})
	_ = cfg.RegisterMigration(types.ModuleName, 7, func(ctx sdk.Context) error {
		return migrations.V7ToV8(ctx, am.keeper.GetStoreKey())
	})
	_ = cfg.RegisterMigration(types.ModuleName, 8, func(ctx sdk.Context) error {
		return migrations.V8ToV9(ctx, am.keeper)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 9, func(ctx sdk.Context) error {
		return migrations.V9ToV10(ctx, am.keeper)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 10, func(ctx sdk.Context) error {
		return migrations.V10ToV11(ctx, am.keeper)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 11, func(ctx sdk.Context) error {
		return migrations.V11ToV12(ctx, am.keeper)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 12, func(ctx sdk.Context) error {
		return migrations.V12ToV13(ctx, am.keeper)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 13, func(ctx sdk.Context) error {
		return migrations.V13ToV14(ctx, am.keeper)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 14, func(ctx sdk.Context) error {
		return migrations.V14ToV15(ctx, am.keeper)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 15, func(ctx sdk.Context) error {
		return migrations.V15ToV16(ctx, am.keeper)
	})
	_ = cfg.RegisterMigration(types.ModuleName, 16, func(ctx sdk.Context) error {
		return nil
	})
	_ = cfg.RegisterMigration(types.ModuleName, 17, func(ctx sdk.Context) error {
		return migrations.V17ToV18(ctx, am.keeper)
	})
}

// RegisterInvariants registers the capability module's invariants.
func (am AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// InitGenesis performs the capability module's genesis initialization It returns
// no validator updates.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, gs json.RawMessage) []abci.ValidatorUpdate {
	var genState types.GenesisState
	// Initialize global index to index in genesis state
	cdc.MustUnmarshalJSON(gs, &genState)

	InitGenesis(ctx, am.keeper, genState)

	return []abci.ValidatorUpdate{}
}

// ExportGenesis returns the capability module's exported genesis state as raw JSON bytes.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	genState := ExportGenesis(ctx, am.keeper)
	return cdc.MustMarshalJSON(genState)
}

func (am AppModule) StreamGenesis(ctx sdk.Context, cdc codec.JSONCodec) <-chan json.RawMessage {
	ch := make(chan json.RawMessage)
	go func() {
		ch <- am.ExportGenesis(ctx, cdc)
		close(ch)
	}()
	return ch
}

// ConsensusVersion implements ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 18 }

// BeginBlock executes all ABCI BeginBlock logic respective to the capability module.
func (am AppModule) BeginBlock(ctx sdk.Context, _ abci.RequestBeginBlock) {
	defer func() {
		_, span := am.tracingInfo.Start("DexBeginBlockRollback")
		defer span.End()
	}()

	dexutils.GetMemState(ctx.Context()).Clear(ctx)
	isNewEpoch, currentEpoch := am.keeper.IsNewEpoch(ctx)
	if isNewEpoch {
		am.keeper.SetEpoch(ctx, currentEpoch)
	}
	cachedCtx, cachedStore := store.GetCachedContext(ctx)
	priceRetention := am.keeper.GetParams(ctx).PriceSnapshotRetention
	cutOffTime := uint64(ctx.BlockTime().Unix()) - priceRetention
	wg := sync.WaitGroup{}
	mutex := sync.Mutex{}
	allContracts := am.keeper.GetAllProcessableContractInfo(ctx)
	allPricesToDelete := make(map[string][]*types.PriceStore, len(allContracts))

	// Parallelize the logic to find all prices to delete
	for _, contract := range allContracts {
		wg.Add(1)
		go func(contract types.ContractInfoV2) {
			priceKeysToDelete := am.getPriceToDelete(cachedCtx, contract, cutOffTime)
			mutex.Lock()
			allPricesToDelete[contract.ContractAddr] = priceKeysToDelete
			mutex.Unlock()
			wg.Done()
		}(contract)
	}
	wg.Wait()

	// Execute the deletion in order
	for _, contract := range allContracts {
		if priceStores, found := allPricesToDelete[contract.ContractAddr]; found {
			for _, priceStore := range priceStores {
				for _, key := range priceStore.PriceKeys {
					priceStore.Store.Delete(key)
				}
			}
		}
	}
	// only write if all contracts have been processed
	cachedStore.Write()
}

func (am AppModule) getPriceToDelete(
	ctx sdk.Context,
	contract types.ContractInfoV2,
	timestamp uint64,
) []*types.PriceStore {
	var result []*types.PriceStore
	if contract.NeedOrderMatching {
		for _, pair := range am.keeper.GetAllRegisteredPairs(ctx, contract.ContractAddr) {
			store := prefix.NewStore(ctx.KVStore(am.keeper.GetStoreKey()), types.PricePrefix(contract.ContractAddr, pair.PriceDenom, pair.AssetDenom))
			keysToDelete := am.keeper.GetPriceKeysToDelete(store, timestamp)
			result = append(result, &types.PriceStore{
				Store:     store,
				PriceKeys: keysToDelete,
			})
		}
	}
	return result
}

// EndBlock executes all ABCI EndBlock logic respective to the capability module. It
// returns no validator updates.
func (am AppModule) EndBlock(ctx sdk.Context, _ abci.RequestEndBlock) (ret []abci.ValidatorUpdate) {
	defer func() {
		if err := recover(); err != nil {
			telemetry.IncrCounter(1, "recovered_panics")
			ctx.Logger().Error(fmt.Sprintf("panic in endblock recovered: %s", err))
		}
	}()
	_, span := am.tracingInfo.Start("DexEndBlock")
	defer span.End()
	defer dexutils.GetMemState(ctx.Context()).Clear(ctx)

	validContractsInfo := am.keeper.GetAllProcessableContractInfo(ctx)
	// Each iteration is atomic. If an iteration finishes without any error, it will return,
	// otherwise it will rollback any state change, filter out contracts that cause the error,
	// and proceed to the next iteration. The loop is guaranteed to finish since
	// `validContractAddresses` will always decrease in size every iteration.
	iterCounter := len(validContractsInfo)
	endBlockerStartTime := time.Now()
	for len(validContractsInfo) > 0 {
		newValidContractsInfo, newOutOfRentContractsInfo, failedContractToReasons, ctx, ok := contract.EndBlockerAtomic(ctx, &am.keeper, validContractsInfo, am.tracingInfo)
		if ok {
			break
		}
		telemetry.IncrCounter(float32(len(newOutOfRentContractsInfo)), am.Name(), "total_out_of_rent_contracts")
		keptContractAddrs := datastructures.NewSyncSet(utils.Map(newValidContractsInfo, func(c types.ContractInfoV2) string { return c.ContractAddr }))
		keptContractAddrs.AddAll(utils.Map(newOutOfRentContractsInfo, func(c types.ContractInfoV2) string { return c.ContractAddr }))
		for failedContract, reason := range failedContractToReasons {
			ctx.Logger().Info(fmt.Sprintf("Suspending invalid contract %s", failedContract))
			err := am.keeper.SuspendContract(ctx, failedContract, reason)
			if err != nil {
				ctx.Logger().Error(fmt.Sprintf("failed to suspend invalid contract %s: %s", failedContract, err))
			}
			telemetry.IncrCounter(float32(1), am.Name(), "total_suspended_contracts")
		}
		validContractsInfo = am.keeper.GetAllProcessableContractInfo(ctx) // reload contract info to get updated dependencies due to unregister above
		if len(failedContractToReasons) != 0 {
			dexutils.GetMemState(ctx.Context()).ClearContractToDependencies(ctx)
		}
		// technically we don't really need this if `EndBlockerAtomic` guarantees that `validContractsInfo` size will
		// always shrink if not `ok`, but just in case, we decided to have an explicit termination criteria here to
		// prevent the chain from being stuck.
		iterCounter--
		if iterCounter == 0 {
			ctx.Logger().Error("All contracts failed in dex EndBlock. Doing nothing.")
			break
		}
	}
	telemetry.MeasureSince(endBlockerStartTime, am.Name(), "total_end_blocker_atomic")

	return []abci.ValidatorUpdate{}
}
