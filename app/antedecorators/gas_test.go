package antedecorators_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	"github.com/stretchr/testify/require"
)

func TestMultiplierGasSetter(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.NewContext(false, types.Header{}).WithBlockHeight(2)
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, *paramtypes.DefaultCosmosGasParams())
	testApp.ParamsKeeper.SetFeesParams(ctx, paramtypes.DefaultGenesis().GetFeesParams())
	testMsg := wasmtypes.MsgExecuteContract{
		Contract: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
		Msg:      []byte("{\"xyz\":{}}"),
	}
	testTx := app.NewTestTx([]sdk.Msg{&testMsg})

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
