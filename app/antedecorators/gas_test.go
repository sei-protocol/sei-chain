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
	tmtypes "github.com/tendermint/tendermint/types"
)

func TestMultiplierGasSetter(t *testing.T) {
	testApp := app.Setup(false, false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	ctx := testApp.NewContext(false, types.Header{}).WithBlockHeight(2)
	blockParams := tmtypes.DefaultBlockParams()
	ctx = ctx.WithConsensusParams(&types.ConsensusParams{
		Block: &types.BlockParams{MaxGas: blockParams.MaxGas, MaxBytes: blockParams.MaxBytes, CosmosGasMultiplierNumerator: blockParams.CosmosGasMultiplierNumerator, CosmosGasMultiplierDenominator: 2},
	})
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

	gasMeterSetter := antedecorators.GetGasMeterSetter()
	ctxWithGasMeter := gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(2, "")
	require.Equal(t, uint64(1), ctxWithGasMeter.GasMeter().GasConsumed())

	ctx = ctx.WithConsensusParams(&types.ConsensusParams{
		Block: &types.BlockParams{MaxGas: blockParams.MaxGas, MaxBytes: blockParams.MaxBytes, CosmosGasMultiplierNumerator: blockParams.CosmosGasMultiplierNumerator, CosmosGasMultiplierDenominator: 4},
	})

	ctxWithGasMeter = gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(100, "")
	require.Equal(t, uint64(25), ctxWithGasMeter.GasMeter().GasConsumed())
}
