package ibc_test

import (
	"math/big"
	"os"
	"strings"
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

type historicalTraceErrorRecorder struct {
	err error
}

func (r *historicalTraceErrorRecorder) RecordHistoricalTraceError(err error) {
	r.err = err
}

func TestEveryIBCVersionIsRetired(t *testing.T) {
	testApp := app.Setup(t, false, true, false)
	ctx := testApp.NewContext(false, tmtypes.Header{})
	evm := &vm.EVM{StateDB: state.NewDBImpl(ctx, &testApp.EvmKeeper, true)}

	manifest, err := os.ReadFile("versions")
	require.NoError(t, err)
	historicalVersions := strings.Fields(string(manifest))

	const futureUpgrade = "future-upgrade"
	versioned := ibc.GetVersioned(futureUpgrade, testApp.GetPrecompileKeepers())
	require.Len(t, versioned, len(historicalVersions)+1)
	for _, version := range historicalVersions {
		require.Contains(t, versioned, version)
	}

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

	ret, remainingGas, err := precompile.RunAndCalculateGas(
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
	require.Zero(t, remainingGas)

	reason, err := abi.UnpackRevert(ret)
	require.NoError(t, err)
	require.Equal(t, ibc.RetiredReason, reason)
}

func TestHistoricalIBCTraceRecordsHardFailure(t *testing.T) {
	testApp := app.Setup(t, false, true, false)
	recorder := &historicalTraceErrorRecorder{}
	historicalCtx := pcommon.WithHistoricalTraceErrorRecorder(
		testApp.NewContext(false, tmtypes.Header{}).
			WithIsTracing(true).
			WithClosestUpgradeName("v6.6"),
		recorder,
	)
	stateDB := state.NewDBImpl(historicalCtx, &testApp.EvmKeeper, true)
	evm := &vm.EVM{StateDB: stateDB}

	precompile, err := ibc.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	_, _, err = precompile.RunAndCalculateGas(
		evm,
		common.Address{},
		common.Address{},
		validCallData(t, precompile.GetABI()),
		1_000_000,
		nil,
		nil,
		false,
		false,
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	var unavailable *pcommon.HistoricalTraceUnavailableError
	require.ErrorAs(t, recorder.err, &unavailable)

	recorder.err = nil
	stateDB.WithCtx(pcommon.WithHistoricalTraceErrorRecorder(
		historicalCtx.WithClosestUpgradeName(ibc.RetirementUpgrade),
		recorder,
	))
	_, _, err = precompile.RunAndCalculateGas(
		evm,
		common.Address{},
		common.Address{},
		validCallData(t, precompile.GetABI()),
		1_000_000,
		nil,
		nil,
		false,
		false,
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.NoError(t, recorder.err)
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
