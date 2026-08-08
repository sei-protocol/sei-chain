package ibc_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/ibc"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

func TestEveryIBCVersionIsRetired(t *testing.T) {
	testApp := app.Setup(t, false, true, false)
	ctx := testApp.NewContext(false, tmtypes.Header{})
	evm := &vm.EVM{StateDB: state.NewDBImpl(ctx, &testApp.EvmKeeper, true)}

	versioned := ibc.GetVersioned("v6.6", testApp.GetPrecompileKeepers())
	require.Len(t, versioned, 15)

	for version, contract := range versioned {
		t.Run(version, func(t *testing.T) {
			precompile, ok := contract.(*pcommon.DynamicGasPrecompile)
			require.True(t, ok)

			input := validCallData(t, precompile.GetABI())
			ret, _, err := precompile.RunAndCalculateGas(
				evm,
				common.Address{},
				common.Address{},
				input,
				1_000_000,
				nil,
				nil,
				false,
				false,
			)
			require.ErrorIs(t, err, vm.ErrExecutionReverted)

			reason, err := abi.UnpackRevert(ret)
			require.NoError(t, err)
			require.Equal(t, ibc.RetiredReason, reason)
		})
	}
}

func TestRetiredIBCPrecompileRemainsNonPayable(t *testing.T) {
	testApp := app.Setup(t, false, true, false)
	ctx := testApp.NewContext(false, tmtypes.Header{})
	evm := &vm.EVM{StateDB: state.NewDBImpl(ctx, &testApp.EvmKeeper, true)}

	precompile, err := ibc.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)

	ret, _, err := precompile.RunAndCalculateGas(
		evm,
		common.Address{},
		common.Address{},
		validCallData(t, precompile.GetABI()),
		1_000_000,
		big.NewInt(1),
		nil,
		false,
		false,
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Empty(t, ret)
}

func validCallData(t *testing.T, contractABI abi.ABI) []byte {
	t.Helper()

	method := contractABI.Methods["transferWithDefaultTimeout"]
	args := make([]interface{}, len(method.Inputs))
	for i, input := range method.Inputs {
		switch input.Type.T {
		case abi.StringTy:
			args[i] = ""
		case abi.UintTy:
			if input.Type.Size == 256 {
				args[i] = new(big.Int)
			} else {
				args[i] = uint64(0)
			}
		default:
			t.Fatalf("unsupported ABI input type %s", input.Type.String())
		}
	}

	encoded, err := method.Inputs.Pack(args...)
	require.NoError(t, err)
	return append(method.ID, encoded...)
}
