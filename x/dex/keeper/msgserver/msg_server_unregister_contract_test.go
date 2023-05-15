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
	"github.com/sei-protocol/sei-chain/x/dex"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestUnregisterContractSetSiblings(t *testing.T) {
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
	contract := types.ContractInfoV2{
		CodeId:       1,
		ContractAddr: contractAddr.String(),
		Creator:      testAccount.String(),
		RentBalance:  types.DefaultParams().MinRentDeposit,
	}
	_, err = server.RegisterContract(wctx, &types.MsgRegisterContract{
		Creator:  testAccount.String(),
		Contract: &contract,
	})
	require.NoError(t, err)
	_, err = keeper.GetContract(ctx, contractAddr.String())
	require.NoError(t, err)
	balance := keeper.BankKeeper.GetBalance(ctx, testAccount, "usei")
	require.Equal(t, int64(89900000), balance.Amount.Int64())

	handler := dex.NewHandler(keeper)
	tickSize := sdk.OneDec()
	_, err = handler(ctx, &types.MsgRegisterPairs{
		Creator: testAccount.String(),
		Batchcontractpair: []types.BatchContractPair{
			{
				ContractAddr: contractAddr.String(),
				Pairs: []*types.Pair{
					{
						PriceDenom:       "usei",
						AssetDenom:       "uatom",
						PriceTicksize:    &tickSize,
						QuantityTicksize: &tickSize,
					},
				},
			},
		},
	})
	require.NoError(t, err)

	_, err = handler(ctx, &types.MsgUnregisterContract{
		Creator:      testAccount.String(),
		ContractAddr: contractAddr.String(),
	})
	require.NoError(t, err)
	_, err = keeper.GetContract(ctx, contractAddr.String())
	require.Error(t, err)
	balance = keeper.BankKeeper.GetBalance(ctx, testAccount, "usei")
	require.Equal(t, int64(99900000), balance.Amount.Int64())
	pairs := keeper.GetAllRegisteredPairs(ctx, contractAddr.String())
	require.Empty(t, pairs)
}
