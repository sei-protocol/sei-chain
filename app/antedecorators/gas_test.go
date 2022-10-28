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

type TestTx struct {
	msgs []sdk.Msg
}

func (t TestTx) GetMsgs() []sdk.Msg {
	return t.msgs
}

func (t TestTx) ValidateBasic() error {
	return nil
}

func TestMultiplierGasSetter(t *testing.T) {
	app := app.Setup(false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	ctx := app.NewContext(false, types.Header{}).WithBlockHeight(2)
	testMsg := wasmtypes.MsgExecuteContract{
		Contract: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
		Msg:      []byte("{}"),
	}
	testTx := TestTx{msgs: []sdk.Msg{&testMsg}}
	// discounted mapping
	app.AccessControlKeeper.SetWasmDependencyMapping(ctx, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperationWithSelector{
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
	gasMeterSetter := antedecorators.GetGasMeterSetter(app.AccessControlKeeper)
	ctxWithGasMeter := gasMeterSetter(false, ctx, 1000, testTx)
	ctxWithGasMeter.GasMeter().ConsumeGas(2, "")
	require.Equal(t, uint64(1), ctxWithGasMeter.GasMeter().GasConsumed())
	// not discounted mapping
	app.AccessControlKeeper.SetWasmDependencyMapping(ctx, contractAddr, accesscontrol.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []accesscontrol.AccessOperationWithSelector{
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
