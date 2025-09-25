package antedecorators_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	oracle "github.com/sei-protocol/sei-chain/x/oracle"
	"github.com/stretchr/testify/require"
)

// PriorityCaptureDecorator captures ctx.Priority seen by the next decorator in the chain
type PriorityCaptureDecorator struct{ captured *int64 }

func (d PriorityCaptureDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if d.captured != nil {
		*d.captured = ctx.Priority()
	}
	return next(ctx, tx, simulate)
}

func (d PriorityCaptureDecorator) AnteDeps(txDeps []accesscontrol.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) ([]accesscontrol.AccessOperation, error) {
	return next(txDeps, tx, txIndex)
}

// Minimal FeeTx to drive ante chain
type fakeTx struct {
	sdk.FeeTx
	msgs []sdk.Msg
}

func (t fakeTx) GetMsgs() []sdk.Msg         { return t.msgs }
func (t fakeTx) ValidateBasic() error       { return nil }
func (t fakeTx) GetGas() uint64             { return 0 }
func (t fakeTx) GetFee() sdk.Coins          { return sdk.NewCoins() }
func (t fakeTx) FeePayer() sdk.AccAddress   { return nil }
func (t fakeTx) FeeGranter() sdk.AccAddress { return nil }

func TestPriorityWithExactAnteChain_BankSend(t *testing.T) {
	testApp := app.Setup(false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{}).WithBlockHeight(2).WithIsCheckTx(true)
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, *paramtypes.DefaultCosmosGasParams())
	testApp.ParamsKeeper.SetFeesParams(ctx, paramtypes.DefaultGenesis().GetFeesParams())

	var seenAfterLimit int64 = -1
	var seenAfterReject int64 = -1
	var seenAfterSpamming int64 = -1
	var seenAfterPriority int64 = -1

	decorators := []sdk.AnteFullDecorator{
		sdk.DefaultWrappedAnteDecorator(authante.NewSetUpContextDecorator(antedecorators.GetGasMeterSetter(testApp.ParamsKeeper))),
		antedecorators.NewGaslessDecorator([]sdk.AnteFullDecorator{authante.NewDeductFeeDecorator(testApp.AccountKeeper, testApp.BankKeeper, testApp.FeeGrantKeeper, testApp.ParamsKeeper, nil)}, testApp.OracleKeeper, &testApp.EvmKeeper),
		func() sdk.AnteFullDecorator {
			var simLimit sdk.Gas = 1_000_000
			return sdk.DefaultWrappedAnteDecorator(wasmkeeper.NewLimitSimulationGasDecorator(&simLimit, antedecorators.GetGasMeterSetter(testApp.ParamsKeeper)))
		}(),
		sdk.DefaultWrappedAnteDecorator(PriorityCaptureDecorator{captured: &seenAfterLimit}),
		sdk.DefaultWrappedAnteDecorator(authante.NewRejectExtensionOptionsDecorator()),
		sdk.DefaultWrappedAnteDecorator(PriorityCaptureDecorator{captured: &seenAfterReject}),
		oracle.NewSpammingPreventionDecorator(testApp.OracleKeeper),
		sdk.DefaultWrappedAnteDecorator(PriorityCaptureDecorator{captured: &seenAfterSpamming}),
		sdk.DefaultWrappedAnteDecorator(antedecorators.NewPriorityDecorator()),
		sdk.DefaultWrappedAnteDecorator(PriorityCaptureDecorator{captured: &seenAfterPriority}),
	}
	handler, _ := sdk.ChainAnteDecorators(decorators...)

	from, _ := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	to, _ := sdk.AccAddressFromBech32("sei1jdppe6fnj2q7hjsepty5crxtrryzhuqsjrj95y")
	msg := &banktypes.MsgSend{FromAddress: from.String(), ToAddress: to.String(), Amount: sdk.NewCoins(sdk.NewInt64Coin("usei", 1))}

	// fund the sender to cover fees
	fund := sdk.NewCoins(sdk.NewInt64Coin("usei", 1_000_000_000))
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, fund))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, from, fund))

	txb := testApp.GetTxConfig().NewTxBuilder()
	require.NoError(t, txb.SetMsgs(msg))
	txb.SetGasLimit(500_000)
	txb.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 100_000)))
	tx := txb.GetTx()

	_, err := handler(ctx, tx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if seenAfterLimit == 0 || seenAfterReject == 0 || seenAfterSpamming == 0 {
		t.Fatalf("expected non zero priority after limit/reject/spamming, got %d/%d/%d", seenAfterLimit, seenAfterReject, seenAfterSpamming)
	}
	if seenAfterPriority == 0 {
		t.Fatalf("expected PriorityDecorator to set correct priority for BankSend, got %d", seenAfterPriority)
	}
}
