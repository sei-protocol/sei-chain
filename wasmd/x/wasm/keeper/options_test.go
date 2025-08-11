package keeper

import (
	"testing"

	authkeeper "github.com/sei-protocol/sei-chain/cosmos-sdk/x/auth/keeper"
	distributionkeeper "github.com/sei-protocol/sei-chain/cosmos-sdk/x/distribution/keeper"
	paramtypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/params/types"
	stakingkeeper "github.com/sei-protocol/sei-chain/cosmos-sdk/x/staking/keeper"
	upgradekeeper "github.com/sei-protocol/sei-chain/cosmos-sdk/x/upgrade/keeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/wasmd/x/wasm/keeper/wasmtesting"
	"github.com/sei-protocol/sei-chain/wasmd/x/wasm/types"
)

func TestConstructorOptions(t *testing.T) {
	specs := map[string]struct {
		srcOpt Option
		verify func(*testing.T, Keeper)
	}{
		"wasm engine": {
			srcOpt: WithWasmEngine(&wasmtesting.MockWasmer{}),
			verify: func(t *testing.T, k Keeper) {
				assert.IsType(t, &wasmtesting.MockWasmer{}, k.wasmVM)
			},
		},
		"message handler": {
			srcOpt: WithMessageHandler(&wasmtesting.MockMessageHandler{}),
			verify: func(t *testing.T, k Keeper) {
				require.IsType(t, callDepthMessageHandler{}, k.messenger)
				messenger, _ := k.messenger.(callDepthMessageHandler)
				assert.IsType(t, &wasmtesting.MockMessageHandler{}, messenger.Messenger)
			},
		},
		"query plugins": {
			srcOpt: WithQueryHandler(&wasmtesting.MockQueryHandler{}),
			verify: func(t *testing.T, k Keeper) {
				assert.IsType(t, &wasmtesting.MockQueryHandler{}, k.wasmVMQueryHandler)
			},
		},
		"message handler decorator": {
			srcOpt: WithMessageHandlerDecorator(func(old Messenger) Messenger {
				require.IsType(t, &MessageHandlerChain{}, old)
				return &wasmtesting.MockMessageHandler{}
			}),
			verify: func(t *testing.T, k Keeper) {
				require.IsType(t, callDepthMessageHandler{}, k.messenger)
				messenger, _ := k.messenger.(callDepthMessageHandler)
				assert.IsType(t, &wasmtesting.MockMessageHandler{}, messenger.Messenger)
			},
		},
		"query plugins decorator": {
			srcOpt: WithQueryHandlerDecorator(func(old WasmVMQueryHandler) WasmVMQueryHandler {
				require.IsType(t, QueryPlugins{}, old)
				return &wasmtesting.MockQueryHandler{}
			}),
			verify: func(t *testing.T, k Keeper) {
				assert.IsType(t, &wasmtesting.MockQueryHandler{}, k.wasmVMQueryHandler)
			},
		},
		"coin transferrer": {
			srcOpt: WithCoinTransferrer(&wasmtesting.MockCoinTransferrer{}),
			verify: func(t *testing.T, k Keeper) {
				assert.IsType(t, &wasmtesting.MockCoinTransferrer{}, k.bank)
			},
		},
		"costs": {
			srcOpt: WithGasRegister(&wasmtesting.MockGasRegister{}),
			verify: func(t *testing.T, k Keeper) {
				assert.IsType(t, &wasmtesting.MockGasRegister{}, k.gasRegister)
			},
		},
		"api costs": {
			srcOpt: WithAPICosts(1, 2),
			verify: func(t *testing.T, k Keeper) {
				t.Cleanup(setApiDefaults)
				assert.Equal(t, uint64(1), costHumanize)
				assert.Equal(t, uint64(2), costCanonical)
			},
		},
		"max query recursion limit": {
			srcOpt: WithMaxQueryStackSize(1),
			verify: func(t *testing.T, k Keeper) {
				assert.IsType(t, uint32(1), k.maxQueryStackSize)
			},
		},
		"max message recursion limit": {
			srcOpt: WithMaxCallDepth(1),
			verify: func(t *testing.T, k Keeper) {
				assert.IsType(t, uint32(1), k.maxCallDepth)
			},
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			k := NewKeeper(nil, nil, nil, paramtypes.NewSubspace(nil, nil, nil, nil, ""), authkeeper.AccountKeeper{}, nil, stakingkeeper.Keeper{}, distributionkeeper.Keeper{}, nil, nil, nil, upgradekeeper.Keeper{}, nil, nil, nil, "tempDir", types.DefaultWasmConfig(), SupportedFeatures, spec.srcOpt)
			spec.verify(t, k)
		})
	}
}

func setApiDefaults() {
	costHumanize = DefaultGasCostHumanAddress * DefaultGasMultiplier
	costCanonical = DefaultGasCostCanonicalAddress * DefaultGasMultiplier
}
