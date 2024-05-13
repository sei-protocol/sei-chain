package pointer_test

import (
	"testing"
	"time"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestAddNative(t *testing.T) {
	testApp := app.Setup(false, false, false)
	p, err := pointer.NewPrecompile(&testApp.EvmKeeper, testApp.BankKeeper, testApp.WasmKeeper)
	require.Nil(t, err)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, caller := testkeeper.MockAddressPair()
	suppliedGas := uint64(10000000)
	cfg := types.DefaultChainConfig().EthereumConfig(testApp.EvmKeeper.ChainID(ctx))

	// token has no metadata
	m, err := p.ABI.MethodById(p.AddNativePointerID)
	require.Nil(t, err)
	args, err := m.Inputs.Pack("test")
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	blockCtx, _ := testApp.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(suppliedGas))
	evm := vm.NewEVM(*blockCtx, vm.TxContext{}, statedb, cfg, vm.Config{})
	_, g, err := p.RunAndCalculateGas(evm, caller, caller, append(p.AddNativePointerID, args...), suppliedGas, nil, nil, false)
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
	statedb = state.NewDBImpl(ctx, &testApp.EvmKeeper, true)
	evm = vm.NewEVM(*blockCtx, vm.TxContext{}, statedb, cfg, vm.Config{})
	ret, g, err := p.RunAndCalculateGas(evm, caller, caller, append(p.AddNativePointerID, args...), suppliedGas, nil, nil, false)
	require.Nil(t, err)
	require.Equal(t, uint64(8907806), g)
	outputs, err := m.Outputs.Unpack(ret)
	require.Nil(t, err)
	addr := outputs[0].(common.Address)
	pointerAddr, version, exists := testApp.EvmKeeper.GetERC20NativePointer(statedb.Ctx(), "test")
	require.Equal(t, addr, pointerAddr)
	require.Equal(t, native.CurrentVersion, version)
	require.True(t, exists)
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

	// pointer already exists
	statedb = state.NewDBImpl(statedb.Ctx(), &testApp.EvmKeeper, true)
	evm = vm.NewEVM(*blockCtx, vm.TxContext{}, statedb, cfg, vm.Config{})
	_, g, err = p.RunAndCalculateGas(evm, caller, caller, append(p.AddNativePointerID, args...), suppliedGas, nil, nil, false)
	require.NotNil(t, err)
	require.NotNil(t, statedb.GetPrecompileError())
	require.Equal(t, uint64(0), g)
}
