package common_test

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/common"
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
	require.NotNil(t, res)
	require.NotNil(t, err)
	// should not emit any event
	require.Empty(t, stateDB.Ctx().EventManager().Events())
}

type MockDynamicGasPrecompileExecutor struct {
	throw     bool
	evmKeeper common.EVMKeeper
}

func (e *MockDynamicGasPrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller ethcommon.Address, callingContract ethcommon.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, _ *tracing.Hooks) (ret []byte, remainingGas uint64, err error) {
	ctx.EventManager().EmitEvent(sdk.NewEvent("test"))
	if e.throw {
		return nil, 0, errors.New("test")
	}
	return []byte("success"), 0, nil
}

func (e *MockDynamicGasPrecompileExecutor) EVMKeeper() common.EVMKeeper {
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
	res, _, err := precompile.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, 0, big.NewInt(0), nil, false, false)
	require.Equal(t, []byte("success"), res)
	require.Nil(t, err)
	require.NotEmpty(t, stateDB.Ctx().EventManager().Events())
	stateDB.WithCtx(ctx.WithEventManager(sdk.NewEventManager()))
	precompile = common.NewDynamicGasPrecompile(newAbi, &MockDynamicGasPrecompileExecutor{throw: true, evmKeeper: k}, ethcommon.Address{}, "test")
	res, _, err = precompile.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, 0, big.NewInt(0), nil, false, false)
	require.NotNil(t, res)
	require.NotNil(t, err)
	// should not emit any event
	require.Empty(t, stateDB.Ctx().EventManager().Events())
}
