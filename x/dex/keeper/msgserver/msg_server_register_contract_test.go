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
	"github.com/sei-protocol/sei-chain/utils"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

const (
	TestContractA = "sei1hrpna9v7vs3stzyd4z3xf00676kf78zpe2u5ksvljswn2vnjp3yslucc3n"
	TestContractB = "sei1nc5tatafv6eyq7llkr2gv50ff9e22mnf70qgjlv737ktmt4eswrqms7u8a"
	TestContractC = "sei1xr3rq8yvd7qplsw5yx90ftsr2zdhg4e9z60h5duusgxpv72hud3shh3qfl"
	TestContractD = "sei1up07dctjqud4fns75cnpejr4frmjtddzsmwgcktlyxd4zekhwecqghxqcp"
	TestContractX = "sei1hw5n2l4v5vz8lk4sj69j7pwdaut0kkn90mw09snlkdd3f7ckld0smdtvee"
	TestContractY = "sei12pwnhtv7yat2s30xuf4gdk9qm85v4j3e6p44let47pdffpklcxlqh8ag0z"
)

func TestRegisterContract(t *testing.T) {
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
	storedContracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))
	require.Nil(t, storedContracts[0].Dependencies)

	// dependency doesn't exist
	err = RegisterContractUtil(server, wctx, contractAddr.String(), []string{TestContractY})
	require.NotNil(t, err)
	storedContracts = keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))
}

func TestRegisterContractCircularDependency(t *testing.T) {
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
	contractAddrFirst, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}
	contractAddrSecond, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	server := msgserver.NewMsgServerImpl(keeper)
	RegisterContractUtil(server, wctx, contractAddrFirst.String(), nil)
	storedContracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, 1, len(storedContracts))

	RegisterContractUtil(server, wctx, contractAddrSecond.String(), []string{contractAddrFirst.String()})
	storedContracts = keeper.GetAllContractInfo(ctx)
	require.Equal(t, 2, len(storedContracts))

	// This contract should fail to be registered because it causes a
	// circular dependency
	err = RegisterContractUtil(server, wctx, contractAddrFirst.String(), []string{contractAddrFirst.String()})
	require.NotNil(t, err)
}

func TestRegisterContractDuplicateDependency(t *testing.T) {
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
	err = RegisterContractUtil(server, wctx, contractAddr.String(), []string{contractAddr.String(), contractAddr.String()})
	require.NotNil(t, err)
	storedContracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, 0, len(storedContracts))
}

func TestRegisterContractNumIncomingPaths(t *testing.T) {
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
	contractAddrFirst, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}
	contractAddrSecond, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	server := msgserver.NewMsgServerImpl(keeper)
	err = RegisterContractUtil(server, wctx, contractAddrFirst.String(), nil)
	require.Nil(t, err)
	storedContract, err := keeper.GetContract(ctx, contractAddrFirst.String())
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)

	RegisterContractUtil(server, wctx, contractAddrSecond.String(), []string{contractAddrFirst.String()})
	storedContract, err = keeper.GetContract(ctx, contractAddrFirst.String())
	require.Nil(t, err)
	require.Equal(t, int64(1), storedContract.NumIncomingDependencies)
	storedContract, err = keeper.GetContract(ctx, contractAddrSecond.String())
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)

	RegisterContractUtil(server, wctx, contractAddrSecond.String(), nil)
	storedContract, err = keeper.GetContract(ctx, contractAddrFirst.String())
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)
	storedContract, err = keeper.GetContract(ctx, contractAddrFirst.String())
	require.Nil(t, err)
	require.Equal(t, int64(0), storedContract.NumIncomingDependencies)
}

func TestRegisterContractSetSiblings(t *testing.T) {
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
	contractAddrC, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}
	contractAddrD, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}
	contractAddrX, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}
	contractAddrY, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	// A -> X, B -> X, C -> Y
	server := msgserver.NewMsgServerImpl(keeper)
	err = RegisterContractUtil(server, wctx, contractAddrX.String(), nil)
	if err != nil {
		panic(err)
	}
	err = RegisterContractUtil(server, wctx, contractAddrY.String(), nil)
	if err != nil {
		panic(err)
	}
	err = RegisterContractUtil(server, wctx, contractAddrA.String(), []string{contractAddrX.String()})
	if err != nil {
		panic(err)
	}
	err = RegisterContractUtil(server, wctx, contractAddrB.String(), []string{contractAddrX.String()})
	if err != nil {
		panic(err)
	}
	err = RegisterContractUtil(server, wctx, contractAddrC.String(), []string{contractAddrY.String()})
	if err != nil {
		panic(err)
	}
	// add D -> X, D -> Y
	err = RegisterContractUtil(server, wctx, contractAddrD.String(), []string{contractAddrX.String(), contractAddrY.String()})
	if err != nil {
		panic(err)
	}
	contract, _ := keeper.GetContract(ctx, contractAddrA.String())
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, contractAddrB.String(), contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, contractAddrB.String())
	require.Equal(t, contractAddrA.String(), contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, contractAddrD.String(), contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, contractAddrC.String())
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, contractAddrD.String(), contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, contractAddrD.String())
	require.Equal(t, contractAddrB.String(), contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[0].ImmediateYoungerSibling)
	require.Equal(t, contractAddrC.String(), contract.Dependencies[1].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[1].ImmediateYoungerSibling)
	// update D -> X only
	err = RegisterContractUtil(server, wctx, contractAddrD.String(), []string{contractAddrX.String()})
	if err != nil {
		panic(err)
	}
	contract, _ = keeper.GetContract(ctx, contractAddrD.String())
	require.Equal(t, 1, len(contract.Dependencies))
	require.Equal(t, contractAddrB.String(), contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, contractAddrA.String())
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, contractAddrB.String(), contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, contractAddrB.String())
	require.Equal(t, contractAddrA.String(), contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, contractAddrD.String(), contract.Dependencies[0].ImmediateYoungerSibling)
	contract, _ = keeper.GetContract(ctx, contractAddrC.String())
	require.Equal(t, "", contract.Dependencies[0].ImmediateElderSibling)
	require.Equal(t, "", contract.Dependencies[0].ImmediateYoungerSibling)
}

func TestRegisterContractWithInvalidRentBalance(t *testing.T) {
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
	contract := types.ContractInfoV2{
		CodeId:       1,
		ContractAddr: contractAddr.String(),
		RentBalance:  math.MaxUint64,
	}
	_, err = server.RegisterContract(wctx, &types.MsgRegisterContract{
		Creator:  keepertest.TestAccount,
		Contract: &contract,
	})
	require.Error(t, err)
}

func TestRegisterContractInvalidRentBalance(t *testing.T) {
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

	contractAddrX, _, err := contractKeeper.Instantiate(ctx, codeId, testAccount, testAccount, []byte(GOOD_CONTRACT_INSTANTIATE), "test",
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000))))
	if err != nil {
		panic(err)
	}

	// register with rent balance amount more than allowed
	server := msgserver.NewMsgServerImpl(keeper)
	rentBalance := uint64(math.MaxUint64)/wasmkeeper.DefaultGasMultiplier + 1
	contract := types.ContractInfoV2{
		CodeId:       1,
		ContractAddr: contractAddrX.String(),
		RentBalance:  rentBalance,
	}

	_, err = server.RegisterContract(wctx, &types.MsgRegisterContract{
		Creator:  keepertest.TestAccount,
		Contract: &contract,
	})
	require.Error(t, err)

	// register with rent balance less than allowed
	rentBalance = keeper.GetParams(ctx).MinRentDeposit - 1
	contract = types.ContractInfoV2{
		CodeId:       1,
		ContractAddr: contractAddrX.String(),
		RentBalance:  rentBalance,
	}

	_, err = server.RegisterContract(wctx, &types.MsgRegisterContract{
		Creator:  keepertest.TestAccount,
		Contract: &contract,
	})
	require.Error(t, err)
}

func RegisterContractUtil(server types.MsgServer, ctx context.Context, contractAddr string, dependencies []string) error {
	contract := types.ContractInfoV2{
		CodeId:       1,
		ContractAddr: contractAddr,
		RentBalance:  types.DefaultMinRentDeposit,
	}
	if dependencies != nil {
		contract.Dependencies = utils.Map(dependencies, func(addr string) *types.ContractDependencyInfo {
			return &types.ContractDependencyInfo{
				Dependency: addr,
			}
		})
	}
	_, err := server.RegisterContract(ctx, &types.MsgRegisterContract{
		Creator:  keepertest.TestAccount,
		Contract: &contract,
	})
	return err
}
