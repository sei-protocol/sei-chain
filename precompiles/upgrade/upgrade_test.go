package upgrade_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/upgrade"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestPrecompile_Run_CurrentPlan(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}

	p, err := upgrade.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	method, err := p.ABI.MethodById(p.GetExecutor().(*upgrade.PrecompileExecutor).CurrentPlanID)
	require.NoError(t, err)

	// no plan scheduled returns a zero-valued plan
	expectedEmptyBz, err := method.Outputs.Pack(upgrade.Plan{})
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, method.ID, 100000, nil, nil, true, false)
	require.NoError(t, err)
	require.Equal(t, expectedEmptyBz, ret)

	// schedule a plan and expect it to round-trip
	plan := upgradetypes.Plan{
		Name:   "test-upgrade",
		Height: ctx.BlockHeight() + 100,
		Info:   "test-info",
	}
	require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(ctx, plan))

	expectedBz, err := method.Outputs.Pack(upgrade.Plan{
		Name:   plan.Name,
		Height: plan.Height,
		Info:   plan.Info,
	})
	require.NoError(t, err)

	ret, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, method.ID, 100000, nil, nil, true, false)
	require.NoError(t, err)
	require.Equal(t, expectedBz, ret)

	// sending value to a view method reverts
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, method.ID, 100000, big.NewInt(1), nil, false, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
}

func TestPrecompile_Run_AppliedPlan(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// seed an applied upgrade through the keeper
	testApp.UpgradeKeeper.SetDone(ctx, "applied-upgrade")

	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}

	p, err := upgrade.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	method, err := p.ABI.MethodById(p.GetExecutor().(*upgrade.PrecompileExecutor).AppliedPlanID)
	require.NoError(t, err)

	inputs, err := method.Inputs.Pack("applied-upgrade")
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 100000, nil, nil, true, false)
	require.NoError(t, err)
	outputs, err := method.Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	require.Equal(t, ctx.BlockHeight(), outputs[0].(int64))

	// unknown upgrade name returns 0 without error
	unknownInputs, err := method.Inputs.Pack("never-applied")
	require.NoError(t, err)
	ret, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, unknownInputs...), 100000, nil, nil, true, false)
	require.NoError(t, err)
	outputs, err = method.Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	require.Equal(t, int64(0), outputs[0].(int64))

	// sending value to a view method reverts
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 100000, big.NewInt(1), nil, false, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
}

func TestPrecompile_Run_UpgradedConsensusState(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}

	p, err := upgrade.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	method, err := p.ABI.MethodById(p.GetExecutor().(*upgrade.PrecompileExecutor).UpgradedConsensusStateID)
	require.NoError(t, err)

	// nothing stored returns empty bytes without error
	inputs, err := method.Inputs.Pack(int64(123))
	require.NoError(t, err)
	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 100000, nil, nil, true, false)
	require.NoError(t, err)
	outputs, err := method.Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	require.Empty(t, outputs[0].([]byte))

	// seed a consensus state through the keeper and expect it to round-trip
	consState := []byte("test-consensus-state")
	require.NoError(t, testApp.UpgradeKeeper.SetUpgradedConsensusState(ctx, 123, consState))

	ret, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 100000, nil, nil, true, false)
	require.NoError(t, err)
	outputs, err = method.Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	require.Equal(t, consState, outputs[0].([]byte))

	// sending value to a view method reverts
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 100000, big.NewInt(1), nil, false, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
}

func TestPrecompile_Run_ModuleVersions(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}

	p, err := upgrade.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	method, err := p.ABI.MethodById(p.GetExecutor().(*upgrade.PrecompileExecutor).ModuleVersionsID)
	require.NoError(t, err)

	// empty module name returns all modules
	inputs, err := method.Inputs.Pack("")
	require.NoError(t, err)
	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 200000, nil, nil, true, false)
	require.NoError(t, err)
	outputs, err := method.Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	allVersions := outputs[0].([]struct {
		Name    string `json:"name"`
		Version uint64 `json:"version"`
	})
	require.NotEmpty(t, allVersions)

	// a specific module name returns exactly one entry
	bankInputs, err := method.Inputs.Pack("bank")
	require.NoError(t, err)
	ret, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, bankInputs...), 200000, nil, nil, true, false)
	require.NoError(t, err)
	outputs, err = method.Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	bankVersions := outputs[0].([]struct {
		Name    string `json:"name"`
		Version uint64 `json:"version"`
	})
	require.Len(t, bankVersions, 1)
	require.Equal(t, "bank", bankVersions[0].Name)
	require.Greater(t, bankVersions[0].Version, uint64(0))

	// unknown module name reverts
	unknownInputs, err := method.Inputs.Pack("notamodule")
	require.NoError(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, unknownInputs...), 200000, nil, nil, true, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)

	// sending value to a view method reverts
	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, bankInputs...), 200000, big.NewInt(1), nil, false, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
}
