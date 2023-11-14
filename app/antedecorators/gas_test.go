package antedecorators_test

import (
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestMultiplierGasSetter(t *testing.T) {
	testApp := app.Setup(false, false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	ctx := testApp.NewContext(false, types.Header{}).WithBlockHeight(2)
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, *paramtypes.DefaultCosmosGasParams())
	testApp.ParamsKeeper.SetFeesParams(ctx, paramtypes.DefaultGenesis().GetFeesParams())
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
	})

	// Test with 1/2 cosmos gas multiplier
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, paramtypes.CosmosGasParams{CosmosGasMultiplierNumerator: 1, CosmosGasMultiplierDenominator: 2})
	gasMeterSetter := antedecorators.GetGasMeterSetter(testApp.ParamsKeeper)
	ctxWithGasMeter := gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(2, "")
	require.Equal(t, uint64(1), ctxWithGasMeter.GasMeter().GasConsumed())

	// Test with 1/4 cosmos gas multiplier
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, paramtypes.CosmosGasParams{CosmosGasMultiplierNumerator: 1, CosmosGasMultiplierDenominator: 4})
	ctxWithGasMeter = gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(100, "")
	require.Equal(t, uint64(25), ctxWithGasMeter.GasMeter().GasConsumed())

	// Test over gas limit even with 1/4 gas multiplier
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, paramtypes.CosmosGasParams{CosmosGasMultiplierNumerator: 1, CosmosGasMultiplierDenominator: 4})
	ctxWithGasMeter = gasMeterSetter(false, ctx, 20, testTx)
	require.Panics(t, func() { ctxWithGasMeter.GasMeter().ConsumeGas(100, "") })
	require.Equal(t, true, ctxWithGasMeter.GasMeter().IsOutOfGas())

	// Simulation mode has infinite gas meter with multiplier
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, paramtypes.CosmosGasParams{CosmosGasMultiplierNumerator: 1, CosmosGasMultiplierDenominator: 4})
	// Gas limit is effectively ignored in simulation
	ctxWithGasMeter = gasMeterSetter(true, ctx, 20, testTx)
	require.NotPanics(t, func() { ctxWithGasMeter.GasMeter().ConsumeGas(100, "") })
	require.Equal(t, uint64(25), ctxWithGasMeter.GasMeter().GasConsumed())
	require.Equal(t, false, ctxWithGasMeter.GasMeter().IsOutOfGas())

}
