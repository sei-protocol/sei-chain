package pointer_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// TestRetiredPrecompileRejectsCalls uses a denom carrying the bank metadata the
// creation path required, so a passing run means the revert is the retirement rather
// than the missing-metadata rejection that preceded it.
func TestRetiredPrecompileRejectsCalls(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	precompile, err := pointer.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)

	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	testApp.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
		Base:   "test",
		Name:   "base_name",
		Symbol: "base_symbol",
		DenomUnits: []*banktypes.DenomUnit{{
			Exponent: 6,
			Denom:    "denom",
			Aliases:  []string{"DENOM"},
		}},
	})

	_, caller := testkeeper.MockAddressPair()
	suppliedGas := uint64(10000000)
	cfg := types.DefaultChainConfig().EthereumConfig(testApp.EvmKeeper.ChainID(ctx))
	blockCtx, _ := testApp.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(suppliedGas))

	for _, name := range []string{
		"addNativePointer",
		"addCW20Pointer",
		"addCW721Pointer",
		"addCW1155Pointer",
	} {
		t.Run(name, func(t *testing.T) {
			method := precompile.ABI.Methods[name]
			inputs, packErr := method.Inputs.Pack("test")
			require.NoError(t, packErr)

			statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
			evm := vm.NewEVM(*blockCtx, statedb, cfg, vm.Config{}, testApp.EvmKeeper.CustomPrecompiles(ctx))
			ret, _, runErr := precompile.RunAndCalculateGas(
				evm,
				caller,
				caller,
				append(method.ID, inputs...),
				suppliedGas,
				nil,
				nil,
				false,
				false,
			)

			require.ErrorIs(t, runErr, vm.ErrExecutionReverted)
			require.ErrorIs(t, statedb.GetPrecompileError(), pointer.ErrPointerPrecompileRetired)
			reason, unpackErr := abi.UnpackRevert(ret)
			require.NoError(t, unpackErr)
			require.Equal(t, pointer.ErrPointerPrecompileRetired.Error(), reason)
		})
	}

	_, _, exists := testApp.EvmKeeper.GetERC20NativePointer(ctx, "test")
	require.False(t, exists)
}

func TestRetiredPrecompileRejectsValue(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	precompile, err := pointer.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)

	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	_, caller := testkeeper.MockAddressPair()
	suppliedGas := uint64(10000000)
	cfg := types.DefaultChainConfig().EthereumConfig(testApp.EvmKeeper.ChainID(ctx))
	blockCtx, _ := testApp.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(suppliedGas))

	method := precompile.ABI.Methods["addNativePointer"]
	inputs, err := method.Inputs.Pack("test")
	require.NoError(t, err)

	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm := vm.NewEVM(*blockCtx, statedb, cfg, vm.Config{}, testApp.EvmKeeper.CustomPrecompiles(ctx))
	_, _, err = precompile.RunAndCalculateGas(
		evm,
		caller,
		caller,
		append(method.ID, inputs...),
		suppliedGas,
		big.NewInt(1),
		nil,
		false,
		false,
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
}
