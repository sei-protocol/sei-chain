package common_test

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestValidateArgsLength(t *testing.T) {
	err := common.ValidateArgsLength(nil, 0)
	require.Nil(t, err)
	err = common.ValidateArgsLength([]interface{}{1, ""}, 2)
	require.Nil(t, err)
	err = common.ValidateArgsLength([]interface{}{""}, 2)
	require.NotNil(t, err)
}

func TestValidteNonPayable(t *testing.T) {
	err := common.ValidateNonPayable(nil)
	require.Nil(t, err)
	err = common.ValidateNonPayable(big.NewInt(0))
	require.Nil(t, err)
	err = common.ValidateNonPayable(big.NewInt(1))
	require.NotNil(t, err)
}

func TestHandlePrecompileError(t *testing.T) {
	_, evmAddr := testkeeper.MockAddressPair()
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	stateDB := state.NewDBImpl(ctx, k, false)
	evm := &vm.EVM{StateDB: stateDB}

	// assert no panic under various conditions
	common.HandlePrecompileError(nil, evm, "no_error")
	common.HandlePrecompileError(types.NewAssociationMissingErr(evmAddr.Hex()), evm, "association")
	common.HandlePrecompileError(errors.New("other error"), evm, "other")
}

type MockPrecompileExecutor struct {
	throw bool
}

func (e *MockPrecompileExecutor) RequiredGas([]byte, *abi.Method) uint64 {
	return 0
}

func (e *MockPrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller ethcommon.Address, callingContract ethcommon.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, _ *tracing.Hooks) ([]byte, error) {
	ctx.EventManager().EmitEvent(sdk.NewEvent("test"))
	if e.throw {
		return nil, errors.New("test")
	}
	return []byte("success"), nil
}

func TestPrecompileRun(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	abiBz, err := os.ReadFile("erc20_abi.json")
	require.Nil(t, err)
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	require.Nil(t, err)
	input, err := newAbi.Pack("decimals")
	require.Nil(t, err)
	precompile := common.NewPrecompile(newAbi, &MockPrecompileExecutor{throw: false}, ethcommon.Address{}, "test")
	stateDB := state.NewDBImpl(ctx, k, false)
	res, err := precompile.Run(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, big.NewInt(0), false, false, nil)
	require.Equal(t, []byte("success"), res)
	require.Nil(t, err)
	require.NotEmpty(t, stateDB.Ctx().EventManager().Events())
	stateDB.WithCtx(ctx.WithEventManager(sdk.NewEventManager()))
	precompile = common.NewPrecompile(newAbi, &MockPrecompileExecutor{throw: true}, ethcommon.Address{}, "test")
	res, err = precompile.Run(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, big.NewInt(0), false, false, nil)
	require.Nil(t, res)
	require.NotNil(t, err)
	// should not emit any event
	require.Empty(t, stateDB.Ctx().EventManager().Events())
}

type MockDynamicGasPrecompileExecutor struct {
	throw     bool
	panicWith interface{}
	writeSlot *ethcommon.Hash
	evmKeeper utils.EVMKeeper
}

func (e *MockDynamicGasPrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller ethcommon.Address, callingContract ethcommon.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, _ *tracing.Hooks) (ret []byte, remainingGas uint64, err error) {
	ctx.EventManager().EmitEvent(sdk.NewEvent("test"))
	if e.writeSlot != nil {
		evm.StateDB.SetState(callingContract, *e.writeSlot, ethcommon.HexToHash("0x1"))
	}
	if e.panicWith != nil {
		panic(e.panicWith)
	}
	if e.throw {
		return nil, 0, errors.New("test")
	}
	return []byte("success"), 0, nil
}

func (e *MockDynamicGasPrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return e.evmKeeper
}

func TestDynamicGasPrecompileRun(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	abiBz, err := os.ReadFile("erc20_abi.json")
	require.Nil(t, err)
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	require.Nil(t, err)
	input, err := newAbi.Pack("decimals")
	require.Nil(t, err)
	precompile := common.NewDynamicGasPrecompile(newAbi, &MockDynamicGasPrecompileExecutor{throw: false, evmKeeper: k}, ethcommon.Address{}, "test")
	stateDB := state.NewDBImpl(ctx, k, false)
	res, _, err := precompile.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, 100000, big.NewInt(0), nil, false, false)
	require.Equal(t, []byte("success"), res)
	require.Nil(t, err)
	require.NotEmpty(t, stateDB.Ctx().EventManager().Events())
	stateDB.WithCtx(ctx.WithEventManager(sdk.NewEventManager()))
	precompile = common.NewDynamicGasPrecompile(newAbi, &MockDynamicGasPrecompileExecutor{throw: true, evmKeeper: k}, ethcommon.Address{}, "test")
	res, _, err = precompile.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, 100000, big.NewInt(0), nil, false, false)
	require.Nil(t, res)
	require.NotNil(t, err)
	// should not emit any event
	require.Empty(t, stateDB.Ctx().EventManager().Events())
}

// TestDynamicGasPrecompileRepanicsNonGas verifies that only gas-meter panics are
// converted to reverts: a non-gas panic must propagate rather than be masked.
// TestDynamicGasPrecompileOutOfGasInCallFrame drives the precompile through a real
// vm.EVM.Call so the frame-level consequences are pinned: the frame's gas is
// consumed, its state is reverted, and the caller can continue with a further call.
func TestDynamicGasPrecompileOutOfGasInCallFrame(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	k := &testApp.EvmKeeper
	ctx := testApp.GetContextForDeliverTx(nil).WithEventManager(sdk.NewEventManager())
	abiBz, err := os.ReadFile("erc20_abi.json")
	require.Nil(t, err)
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	require.Nil(t, err)
	input, err := newAbi.Pack("decimals")
	require.Nil(t, err)

	precompileAddr := ethcommon.HexToAddress("0x0000000000000000000000000000000000009999")
	_, caller := testkeeper.MockAddressPair()
	slot := ethcommon.HexToHash("0xabc")
	oog := common.NewDynamicGasPrecompile(newAbi, &MockDynamicGasPrecompileExecutor{panicWith: sdk.ErrorOutOfGas{Descriptor: "executor"}, writeSlot: &slot, evmKeeper: k}, precompileAddr, "test")

	stateDB := state.NewDBImpl(ctx, k, false)
	cfg := types.DefaultChainConfig().EthereumConfig(k.ChainID(ctx))
	blockCtx, err := k.GetVMBlockContext(ctx, core.GasPool(1000000))
	require.Nil(t, err)
	evm := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, map[ethcommon.Address]vm.PrecompiledContract{precompileAddr: oog})

	ret, leftover, err := evm.Call(caller, precompileAddr, input, 100000, uint256.NewInt(0))
	require.Nil(t, ret)
	require.Equal(t, uint64(0), leftover)
	require.Equal(t, vm.ErrOutOfGas, err)
	require.Equal(t, ethcommon.Hash{}, stateDB.GetState(caller, slot))
	require.Empty(t, stateDB.Ctx().EventManager().Events())
	require.Nil(t, stateDB.Err())

	// The enclosing frame is unaffected and can keep executing.
	ret, leftover, err = evm.Call(caller, ethcommon.HexToAddress("0x0000000000000000000000000000000000009998"), nil, 50000, uint256.NewInt(0))
	require.Nil(t, err)
	require.Nil(t, ret)
	require.Equal(t, uint64(50000), leftover)
}

func TestDynamicGasPrecompileRepanicsNonGas(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	abiBz, err := os.ReadFile("erc20_abi.json")
	require.Nil(t, err)
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	require.Nil(t, err)
	input, err := newAbi.Pack("decimals")
	require.Nil(t, err)

	precompile := common.NewDynamicGasPrecompile(newAbi, &MockDynamicGasPrecompileExecutor{panicWith: "boom", evmKeeper: k}, ethcommon.Address{}, "test")
	stateDB := state.NewDBImpl(ctx.WithEventManager(sdk.NewEventManager()), k, false)
	// Ample gas so the decode charges pass and the (panicking) executor runs.
	require.PanicsWithValue(t, "boom", func() {
		_, _, _ = precompile.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, 100000, big.NewInt(0), nil, false, false)
	})
}

// TestDynamicGasPrecompileGasGate is a regression test for a DoS in the dynamic
// precompile framework: the ABI decode of (attacker-controlled) calldata used to
// run before the supplied EVM gas was turned into a gas meter, so a call that
// forwarded ~zero gas could still force every validator to parse and allocate
// the calldata for free. The decode must now be gated by the supplied gas.
func TestDynamicGasPrecompileGasGate(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	abiBz, err := os.ReadFile("erc20_abi.json")
	require.Nil(t, err)
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	require.Nil(t, err)
	input, err := newAbi.Pack("decimals")
	require.Nil(t, err)

	precompile := common.NewDynamicGasPrecompile(newAbi, &MockDynamicGasPrecompileExecutor{throw: false, evmKeeper: k}, ethcommon.Address{}, "test")

	// Zero supplied gas (the PoC scenario): the call must be rejected before the
	// executor runs, so no event is emitted (Execute is what emits it) and no ABI
	// decode is performed.
	stateDB := state.NewDBImpl(ctx.WithEventManager(sdk.NewEventManager()), k, false)
	res, remainingGas, err := precompile.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, 0, big.NewInt(0), nil, false, false)
	require.Nil(t, res)
	require.Equal(t, uint64(0), remainingGas)
	require.Equal(t, vm.ErrExecutionReverted, err)
	require.Empty(t, stateDB.Ctx().EventManager().Events())

	// With enough gas the same call proceeds through decode and execution.
	stateDB = state.NewDBImpl(ctx.WithEventManager(sdk.NewEventManager()), k, false)
	res, _, err = precompile.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, 100000, big.NewInt(0), nil, false, false)
	require.Equal(t, []byte("success"), res)
	require.Nil(t, err)
	require.NotEmpty(t, stateDB.Ctx().EventManager().Events())
}

// TestDynamicGasPrecompileExecutorOutOfGas verifies that an executor exhausting
// its gas mid-execution (after the decode charges) fails the call frame with
// vm.ErrOutOfGas and zero remaining gas, and emits none of the executor's
// events, instead of letting the sdk gas-meter panic escape the EVM and fail
// the whole tx at the Cosmos layer with a zero-gas receipt.
func TestDynamicGasPrecompileExecutorOutOfGas(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	abiBz, err := os.ReadFile("erc20_abi.json")
	require.Nil(t, err)
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	require.Nil(t, err)
	input, err := newAbi.Pack("decimals")
	require.Nil(t, err)

	for name, gasPanic := range map[string]interface{}{
		"out of gas":   sdk.ErrorOutOfGas{Descriptor: "executor"},
		"gas overflow": sdk.ErrorGasOverflow{Descriptor: "executor"},
	} {
		t.Run(name, func(t *testing.T) {
			precompile := common.NewDynamicGasPrecompile(newAbi, &MockDynamicGasPrecompileExecutor{panicWith: gasPanic, evmKeeper: k}, ethcommon.Address{}, "test")
			stateDB := state.NewDBImpl(ctx.WithEventManager(sdk.NewEventManager()), k, false)
			// Ample gas so the decode charges pass and the executor (which OOGs) runs.
			res, remainingGas, err := precompile.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, 100000, big.NewInt(0), nil, false, false)
			require.Nil(t, res)
			require.Equal(t, uint64(0), remainingGas)
			require.Equal(t, vm.ErrOutOfGas, err)
			require.Nil(t, stateDB.GetPrecompileError())
			require.Empty(t, stateDB.Ctx().EventManager().Events())
		})
	}
}
