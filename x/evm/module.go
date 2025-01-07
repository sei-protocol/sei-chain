package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	// this line is used by starport scaffolding # 1
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/client/cli"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/migrations"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

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

// ValidateGenesis performs genesis state validation for the evm module.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var genState types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &genState); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return genState.Validate()
}

// ValidateGenesisStream performs genesis state validation for the evm module in a streaming fashion.
func (am AppModuleBasic) ValidateGenesisStream(cdc codec.JSONCodec, config client.TxEncodingConfig, genesisCh <-chan json.RawMessage) error {
	genesisStateCh := make(chan types.GenesisState)
	var err error
	doneCh := make(chan struct{})
	go func() {
		err = types.ValidateStream(genesisStateCh)
		doneCh <- struct{}{}
	}()
	go func() {
		defer close(genesisStateCh)
		for genesis := range genesisCh {
			var data types.GenesisState
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

// RegisterRESTRoutes registers the capability module's REST service handlers.
func (AppModuleBasic) RegisterRESTRoutes(_ client.Context, _ *mux.Router) {}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	_ = types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx))
}

// GetTxCmd returns the capability module's root tx command.
func (a AppModuleBasic) GetTxCmd() *cobra.Command {
	return cli.GetTxCmd()
}

// GetQueryCmd returns the capability module's root query command.
func (AppModuleBasic) GetQueryCmd() *cobra.Command {
	return cli.GetQueryCmd(types.StoreKey)
}

// ----------------------------------------------------------------------------
// AppModule
// ----------------------------------------------------------------------------

// AppModule implements the AppModule interface for the capability module.
type AppModule struct {
	AppModuleBasic

	keeper *keeper.Keeper
}

func NewAppModule(
	cdc codec.Codec,
	keeper *keeper.Keeper,
) AppModule {
	return AppModule{
		AppModuleBasic: NewAppModuleBasic(cdc),
		keeper:         keeper,
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
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQuerier(am.keeper))

	_ = cfg.RegisterMigration(types.ModuleName, 2, func(ctx sdk.Context) error {
		return migrations.AddNewParamsAndSetAllToDefaults(ctx, am.keeper)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 3, func(ctx sdk.Context) error {
		return migrations.AddNewParamsAndSetAllToDefaults(ctx, am.keeper)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 4, func(ctx sdk.Context) error {
		return migrations.StoreCWPointerCode(ctx, am.keeper, true, true, false)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 5, func(ctx sdk.Context) error {
		return migrations.FixTotalSupply(ctx, am.keeper)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 6, func(ctx sdk.Context) error {
		return migrations.StoreCWPointerCode(ctx, am.keeper, false, true, false)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 7, func(ctx sdk.Context) error {
		return migrations.StoreCWPointerCode(ctx, am.keeper, false, true, false)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 8, func(ctx sdk.Context) error {
		if err := migrations.MigrateERCNativePointers(ctx, am.keeper); err != nil {
			return err
		}
		if err := migrations.MigrateERCCW20Pointers(ctx, am.keeper); err != nil {
			return err
		}
		return migrations.MigrateERCCW721Pointers(ctx, am.keeper)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 9, func(ctx sdk.Context) error {
		if err := migrations.StoreCWPointerCode(ctx, am.keeper, true, true, false); err != nil {
			return err
		}
		if err := migrations.MigrateCWERC20Pointers(ctx, am.keeper); err != nil {
			return err
		}
		return migrations.MigrateCWERC721Pointers(ctx, am.keeper)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 10, func(ctx sdk.Context) error {
		return migrations.MigrateCastAddressBalances(ctx, am.keeper)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 11, func(ctx sdk.Context) error {
		return migrations.MigrateDeliverTxHookWasmGasLimitParam(ctx, am.keeper)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 12, func(ctx sdk.Context) error {
		return migrations.MigrateBlockBloom(ctx, am.keeper)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 13, func(ctx sdk.Context) error {
		return migrations.MigrateEip1559Params(ctx, am.keeper)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 14, func(ctx sdk.Context) error {
		return migrations.MigrateEip1559MaxFeePerGas(ctx, am.keeper)
	})

	_ = cfg.RegisterMigration(types.ModuleName, 15, func(ctx sdk.Context) error {
		return migrations.StoreCWPointerCode(ctx, am.keeper, false, false, true)
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

// ExportGenesisStream returns the evm module's exported genesis state as raw JSON bytes in a streaming fashion.
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

// ConsensusVersion implements ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 15 }

// BeginBlock executes all ABCI BeginBlock logic respective to the capability module.
func (am AppModule) BeginBlock(ctx sdk.Context, _ abci.RequestBeginBlock) {
	// clear tx/tx responses from last block
	am.keeper.SetMsgs([]*types.MsgEVMTransaction{})
	am.keeper.SetTxResults([]*abci.ExecTxResult{})
	// mock beacon root if replaying
	if am.keeper.EthReplayConfig.Enabled {
		if beaconRoot := am.keeper.ReplayBlock.BeaconRoot(); beaconRoot != nil {
			blockCtx, err := am.keeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
			if err != nil {
				panic(err)
			}
			statedb := state.NewDBImpl(ctx, am.keeper, false)
			vmenv := vm.NewEVM(*blockCtx, vm.TxContext{}, statedb, types.DefaultChainConfig().EthereumConfig(am.keeper.ChainID(ctx)), vm.Config{})
			core.ProcessBeaconBlockRoot(*beaconRoot, vmenv, statedb)
			_, err = statedb.Finalize()
			if err != nil {
				panic(err)
			}
		}
	}
}

// EndBlock executes all ABCI EndBlock logic respective to the evm module. It
// returns no validator updates.
func (am AppModule) EndBlock(ctx sdk.Context, req abci.RequestEndBlock) []abci.ValidatorUpdate {
	newBaseFee := am.keeper.AdjustDynamicBaseFeePerGas(ctx, uint64(req.BlockGasUsed))
	if newBaseFee != nil {
		metrics.GaugeEvmBlockBaseFee(newBaseFee.TruncateInt().BigInt(), req.Height)
	}
	var coinbase sdk.AccAddress
	if am.keeper.EthBlockTestConfig.Enabled {
		blocks := am.keeper.BlockTest.Json.Blocks
		block, err := blocks[ctx.BlockHeight()-1].Decode()
		if err != nil {
			panic(err)
		}
		coinbase = am.keeper.GetSeiAddressOrDefault(ctx, block.Header_.Coinbase)
	} else if am.keeper.EthReplayConfig.Enabled {
		coinbase = am.keeper.GetSeiAddressOrDefault(ctx, am.keeper.ReplayBlock.Header_.Coinbase)
		am.keeper.SetReplayedHeight(ctx)
	} else {
		coinbase = am.keeper.AccountKeeper().GetModuleAddress(authtypes.FeeCollectorName)
	}
	evmTxDeferredInfoList := am.keeper.GetAllEVMTxDeferredInfo(ctx)
	denom := am.keeper.GetBaseDenom(ctx)
	surplus := am.keeper.GetAnteSurplusSum(ctx)
	for _, deferredInfo := range evmTxDeferredInfoList {
		txHash := common.BytesToHash(deferredInfo.TxHash)
		if deferredInfo.Error != "" && txHash.Cmp(ethtypes.EmptyTxsHash) != 0 {
			_ = am.keeper.SetTransientReceipt(ctx, txHash, &types.Receipt{
				TxHashHex:        txHash.Hex(),
				TransactionIndex: deferredInfo.TxIndex,
				VmError:          deferredInfo.Error,
				BlockNumber:      uint64(ctx.BlockHeight()),
			})
			continue
		}
		idx := int(deferredInfo.TxIndex)
		coinbaseAddress := state.GetCoinbaseAddress(idx)
		balance := am.keeper.BankKeeper().SpendableCoins(ctx, coinbaseAddress).AmountOf(denom)
		weiBalance := am.keeper.BankKeeper().GetWeiBalance(ctx, coinbaseAddress)
		if !balance.IsZero() || !weiBalance.IsZero() {
			if err := am.keeper.BankKeeper().SendCoinsAndWei(ctx, coinbaseAddress, coinbase, balance, weiBalance); err != nil {
				ctx.Logger().Error(fmt.Sprintf("failed to send usei surplus from %s to coinbase account due to %s", coinbaseAddress.String(), err))
			}
		}
		surplus = surplus.Add(deferredInfo.Surplus)
	}
	if surplus.IsPositive() {
		surplusUsei, surplusWei := state.SplitUseiWeiAmount(surplus.BigInt())
		if surplusUsei.GT(sdk.ZeroInt()) {
			if err := am.keeper.BankKeeper().AddCoins(ctx, am.keeper.AccountKeeper().GetModuleAddress(types.ModuleName), sdk.NewCoins(sdk.NewCoin(am.keeper.GetBaseDenom(ctx), surplusUsei)), true); err != nil {
				ctx.Logger().Error("failed to send usei surplus of %s to EVM module account", surplusUsei)
			}
		}
		if surplusWei.GT(sdk.ZeroInt()) {
			if err := am.keeper.BankKeeper().AddWei(ctx, am.keeper.AccountKeeper().GetModuleAddress(types.ModuleName), surplusWei); err != nil {
				ctx.Logger().Error("failed to send wei surplus of %s to EVM module account", surplusWei)
			}
		}
	}
	am.keeper.SetBlockBloom(ctx, utils.Map(evmTxDeferredInfoList, func(i *types.DeferredInfo) ethtypes.Bloom { return ethtypes.BytesToBloom(i.TxBloom) }))
	return []abci.ValidatorUpdate{}
}
