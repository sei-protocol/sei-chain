package ibc_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/app"
	pcommonv66 "github.com/sei-protocol/sei-chain/precompiles/common/legacy/v66"
	"github.com/sei-protocol/sei-chain/precompiles/ibc"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestRetiredPrecompileRejectsCalls(t *testing.T) {
	testApp := app.Setup(t, false, true, false)
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	sender, senderEVM := testkeeper.MockAddressPair()
	testApp.EvmKeeper.SetAddressMapping(ctx, sender, senderEVM)
	evm := vm.EVM{
		StateDB:   state.NewDBImpl(ctx, &testApp.EvmKeeper, true),
		TxContext: vm.TxContext{Origin: senderEVM},
	}

	precompile, err := ibc.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)

	tests := map[string][]interface{}{
		"transfer": {
			"receiver", "transfer", "channel-0", "usei", big.NewInt(1),
			uint64(1), uint64(1), uint64(1), "",
		},
		"transferWithDefaultTimeout": {
			"receiver", "transfer", "channel-0", "usei", big.NewInt(1), "",
		},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			method := precompile.ABI.Methods[name]
			inputs, packErr := method.Inputs.Pack(args...)
			require.NoError(t, packErr)

			ret, _, runErr := precompile.RunAndCalculateGas(
				&evm,
				common.Address{},
				common.Address{},
				append(method.ID, inputs...),
				1_000_000,
				nil,
				nil,
				false,
				false,
			)
			require.ErrorIs(t, runErr, vm.ErrExecutionReverted)
			reason, unpackErr := abi.UnpackRevert(ret)
			require.NoError(t, unpackErr)
			require.Equal(t, ibc.ErrIBCPrecompileRetired.Error(), reason)
		})
	}
}

func TestVersionedPrecompilesRetainHistoricalImplementations(t *testing.T) {
	versioned := ibc.GetVersioned("v6.7", &utils.EmptyKeepers{})

	require.Contains(t, versioned, "v6.7")
	require.IsType(t, &pcommonv66.DynamicGasPrecompile{}, versioned["v6.6"])
}
