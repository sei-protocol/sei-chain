package feegrant_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/precompiles/feegrant"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestFeegrantPrecompileRetired(t *testing.T) {
	testApp := app.Setup(t, false, true, false)
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	evm := vm.EVM{StateDB: state.NewDBImpl(ctx, &testApp.EvmKeeper, true)}

	precompile, err := feegrant.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	require.Equal(t, common.HexToAddress(feegrant.FeegrantAddress), precompile.Address())

	tests := []struct {
		name string
		args []interface{}
	}{
		{name: feegrant.AllowanceMethod, args: []interface{}{common.Address{}, common.Address{}}},
		{name: feegrant.AllowancesMethod, args: []interface{}{common.Address{}, []byte{}}},
		{name: feegrant.AllowancesByGranterMethod, args: []interface{}{common.Address{}, []byte{}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			method := precompile.ABI.Methods[tc.name]
			args, err := method.Inputs.Pack(tc.args...)
			require.NoError(t, err)

			ret, _, err := precompile.RunAndCalculateGas(
				&evm,
				common.Address{},
				common.Address{},
				append(method.ID, args...),
				100_000,
				nil,
				nil,
				true,
				false,
			)
			require.ErrorIs(t, err, vm.ErrExecutionReverted)

			reason, err := abi.UnpackRevert(ret)
			require.NoError(t, err)
			require.Equal(t, feegrant.ErrFeegrantPrecompileRetired.Error(), reason)
		})
	}
}
