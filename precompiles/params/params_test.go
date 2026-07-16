package params_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/params"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestPrecompile_Run_Params(t *testing.T) {
	tests := []struct {
		name     string
		subspace string
		key      string
		wantErr  bool
	}{
		{
			name:     "returns staking MaxValidators param",
			subspace: "staking",
			key:      "MaxValidators",
			wantErr:  false,
		},
		{
			name:     "fails for unknown subspace",
			subspace: "notasubspace",
			key:      "NotAKey",
			wantErr:  true,
		},
		{
			name:     "fails for empty subspace",
			subspace: "",
			key:      "MaxValidators",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testApp := testkeeper.EVMTestApp
			ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
			k := &testApp.EvmKeeper
			statedb := state.NewDBImpl(ctx, k, true)
			evm := vm.EVM{
				StateDB: statedb,
			}

			p, err := params.NewPrecompile(testApp.GetPrecompileKeepers())
			require.NoError(t, err)
			method, err := p.ABI.MethodById(p.GetExecutor().(*params.PrecompileExecutor).ParamsID)
			require.NoError(t, err)

			inputs, err := method.Inputs.Pack(tt.subspace, tt.key)
			require.NoError(t, err)

			ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 100000, nil, nil, true, false)
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, vm.ErrExecutionReverted, err)
				return
			}
			require.NoError(t, err)

			outputs, err := method.Outputs.Unpack(ret)
			require.NoError(t, err)
			require.Len(t, outputs, 1)
			value := outputs[0].(string)
			require.NotEmpty(t, value)
			// MaxValidators is a JSON-encoded uint32 matching the staking keeper's value
			expected := fmt.Sprintf("%d", testApp.StakingKeeper.MaxValidators(ctx))
			require.Equal(t, expected, value)
		})
	}
}

func TestPrecompile_Run_Params_NonPayable(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB: statedb,
	}

	p, err := params.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	method, err := p.ABI.MethodById(p.GetExecutor().(*params.PrecompileExecutor).ParamsID)
	require.NoError(t, err)

	inputs, err := method.Inputs.Pack("staking", "MaxValidators")
	require.NoError(t, err)

	_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 100000, big.NewInt(1), nil, false, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
}
