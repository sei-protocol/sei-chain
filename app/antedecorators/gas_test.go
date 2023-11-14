package antedecorators_test

import (
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestMultiplierGasSetter(t *testing.T) {
	testApp := app.Setup(false, false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	otherContractAddr, err := sdk.AccAddressFromBech32("sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m")
	require.NoError(t, err)
	ctx := testApp.NewContext(false, types.Header{}).WithBlockHeight(2)
	testMsg := wasmtypes.MsgExecuteContract{
		Contract: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
		Msg:      []byte("{\"xyz\":{}}"),
	}
	testTx := app.NewTestTx([]sdk.Msg{&testMsg})
	// discounted mapping
	testApp.AccessControlKeeper.SetWasmDependencyMapping(ctx, accesscontrol.WasmDependencyMapping{
		ContractAddress: contractAddr.String(),
		BaseAccessOps: []*accesscontrol.WasmAccessOperation{
			{
				Operation: &accesscontrol.AccessOperation{
					AccessType:         accesscontrol.AccessType_READ,
					ResourceType:       accesscontrol.ResourceType_KV,
					IdentifierTemplate: "something",
				},
			},
			{
				Operation: acltypes.CommitAccessOp(),
			},
		},
	})
	// other contract not discounted
	testApp.AccessControlKeeper.SetWasmDependencyMapping(ctx, accesscontrol.WasmDependencyMapping{
		ContractAddress: otherContractAddr.String(),
		BaseAccessOps: []*accesscontrol.WasmAccessOperation{
			{
				Operation: &accesscontrol.AccessOperation{
					AccessType:         accesscontrol.AccessType_READ,
					ResourceType:       accesscontrol.ResourceType_KV,
					IdentifierTemplate: "*",
				},
			},
			{
				Operation: acltypes.CommitAccessOp(),
			},
		},
	})

	gasMeterSetter := antedecorators.GetGasMeterSetter(testApp.AccessControlKeeper)
	ctxWithGasMeter := gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(2, "")
	require.Equal(t, uint64(1), ctxWithGasMeter.GasMeter().GasConsumed())

	otherTestMsg := wasmtypes.MsgExecuteContract{
		Contract: "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m",
		Msg:      []byte("{\"xyz\":{}}"),
	}
	testTx2 := app.NewTestTx([]sdk.Msg{&testMsg, &otherTestMsg})
	ctxWithGasMeter = gasMeterSetter(false, ctx, 1000, testTx2)
	ctxWithGasMeter.GasMeter().ConsumeGas(2, "")
	// should still not give discount because of other contract being non-discounted
	require.Equal(t, uint64(2), ctxWithGasMeter.GasMeter().GasConsumed())

	// not discounted mapping
	testApp.AccessControlKeeper.SetWasmDependencyMapping(ctx, accesscontrol.WasmDependencyMapping{
		ContractAddress: contractAddr.String(),
		BaseAccessOps: []*accesscontrol.WasmAccessOperation{
			{
				Operation: &accesscontrol.AccessOperation{
					AccessType:         accesscontrol.AccessType_READ,
					ResourceType:       accesscontrol.ResourceType_KV,
					IdentifierTemplate: "*",
				},
			},
			{
				Operation: acltypes.CommitAccessOp(),
			},
		},
	})
	ctxWithGasMeter = gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(2, "")
	require.Equal(t, uint64(2), ctxWithGasMeter.GasMeter().GasConsumed())
}

func TestMultiplierGasSetterWithWasmReference(t *testing.T) {
	testApp := app.Setup(false, false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	referredContractAddr, err := sdk.AccAddressFromBech32("sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m")
	require.NoError(t, err)
	ctx := testApp.NewContext(false, types.Header{}).WithBlockHeight(2)
	testMsg := wasmtypes.MsgExecuteContract{
		Contract: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
		Msg:      []byte("{\"xyz\":{}}"),
	}
	testTx := app.NewTestTx([]sdk.Msg{&testMsg})
	// discounted mapping
	testApp.AccessControlKeeper.SetWasmDependencyMapping(ctx, accesscontrol.WasmDependencyMapping{
		ContractAddress: contractAddr.String(),
		BaseAccessOps: []*accesscontrol.WasmAccessOperation{
			{
				Operation: &accesscontrol.AccessOperation{
					AccessType:         accesscontrol.AccessType_READ,
					ResourceType:       accesscontrol.ResourceType_KV,
					IdentifierTemplate: "something",
				},
			},
			{
				Operation: acltypes.CommitAccessOp(),
			},
		},
		BaseContractReferences: []*accesscontrol.WasmContractReference{
			{
				ContractAddress: referredContractAddr.String(),
				MessageType:     accesscontrol.WasmMessageSubtype_EXECUTE,
				MessageName:     "abc",
			},
		},
	})
	testApp.AccessControlKeeper.SetWasmDependencyMapping(ctx, accesscontrol.WasmDependencyMapping{
		ContractAddress: referredContractAddr.String(),
		BaseAccessOps: []*accesscontrol.WasmAccessOperation{
			{
				Operation: acltypes.CommitAccessOp(),
			},
		},
		ExecuteAccessOps: []*accesscontrol.WasmAccessOperations{
			{
				MessageName: "abc",
				WasmOperations: []*accesscontrol.WasmAccessOperation{
					{
						Operation: &accesscontrol.AccessOperation{
							AccessType:         accesscontrol.AccessType_WRITE,
							ResourceType:       accesscontrol.ResourceType_KV,
							IdentifierTemplate: "something else",
						},
					},
				},
			},
		},
	})
	gasMeterSetter := antedecorators.GetGasMeterSetter(testApp.AccessControlKeeper)
	ctxWithGasMeter := gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(2, "")
	require.Equal(t, uint64(1), ctxWithGasMeter.GasMeter().GasConsumed())
	// not discounted mapping
	testApp.AccessControlKeeper.SetWasmDependencyMapping(ctx, accesscontrol.WasmDependencyMapping{
		ContractAddress: referredContractAddr.String(),
		BaseAccessOps: []*accesscontrol.WasmAccessOperation{
			{
				Operation: acltypes.CommitAccessOp(),
			},
		},
		ExecuteAccessOps: []*accesscontrol.WasmAccessOperations{
			{
				MessageName: "abc",
				WasmOperations: []*accesscontrol.WasmAccessOperation{
					{
						Operation: &accesscontrol.AccessOperation{
							AccessType:         accesscontrol.AccessType_WRITE,
							ResourceType:       accesscontrol.ResourceType_KV,
							IdentifierTemplate: "*",
						},
					},
				},
			},
		},
	})
	ctxWithGasMeter = gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(2, "")
	require.Equal(t, uint64(2), ctxWithGasMeter.GasMeter().GasConsumed())
}

func TestMultiplierGasSetterWithWasmReferenceCycle(t *testing.T) {
	testApp := app.Setup(false, false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	referredContractAddr, err := sdk.AccAddressFromBech32("sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m")
	require.NoError(t, err)
	ctx := testApp.NewContext(false, types.Header{}).WithBlockHeight(2)
	testMsg := wasmtypes.MsgExecuteContract{
		Contract: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
		Msg:      []byte("{\"xyz\":{}}"),
	}
	testTx := app.NewTestTx([]sdk.Msg{&testMsg})

	testApp.AccessControlKeeper.SetWasmDependencyMapping(ctx, accesscontrol.WasmDependencyMapping{
		ContractAddress: contractAddr.String(),
		BaseAccessOps: []*accesscontrol.WasmAccessOperation{
			{
				Operation: &accesscontrol.AccessOperation{
					AccessType:         accesscontrol.AccessType_READ,
					ResourceType:       accesscontrol.ResourceType_KV,
					IdentifierTemplate: "something",
				},
			},
			{
				Operation: acltypes.CommitAccessOp(),
			},
		},
		BaseContractReferences: []*accesscontrol.WasmContractReference{
			{
				ContractAddress: referredContractAddr.String(),
				MessageType:     accesscontrol.WasmMessageSubtype_EXECUTE,
				MessageName:     "abc",
			},
		},
	})
	testApp.AccessControlKeeper.SetWasmDependencyMapping(ctx, accesscontrol.WasmDependencyMapping{
		ContractAddress: referredContractAddr.String(),
		BaseAccessOps: []*accesscontrol.WasmAccessOperation{
			{
				Operation: acltypes.CommitAccessOp(),
			},
		},
		ExecuteAccessOps: []*accesscontrol.WasmAccessOperations{
			{
				MessageName: "abc",
				WasmOperations: []*accesscontrol.WasmAccessOperation{
					{
						Operation: &accesscontrol.AccessOperation{
							AccessType:         accesscontrol.AccessType_WRITE,
							ResourceType:       accesscontrol.ResourceType_KV,
							IdentifierTemplate: "something else",
						},
					},
				},
			},
		},
		BaseContractReferences: []*accesscontrol.WasmContractReference{
			{
				ContractAddress: contractAddr.String(),
				MessageType:     accesscontrol.WasmMessageSubtype_EXECUTE,
				MessageName:     "xyz",
			},
		},
	})
	gasMeterSetter := antedecorators.GetGasMeterSetter(testApp.AccessControlKeeper)
	ctxWithGasMeter := gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(2, "")
	require.Equal(t, uint64(2), ctxWithGasMeter.GasMeter().GasConsumed())
}
