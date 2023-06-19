package dex_test

import (
	"context"
	"io/ioutil"
	"testing"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/utils/tracing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const (
	GOOD_CONTRACT_INSTANTIATE = `{"whitelist": ["sei1h9yjz89tl0dl6zu65dpxcqnxfhq60wxx8s5kag"],
    "use_whitelist":false,"admin":"sei1h9yjz89tl0dl6zu65dpxcqnxfhq60wxx8s5kag",
	"limit_order_fee":{"decimal":"0.0001","negative":false},
	"market_order_fee":{"decimal":"0.0001","negative":false},
	"liquidation_order_fee":{"decimal":"0.0001","negative":false},
	"margin_ratio":{"decimal":"0.0625","negative":false},
	"max_leverage":{"decimal":"4","negative":false},
	"default_base":"USDC",
	"native_token":"USDC","denoms": ["SEI","ATOM","USDC","SOL","ETH","OSMO","AVAX","BTC"],
	"full_denom_mapping": [["usei","SEI","0.000001"],["uatom","ATOM","0.000001"],["uusdc","USDC","0.000001"]],
	"funding_payment_lookback":3600,"spot_market_contract":"sei1h9yjz89tl0dl6zu65dpxcqnxfhq60wxx8s5kag",
	"supported_collateral_denoms": ["USDC"],
	"supported_multicollateral_denoms": ["ATOM"],
	"oracle_denom_mapping": [["usei","SEI","1"],["uatom","ATOM","1"],["uusdc","USDC","1"],["ueth","ETH","1"]],
	"multicollateral_whitelist": ["sei1h9yjz89tl0dl6zu65dpxcqnxfhq60wxx8s5kag"],
	"multicollateral_whitelist_enable": true,
	"funding_payment_pairs": [["USDC","ETH"]],
	"default_margin_ratios":{
		"initial":"0.3",
		"partial":"0.25",
		"maintenance":"0.06"
	}}`
)

func TestEndBlockMarketOrder(t *testing.T) {
	testApp := keepertest.TestApp()
	dexkeeper := testApp.DexKeeper
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(dexkeeper.GetMemStoreKey())))
	pair := types.Pair{PriceDenom: "SEI", AssetDenom: "ATOM"}

	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000)), sdk.NewCoin("uusdc", sdk.NewInt(10000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	dexAmounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(5000000)), sdk.NewCoin("uusdc", sdk.NewInt(10000000)))
	bankkeeper.SendCoinsFromAccountToModule(ctx, testAccount, types.ModuleName, dexAmounts)
	wasm, err := ioutil.ReadFile("./testdata/mars.wasm")
	if err != nil {
		panic(err)
	}
	wasmKeeper := testApp.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)
	var perm *wasmtypes.AccessConfig
	codeId, err := contractKeeper.Create(ctx, testAccount, wasm, perm)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}
	err = dexkeeper.SetContract(ctx, &types.ContractInfoV2{CodeId: 123, ContractAddr: contractAddr.String(), NeedHook: false, NeedOrderMatching: true, RentBalance: 100000000})
	if err != nil {
		panic(err)
	}
	dexkeeper.AddRegisteredPair(ctx, contractAddr.String(), pair)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                1,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("1"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                2,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("2"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetDepositInfo(ctx, types.ContractAddress(contractAddr.String())).Add(
		&types.DepositInfoEntry{
			Creator: testAccount.String(),
			Denom:   "uusdc",
			Amount:  sdk.MustNewDecFromStr("2000000"),
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, contractAddr.String(), dexkeeper.GetContractWithoutGasCharge)

	ctx = ctx.WithBlockHeight(1)
	testApp.EndBlocker(ctx, abci.RequestEndBlock{})
	_, found := dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("1"), pair.PriceDenom, pair.AssetDenom)
	// Long book should be populated
	require.True(t, found)

	dexutils.GetMemState(ctx.Context()).Clear(ctx)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                3,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("1"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_MARKET,
			PositionDirection: types.PositionDirection_SHORT,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, contractAddr.String(), dexkeeper.GetContractWithoutGasCharge)

	ctx = ctx.WithBlockHeight(2)
	testApp.EndBlocker(ctx, abci.RequestEndBlock{})

	// Long book should be removed since it's executed
	// No state change should've been persisted for bad contract
	_, found = dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("2"), pair.PriceDenom, pair.AssetDenom)
	// Long book should be populated
	require.False(t, found)
	_, found = dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("1"), pair.PriceDenom, pair.AssetDenom)
	require.True(t, found)

	matchResults, _ := dexkeeper.GetMatchResultState(ctx, contractAddr.String())
	require.Equal(t, 1, len(matchResults.Orders))
	require.Equal(t, 2, len(matchResults.Settlements))

	dexutils.GetMemState(ctx.Context()).Clear(ctx)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                4,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("1000000"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_MARKET,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, contractAddr.String(), dexkeeper.GetContractWithoutGasCharge)

	ctx = ctx.WithBlockHeight(3)
	testApp.EndBlocker(ctx, abci.RequestEndBlock{})

	matchResults, _ = dexkeeper.GetMatchResultState(ctx, contractAddr.String())
	require.Equal(t, 1, len(matchResults.Orders))
	require.Equal(t, 0, len(matchResults.Settlements))
}

func TestEndBlockLimitOrder(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	dexkeeper := testApp.DexKeeper
	pair := types.Pair{PriceDenom: "SEI", AssetDenom: "ATOM"}

	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000)), sdk.NewCoin("uusdc", sdk.NewInt(10000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	dexAmounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(5000000)), sdk.NewCoin("uusdc", sdk.NewInt(10000000)))
	bankkeeper.SendCoinsFromAccountToModule(ctx, testAccount, types.ModuleName, dexAmounts)
	wasm, err := ioutil.ReadFile("./testdata/mars.wasm")
	if err != nil {
		panic(err)
	}
	wasmKeeper := testApp.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)
	var perm *wasmtypes.AccessConfig
	codeId, err := contractKeeper.Create(ctx, testAccount, wasm, perm)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	dexkeeper.SetContract(ctx, &types.ContractInfoV2{CodeId: 123, ContractAddr: contractAddr.String(), NeedHook: false, NeedOrderMatching: true, RentBalance: 100000000})
	dexkeeper.AddRegisteredPair(ctx, contractAddr.String(), pair)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                1,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("1"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                2,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("2"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                3,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("3"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_SHORT,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetDepositInfo(ctx, types.ContractAddress(contractAddr.String())).Add(
		&types.DepositInfoEntry{
			Creator: testAccount.String(),
			Denom:   "uusdc",
			Amount:  sdk.MustNewDecFromStr("2000000"),
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, contractAddr.String(), dexkeeper.GetContractWithoutGasCharge)

	ctx = ctx.WithBlockHeight(1)
	testApp.EndBlocker(ctx, abci.RequestEndBlock{})
	_, found := dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("1"), pair.PriceDenom, pair.AssetDenom)
	require.True(t, found)
	_, found = dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("2"), pair.PriceDenom, pair.AssetDenom)
	require.True(t, found)
	_, found = dexkeeper.GetShortBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("3"), pair.PriceDenom, pair.AssetDenom)
	require.True(t, found)

	dexutils.GetMemState(ctx.Context()).Clear(ctx)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                4,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("2"),
			Quantity:          sdk.MustNewDecFromStr("2"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_SHORT,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                5,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("3"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, contractAddr.String(), dexkeeper.GetContractWithoutGasCharge)

	ctx = ctx.WithBlockHeight(2)
	testApp.EndBlocker(ctx, abci.RequestEndBlock{})

	_, found = dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("2"), pair.PriceDenom, pair.AssetDenom)
	require.False(t, found)
	_, found = dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("1"), pair.PriceDenom, pair.AssetDenom)
	require.True(t, found)
	_, found = dexkeeper.GetShortBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("3"), pair.PriceDenom, pair.AssetDenom)
	require.True(t, found)
	_, found = dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("3"), pair.PriceDenom, pair.AssetDenom)
	require.False(t, found)

	matchResults, _ := dexkeeper.GetMatchResultState(ctx, contractAddr.String())
	require.Equal(t, 2, len(matchResults.Orders))
	require.Equal(t, 4, len(matchResults.Settlements))

	dexutils.GetMemState(ctx.Context()).Clear(ctx)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                6,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("1000000"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, contractAddr.String(), dexkeeper.GetContractWithoutGasCharge)

	ctx = ctx.WithBlockHeight(3)
	testApp.EndBlocker(ctx, abci.RequestEndBlock{})

	_, found = dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("1"), pair.PriceDenom, pair.AssetDenom)
	require.True(t, found)
	_, found = dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("1000000"), pair.PriceDenom, pair.AssetDenom)
	require.False(t, found)
	_, found = dexkeeper.GetShortBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("3"), pair.PriceDenom, pair.AssetDenom)
	require.False(t, found)

	matchResults, _ = dexkeeper.GetMatchResultState(ctx, contractAddr.String())
	require.Equal(t, 1, len(matchResults.Orders))
	require.Equal(t, 2, len(matchResults.Settlements))
}

func TestEndBlockRollback(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	dexkeeper := testApp.DexKeeper
	pair := TEST_PAIR()
	// register contract and pair
	dexkeeper.SetContract(ctx, &types.ContractInfoV2{CodeId: 123, ContractAddr: keepertest.TestContract, NeedHook: false, NeedOrderMatching: true, RentBalance: 100000000})
	dexkeeper.AddRegisteredPair(ctx, keepertest.TestContract, pair)
	// place one order to a nonexistent contract
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(keepertest.TestContract), pair).Add(
		&types.Order{
			Id:                1,
			Account:           keepertest.TestAccount,
			ContractAddr:      keepertest.TestContract,
			Price:             sdk.MustNewDecFromStr("1"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, keepertest.TestContract, dexkeeper.GetContractWithoutGasCharge)
	ctx = ctx.WithBlockHeight(1)
	testApp.EndBlocker(ctx, abci.RequestEndBlock{})
	// No state change should've been persisted
	matchResult, _ := dexkeeper.GetMatchResultState(ctx, keepertest.TestContract)
	require.Equal(t, &types.MatchResult{}, matchResult)
	// contract should be suspended
	contract, err := dexkeeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.True(t, contract.Suspended)
}

func TestEndBlockPartialRollback(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	// BAD CONTRACT
	dexkeeper := testApp.DexKeeper
	pair := TEST_PAIR()
	// register contract and pair
	dexkeeper.SetContract(ctx, &types.ContractInfoV2{CodeId: 123, ContractAddr: keepertest.TestContract, NeedHook: false, NeedOrderMatching: true, RentBalance: 100000000})
	dexkeeper.AddRegisteredPair(ctx, keepertest.TestContract, pair)
	// place one order to a nonexistent contract
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(keepertest.TestContract), pair).Add(
		&types.Order{
			Id:                1,
			Account:           keepertest.TestAccount,
			ContractAddr:      keepertest.TestContract,
			Price:             sdk.MustNewDecFromStr("1"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, keepertest.TestContract, dexkeeper.GetContractWithoutGasCharge)
	// GOOD CONTRACT
	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000)), sdk.NewCoin("uusdc", sdk.NewInt(1000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	dexAmounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(500000)), sdk.NewCoin("uusdc", sdk.NewInt(1000000)))
	bankkeeper.SendCoinsFromAccountToModule(ctx, testAccount, types.ModuleName, dexAmounts)
	wasm, err := ioutil.ReadFile("./testdata/mars.wasm")
	if err != nil {
		panic(err)
	}
	wasmKeeper := testApp.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)
	var perm *wasmtypes.AccessConfig
	codeId, err := contractKeeper.Create(ctx, testAccount, wasm, perm)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}
	dexkeeper.SetContract(ctx, &types.ContractInfoV2{CodeId: 123, ContractAddr: contractAddr.String(), NeedHook: false, NeedOrderMatching: true, RentBalance: 100000000})
	dexkeeper.AddRegisteredPair(ctx, contractAddr.String(), pair)
	// place one order to the good contract
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                2,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("0.0001"),
			Quantity:          sdk.MustNewDecFromStr("0.0001"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetDepositInfo(ctx, types.ContractAddress(contractAddr.String())).Add(
		&types.DepositInfoEntry{
			Creator: testAccount.String(),
			Denom:   "uusdc",
			Amount:  sdk.MustNewDecFromStr("10000"),
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, contractAddr.String(), dexkeeper.GetContractWithoutGasCharge)

	ctx = ctx.WithBlockHeight(1)
	testApp.EndBlocker(ctx, abci.RequestEndBlock{})
	// No state change should've been persisted for bad contract
	matchResult, _ := dexkeeper.GetMatchResultState(ctx, keepertest.TestContract)
	require.Equal(t, &types.MatchResult{}, matchResult)
	// bad contract should be suspended
	contract, err := dexkeeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.True(t, contract.Suspended)
	// state change should've been persisted for good contract
	matchResult, _ = dexkeeper.GetMatchResultState(ctx, contractAddr.String())
	require.Equal(t, 1, len(matchResult.Orders))
	_, found := dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("0.0001"), pair.PriceDenom, pair.AssetDenom)
	require.True(t, found)
}

func TestBeginBlock(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	dexkeeper := testApp.DexKeeper

	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	wasm, err := ioutil.ReadFile("./testdata/mars.wasm")
	if err != nil {
		panic(err)
	}
	wasmKeeper := testApp.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)
	var perm *wasmtypes.AccessConfig
	codeId, err := contractKeeper.Create(ctx, testAccount, wasm, perm)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}
	dexkeeper.SetContract(ctx, &types.ContractInfoV2{CodeId: 123, ContractAddr: contractAddr.String(), NeedHook: false, NeedOrderMatching: true, RentBalance: 100000000})

	// right now just make sure it doesn't crash since it doesn't register any state to be checked against
	testApp.BeginBlocker(ctx, abci.RequestBeginBlock{})
}

// Note that once the bug that causes EndBlock to panic is fixed, this test will need to be
// updated to trigger the next bug that causes panics, if any.
func TestEndBlockPanicHandling(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	dexkeeper := testApp.DexKeeper
	pair := types.Pair{PriceDenom: "SEI", AssetDenom: "ATOM"}

	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	dexAmounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(5000000)))
	bankkeeper.SendCoinsFromAccountToModule(ctx, testAccount, types.ModuleName, dexAmounts)
	wasm, err := ioutil.ReadFile("./testdata/mars.wasm")
	if err != nil {
		panic(err)
	}
	wasmKeeper := testApp.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)
	var perm *wasmtypes.AccessConfig
	codeId, err := contractKeeper.Create(ctx, testAccount, wasm, perm)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}
	dexkeeper.SetContract(ctx, &types.ContractInfoV2{CodeId: 123, ContractAddr: contractAddr.String(), NeedHook: false, NeedOrderMatching: true, RentBalance: 100000000})
	dexkeeper.AddRegisteredPair(ctx, contractAddr.String(), pair)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                1,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.Dec{},
			Quantity:          sdk.Dec{},
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetDepositInfo(ctx, types.ContractAddress(contractAddr.String())).Add(
		&types.DepositInfoEntry{
			Creator: testAccount.String(),
			Denom:   "usei",
			Amount:  sdk.MustNewDecFromStr("2000000"),
		},
	)

	require.NotPanics(t, func() { testApp.EndBlocker(ctx, abci.RequestEndBlock{}) })
	_, found := dexkeeper.GetLongBookByPrice(ctx, contractAddr.String(), sdk.MustNewDecFromStr("1"), pair.PriceDenom, pair.AssetDenom)
	require.False(t, found)
}

func TestEndBlockRollbackWithRentCharge(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	dexkeeper := testApp.DexKeeper
	pair := TEST_PAIR()
	// GOOD CONTRACT
	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000)), sdk.NewCoin("uusdc", sdk.NewInt(1000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	dexAmounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(500000)), sdk.NewCoin("uusdc", sdk.NewInt(1000000)))
	bankkeeper.SendCoinsFromAccountToModule(ctx, testAccount, types.ModuleName, dexAmounts)
	wasm, err := ioutil.ReadFile("./testdata/mars.wasm")
	if err != nil {
		panic(err)
	}
	wasmKeeper := testApp.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)
	var perm *wasmtypes.AccessConfig
	codeId, err := contractKeeper.Create(ctx, testAccount, wasm, perm)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}
	dexkeeper.SetContract(ctx, &types.ContractInfoV2{CodeId: 123, ContractAddr: contractAddr.String(), NeedHook: false, NeedOrderMatching: true, RentBalance: 1})
	dexkeeper.AddRegisteredPair(ctx, contractAddr.String(), pair)
	// place one order to a nonexistent contract
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                2,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("0.0001"),
			Quantity:          sdk.MustNewDecFromStr("0.0001"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetDepositInfo(ctx, types.ContractAddress(contractAddr.String())).Add(
		&types.DepositInfoEntry{
			Creator: testAccount.String(),
			Denom:   "uusdc",
			Amount:  sdk.MustNewDecFromStr("10000"),
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, contractAddr.String(), dexkeeper.GetContractWithoutGasCharge)
	// overwrite params for testing
	params := dexkeeper.GetParams(ctx)
	params.MinProcessableRent = 0
	dexkeeper.SetParams(ctx, params)

	ctx = ctx.WithBlockHeight(1)
	creatorBalanceBefore := bankkeeper.GetBalance(ctx, testAccount, "usei")
	testApp.EndBlocker(ctx, abci.RequestEndBlock{})
	// no state change should've been persisted for good contract because it should've run out of gas
	matchResult, _ := dexkeeper.GetMatchResultState(ctx, contractAddr.String())
	require.Equal(t, 0, len(matchResult.Orders))
	// rent should still be charged even if the contract failed
	c, err := dexkeeper.GetContract(ctx, contractAddr.String())
	require.Nil(t, err)
	require.True(t, c.Suspended)               // bad contract is suspended not because of out-of-rent but because of execution error
	require.Equal(t, uint64(0), c.RentBalance) // rent balance should be drained
	require.Equal(t, int64(1), dexkeeper.BankKeeper.GetBalance(
		ctx,
		dexkeeper.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName),
		"usei",
	).Amount.Int64()) // bad contract rent should be sent to fee collector
	creatorBalanceAfter := bankkeeper.GetBalance(ctx, testAccount, "usei")
	require.Equal(t, creatorBalanceBefore, creatorBalanceAfter)
}

func TestEndBlockContractWithoutPair(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	dexkeeper := testApp.DexKeeper

	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000)), sdk.NewCoin("uusdc", sdk.NewInt(10000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	dexAmounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(5000000)), sdk.NewCoin("uusdc", sdk.NewInt(10000000)))
	bankkeeper.SendCoinsFromAccountToModule(ctx, testAccount, types.ModuleName, dexAmounts)
	wasm, err := ioutil.ReadFile("./testdata/mars.wasm")
	if err != nil {
		panic(err)
	}
	wasmKeeper := testApp.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)
	var perm *wasmtypes.AccessConfig
	codeId, err := contractKeeper.Create(ctx, testAccount, wasm, perm)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	// no pair registered
	contractInfo := types.ContractInfoV2{CodeId: 123, ContractAddr: contractAddr.String(), NeedHook: false, NeedOrderMatching: true, RentBalance: 100000000}
	dexkeeper.SetContract(ctx, &contractInfo)

	tp := trace.NewNoopTracerProvider()
	otel.SetTracerProvider(trace.NewNoopTracerProvider())
	tr := tp.Tracer("component-main")
	ti := tracing.Info{
		Tracer: &tr,
	}
	_, _, _, _, success := contract.EndBlockerAtomic(ctx, &testApp.DexKeeper, []types.ContractInfoV2{contractInfo}, &ti)
	require.True(t, success)

	contractInfo, err = dexkeeper.GetContract(ctx, contractInfo.ContractAddr)
	require.Nil(t, err)
	require.False(t, contractInfo.Suspended)
}

func TestOrderCountUpdate(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	dexkeeper := testApp.DexKeeper
	pair := types.Pair{PriceDenom: "SEI", AssetDenom: "ATOM"}

	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000)), sdk.NewCoin("uusdc", sdk.NewInt(10000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	dexAmounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(5000000)), sdk.NewCoin("uusdc", sdk.NewInt(10000000)))
	bankkeeper.SendCoinsFromAccountToModule(ctx, testAccount, types.ModuleName, dexAmounts)
	wasm, err := ioutil.ReadFile("./testdata/mars.wasm")
	if err != nil {
		panic(err)
	}
	wasmKeeper := testApp.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)
	var perm *wasmtypes.AccessConfig
	codeId, err := contractKeeper.Create(ctx, testAccount, wasm, perm)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	dexkeeper.SetContract(ctx, &types.ContractInfoV2{CodeId: 123, ContractAddr: contractAddr.String(), NeedHook: false, NeedOrderMatching: true, RentBalance: 100000000})
	dexkeeper.AddRegisteredPair(ctx, contractAddr.String(), pair)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                1,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("1"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                2,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("2"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                3,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("3"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_SHORT,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetDepositInfo(ctx, types.ContractAddress(contractAddr.String())).Add(
		&types.DepositInfoEntry{
			Creator: testAccount.String(),
			Denom:   "uusdc",
			Amount:  sdk.MustNewDecFromStr("2000000"),
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, contractAddr.String(), dexkeeper.GetContractWithoutGasCharge)

	ctx = ctx.WithBlockHeight(1)
	testApp.EndBlocker(ctx, abci.RequestEndBlock{})
	require.Equal(t, uint64(1), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_LONG, sdk.NewDec(1)))
	require.Equal(t, uint64(0), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_SHORT, sdk.NewDec(1)))
	require.Equal(t, uint64(1), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_LONG, sdk.NewDec(2)))
	require.Equal(t, uint64(0), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_SHORT, sdk.NewDec(2)))
	require.Equal(t, uint64(0), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_LONG, sdk.NewDec(3)))
	require.Equal(t, uint64(1), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_SHORT, sdk.NewDec(3)))

	dexutils.GetMemState(ctx.Context()).Clear(ctx)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Order{
			Id:                4,
			Account:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("1"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockCancels(ctx, types.ContractAddress(contractAddr.String()), pair).Add(
		&types.Cancellation{
			Id:                2,
			Creator:           testAccount.String(),
			ContractAddr:      contractAddr.String(),
			Price:             sdk.MustNewDecFromStr("2"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			PositionDirection: types.PositionDirection_LONG,
		},
	)
	dexutils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, contractAddr.String(), dexkeeper.GetContractWithoutGasCharge)
	ctx = ctx.WithBlockHeight(2)
	testApp.EndBlocker(ctx, abci.RequestEndBlock{})
	require.Equal(t, uint64(2), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_LONG, sdk.NewDec(1)))
	require.Equal(t, uint64(0), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_SHORT, sdk.NewDec(1)))
	require.Equal(t, uint64(0), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_LONG, sdk.NewDec(2)))
	require.Equal(t, uint64(0), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_SHORT, sdk.NewDec(2)))
	require.Equal(t, uint64(0), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_LONG, sdk.NewDec(3)))
	require.Equal(t, uint64(1), dexkeeper.GetOrderCountState(ctx, contractAddr.String(), pair.PriceDenom, pair.AssetDenom, types.PositionDirection_SHORT, sdk.NewDec(3)))
}
