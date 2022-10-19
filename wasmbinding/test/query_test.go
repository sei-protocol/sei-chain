package wasmbinding

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/sei-protocol/sei-chain/app"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	dexwasm "github.com/sei-protocol/sei-chain/x/dex/client/wasm"
	dexbinding "github.com/sei-protocol/sei-chain/x/dex/client/wasm/bindings"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	epochwasm "github.com/sei-protocol/sei-chain/x/epoch/client/wasm"
	epochbinding "github.com/sei-protocol/sei-chain/x/epoch/client/wasm/bindings"
	epochtypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclebinding "github.com/sei-protocol/sei-chain/x/oracle/client/wasm/bindings"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	oracleutils "github.com/sei-protocol/sei-chain/x/oracle/utils"
	tokenfactorywasm "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm"
	tokenfactorybinding "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm/bindings"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

func SetupWasmbindingTest(t *testing.T) (*app.TestWrapper, func(ctx sdk.Context, request json.RawMessage) ([]byte, error)) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)

	oh := oraclewasm.NewOracleWasmQueryHandler(&testWrapper.App.OracleKeeper)
	dh := dexwasm.NewDexWasmQueryHandler(&testWrapper.App.DexKeeper)
	eh := epochwasm.NewEpochWasmQueryHandler(&testWrapper.App.EpochKeeper)
	th := tokenfactorywasm.NewTokenFactoryWasmQueryHandler(&testWrapper.App.TokenFactoryKeeper)
	qp := wasmbinding.NewQueryPlugin(oh, dh, eh, th)
	return testWrapper, wasmbinding.CustomQuerier(qp)
}

func TestWasmUnknownQuery(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	oracle_req := oraclebinding.SeiOracleQuery{}
	queryData, err := json.Marshal(oracle_req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.OracleRoute, QueryData: queryData}
	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.Error(t, err)
	require.Equal(t, err, oracletypes.ErrUnknownSeiOracleQuery)

	dex_req := dexbinding.SeiDexQuery{}
	queryData, err = json.Marshal(dex_req)
	require.NoError(t, err)
	query = wasmbinding.SeiQueryWrapper{Route: wasmbinding.DexRoute, QueryData: queryData}
	rawQuery, err = json.Marshal(query)
	require.NoError(t, err)

	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.Error(t, err)
	require.Equal(t, err, dextypes.ErrUnknownSeiDexQuery)

	epoch_req := epochbinding.SeiEpochQuery{}
	queryData, err = json.Marshal(epoch_req)
	require.NoError(t, err)
	query = wasmbinding.SeiQueryWrapper{Route: wasmbinding.EpochRoute, QueryData: queryData}
	rawQuery, err = json.Marshal(query)
	require.NoError(t, err)

	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.Error(t, err)
	require.Equal(t, err, epochtypes.ErrUnknownSeiEpochQuery)

	token_factory_req := tokenfactorybinding.SeiTokenFactoryQuery{}
	queryData, err = json.Marshal(token_factory_req)
	require.NoError(t, err)
	query = wasmbinding.SeiQueryWrapper{Route: wasmbinding.TokenFactoryRoute, QueryData: queryData}
	rawQuery, err = json.Marshal(query)
	require.NoError(t, err)

	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.Error(t, err)
	require.Equal(t, err, tokenfactorytypes.ErrUnknownSeiTokenFactoryQuery)
}

func TestWasmGetOracleExchangeRates(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	req := oraclebinding.SeiOracleQuery{ExchangeRates: &oracletypes.QueryExchangeRatesRequest{}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.OracleRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes oracletypes.QueryExchangeRatesResponse
	err = json.Unmarshal(res, &parsedRes)
	require.NoError(t, err)
	require.Equal(t, oracletypes.QueryExchangeRatesResponse{DenomOracleExchangeRatePairs: oracletypes.DenomOracleExchangeRatePairs{}}, parsedRes)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11)
	testWrapper.App.OracleKeeper.SetBaseExchangeRate(testWrapper.Ctx, oracleutils.MicroAtomDenom, sdk.NewDec(12))

	res, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes2 oracletypes.QueryExchangeRatesResponse
	err = json.Unmarshal(res, &parsedRes2)
	require.NoError(t, err)
	require.Equal(t, oracletypes.QueryExchangeRatesResponse{DenomOracleExchangeRatePairs: oracletypes.DenomOracleExchangeRatePairs{oracletypes.NewDenomOracleExchangeRatePair(oracleutils.MicroAtomDenom, sdk.NewDec(12), sdk.NewInt(11))}}, parsedRes2)
}

func TestWasmGetOracleTwaps(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	req := oraclebinding.SeiOracleQuery{OracleTwaps: &oracletypes.QueryTwapsRequest{LookbackSeconds: 200}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.OracleRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	// this should error because there is no snapshots to build twap from
	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.Error(t, err)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11).WithBlockTime(time.Unix(3600, 0))
	testWrapper.App.OracleKeeper.SetBaseExchangeRate(testWrapper.Ctx, oracleutils.MicroAtomDenom, sdk.NewDec(12))

	priceSnapshot := oracletypes.PriceSnapshot{SnapshotTimestamp: 3600, PriceSnapshotItems: oracletypes.PriceSnapshotItems{
		oracletypes.NewPriceSnapshotItem(oracleutils.MicroAtomDenom, oracletypes.OracleExchangeRate{ExchangeRate: sdk.NewDec(20), LastUpdate: sdk.NewInt(10)}),
	}}
	testWrapper.App.OracleKeeper.AddPriceSnapshot(testWrapper.Ctx, priceSnapshot)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(14).WithBlockTime(time.Unix(3700, 0))

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes2 oracletypes.QueryTwapsResponse
	err = json.Unmarshal(res, &parsedRes2)
	require.NoError(t, err)
	// should be 100 isntead of 200 because thats the oldest data timestamp we have
	require.Equal(t, oracletypes.QueryTwapsResponse{OracleTwaps: oracletypes.OracleTwaps{
		oracletypes.OracleTwap{Denom: oracleutils.MicroAtomDenom, Twap: sdk.NewDec(20), LookbackSeconds: 100},
	}}, parsedRes2)
}

func TestWasmGetOracleTwapsErrorHandling(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	req := oraclebinding.SeiOracleQuery{OracleTwaps: &oracletypes.QueryTwapsRequest{LookbackSeconds: 200}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.OracleRoute, QueryData: queryData}
	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.Error(t, err)
	require.Equal(t, err, oracletypes.ErrNoTwapData)

	req = oraclebinding.SeiOracleQuery{OracleTwaps: &oracletypes.QueryTwapsRequest{LookbackSeconds: 3601}}
	queryData, err = json.Marshal(req)
	require.NoError(t, err)
	query = wasmbinding.SeiQueryWrapper{Route: wasmbinding.OracleRoute, QueryData: queryData}
	rawQuery, err = json.Marshal(query)
	require.NoError(t, err)

	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.Error(t, err)
	require.Equal(t, err, oracletypes.ErrInvalidTwapLookback)
}

func TestWasmGetDexTwaps(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	req := dexbinding.SeiDexQuery{DexTwaps: &dextypes.QueryGetTwapsRequest{
		ContractAddr:    app.TestContract,
		LookbackSeconds: 200,
	}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.DexRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11).WithBlockTime(time.Unix(3600, 0))
	testWrapper.App.DexKeeper.AddRegisteredPair(
		testWrapper.Ctx,
		app.TestContract,
		dextypes.Pair{PriceDenom: "sei", AssetDenom: "atom"},
	)
	testWrapper.App.DexKeeper.SetPriceState(testWrapper.Ctx, dextypes.Price{
		SnapshotTimestampInSeconds: 3600,
		Price:                      sdk.NewDec(20),
		Pair:                       &dextypes.Pair{PriceDenom: "sei", AssetDenom: "atom"},
	}, app.TestContract)
	testWrapper.App.OracleKeeper.SetBaseExchangeRate(testWrapper.Ctx, oracleutils.MicroAtomDenom, sdk.NewDec(12))
	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(14).WithBlockTime(time.Unix(3700, 0))

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes dextypes.QueryGetTwapsResponse
	err = json.Unmarshal(res, &parsedRes)
	require.NoError(t, err)
	require.Equal(t, 1, len(parsedRes.Twaps))
	twap := *parsedRes.Twaps[0]
	require.Equal(t, "sei", twap.Pair.PriceDenom)
	require.Equal(t, "atom", twap.Pair.AssetDenom)
	require.Equal(t, sdk.NewDec(20), twap.Twap)
}

func TestWasmDexGetOrderByIdErrorHandling(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	req := dexbinding.SeiDexQuery{GetOrderByID: &dextypes.QueryGetOrderByIDRequest{
		ContractAddr: keepertest.TestContract,
		PriceDenom:   keepertest.TestPriceDenom,
		AssetDenom:   keepertest.TestAssetDenom,
		Id:           1,
	}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.DexRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.Error(t, err)
	require.IsType(t, dextypes.ErrInvalidOrderID, err)
}

func TestWasmGetOrderSimulation(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	order := dextypes.Order{
		PositionDirection: dextypes.PositionDirection_LONG,
		OrderType:         dextypes.OrderType_LIMIT,
		PriceDenom:        "USDC",
		AssetDenom:        "SEI",
		Price:             sdk.MustNewDecFromStr("10"),
		Quantity:          sdk.OneDec(),
		Data:              "{\"position_effect\":\"OPEN\", \"leverage\":\"1\"}",
	}

	req := dexbinding.SeiDexQuery{GetOrderSimulation: &dextypes.QueryOrderSimulationRequest{
		Order: &order,
	}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.DexRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11).WithBlockTime(time.Unix(3600, 0))
	testWrapper.Ctx = testWrapper.Ctx.WithContext(context.WithValue(testWrapper.Ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState()))
	testWrapper.App.DexKeeper.AddRegisteredPair(
		testWrapper.Ctx,
		app.TestContract,
		dextypes.Pair{PriceDenom: "sei", AssetDenom: "atom"},
	)
	testWrapper.App.DexKeeper.SetPriceState(testWrapper.Ctx, dextypes.Price{
		SnapshotTimestampInSeconds: 3600,
		Price:                      sdk.NewDec(20),
		Pair:                       &dextypes.Pair{PriceDenom: "sei", AssetDenom: "atom"},
	}, app.TestContract)
	testWrapper.App.OracleKeeper.SetBaseExchangeRate(testWrapper.Ctx, oracleutils.MicroAtomDenom, sdk.NewDec(12))
	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(14).WithBlockTime(time.Unix(3700, 0))
	testWrapper.Ctx = testWrapper.Ctx.WithContext(context.WithValue(testWrapper.Ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState()))

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes dextypes.QueryOrderSimulationResponse
	err = json.Unmarshal(res, &parsedRes)
	require.NoError(t, err)
	require.Equal(t, sdk.NewDec(0), *parsedRes.ExecutedQuantity)
}

func TestWasmGetEpoch(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	req := epochbinding.SeiEpochQuery{
		Epoch: &epochtypes.QueryEpochRequest{},
	}

	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.EpochRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(45).WithBlockTime(time.Unix(12500, 0))
	testWrapper.App.EpochKeeper.SetEpoch(testWrapper.Ctx, epochtypes.Epoch{
		GenesisTime:           time.Unix(1000, 0).UTC(),
		EpochDuration:         time.Minute,
		CurrentEpoch:          uint64(69),
		CurrentEpochStartTime: time.Unix(12345, 0).UTC(),
		CurrentEpochHeight:    int64(40),
	})

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes epochtypes.QueryEpochResponse
	err = json.Unmarshal(res, &parsedRes)
	require.NoError(t, err)
	epoch := parsedRes.Epoch
	require.Equal(t, time.Unix(1000, 0).UTC(), epoch.GenesisTime)
	require.Equal(t, time.Minute, epoch.EpochDuration)
	require.Equal(t, uint64(69), epoch.CurrentEpoch)
	require.Equal(t, time.Unix(12345, 0).UTC(), epoch.CurrentEpochStartTime)
	require.Equal(t, int64(40), epoch.CurrentEpochHeight)
}

func TestWasmGetDenomCreationFeeWhitelist(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	req := tokenfactorybinding.SeiTokenFactoryQuery{
		GetDenomFeeWhitelist: &tokenfactorytypes.QueryDenomCreationFeeWhitelistRequest{},
	}

	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.TokenFactoryRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	// Should be an empty whitelist
	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes1 tokenfactorytypes.QueryDenomCreationFeeWhitelistResponse
	err = json.Unmarshal(res, &parsedRes1)
	require.NoError(t, err)
	require.Equal(t, tokenfactorytypes.QueryDenomCreationFeeWhitelistResponse{Creators: []string(nil)}, parsedRes1)

	// Add two creators to whitelist
	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11).WithBlockTime(time.Unix(3600, 0))
	testWrapper.App.TokenFactoryKeeper.AddCreatorToWhitelist(testWrapper.Ctx, "creator_1")
	testWrapper.App.TokenFactoryKeeper.AddCreatorToWhitelist(testWrapper.Ctx, "creator_2")

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(14).WithBlockTime(time.Unix(3700, 0))

	res, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes2 tokenfactorytypes.QueryDenomCreationFeeWhitelistResponse
	err = json.Unmarshal(res, &parsedRes2)
	require.NoError(t, err)
	require.Equal(t, tokenfactorytypes.QueryDenomCreationFeeWhitelistResponse{Creators: []string{"creator_1", "creator_2"}}, parsedRes2)
}

func TestWasmGetCreatorInDenomFeeWhitelist(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	req := tokenfactorybinding.SeiTokenFactoryQuery{
		CreatorInDenomFeeWhitelist: &tokenfactorytypes.QueryCreatorInDenomFeeWhitelistRequest{Creator: "creator_1"},
	}

	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.TokenFactoryRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	// Should not be in whitelist
	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes1 tokenfactorytypes.QueryCreatorInDenomFeeWhitelistResponse
	err = json.Unmarshal(res, &parsedRes1)
	require.NoError(t, err)
	require.Equal(t, tokenfactorytypes.QueryCreatorInDenomFeeWhitelistResponse{Whitelisted: false}, parsedRes1)

	// Add two creators to whitelist and check membership
	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11).WithBlockTime(time.Unix(3600, 0))
	testWrapper.App.TokenFactoryKeeper.AddCreatorToWhitelist(testWrapper.Ctx, "creator_1")
	testWrapper.App.TokenFactoryKeeper.AddCreatorToWhitelist(testWrapper.Ctx, "creator_2")

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(14).WithBlockTime(time.Unix(3700, 0))

	res, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes2 tokenfactorytypes.QueryCreatorInDenomFeeWhitelistResponse
	err = json.Unmarshal(res, &parsedRes2)
	require.NoError(t, err)
	require.Equal(t, tokenfactorytypes.QueryCreatorInDenomFeeWhitelistResponse{Whitelisted: true}, parsedRes2)
}

func MockQueryPlugins() wasmkeeper.QueryPlugins {
	return wasmkeeper.QueryPlugins{
		Bank: func(ctx sdk.Context, request *wasmvmtypes.BankQuery) ([]byte, error) { return []byte{}, nil },
		IBC: func(ctx sdk.Context, caller sdk.AccAddress, request *wasmvmtypes.IBCQuery) ([]byte, error) {
			return []byte{}, nil
		},
		Custom: func(ctx sdk.Context, request json.RawMessage) ([]byte, error) {
			println("test")
			return []byte{}, nil
		},
		Stargate: func(ctx sdk.Context, request *wasmvmtypes.StargateQuery) ([]byte, error) { return []byte{}, nil },
		Staking:  func(ctx sdk.Context, request *wasmvmtypes.StakingQuery) ([]byte, error) { return []byte{}, nil },
		Wasm:     func(ctx sdk.Context, request *wasmvmtypes.WasmQuery) ([]byte, error) { return []byte{}, nil },
	}
}

func TestQueryHandlerDependencyDecoratorBank(t *testing.T) {
	app := app.Setup(false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	queryDecorator := wasmbinding.NewCustomQueryHandler(MockQueryPlugins(), app.AccessControlKeeper)
	testContext := app.NewContext(false, types.Header{})

	// setup the wasm contract's dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_KV_BANK,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Bank: &wasmvmtypes.BankQuery{},
	})
	require.NoError(t, err)

	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_WRITE,
				ResourceType:       accesscontrol.ResourceType_KV_DEX,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Bank: &wasmvmtypes.BankQuery{},
	})
	require.Error(t, wasmbinding.ErrUnexpectedWasmDependency, err)
}

func TestQueryHandlerDependencyDecoratorIBC(t *testing.T) {
	app := app.Setup(false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	queryDecorator := wasmbinding.NewCustomQueryHandler(MockQueryPlugins(), app.AccessControlKeeper)
	testContext := app.NewContext(false, types.Header{})

	// setup the wasm contract's dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_ANY,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		IBC: &wasmvmtypes.IBCQuery{},
	})
	require.NoError(t, err)

	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_WRITE,
				ResourceType:       accesscontrol.ResourceType_KV,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		IBC: &wasmvmtypes.IBCQuery{},
	})
	require.Error(t, wasmbinding.ErrUnexpectedWasmDependency, err)
}

func TestQueryHandlerDependencyDecoratorStaking(t *testing.T) {
	app := app.Setup(false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	queryDecorator := wasmbinding.NewCustomQueryHandler(MockQueryPlugins(), app.AccessControlKeeper)
	testContext := app.NewContext(false, types.Header{})

	// setup the wasm contract's dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_KV_STAKING,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Staking: &wasmvmtypes.StakingQuery{},
	})
	require.NoError(t, err)

	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_WRITE,
				ResourceType:       accesscontrol.ResourceType_KV_DEX,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Staking: &wasmvmtypes.StakingQuery{},
	})
	require.Error(t, wasmbinding.ErrUnexpectedWasmDependency, err)
}

func TestQueryHandlerDependencyDecoratorStargate(t *testing.T) {
	app := app.Setup(false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	queryDecorator := wasmbinding.NewCustomQueryHandler(MockQueryPlugins(), app.AccessControlKeeper)
	testContext := app.NewContext(false, types.Header{})

	// setup the wasm contract's dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_ANY,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Stargate: &wasmvmtypes.StargateQuery{},
	})
	require.NoError(t, err)

	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_WRITE,
				ResourceType:       accesscontrol.ResourceType_KV,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Stargate: &wasmvmtypes.StargateQuery{},
	})
	require.Error(t, wasmbinding.ErrUnexpectedWasmDependency, err)
}

func TestQueryHandlerDependencyDecoratorWasm(t *testing.T) {
	app := app.Setup(false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	queryDecorator := wasmbinding.NewCustomQueryHandler(MockQueryPlugins(), app.AccessControlKeeper)
	testContext := app.NewContext(false, types.Header{})

	// setup the wasm contract's dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_KV_WASM,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Wasm: &wasmvmtypes.WasmQuery{},
	})
	require.NoError(t, err)

	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_WRITE,
				ResourceType:       accesscontrol.ResourceType_KV_DEX,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Wasm: &wasmvmtypes.WasmQuery{},
	})
	require.Error(t, wasmbinding.ErrUnexpectedWasmDependency, err)
}

func TestQueryHandlerDependencyDecoratorDex(t *testing.T) {
	app := app.Setup(false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	queryDecorator := wasmbinding.NewCustomQueryHandler(MockQueryPlugins(), app.AccessControlKeeper)
	testContext := app.NewContext(false, types.Header{})

	// setup the wasm contract's dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_KV_DEX,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	customQuery, err := json.Marshal(wasmbinding.SeiQueryWrapper{
		Route: wasmbinding.DexRoute,
	})
	require.NoError(t, err)
	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Custom: customQuery,
	})
	require.NoError(t, err)

	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_KV_ORACLE,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Custom: customQuery,
	})
	require.Error(t, wasmbinding.ErrUnexpectedWasmDependency, err)
}

func TestQueryHandlerDependencyDecoratorOracle(t *testing.T) {
	app := app.Setup(false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	queryDecorator := wasmbinding.NewCustomQueryHandler(MockQueryPlugins(), app.AccessControlKeeper)
	testContext := app.NewContext(false, types.Header{})

	// setup the wasm contract's dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_KV_ORACLE,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	customQuery, err := json.Marshal(wasmbinding.SeiQueryWrapper{
		Route: wasmbinding.OracleRoute,
	})
	require.NoError(t, err)
	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Custom: customQuery,
	})
	require.NoError(t, err)

	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_KV_BANK,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Custom: customQuery,
	})
	require.Error(t, wasmbinding.ErrUnexpectedWasmDependency, err)
}

func TestQueryHandlerDependencyDecoratorEpoch(t *testing.T) {
	app := app.Setup(false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	queryDecorator := wasmbinding.NewCustomQueryHandler(MockQueryPlugins(), app.AccessControlKeeper)
	testContext := app.NewContext(false, types.Header{})

	// setup the wasm contract's dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_KV_EPOCH,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	customQuery, err := json.Marshal(wasmbinding.SeiQueryWrapper{
		Route: wasmbinding.EpochRoute,
	})
	require.NoError(t, err)
	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Custom: customQuery,
	})
	require.NoError(t, err)

	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_KV_BANK,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Custom: customQuery,
	})
	require.Error(t, wasmbinding.ErrUnexpectedWasmDependency, err)
}

func TestQueryHandlerDependencyDecoratorTokenFactory(t *testing.T) {
	app := app.Setup(false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	queryDecorator := wasmbinding.NewCustomQueryHandler(MockQueryPlugins(), app.AccessControlKeeper)
	testContext := app.NewContext(false, types.Header{})

	// setup the wasm contract's dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_KV,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	customQuery, err := json.Marshal(wasmbinding.SeiQueryWrapper{
		Route: wasmbinding.TokenFactoryRoute,
	})
	require.NoError(t, err)
	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Custom: customQuery,
	})
	require.NoError(t, err)

	err = app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperation{
			{
				AccessType:         accesscontrol.AccessType_READ,
				ResourceType:       accesscontrol.ResourceType_Mem,
				IdentifierTemplate: "*",
			},
			acltypes.CommitAccessOp(),
		},
	})
	require.NoError(t, err)

	_, err = queryDecorator.HandleQuery(testContext, contractAddr, wasmvmtypes.QueryRequest{
		Custom: customQuery,
	})
	require.Error(t, wasmbinding.ErrUnexpectedWasmDependency, err)
}
