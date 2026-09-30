package wasmd_test

import (
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	abitypes "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/app"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestAddress(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	p, err := wasmd.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	require.Equal(t, "0x0000000000000000000000000000000000001002", p.Address().Hex())
}

func TestInstantiate(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	mockAddr, mockEVMAddr := testkeeper.MockAddressPair()
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithIsEVM(true)
	testApp.EvmKeeper.SetAddressMapping(ctx, mockAddr, mockEVMAddr)
	wasmKeeper := wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper)
	p, err := wasmd.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	code, err := os.ReadFile("../../example/cosmwasm/echo/artifacts/echo.wasm")
	require.Nil(t, err)
	codeID, err := wasmKeeper.Create(ctx, mockAddr, code, nil)
	require.Nil(t, err)
	instantiateMethod, err := p.ABI.MethodById(p.GetExecutor().(*wasmd.PrecompileExecutor).InstantiateID)
	require.Nil(t, err)
	amts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000)))
	amtsbz, err := amts.MarshalJSON()
	testApp.BankKeeper.MintCoins(ctx, "evm", amts)
	testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", mockAddr, amts)
	testApp.BankKeeper.MintCoins(ctx, "evm", amts)
	testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", mockAddr, amts)
	require.Nil(t, err)
	args, err := instantiateMethod.Inputs.Pack(
		codeID,
		mockAddr.String(),
		[]byte("{}"),
		"test",
		amtsbz,
	)
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm := vm.EVM{
		StateDB: statedb,
	}
	testApp.BankKeeper.SendCoins(ctx, mockAddr, testApp.EvmKeeper.GetSeiAddressOrDefault(ctx, common.HexToAddress(wasmd.WasmdAddress)), amts)
	suppliedGas := uint64(1000000)
	res, g, err := p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).InstantiateID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false, false)
	require.Nil(t, err)
	outputs, err := instantiateMethod.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 2, len(outputs))
	require.Equal(t, "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m", outputs[0].(string))
	require.Empty(t, outputs[1].([]byte))
	require.NotZero(t, g)

	amtsbz, err = sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	args, err = instantiateMethod.Inputs.Pack(
		codeID,
		mockAddr.String(),
		[]byte("{}"),
		"test",
		amtsbz,
	)
	require.Nil(t, err)
	statedb = state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm = vm.EVM{
		StateDB: statedb,
	}
	res, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).InstantiateID, args...), suppliedGas, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err = instantiateMethod.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 2, len(outputs))
	require.Equal(t, "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m", outputs[0].(string))
	require.Empty(t, outputs[1].([]byte))
	require.NotZero(t, g)

	// non-existent code ID
	args, _ = instantiateMethod.Inputs.Pack(
		codeID+1,
		mockAddr.String(),
		[]byte("{}"),
		"test",
		amtsbz,
	)
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).InstantiateID, args...), suppliedGas, nil, nil, false, false)
	require.NotNil(t, err)
	require.NotNil(t, statedb.GetPrecompileError())
	require.Equal(t, uint64(0), g)

	// bad inputs
	badArgs, _ := instantiateMethod.Inputs.Pack(codeID, "not bech32", []byte("{}"), "test", amtsbz)
	statedb.SetPrecompileError(nil)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).InstantiateID, badArgs...), suppliedGas, nil, nil, false, false)
	require.NotNil(t, err)
	require.NotNil(t, statedb.GetPrecompileError())
	badArgs, _ = instantiateMethod.Inputs.Pack(codeID, mockAddr.String(), []byte("{}"), "test", []byte("bad coins"))
	statedb.SetPrecompileError(nil)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).InstantiateID, badArgs...), suppliedGas, nil, nil, false, false)
	require.NotNil(t, err)
	require.NotNil(t, statedb.GetPrecompileError())
}

func TestExecute(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	mockAddr, mockEVMAddr := testkeeper.MockAddressPair()
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithIsEVM(true)
	testApp.EvmKeeper.SetAddressMapping(ctx, mockAddr, mockEVMAddr)
	wasmKeeper := wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper)
	p, err := wasmd.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	code, err := os.ReadFile("../../example/cosmwasm/echo/artifacts/echo.wasm")
	require.Nil(t, err)
	codeID, err := wasmKeeper.Create(ctx, mockAddr, code, nil)
	require.Nil(t, err)
	contractAddr, _, err := wasmKeeper.Instantiate(ctx, codeID, mockAddr, mockAddr, []byte("{}"), "test", sdk.NewCoins())
	require.Nil(t, err)

	amts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000)))
	testApp.BankKeeper.MintCoins(ctx, "evm", amts)
	testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", mockAddr, amts)
	testApp.BankKeeper.MintCoins(ctx, "evm", amts)
	testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", mockAddr, amts)
	amtsbz, err := amts.MarshalJSON()
	require.Nil(t, err)
	executeMethod, err := p.ABI.MethodById(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID)
	require.Nil(t, err)
	args, err := executeMethod.Inputs.Pack(contractAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm := vm.EVM{
		StateDB: statedb,
	}
	suppliedGas := uint64(1000000)
	testApp.BankKeeper.SendCoins(ctx, mockAddr, testApp.EvmKeeper.GetSeiAddressOrDefault(ctx, common.HexToAddress(wasmd.WasmdAddress)), amts)
	// circular interop
	statedb.WithCtx(statedb.Ctx().WithIsEVM(false))
	testApp.EvmKeeper.SetCode(statedb.Ctx(), mockEVMAddr, []byte{1, 2, 3})
	res, _, err := p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false, false)
	require.Nil(t, res)
	require.Equal(t, vm.ErrExecutionReverted, err)
	statedb.WithCtx(statedb.Ctx().WithIsEVM(true))
	res, g, err := p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false, false)
	require.Nil(t, err)
	outputs, err := executeMethod.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(outputs))
	require.Equal(t, fmt.Sprintf("received test msg from %s with 1000usei", mockAddr.String()), string(outputs[0].([]byte)))
	require.NotZero(t, g)
	require.Equal(t, int64(1000), testApp.BankKeeper.GetBalance(statedb.Ctx(), contractAddr, "usei").Amount.Int64())

	amtsbz, err = sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	args, err = executeMethod.Inputs.Pack(contractAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false, false)
	require.NotNil(t, err) // used coins instead of `value` to send usei to the contract

	args, err = executeMethod.Inputs.Pack(contractAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false, false)
	require.NotNil(t, err)

	amtsbz, err = sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	args, err = executeMethod.Inputs.Pack(contractAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false, false)
	require.NotNil(t, err)

	// allowed delegatecall
	contractAddrAllowed := common.BytesToAddress([]byte("contractA"))
	testApp.EvmKeeper.SetERC20CW20Pointer(ctx, contractAddr.String(), contractAddrAllowed)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, contractAddrAllowed, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, nil, nil, false, false)
	require.Nil(t, err)

	// disallowed delegatecall
	contractAddrDisallowed := common.BytesToAddress([]byte("contractB"))
	statedb.SetPrecompileError(nil)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, contractAddrDisallowed, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, nil, nil, false, true)
	require.NotNil(t, err)
	require.NotNil(t, statedb.GetPrecompileError())

	// bad contract address
	args, _ = executeMethod.Inputs.Pack(mockAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	statedb.SetPrecompileError(nil)
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, nil, nil, false, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
	require.NotNil(t, statedb.GetPrecompileError())

	// bad inputs
	args, _ = executeMethod.Inputs.Pack("not bech32", []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	statedb.SetPrecompileError(nil)
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, nil, nil, false, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
	require.NotNil(t, statedb.GetPrecompileError())
	args, _ = executeMethod.Inputs.Pack(contractAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), []byte("bad coins"))
	statedb.SetPrecompileError(nil)
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, nil, nil, false, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
	require.NotNil(t, statedb.GetPrecompileError())
}

func TestQuery(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	mockAddr, mockEVMAddr := testkeeper.MockAddressPair()
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithIsEVM(true)
	testApp.EvmKeeper.SetAddressMapping(ctx, mockAddr, mockEVMAddr)
	wasmKeeper := wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper)
	p, err := wasmd.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	code, err := os.ReadFile("../../example/cosmwasm/echo/artifacts/echo.wasm")
	require.Nil(t, err)
	codeID, err := wasmKeeper.Create(ctx, mockAddr, code, nil)
	require.Nil(t, err)
	contractAddr, _, err := wasmKeeper.Instantiate(ctx, codeID, mockAddr, mockAddr, []byte("{}"), "test", sdk.NewCoins())
	require.Nil(t, err)

	queryMethod, err := p.ABI.MethodById(p.GetExecutor().(*wasmd.PrecompileExecutor).QueryID)
	require.Nil(t, err)
	args, err := queryMethod.Inputs.Pack(contractAddr.String(), []byte("{\"info\":{}}"))
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm := vm.EVM{
		StateDB: statedb,
	}
	suppliedGas := uint64(1000000)
	res, g, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*wasmd.PrecompileExecutor).QueryID, args...), suppliedGas, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err := queryMethod.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(outputs))
	require.Equal(t, "{\"message\":\"query test\"}", string(outputs[0].([]byte)))
	require.NotZero(t, g)

	// bad contract address
	args, _ = queryMethod.Inputs.Pack(mockAddr.String(), []byte("{\"info\":{}}"))
	_, g, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, nil, nil, false, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)

	// bad input
	args, _ = queryMethod.Inputs.Pack("not bech32", []byte("{\"info\":{}}"))
	_, g, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, nil, nil, false, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
	args, _ = queryMethod.Inputs.Pack(contractAddr.String(), []byte("{\"bad\":{}}"))
	_, g, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*wasmd.PrecompileExecutor).ExecuteID, args...), suppliedGas, nil, nil, false, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
}

// setupWasmdQueryTest instantiates the echo contract and returns the fixtures
// shared by the read-only query tests.
func setupWasmdQueryTest(t *testing.T) (testApp *app.App, ctx sdk.Context, mockAddr sdk.AccAddress, wasmKeeper *wasmkeeper.PermissionedKeeper, p *pcommon.DynamicGasPrecompile, codeID uint64, contractAddr sdk.AccAddress, code []byte) {
	testApp = app.Setup(t, false, false, false)
	mockAddr, mockEVMAddr := testkeeper.MockAddressPair()
	ctx = testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithIsEVM(true)
	testApp.EvmKeeper.SetAddressMapping(ctx, mockAddr, mockEVMAddr)
	wasmKeeper = wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper)
	p, err := wasmd.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	code, err = os.ReadFile("../../example/cosmwasm/echo/artifacts/echo.wasm")
	require.Nil(t, err)
	codeID, err = wasmKeeper.Create(ctx, mockAddr, code, nil)
	require.Nil(t, err)
	contractAddr, _, err = wasmKeeper.Instantiate(ctx, codeID, mockAddr, mockAddr, []byte("{}"), "test", sdk.NewCoins())
	require.Nil(t, err)
	return
}

// runWasmdQuery packs args, invokes the precompile method identified by
// methodID as a view call, and returns the raw ABI-encoded response.
func runWasmdQuery(t *testing.T, ctx sdk.Context, testApp *app.App, p *pcommon.DynamicGasPrecompile, methodID []byte, args ...interface{}) ([]byte, *abitypes.Method) {
	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm := vm.EVM{StateDB: statedb}
	method, err := p.ABI.MethodById(methodID)
	require.Nil(t, err)
	inputs, err := method.Inputs.Pack(args...)
	require.Nil(t, err)
	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), uint64(1000000), nil, nil, false, false)
	require.Nil(t, err)
	return ret, method
}

func TestQueryContractInfo(t *testing.T) {
	testApp, ctx, mockAddr, _, p, codeID, contractAddr, _ := setupWasmdQueryTest(t)

	ret, method := runWasmdQuery(t, ctx, testApp, p, p.GetExecutor().(*wasmd.PrecompileExecutor).ContractInfoID, contractAddr.String())
	expected, err := method.Outputs.Pack(wasmd.ContractInfo{
		CodeID:  codeID,
		Creator: mockAddr.String(),
		Admin:   mockAddr.String(),
		Label:   "test",
	})
	require.Nil(t, err)
	require.Equal(t, expected, ret)
}

func TestQueryContractHistory(t *testing.T) {
	testApp, ctx, _, _, p, codeID, contractAddr, _ := setupWasmdQueryTest(t)

	ret, method := runWasmdQuery(t, ctx, testApp, p, p.GetExecutor().(*wasmd.PrecompileExecutor).ContractHistoryID, contractAddr.String(), []byte{})
	expected, err := method.Outputs.Pack([]wasmd.ContractCodeHistoryEntry{
		{
			Operation: 1, // init
			CodeID:    codeID,
			Msg:       []byte("{}"),
		},
	}, []byte{})
	require.Nil(t, err)
	require.Equal(t, expected, ret)
}

func TestQueryContractsByCode(t *testing.T) {
	testApp, ctx, _, _, p, codeID, contractAddr, _ := setupWasmdQueryTest(t)

	ret, method := runWasmdQuery(t, ctx, testApp, p, p.GetExecutor().(*wasmd.PrecompileExecutor).ContractsByCodeID, codeID, []byte{})
	expected, err := method.Outputs.Pack([]string{contractAddr.String()}, []byte{})
	require.Nil(t, err)
	require.Equal(t, expected, ret)
}

func TestQueryAllContractState(t *testing.T) {
	testApp, ctx, _, _, p, _, contractAddr, _ := setupWasmdQueryTest(t)

	ret, method := runWasmdQuery(t, ctx, testApp, p, p.GetExecutor().(*wasmd.PrecompileExecutor).AllContractStateID, contractAddr.String(), []byte{})
	expected, err := method.Outputs.Pack([]wasmd.Model{}, []byte{})
	require.Nil(t, err)
	require.Equal(t, expected, ret)
}

func TestQueryRawContractState(t *testing.T) {
	testApp, ctx, _, _, p, _, contractAddr, _ := setupWasmdQueryTest(t)

	ret, method := runWasmdQuery(t, ctx, testApp, p, p.GetExecutor().(*wasmd.PrecompileExecutor).RawContractStateID, contractAddr.String(), []byte("unused-key"))
	expected, err := method.Outputs.Pack([]byte{})
	require.Nil(t, err)
	require.Equal(t, expected, ret)
}

func TestQueryCode(t *testing.T) {
	testApp, ctx, mockAddr, _, p, codeID, _, code := setupWasmdQueryTest(t)
	codeInfo := testApp.WasmKeeper.GetCodeInfo(ctx, codeID)
	require.NotNil(t, codeInfo)

	ret, method := runWasmdQuery(t, ctx, testApp, p, p.GetExecutor().(*wasmd.PrecompileExecutor).CodeID, codeID)
	expected, err := method.Outputs.Pack(wasmd.CodeInfo{
		CodeID:   codeID,
		Creator:  mockAddr.String(),
		DataHash: codeInfo.CodeHash,
		InstantiatePermission: wasmd.AccessConfig{
			Permission: uint8(codeInfo.InstantiateConfig.Permission),
			Address:    codeInfo.InstantiateConfig.Address,
		},
	}, code)
	require.Nil(t, err)
	require.Equal(t, expected, ret)
}

func TestQueryCodes(t *testing.T) {
	testApp, ctx, mockAddr, _, p, codeID, _, _ := setupWasmdQueryTest(t)
	codeInfo := testApp.WasmKeeper.GetCodeInfo(ctx, codeID)
	require.NotNil(t, codeInfo)

	ret, method := runWasmdQuery(t, ctx, testApp, p, p.GetExecutor().(*wasmd.PrecompileExecutor).CodesID, []byte{})
	expected, err := method.Outputs.Pack([]wasmd.CodeInfo{
		{
			CodeID:   codeID,
			Creator:  mockAddr.String(),
			DataHash: codeInfo.CodeHash,
			InstantiatePermission: wasmd.AccessConfig{
				Permission: uint8(codeInfo.InstantiateConfig.Permission),
				Address:    codeInfo.InstantiateConfig.Address,
			},
		},
	}, []byte{})
	require.Nil(t, err)
	require.Equal(t, expected, ret)
}

func TestQueryPinnedCodes(t *testing.T) {
	testApp, ctx, _, wasmKeeper, p, codeID, _, _ := setupWasmdQueryTest(t)
	require.Nil(t, wasmKeeper.PinCode(ctx, codeID))

	ret, method := runWasmdQuery(t, ctx, testApp, p, p.GetExecutor().(*wasmd.PrecompileExecutor).PinnedCodesID, []byte{})
	expected, err := method.Outputs.Pack([]uint64{codeID}, []byte{})
	require.Nil(t, err)
	require.Equal(t, expected, ret)
}
