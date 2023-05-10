package msgserver_test

import (
	"context"
	"io/ioutil"
	"math"
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

func TestDepositRent(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	wctx := sdk.WrapSDKContext(ctx)
	dexkeeper := testApp.DexKeeper

	testAccount, _ := sdk.AccAddressFromBech32("sei1h9yjz89tl0dl6zu65dpxcqnxfhq60wxx8s5kag")
	depositAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000000)), sdk.NewCoin("uusdc", sdk.NewInt(100000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, depositAccount, amounts)
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

	server := msgserver.NewMsgServerImpl(dexkeeper)
	contract := types.ContractInfoV2{
		CodeId:       codeId,
		ContractAddr: contractAddr.String(),
		Creator:      testAccount.String(),
		RentBalance:  types.DefaultParams().MinRentDeposit,
	}
	_, err = server.RegisterContract(wctx, &types.MsgRegisterContract{
		Creator:  testAccount.String(),
		Contract: &contract,
	})
	require.NoError(t, err)
	_, err = dexkeeper.GetContract(ctx, TestContractA)
	require.NoError(t, err)
	balance := dexkeeper.BankKeeper.GetBalance(ctx, testAccount, "usei")
	require.Equal(t, int64(89900000), balance.Amount.Int64())

	handler := dex.NewHandler(dexkeeper)
	_, err = handler(ctx, &types.MsgContractDepositRent{
		Sender:       depositAccount.String(),
		ContractAddr: TestContractA,
		Amount:       types.DefaultParams().MinRentDeposit,
	})
	require.NoError(t, err)
	_, err = dexkeeper.GetContract(ctx, TestContractA)
	require.NoError(t, err)
	balance = dexkeeper.BankKeeper.GetBalance(ctx, testAccount, "usei")
	require.Equal(t, int64(89900000), balance.Amount.Int64())
	balance = dexkeeper.BankKeeper.GetBalance(ctx, depositAccount, "usei")
	require.Equal(t, int64(90000000), balance.Amount.Int64())

	// deposit exceeds limit
	_, err = handler(ctx, &types.MsgContractDepositRent{
		Sender:       testAccount.String(),
		ContractAddr: TestContractA,
		Amount:       math.MaxUint64,
	})
	require.Error(t, err)
	// deposit + prev balance exceeds limit
	_, err = handler(ctx, &types.MsgContractDepositRent{
		Sender:       testAccount.String(),
		ContractAddr: TestContractA,
		Amount:       math.MaxUint64/140000000 - 500000,
	})
	require.Error(t, err)
	// deposit + prev balance overflows
	_, err = handler(ctx, &types.MsgContractDepositRent{
		Sender:       testAccount.String(),
		ContractAddr: TestContractA,
		Amount:       math.MaxUint64 - 500000,
	})
	require.Error(t, err)
}
