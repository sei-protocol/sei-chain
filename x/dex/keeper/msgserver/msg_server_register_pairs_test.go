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

func TestRegisterPairs(t *testing.T) {
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
	contractAddrA, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	contractAddrB, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	server := msgserver.NewMsgServerImpl(keeper)
	err = RegisterContractUtil(server, wctx, contractAddrA.String(), nil)
	require.NoError(t, err)

	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: contractAddrA.String(),
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})

	require.NoError(t, err)
	storedRegisteredPairs := keeper.GetAllRegisteredPairs(ctx, contractAddrA.String())
	require.Equal(t, 1, len(storedRegisteredPairs))
	require.Equal(t, keepertest.TestPair, storedRegisteredPairs[0])

	// Test multiple pairs registered at once
	err = RegisterContractUtil(server, wctx, contractAddrB.String(), nil)
	require.NoError(t, err)
	multiplePairs := []types.BatchContractPair{}
	secondTestPair := types.Pair{
		PriceDenom:       "sei",
		AssetDenom:       "osmo",
		PriceTicksize:    &keepertest.TestTicksize,
		QuantityTicksize: &keepertest.TestTicksize,
	}
	multiplePairs = append(multiplePairs, types.BatchContractPair{
		ContractAddr: contractAddrB.String(),
		Pairs:        []*types.Pair{&keepertest.TestPair, &secondTestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: multiplePairs,
	})

	require.NoError(t, err)
	storedRegisteredPairs = keeper.GetAllRegisteredPairs(ctx, contractAddrB.String())
	require.Equal(t, 2, len(storedRegisteredPairs))
	require.Equal(t, secondTestPair, storedRegisteredPairs[0])
	require.Equal(t, keepertest.TestPair, storedRegisteredPairs[1])
}

func TestRegisterPairsInvalidMsg(t *testing.T) {
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
	contractAddrA, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	server := msgserver.NewMsgServerImpl(keeper)
	err = RegisterContractUtil(server, wctx, contractAddrA.String(), nil)
	require.NoError(t, err)
	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: contractAddrA.String(),
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})

	// Test with empty creator address
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           "",
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)

	// Test with empty msg
	batchContractPairs = []types.BatchContractPair{}
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)

	// Test with invalid Creator address
	batchContractPairs = []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: contractAddrA.String(),
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           "invalidAddress",
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)

	// Test with empty contract address
	batchContractPairs = []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: "",
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)

	// Test with empty pairs list
	batchContractPairs = []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: contractAddrA.String(),
		Pairs:        []*types.Pair{},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)

	// Test with nil pair
	batchContractPairs = []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: contractAddrA.String(),
		Pairs:        []*types.Pair{nil},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	require.NotNil(t, err)
}

// Test only contract creator can update registered pairs for contract
func TestInvalidRegisterPairCreator(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	eventManger := ctx.EventManager()
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
	contractAddrA, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	server := msgserver.NewMsgServerImpl(keeper)
	err = RegisterContractUtil(server, wctx, contractAddrA.String(), nil)
	require.NoError(t, err)

	// Expect error when registering pair with an address not contract creator
	batchContractPairs := []types.BatchContractPair{}

	initalEventSize := len(eventManger.Events())
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: contractAddrA.String(),
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           "sei18rrckuelmacz4fv4v2hl9t3kaw7mm4wpe8v36m",
		Batchcontractpair: batchContractPairs,
	})
	// Nothing emitted when creator != address
	require.Equal(t, len(eventManger.Events()), initalEventSize)
	require.NotNil(t, err)

	// Works when creator = address
	initalEventSize = len(eventManger.Events())
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	// One pair emitted
	require.Greater(t, len(eventManger.Events()), initalEventSize)
	require.NoError(t, err)

	// Remit the process the same pairs again
	initalEventSize = len(eventManger.Events())
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})
	// No event change this time
	require.Equal(t, len(eventManger.Events()), initalEventSize)
	require.NoError(t, err)

}

func TestRegisterPairsExceedingLimit(t *testing.T) {
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
	contractAddrA, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	params := keeper.GetParams(ctx)
	params.MaxPairsPerContract = 0
	keeper.SetParams(ctx, params)
	server := msgserver.NewMsgServerImpl(keeper)
	err = RegisterContractUtil(server, wctx, contractAddrA.String(), nil)
	require.NoError(t, err)

	batchContractPairs := []types.BatchContractPair{}
	batchContractPairs = append(batchContractPairs, types.BatchContractPair{
		ContractAddr: contractAddrA.String(),
		Pairs:        []*types.Pair{&keepertest.TestPair},
	})
	_, err = server.RegisterPairs(wctx, &types.MsgRegisterPairs{
		Creator:           keepertest.TestAccount,
		Batchcontractpair: batchContractPairs,
	})

	require.NotNil(t, err)
}
