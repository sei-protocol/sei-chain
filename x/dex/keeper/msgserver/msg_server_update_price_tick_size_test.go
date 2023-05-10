package msgserver_test

import (
	"context"
	"io/ioutil"
	"testing"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestUpdatePriceTickSize(t *testing.T) {
	// Instantiate and get contract address
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	wctx := sdk.WrapSDKContext(ctx)
	keeper := testApp.DexKeeper

	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000000)), sdk.NewCoin("uusdc", sdk.NewInt(100000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	wasm, err := ioutil.ReadFile("../../testdata/mars.wasm")
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

	server := msgserver.NewMsgServerImpl(keeper)
	err = RegisterContractUtil(server, wctx, contractAddr.String(), nil)
	require.NoError(t, err)

	// First register pair
	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: contractAddr.String(),
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NoError(t, err)

	// Test updated tick size
	tickUpdates := []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: contractAddr.String(),
		Pair:         &keepertest.TestPair,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdatePriceTickSize(wctx, &types.MsgUpdatePriceTickSize{
		Creator:      keepertest.TestAccount,
		TickSizeList: tickUpdates,
	})
	require.NoError(t, err)

	storedTickSize, _ := keeper.GetPriceTickSizeForPair(ctx, contractAddr.String(), keepertest.TestPair)
	require.Equal(t, sdk.MustNewDecFromStr("0.1"), storedTickSize)
}

func TestUpdatePriceTickSizeInvalidMsg(t *testing.T) {
	// Instantiate and get contract address
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	wctx := sdk.WrapSDKContext(ctx)
	keeper := testApp.DexKeeper

	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000000)), sdk.NewCoin("uusdc", sdk.NewInt(100000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	wasm, err := ioutil.ReadFile("../../testdata/mars.wasm")
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

	server := msgserver.NewMsgServerImpl(keeper)
	err = RegisterContractUtil(server, wctx, contractAddr.String(), nil)
	require.NoError(t, err)
	// First register pair
	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: contractAddr.String(),
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NoError(t, err)

	// Test with empty creator address
	tickUpdates := []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: contractAddr.String(),
		Pair:         &keepertest.TestPair,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdatePriceTickSize(wctx, &types.MsgUpdatePriceTickSize{
		Creator:      "",
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)

	// Test with empty msg
	tickUpdates = []types.TickSize{}
	_, err = server.UpdatePriceTickSize(wctx, &types.MsgUpdatePriceTickSize{
		Creator:      keepertest.TestAccount,
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)

	// Test with invalid Creator address
	tickUpdates = []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: contractAddr.String(),
		Pair:         &keepertest.TestPair,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdatePriceTickSize(wctx, &types.MsgUpdatePriceTickSize{
		Creator:      "invalidAddress",
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)

	// Test with empty contract address
	tickUpdates = []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: "",
		Pair:         &keepertest.TestPair,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdatePriceTickSize(wctx, &types.MsgUpdatePriceTickSize{
		Creator:      keepertest.TestAccount,
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)

	// Test with nil pair
	tickUpdates = []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: "",
		Pair:         nil,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdatePriceTickSize(wctx, &types.MsgUpdatePriceTickSize{
		Creator:      keepertest.TestAccount,
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)
}

// Test only contract creator can update tick size for contract
func TestInvalidUpdatePriceTickSizeCreator(t *testing.T) {
	// Instantiate and get contract address
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	wctx := sdk.WrapSDKContext(ctx)
	keeper := testApp.DexKeeper

	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000000)), sdk.NewCoin("uusdc", sdk.NewInt(100000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	wasm, err := ioutil.ReadFile("../../testdata/mars.wasm")
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

	server := msgserver.NewMsgServerImpl(keeper)
	err = RegisterContractUtil(server, wctx, contractAddr.String(), nil)
	require.NoError(t, err)

	// First register pair
	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: contractAddr.String(),
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NoError(t, err)

	// Test invalid tx creator
	tickUpdates := []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: contractAddr.String(),
		Pair:         &keepertest.TestPair,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err = server.UpdatePriceTickSize(wctx, &types.MsgUpdatePriceTickSize{
		Creator:      "sei18rrckuelmacz4fv4v2hl9t3kaw7mm4wpe8v36m",
		TickSizeList: tickUpdates,
	})
	require.NotNil(t, err)
}
