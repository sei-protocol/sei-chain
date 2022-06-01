package dex

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/attribute"

	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/sei-protocol/sei-chain/utils/tracing"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/client/cli"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
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
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, config client.TxEncodingConfig, bz json.RawMessage) error {
	var genState types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &genState); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return genState.Validate()
}

// RegisterRESTRoutes registers the capability module's REST service handlers.
func (AppModuleBasic) RegisterRESTRoutes(clientCtx client.Context, rtr *mux.Router) {
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx))
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

	keeper        keeper.Keeper
	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	wasmKeeper    wasm.Keeper

	tracingInfo *tracing.TracingInfo
}

func NewAppModule(
	cdc codec.Codec,
	keeper keeper.Keeper,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	wasmKeeper wasm.Keeper,
	tracingInfo *tracing.TracingInfo,
) AppModule {
	return AppModule{
		AppModuleBasic: NewAppModuleBasic(cdc),
		keeper:         keeper,
		accountKeeper:  accountKeeper,
		bankKeeper:     bankKeeper,
		wasmKeeper:     wasmKeeper,
		tracingInfo:    tracingInfo,
	}
}

// Name returns the capability module's name.
func (am AppModule) Name() string {
	return am.AppModuleBasic.Name()
}

// Route returns the capability module's message routing key.
func (am AppModule) Route() sdk.Route {
	return sdk.NewRoute(types.RouterKey, NewHandler(am.keeper, am.tracingInfo))
}

// QuerierRoute returns the capability module's query routing key.
func (AppModule) QuerierRoute() string { return types.QuerierRoute }

// LegacyQuerierHandler returns the capability module's Querier.
func (am AppModule) LegacyQuerierHandler(legacyQuerierCdc *codec.LegacyAmino) sdk.Querier {
	return nil
}

// RegisterServices registers a GRPC query service to respond to the
// module-specific GRPC queries.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper, am.tracingInfo))
	types.RegisterQueryServer(cfg.QueryServer(), am.keeper)

	cfg.RegisterMigration(types.ModuleName, 1, func(ctx sdk.Context) error { return nil })
	cfg.RegisterMigration(types.ModuleName, 2, func(ctx sdk.Context) error { return migrations.DataTypeUpdate(ctx, am.keeper.Cdc) })
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

// ConsensusVersion implements ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 3 }

func (am AppModule) getAllContractAddresses(ctx sdk.Context) []string {
	return am.keeper.GetAllContractAddresses(ctx)
}

func (am AppModule) callClearingHouseContractSudo(ctx sdk.Context, msg []byte, contractAddrStr string) {
	contractAddr, err := sdk.AccAddressFromBech32(contractAddrStr)
	if err != nil {
		ctx.Logger().Info(err.Error())
	}
	_, err = am.wasmKeeper.Sudo(
		ctx, contractAddr, msg,
	)
	if err != nil {
		ctx.Logger().Error(err.Error())
	}
}

// BeginBlock executes all ABCI BeginBlock logic respective to the capability module.
func (am AppModule) BeginBlock(ctx sdk.Context, _ abci.RequestBeginBlock) {
	for _, contractAddr := range am.getAllContractAddresses(ctx) {
		am.beginBlockForContract(ctx, contractAddr)
	}
}

func (am AppModule) beginBlockForContract(ctx sdk.Context, contractAddr string) {
	_, span := (*am.tracingInfo.Tracer).Start(am.tracingInfo.TracerContext, "DexBeginBlock")
	span.SetAttributes(attribute.String("contract", contractAddr))
	defer span.End()

	am.keeper.Orders[contractAddr] = map[string]*dexcache.Orders{}
	am.keeper.OrderPlacements[contractAddr] = map[string]*dexcache.OrderPlacements{}
	am.keeper.OrderCancellations[contractAddr] = map[string]*dexcache.OrderCancellations{}
	am.keeper.DepositInfo[contractAddr] = dexcache.NewDepositInfo()
	am.keeper.LiquidationRequests[contractAddr] = map[string]string{}
	for _, pair := range am.keeper.GetAllRegisteredPairs(ctx, contractAddr) {
		ctx.Logger().Info(pair.String())
		am.keeper.Orders[contractAddr][pair.String()] = dexcache.NewOrders()
		am.keeper.OrderPlacements[contractAddr][pair.String()] = dexcache.NewOrderPlacements()
		am.keeper.OrderCancellations[contractAddr][pair.String()] = dexcache.NewOrderCancellations()
	}
	ctx.Logger().Info(fmt.Sprintf("Orders %s, %s", am.keeper.Orders, contractAddr))

	if isNewEpoch, currentEpoch := am.keeper.IsNewEpoch(ctx); isNewEpoch {
		ctx.Logger().Info(fmt.Sprintf("Updating funding payment rate for epoch %d", currentEpoch))
		for _, twap := range am.keeper.GetAllTwaps(ctx, contractAddr) {
			dexPrice := twap.TwapPrice
			ctx.Logger().Info(fmt.Sprintf("%s/%s: %d", twap.PriceDenom, twap.AssetDenom, dexPrice))
			oraclePrice := uint64(100) // TODO: replace with oracle call
			var diff uint64
			var negative bool
			if dexPrice < oraclePrice {
				diff = oraclePrice - dexPrice
				negative = true
			} else {
				diff = dexPrice - oraclePrice
				negative = false
			}
			nativeSetFPRMsg := types.SudoSetFundingPaymentRateMsg{
				SetFundingPaymentRate: types.SetFundingPaymentRate{
					Epoch:      currentEpoch,
					AssetDenom: twap.AssetDenom,
					PriceDiff:  strconv.FormatUint(diff, 10),
					Negative:   negative,
				},
			}
			wasmMsg, err := json.Marshal(nativeSetFPRMsg)
			if err != nil {
				ctx.Logger().Info(err.Error())
			}

			ctx.Logger().Info("Setting funding payment rate")
			am.callClearingHouseContractSudo(ctx, wasmMsg, contractAddr)

			var newPrices []uint64
			if len(twap.Prices) == 24 { // replace with config
				newPrices = append(twap.Prices[1:], twap.Prices[len(twap.Prices)-1])
			} else {
				newPrices = append(twap.Prices, twap.Prices[len(twap.Prices)-1])
			}
			am.keeper.SetTwap(ctx, types.Twap{
				LastEpoch:  currentEpoch,
				Prices:     newPrices,
				TwapPrice:  getTwapPrice(newPrices),
				PriceDenom: twap.PriceDenom,
				AssetDenom: twap.AssetDenom,
			}, contractAddr)
		}
		am.keeper.SetEpoch(ctx, currentEpoch)
	}
}

// EndBlock executes all ABCI EndBlock logic respective to the capability module. It
// returns no validator updates.
func (am AppModule) EndBlock(ctx sdk.Context, _ abci.RequestEndBlock) []abci.ValidatorUpdate {
	for _, contractAddr := range am.getAllContractAddresses(ctx) {
		ctx.Logger().Info(fmt.Sprintf("End block for %s", contractAddr))
		am.endBlockForContract(ctx, contractAddr)
	}
	return []abci.ValidatorUpdate{}
}

func (am AppModule) endBlockForContract(ctx sdk.Context, contractAddr string) {
	spanCtx, span := (*am.tracingInfo.Tracer).Start(am.tracingInfo.TracerContext, "DexEndBlock")
	span.SetAttributes(attribute.String("contract", contractAddr))
	defer span.End()

	am.keeper.HandleEBLiquidation(spanCtx, ctx, am.tracingInfo.Tracer, contractAddr)
	am.keeper.HandleEBCancelOrders(spanCtx, ctx, am.tracingInfo.Tracer, contractAddr)
	am.keeper.HandleEBPlaceOrders(spanCtx, ctx, am.tracingInfo.Tracer, contractAddr)

	am.keeper.OrderCancellations[contractAddr] = map[string]*dexcache.OrderCancellations{}
	for _, pair := range am.keeper.GetAllRegisteredPairs(ctx, contractAddr) {
		am.keeper.OrderCancellations[contractAddr][pair.String()] = dexcache.NewOrderCancellations()
		orders := am.keeper.Orders[contractAddr][pair.String()]
		ctx.Logger().Info(pair.String())
		ctx.Logger().Info(fmt.Sprintf("Orders %s", am.keeper.Orders))
		ctx.Logger().Info(fmt.Sprintf("Number of LB: %d, LS: %d, MB: %d, MS: %d", len(orders.LimitBuys), len(orders.LimitSells), len(orders.MarketBuys), len(orders.MarketSells)))
		allExistingBuys := am.keeper.GetAllLongBookForPair(ctx, contractAddr, pair.PriceDenom, pair.AssetDenom)
		allExistingSells := am.keeper.GetAllShortBookForPair(ctx, contractAddr, pair.PriceDenom, pair.AssetDenom)

		longDirtyPrices, shortDirtyPrices := exchange.NewDirtyPrices(), exchange.NewDirtyPrices()
		liquidationCancels := am.keeper.OrderCancellations[contractAddr][pair.String()].LiquidationCancellations
		exchange.CancelForLiquidation(ctx, liquidationCancels, allExistingBuys, &longDirtyPrices)
		exchange.CancelForLiquidation(ctx, liquidationCancels, allExistingSells, &shortDirtyPrices)
		exchange.CancelOrders(ctx, orders.CancelBuys, allExistingBuys, types.PositionDirection_LONG, &longDirtyPrices)
		exchange.CancelOrders(ctx, orders.CancelBuys, allExistingSells, types.PositionDirection_SHORT, &shortDirtyPrices)

		settlements := []*types.Settlement{}
		marketBuyTotalPrice, marketBuyTotalQuantity := exchange.MatchMarketOrders(
			ctx,
			orders.MarketBuys,
			allExistingSells,
			pair,
			types.PositionDirection_LONG,
			&longDirtyPrices,
			&settlements,
		)
		marketSellTotalPrice, marketSellTotalQuantity := exchange.MatchMarketOrders(
			ctx,
			orders.MarketSells,
			allExistingBuys,
			pair,
			types.PositionDirection_SHORT,
			&shortDirtyPrices,
			&settlements,
		)
		limitTotalPrice, limitTotalQuantity := exchange.MatchLimitOrders(
			ctx,
			orders.LimitBuys,
			orders.LimitSells,
			&allExistingBuys,
			&allExistingSells,
			pair,
			&longDirtyPrices,
			&shortDirtyPrices,
			&settlements,
		)
		var avgPrice sdk.Dec
		if marketBuyTotalQuantity.Add(marketSellTotalQuantity).Add(limitTotalQuantity).IsZero() {
			avgPrice = sdk.ZeroDec()
		} else {
			avgPrice = (marketBuyTotalPrice.Add(marketSellTotalPrice).Add(limitTotalPrice)).Quo(marketBuyTotalQuantity.Add(marketSellTotalQuantity).Add(limitTotalQuantity))
			twap := am.keeper.GetTwapState(ctx, contractAddr, pair.PriceDenom, pair.AssetDenom)
			newPrices := twap.Prices
			if len(newPrices) == 0 {
				newPrices = []uint64{avgPrice.BigInt().Uint64()}
			} else {
				newPrices[len(newPrices)-1] = avgPrice.BigInt().Uint64()
			}
			am.keeper.SetTwap(ctx, types.Twap{
				LastEpoch:  am.keeper.EpochKeeper.GetEpoch(ctx).CurrentEpoch,
				Prices:     newPrices,
				TwapPrice:  getTwapPrice(newPrices),
				PriceDenom: pair.PriceDenom.String(),
				AssetDenom: pair.AssetDenom.String(),
			}, contractAddr)
		}
		ctx.Logger().Info(fmt.Sprintf("Average price for %s/%s: %d", pair.PriceDenom, pair.AssetDenom, avgPrice))
		for _, buy := range allExistingBuys {
			if longDirtyPrices.Has(buy.GetPrice()) {
				am.keeper.FlushDirtyLongBook(ctx, contractAddr, buy)
			}
		}
		for _, sell := range allExistingSells {
			if shortDirtyPrices.Has(sell.GetPrice()) {
				am.keeper.FlushDirtyShortBook(ctx, contractAddr, sell)
			}
		}
		_, currentEpoch := am.keeper.IsNewEpoch(ctx)
		allSettlements := types.Settlements{
			Epoch:   int64(currentEpoch),
			Entries: []*types.SettlementEntry{},
		}
		settlementMap := map[types.Pair]*types.Settlements{}

		for _, s := range settlements {
			ctx.Logger().Info(s.String())
			settlementEntry := s.ToEntry()
			priceDenom, _ := types.GetDenomFromStr(settlementEntry.PriceDenom)
			assetDenom, _ := types.GetDenomFromStr(settlementEntry.AssetDenom)
			pair := types.Pair{
				PriceDenom: priceDenom,
				AssetDenom: assetDenom,
			}
			if settlements, ok := settlementMap[pair]; ok {
				settlements.Entries = append(settlements.Entries, &settlementEntry)
			} else {
				settlementMap[pair] = &types.Settlements{
					Epoch:   int64(currentEpoch),
					Entries: []*types.SettlementEntry{&settlementEntry},
				}
			}
			allSettlements.Entries = append(allSettlements.Entries, &settlementEntry)
		}
		for s, settlementEntries := range settlementMap {
			am.keeper.SetSettlements(ctx, contractAddr, s.PriceDenom, s.AssetDenom, *settlementEntries)
		}

		nativeSettlementMsg := types.SudoSettlementMsg{
			Settlement: allSettlements,
		}
		ctx.Logger().Info(nativeSettlementMsg.Settlement.String())
		wasmMsg, err := json.Marshal(nativeSettlementMsg)
		if err != nil {
			ctx.Logger().Info(err.Error())
		}

		am.callClearingHouseContractSudo(ctx, wasmMsg, contractAddr)

		for _, marketOrder := range orders.MarketBuys {
			if marketOrder.Quantity.IsPositive() {
				am.keeper.OrderCancellations[contractAddr][pair.String()].OrderCancellations = append(
					am.keeper.OrderCancellations[contractAddr][pair.String()].OrderCancellations,
					dexcache.OrderCancellation{
						Price:      marketOrder.WorstPrice,
						Quantity:   marketOrder.Quantity,
						Creator:    marketOrder.Creator,
						Direction:  marketOrder.Direction,
						Effect:     marketOrder.Effect,
						PriceDenom: pair.PriceDenom,
						AssetDenom: pair.AssetDenom,
						Leverage:   marketOrder.Leverage,
					},
				)
			}
		}
		for _, marketOrder := range orders.MarketSells {
			if marketOrder.Quantity.IsPositive() {
				am.keeper.OrderCancellations[contractAddr][pair.String()].OrderCancellations = append(
					am.keeper.OrderCancellations[contractAddr][pair.String()].OrderCancellations,
					dexcache.OrderCancellation{
						Price:      marketOrder.WorstPrice,
						Quantity:   marketOrder.Quantity,
						Creator:    marketOrder.Creator,
						Direction:  marketOrder.Direction,
						Effect:     marketOrder.Effect,
						PriceDenom: pair.PriceDenom,
						AssetDenom: pair.AssetDenom,
						Leverage:   marketOrder.Leverage,
					},
				)
			}
		}
	}
	// Cancel unfilled market orders
	am.keeper.HandleEBCancelOrders(spanCtx, ctx, am.tracingInfo.Tracer, contractAddr)
}

func getTwapPrice(prices []uint64) uint64 {
	var total uint64 = 0
	for _, price := range prices {
		total += price
	}
	return total / uint64(len(prices))
}
