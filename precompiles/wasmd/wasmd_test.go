package wasmd_test

import (
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestRequiredGas(t *testing.T) {
	testApp := app.Setup(false, false)
	p, err := wasmd.NewPrecompile(&testApp.EvmKeeper, wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper), testApp.WasmKeeper, testApp.BankKeeper)
	require.Nil(t, err)
	require.Equal(t, uint64(2000), p.RequiredGas(p.ExecuteID))
	require.Equal(t, uint64(2000), p.RequiredGas(p.InstantiateID))
	require.Equal(t, uint64(2000), p.RequiredGas(p.ExecuteBatchID))
	require.Equal(t, uint64(1000), p.RequiredGas(p.QueryID))
	require.Equal(t, uint64(3000), p.RequiredGas([]byte{15, 15, 15, 15})) // invalid method
}

func TestAddress(t *testing.T) {
	testApp := app.Setup(false, false)
	p, err := wasmd.NewPrecompile(&testApp.EvmKeeper, wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper), testApp.WasmKeeper, testApp.BankKeeper)
	require.Nil(t, err)
	require.Equal(t, "0x0000000000000000000000000000000000001002", p.Address().Hex())
}

func TestInstantiate(t *testing.T) {
	testApp := app.Setup(false, false)
	mockAddr, mockEVMAddr := testkeeper.MockAddressPair()
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithIsEVM(true)
	testApp.EvmKeeper.SetAddressMapping(ctx, mockAddr, mockEVMAddr)
	wasmKeeper := wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper)
	p, err := wasmd.NewPrecompile(&testApp.EvmKeeper, wasmKeeper, testApp.WasmKeeper, testApp.BankKeeper)
	require.Nil(t, err)
	code, err := os.ReadFile("../../example/cosmwasm/echo/artifacts/echo.wasm")
	require.Nil(t, err)
	codeID, err := wasmKeeper.Create(ctx, mockAddr, code, nil)
	require.Nil(t, err)
	instantiateMethod, err := p.ABI.MethodById(p.InstantiateID)
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
	res, g, err := p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.InstantiateID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false)
	require.Nil(t, err)
	outputs, err := instantiateMethod.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 2, len(outputs))
	require.Equal(t, "sei1hrpna9v7vs3stzyd4z3xf00676kf78zpe2u5ksvljswn2vnjp3yslucc3n", outputs[0].(string))
	require.Empty(t, outputs[1].([]byte))
	require.Equal(t, uint64(881127), g)

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
	res, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.InstantiateID, args...), suppliedGas, nil, nil, false)
	require.Nil(t, err)
	outputs, err = instantiateMethod.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 2, len(outputs))
	require.Equal(t, "sei1hrpna9v7vs3stzyd4z3xf00676kf78zpe2u5ksvljswn2vnjp3yslucc3n", outputs[0].(string))
	require.Empty(t, outputs[1].([]byte))
	require.Equal(t, uint64(904183), g)

	// non-existent code ID
	args, _ = instantiateMethod.Inputs.Pack(
		codeID+1,
		mockAddr.String(),
		[]byte("{}"),
		"test",
		amtsbz,
	)
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.InstantiateID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.NotNil(t, statedb.GetPrecompileError())
	require.Equal(t, uint64(0), g)

	// bad inputs
	badArgs, _ := instantiateMethod.Inputs.Pack(codeID, "not bech32", []byte("{}"), "test", amtsbz)
	statedb.SetPrecompileError(nil)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.InstantiateID, badArgs...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.NotNil(t, statedb.GetPrecompileError())
	badArgs, _ = instantiateMethod.Inputs.Pack(codeID, mockAddr.String(), []byte("{}"), "test", []byte("bad coins"))
	statedb.SetPrecompileError(nil)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.InstantiateID, badArgs...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.NotNil(t, statedb.GetPrecompileError())
}

func TestExecute(t *testing.T) {
	testApp := app.Setup(false, false)
	mockAddr, mockEVMAddr := testkeeper.MockAddressPair()
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithIsEVM(true)
	testApp.EvmKeeper.SetAddressMapping(ctx, mockAddr, mockEVMAddr)
	wasmKeeper := wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper)
	p, err := wasmd.NewPrecompile(&testApp.EvmKeeper, wasmKeeper, testApp.WasmKeeper, testApp.BankKeeper)
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
	executeMethod, err := p.ABI.MethodById(p.ExecuteID)
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
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false)
	require.Equal(t, "sei does not support CW->EVM->CW call pattern", err.Error())
	statedb.WithCtx(statedb.Ctx().WithIsEVM(true))
	res, g, err := p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false)
	require.Nil(t, err)
	outputs, err := executeMethod.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(outputs))
	require.Equal(t, fmt.Sprintf("received test msg from %s with 1000usei", mockAddr.String()), string(outputs[0].([]byte)))
	require.Equal(t, uint64(907386), g)
	require.Equal(t, int64(1000), testApp.BankKeeper.GetBalance(statedb.Ctx(), contractAddr, "usei").Amount.Int64())

	amtsbz, err = sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	args, err = executeMethod.Inputs.Pack(contractAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false)
	require.NotNil(t, err) // used coins instead of `value` to send usei to the contract

	args, err = executeMethod.Inputs.Pack(contractAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false)
	require.NotNil(t, err)

	amtsbz, err = sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	args, err = executeMethod.Inputs.Pack(contractAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false)
	require.NotNil(t, err)

	// allowed delegatecall
	contractAddrAllowed := common.BytesToAddress([]byte("contractA"))
	testApp.EvmKeeper.SetERC20CW20Pointer(ctx, contractAddr.String(), contractAddrAllowed)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, contractAddrAllowed, append(p.ExecuteID, args...), suppliedGas, nil, nil, false)
	require.Nil(t, err)

	// disallowed delegatecall
	contractAddrDisallowed := common.BytesToAddress([]byte("contractB"))
	statedb.SetPrecompileError(nil)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, contractAddrDisallowed, append(p.ExecuteID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.NotNil(t, statedb.GetPrecompileError())

	// bad contract address
	args, _ = executeMethod.Inputs.Pack(mockAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	statedb.SetPrecompileError(nil)
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
	require.NotNil(t, statedb.GetPrecompileError())

	// bad inputs
	args, _ = executeMethod.Inputs.Pack("not bech32", []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	statedb.SetPrecompileError(nil)
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
	require.NotNil(t, statedb.GetPrecompileError())
	args, _ = executeMethod.Inputs.Pack(contractAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), []byte("bad coins"))
	statedb.SetPrecompileError(nil)
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
	require.NotNil(t, statedb.GetPrecompileError())
}

func TestQuery(t *testing.T) {
	testApp := app.Setup(false, false)
	mockAddr, mockEVMAddr := testkeeper.MockAddressPair()
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithIsEVM(true)
	testApp.EvmKeeper.SetAddressMapping(ctx, mockAddr, mockEVMAddr)
	wasmKeeper := wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper)
	p, err := wasmd.NewPrecompile(&testApp.EvmKeeper, wasmKeeper, testApp.WasmKeeper, testApp.BankKeeper)
	require.Nil(t, err)
	code, err := os.ReadFile("../../example/cosmwasm/echo/artifacts/echo.wasm")
	require.Nil(t, err)
	codeID, err := wasmKeeper.Create(ctx, mockAddr, code, nil)
	require.Nil(t, err)
	contractAddr, _, err := wasmKeeper.Instantiate(ctx, codeID, mockAddr, mockAddr, []byte("{}"), "test", sdk.NewCoins())
	require.Nil(t, err)

	queryMethod, err := p.ABI.MethodById(p.QueryID)
	require.Nil(t, err)
	args, err := queryMethod.Inputs.Pack(contractAddr.String(), []byte("{\"info\":{}}"))
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm := vm.EVM{
		StateDB: statedb,
	}
	suppliedGas := uint64(1000000)
	res, g, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.QueryID, args...), suppliedGas, nil, nil, false)
	require.Nil(t, err)
	outputs, err := queryMethod.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(outputs))
	require.Equal(t, "{\"message\":\"query test\"}", string(outputs[0].([]byte)))
	require.Equal(t, uint64(931712), g)

	// bad contract address
	args, _ = queryMethod.Inputs.Pack(mockAddr.String(), []byte("{\"info\":{}}"))
	_, g, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.ExecuteID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)

	// bad input
	args, _ = queryMethod.Inputs.Pack("not bech32", []byte("{\"info\":{}}"))
	_, g, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.ExecuteID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
	args, _ = queryMethod.Inputs.Pack(contractAddr.String(), []byte("{\"bad\":{}}"))
	_, g, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.ExecuteID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
}

func TestExecuteBatchOneMessage(t *testing.T) {
	testApp := app.Setup(false, false)
	mockAddr, mockEVMAddr := testkeeper.MockAddressPair()
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithIsEVM(true)
	testApp.EvmKeeper.SetAddressMapping(ctx, mockAddr, mockEVMAddr)
	wasmKeeper := wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper)
	p, err := wasmd.NewPrecompile(&testApp.EvmKeeper, wasmKeeper, testApp.WasmKeeper, testApp.BankKeeper)
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
	executeMethod, err := p.ABI.MethodById(p.ExecuteBatchID)
	require.Nil(t, err)
	executeMsg := wasmd.ExecuteMsg{
		ContractAddress: contractAddr.String(),
		Msg:             []byte("{\"echo\":{\"message\":\"test msg\"}}"),
		Coins:           amtsbz,
	}
	args, err := executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsg})
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm := vm.EVM{
		StateDB: statedb,
	}
	suppliedGas := uint64(1000000)
	testApp.BankKeeper.SendCoins(ctx, mockAddr, testApp.EvmKeeper.GetSeiAddressOrDefault(ctx, common.HexToAddress(wasmd.WasmdAddress)), amts)
	res, g, err := p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteBatchID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false)
	require.Nil(t, err)
	outputs, err := executeMethod.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(outputs))
	require.Equal(t, fmt.Sprintf("received test msg from %s with 1000usei", mockAddr.String()), string((outputs[0].([][]byte))[0]))
	require.Equal(t, uint64(907386), g)
	require.Equal(t, int64(1000), testApp.BankKeeper.GetBalance(statedb.Ctx(), contractAddr, "usei").Amount.Int64())

	amtsbz, err = sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	executeMsg = wasmd.ExecuteMsg{
		ContractAddress: contractAddr.String(),
		Msg:             []byte("{\"echo\":{\"message\":\"test msg\"}}"),
		Coins:           amtsbz,
	}
	args, err = executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsg})
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteBatchID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false)
	require.NotNil(t, err) // value and amounts not equal

	// allowed delegatecall
	contractAddrAllowed := common.BytesToAddress([]byte("contractA"))
	testApp.EvmKeeper.SetERC20CW20Pointer(ctx, contractAddr.String(), contractAddrAllowed)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, contractAddrAllowed, append(p.ExecuteBatchID, args...), suppliedGas, nil, nil, false)
	require.Nil(t, err)

	// disallowed delegatecall
	contractAddrDisallowed := common.BytesToAddress([]byte("contractB"))
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, contractAddrDisallowed, append(p.ExecuteBatchID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)

	// bad contract address
	executeMsg = wasmd.ExecuteMsg{
		ContractAddress: mockAddr.String(),
		Msg:             []byte("{\"echo\":{\"message\":\"test msg\"}}"),
		Coins:           amtsbz,
	}
	args, _ = executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsg})
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteBatchID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)

	// bad inputs
	executeMsg = wasmd.ExecuteMsg{
		ContractAddress: "not bech32",
		Msg:             []byte("{\"echo\":{\"message\":\"test msg\"}}"),
		Coins:           amtsbz,
	}
	args, _ = executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsg})
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteBatchID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
	executeMsg = wasmd.ExecuteMsg{
		ContractAddress: contractAddr.String(),
		Msg:             []byte("{\"echo\":{\"message\":\"test msg\"}}"),
		Coins:           []byte("bad coins"),
	}
	args, _ = executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsg})
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteBatchID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
}

func TestExecuteBatchValueImmutability(t *testing.T) {
	testApp := app.Setup(false, false)
	mockAddr, mockEVMAddr := testkeeper.MockAddressPair()
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithIsEVM(true)
	testApp.EvmKeeper.SetAddressMapping(ctx, mockAddr, mockEVMAddr)
	wasmKeeper := wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper)
	p, err := wasmd.NewPrecompile(&testApp.EvmKeeper, wasmKeeper, testApp.WasmKeeper, testApp.BankKeeper)
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
	executeMethod, err := p.ABI.MethodById(p.ExecuteBatchID)
	require.Nil(t, err)
	executeMsg := wasmd.ExecuteMsg{
		ContractAddress: contractAddr.String(),
		Msg:             []byte("{\"echo\":{\"message\":\"test msg\"}}"),
		Coins:           amtsbz,
	}
	args, err := executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsg})
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm := vm.EVM{
		StateDB: statedb,
	}
	suppliedGas := uint64(1000000)
	testApp.BankKeeper.SendCoins(ctx, mockAddr, testApp.EvmKeeper.GetSeiAddressOrDefault(ctx, common.HexToAddress(wasmd.WasmdAddress)), amts)
	value := big.NewInt(1000_000_000_000_000)
	valueCopy := new(big.Int).Set(value)
	p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteBatchID, args...), suppliedGas, value, nil, false)

	require.Equal(t, valueCopy, value)
}

func TestExecuteBatchMultipleMessages(t *testing.T) {
	testApp := app.Setup(false, false)
	mockAddr, mockEVMAddr := testkeeper.MockAddressPair()
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithIsEVM(true)
	testApp.EvmKeeper.SetAddressMapping(ctx, mockAddr, mockEVMAddr)
	wasmKeeper := wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper)
	p, err := wasmd.NewPrecompile(&testApp.EvmKeeper, wasmKeeper, testApp.WasmKeeper, testApp.BankKeeper)
	require.Nil(t, err)
	code, err := os.ReadFile("../../example/cosmwasm/echo/artifacts/echo.wasm")
	require.Nil(t, err)
	codeID, err := wasmKeeper.Create(ctx, mockAddr, code, nil)
	require.Nil(t, err)
	contractAddr, _, err := wasmKeeper.Instantiate(ctx, codeID, mockAddr, mockAddr, []byte("{}"), "test", sdk.NewCoins())
	require.Nil(t, err)

	amts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000)))
	largeAmts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(3000)))
	testApp.BankKeeper.MintCoins(ctx, "evm", sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(13000))))
	testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", mockAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(13000))))
	amtsbz, err := amts.MarshalJSON()
	require.Nil(t, err)
	executeMethod, err := p.ABI.MethodById(p.ExecuteBatchID)
	require.Nil(t, err)
	executeMsgWithCoinsAmt := wasmd.ExecuteMsg{
		ContractAddress: contractAddr.String(),
		Msg:             []byte("{\"echo\":{\"message\":\"test msg\"}}"),
		Coins:           amtsbz,
	}

	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm := vm.EVM{
		StateDB: statedb,
	}
	suppliedGas := uint64(1000000)
	err = testApp.BankKeeper.SendCoins(ctx, mockAddr, testApp.EvmKeeper.GetSeiAddressOrDefault(ctx, common.HexToAddress(wasmd.WasmdAddress)), largeAmts)
	require.Nil(t, err)
	args, err := executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsgWithCoinsAmt, executeMsgWithCoinsAmt, executeMsgWithCoinsAmt})
	require.Nil(t, err)
	res, g, err := p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteBatchID, args...), suppliedGas, big.NewInt(3000_000_000_000_000), nil, false)
	require.Nil(t, err)
	outputs, err := executeMethod.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(outputs))
	parsedOutputs := outputs[0].([][]byte)
	require.Equal(t, fmt.Sprintf("received test msg from %s with 1000usei", mockAddr.String()), string(parsedOutputs[0]))
	require.Equal(t, fmt.Sprintf("received test msg from %s with 1000usei", mockAddr.String()), string(parsedOutputs[1]))
	require.Equal(t, fmt.Sprintf("received test msg from %s with 1000usei", mockAddr.String()), string(parsedOutputs[2]))
	require.Equal(t, uint64(726724), g)
	require.Equal(t, int64(3000), testApp.BankKeeper.GetBalance(statedb.Ctx(), contractAddr, "usei").Amount.Int64())

	amtsbz2, err := sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	executeMsgWithNoCoins := wasmd.ExecuteMsg{
		ContractAddress: contractAddr.String(),
		Msg:             []byte("{\"echo\":{\"message\":\"test msg\"}}"),
		Coins:           amtsbz2,
	}
	statedb = state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm = vm.EVM{
		StateDB: statedb,
	}
	err = testApp.BankKeeper.SendCoins(ctx, mockAddr, testApp.EvmKeeper.GetSeiAddressOrDefault(ctx, common.HexToAddress(wasmd.WasmdAddress)), amts)
	require.Nil(t, err)
	args, err = executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsgWithNoCoins, executeMsgWithCoinsAmt, executeMsgWithNoCoins})
	require.Nil(t, err)
	res, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteBatchID, args...), suppliedGas, big.NewInt(1000_000_000_000_000), nil, false)
	require.Nil(t, err)
	outputs, err = executeMethod.Outputs.Unpack(res)
	require.Nil(t, err)
	require.Equal(t, 1, len(outputs))
	parsedOutputs = outputs[0].([][]byte)
	require.Equal(t, fmt.Sprintf("received test msg from %s with", mockAddr.String()), string(parsedOutputs[0]))
	require.Equal(t, fmt.Sprintf("received test msg from %s with 1000usei", mockAddr.String()), string(parsedOutputs[1]))
	require.Equal(t, fmt.Sprintf("received test msg from %s with", mockAddr.String()), string(parsedOutputs[2]))
	require.Equal(t, uint64(775245), g)
	require.Equal(t, int64(1000), testApp.BankKeeper.GetBalance(statedb.Ctx(), contractAddr, "usei").Amount.Int64())

	// allowed delegatecall
	args, err = executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsgWithNoCoins, executeMsgWithNoCoins})
	require.Nil(t, err)
	contractAddrAllowed := common.BytesToAddress([]byte("contractA"))
	testApp.EvmKeeper.SetERC20CW20Pointer(ctx, contractAddr.String(), contractAddrAllowed)
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, contractAddrAllowed, append(p.ExecuteBatchID, args...), suppliedGas, nil, nil, false)
	require.Nil(t, err)

	// disallowed delegatecall
	contractAddrDisallowed := common.BytesToAddress([]byte("contractB"))
	_, _, err = p.RunAndCalculateGas(&evm, mockEVMAddr, contractAddrDisallowed, append(p.ExecuteBatchID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)

	// bad contract address
	executeMsgBadContract := wasmd.ExecuteMsg{
		ContractAddress: mockAddr.String(),
		Msg:             []byte("{\"echo\":{\"message\":\"test msg\"}}"),
		Coins:           amtsbz,
	}
	statedb = state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm = vm.EVM{
		StateDB: statedb,
	}
	err = testApp.BankKeeper.SendCoins(ctx, mockAddr, testApp.EvmKeeper.GetSeiAddressOrDefault(ctx, common.HexToAddress(wasmd.WasmdAddress)), largeAmts)
	require.Nil(t, err)
	args, err = executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsgWithCoinsAmt, executeMsgBadContract, executeMsgWithCoinsAmt})
	require.Nil(t, err)
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteBatchID, args...), suppliedGas, big.NewInt(3000_000_000_000_000), nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)

	// bad inputs
	executeMsgBadInputs := wasmd.ExecuteMsg{
		ContractAddress: "not bech32",
		Msg:             []byte("{\"echo\":{\"message\":\"test msg\"}}"),
		Coins:           amtsbz,
	}
	statedb = state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm = vm.EVM{
		StateDB: statedb,
	}
	err = testApp.BankKeeper.SendCoins(ctx, mockAddr, testApp.EvmKeeper.GetSeiAddressOrDefault(ctx, common.HexToAddress(wasmd.WasmdAddress)), largeAmts)
	require.Nil(t, err)
	args, _ = executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsgWithCoinsAmt, executeMsgBadInputs, executeMsgWithCoinsAmt})
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteBatchID, args...), suppliedGas, big.NewInt(3000_000_000_000_000), nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
	executeMsgBadInputCoins := wasmd.ExecuteMsg{
		ContractAddress: contractAddr.String(),
		Msg:             []byte("{\"echo\":{\"message\":\"test msg\"}}"),
		Coins:           []byte("bad coins"),
	}
	statedb = state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm = vm.EVM{
		StateDB: statedb,
	}
	err = testApp.BankKeeper.SendCoins(ctx, mockAddr, testApp.EvmKeeper.GetSeiAddressOrDefault(ctx, common.HexToAddress(wasmd.WasmdAddress)), largeAmts)
	require.Nil(t, err)
	args, _ = executeMethod.Inputs.Pack([]wasmd.ExecuteMsg{executeMsgWithCoinsAmt, executeMsgBadInputCoins, executeMsgWithCoinsAmt})
	_, g, err = p.RunAndCalculateGas(&evm, mockEVMAddr, mockEVMAddr, append(p.ExecuteBatchID, args...), suppliedGas, big.NewInt(3000_000_000_000_000), nil, false)
	require.NotNil(t, err)
	require.Equal(t, uint64(0), g)
}
