package pointer_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestAddNative(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	p, err := pointer.NewPrecompile(&testApp.EvmKeeper, testApp.BankKeeper, testApp.WasmKeeper)
	require.Nil(t, err)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	_, caller := testkeeper.MockAddressPair()
	suppliedGas := uint64(10000000)
	cfg := types.DefaultChainConfig().EthereumConfig(testApp.EvmKeeper.ChainID(ctx))

	// token has no metadata
	m, err := p.ABI.MethodById(p.GetExecutor().(*pointer.PrecompileExecutor).AddNativePointerID)
	require.Nil(t, err)
	args, err := m.Inputs.Pack("test")
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	blockCtx, _ := testApp.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(suppliedGas))
	evm := vm.NewEVM(*blockCtx, vm.TxContext{}, statedb, cfg, vm.Config{}, testApp.EvmKeeper.CustomPrecompiles())
	_, g, err := p.RunAndCalculateGas(evm, caller, caller, append(p.GetExecutor().(*pointer.PrecompileExecutor).AddNativePointerID, args...), suppliedGas, nil, nil, false, false)
	require.NotNil(t, err)
	require.NotNil(t, statedb.GetPrecompileError())
	require.Equal(t, uint64(0), g)
	_, _, exists := testApp.EvmKeeper.GetERC20NativePointer(statedb.Ctx(), "test")
	require.False(t, exists)

	// token has metadata
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
	statedb = state.NewDBImpl(ctx, &testApp.EvmKeeper, false)
	evm = vm.NewEVM(*blockCtx, vm.TxContext{}, statedb, cfg, vm.Config{}, testApp.EvmKeeper.CustomPrecompiles())
	ret, g, err := p.RunAndCalculateGas(evm, caller, caller, append(p.GetExecutor().(*pointer.PrecompileExecutor).AddNativePointerID, args...), suppliedGas, nil, nil, false, false)
	require.Nil(t, err)
	require.Equal(t, uint64(8889527), g)
	outputs, err := m.Outputs.Unpack(ret)
	require.Nil(t, err)
	addr := outputs[0].(common.Address)
	pointerAddr, version, exists := testApp.EvmKeeper.GetERC20NativePointer(statedb.Ctx(), "test")
	require.Equal(t, addr, pointerAddr)
	require.Equal(t, native.CurrentVersion, version)
	require.True(t, exists)
	_, err = statedb.Finalize()
	require.Nil(t, err)
	hasRegisteredEvent := false
	for _, e := range ctx.EventManager().Events() {
		if e.Type != types.EventTypePointerRegistered {
			continue
		}
		hasRegisteredEvent = true
		require.Equal(t, types.EventTypePointerRegistered, e.Type)
		require.Equal(t, "native", string(e.Attributes[0].Value))
	}
	require.True(t, hasRegisteredEvent)

	// upgrade to a newer version
	// hacky way to get the existing version number to be below CurrentVersion
	testApp.EvmKeeper.DeleteERC20NativePointer(statedb.Ctx(), "test", version)
	testApp.EvmKeeper.SetERC20NativePointerWithVersion(statedb.Ctx(), "test", pointerAddr, version-1)
	statedb = state.NewDBImpl(statedb.Ctx(), &testApp.EvmKeeper, true)
	evm = vm.NewEVM(*blockCtx, vm.TxContext{}, statedb, cfg, vm.Config{}, testApp.EvmKeeper.CustomPrecompiles())
	_, _, err = p.RunAndCalculateGas(evm, caller, caller, append(p.GetExecutor().(*pointer.PrecompileExecutor).AddNativePointerID, args...), suppliedGas, nil, nil, false, false)
	require.Nil(t, err)
	require.Nil(t, statedb.GetPrecompileError())
	newAddr, _, exists := testApp.EvmKeeper.GetERC20NativePointer(statedb.Ctx(), "test")
	require.True(t, exists)
	require.Equal(t, addr, pointerAddr)
	require.Equal(t, newAddr, pointerAddr) // address should stay the same as before
}
