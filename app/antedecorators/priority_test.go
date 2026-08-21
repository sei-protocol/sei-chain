package antedecorators_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authante "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/ante"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

func TestPriorityAnteDecorator(t *testing.T) {
	output = ""
	anteDecorators := []sdk.AnteDecorator{
		antedecorators.NewPriorityDecorator(),
	}
	ctx := sdk.NewContext(nil, tmproto.Header{}, false)
	chainedHandler := sdk.ChainAnteDecorators(anteDecorators...)
	// test with normal priority
	newCtx, err := chainedHandler(
		ctx.WithPriority(125),
		FakeTx{},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, int64(125), newCtx.Priority())
}

func TestPriorityAnteDecoratorTooHighPriority(t *testing.T) {
	output = ""
	anteDecorators := []sdk.AnteDecorator{
		antedecorators.NewPriorityDecorator(),
	}
	ctx := sdk.NewContext(nil, tmproto.Header{}, false)
	chainedHandler := sdk.ChainAnteDecorators(anteDecorators...)
	// test with too high priority, should be auto capped
	newCtx, err := chainedHandler(
		ctx.WithPriority(math.MaxInt64-50),
		FakeTx{
			FakeMsgs: []sdk.Msg{
				&oracletypes.MsgDelegateFeedConsent{},
			},
		},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64-1000), newCtx.Priority())
}

func TestPriorityAnteDecoratorOracleMsgUsesNormalPriority(t *testing.T) {
	output = ""
	anteDecorators := []sdk.AnteDecorator{
		antedecorators.NewPriorityDecorator(),
	}
	ctx := sdk.NewContext(nil, tmproto.Header{}, false)
	chainedHandler := sdk.ChainAnteDecorators(anteDecorators...)
	// Oracle messages no longer receive a priority boost.
	newCtx, err := chainedHandler(
		ctx.WithPriority(0),
		FakeTx{
			FakeMsgs: []sdk.Msg{
				&oracletypes.MsgAggregateExchangeRateVote{},
			},
		},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, int64(0), newCtx.Priority())
}

// PriorityCaptureDecorator captures ctx.Priority seen by the next decorator in the chain
type PriorityCaptureDecorator struct{ captured *int64 }

func (d PriorityCaptureDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if d.captured != nil {
		*d.captured = ctx.Priority()
	}
	return next(ctx, tx, simulate)
}

func TestPriorityWithExactAnteChain_BankSend(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{ChainID: "sei-test"}).WithBlockHeight(2).WithIsCheckTx(true)
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, *paramtypes.DefaultCosmosGasParams())
	testApp.ParamsKeeper.SetFeesParams(ctx, paramtypes.DefaultGenesis().GetFeesParams())

	var seenAfterLimit int64 = -1
	var seenAfterReject int64 = -1
	var seenAfterPriority int64 = -1

	decorators := []sdk.AnteDecorator{
		authante.NewSetUpContextDecorator(antedecorators.GetGasMeterSetter(testApp.ParamsKeeper)),
		authante.NewDeductFeeDecorator(testApp.AccountKeeper, testApp.BankKeeper, testApp.ParamsKeeper, nil),
		func() sdk.AnteDecorator {
			var simLimit sdk.Gas = 1_000_000
			return wasmkeeper.NewLimitSimulationGasDecorator(&simLimit, antedecorators.GetGasMeterSetter(testApp.ParamsKeeper))
		}(),
		PriorityCaptureDecorator{captured: &seenAfterLimit},
		authante.NewRejectExtensionOptionsDecorator(),
		PriorityCaptureDecorator{captured: &seenAfterReject},
		antedecorators.NewPriorityDecorator(),
		PriorityCaptureDecorator{captured: &seenAfterPriority},
	}
	handler := sdk.ChainAnteDecorators(decorators...)

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

	if seenAfterLimit <= 0 || seenAfterReject <= 0 {
		t.Fatalf("expected non zero priority after limit/reject, got %d/%d", seenAfterLimit, seenAfterReject)
	}
	if seenAfterPriority <= 0 {
		t.Fatalf("expected PriorityDecorator to set correct priority for BankSend, got %d", seenAfterPriority)
	}
}

func TestPriorityWithExactAnteChain_MsgAssociate(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{ChainID: "sei-test"}).WithBlockHeight(2).WithIsCheckTx(true)
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, *paramtypes.DefaultCosmosGasParams())
	testApp.ParamsKeeper.SetFeesParams(ctx, paramtypes.DefaultGenesis().GetFeesParams())

	decorators := []sdk.AnteDecorator{
		authante.NewSetUpContextDecorator(antedecorators.GetGasMeterSetter(testApp.ParamsKeeper)),
		authante.NewDeductFeeDecorator(testApp.AccountKeeper, testApp.BankKeeper, testApp.ParamsKeeper, nil),
		antedecorators.NewPriorityDecorator(),
	}
	handler := sdk.ChainAnteDecorators(decorators...)

	from, _ := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	fund := sdk.NewCoins(sdk.NewInt64Coin("usei", 1_000_000_000))
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, fund))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, from, fund))

	const gasLimit = uint64(500_000)
	fees := sdk.NewCoins(sdk.NewInt64Coin("usei", 100_000))
	txb := testApp.GetTxConfig().NewTxBuilder()
	require.NoError(t, txb.SetMsgs(evmtypes.NewMsgAssociate(from, "test")))
	txb.SetGasLimit(gasLimit)
	txb.SetFeeAmount(fees)

	newCtx, err := handler(ctx, txb.GetTx(), false)
	require.NoError(t, err)
	require.Equal(t, authante.GetTxPriority(fees, int64(gasLimit)), newCtx.Priority())
}

// PriorityCaptureDecorator captures ctx.Priority seen by the next decorator in the chain
type PrioritySetterDecorator struct{ priority int64 }

func (d PrioritySetterDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	newCtx := ctx.WithPriority(d.priority)
	return next(newCtx, tx, simulate)
}

func TestPrioritySetterWithAnteHandlers(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{}).WithBlockHeight(2).WithIsCheckTx(true)
	testApp.ParamsKeeper.SetCosmosGasParams(ctx, *paramtypes.DefaultCosmosGasParams())
	testApp.ParamsKeeper.SetFeesParams(ctx, paramtypes.DefaultGenesis().GetFeesParams())

	var expectedPriority int64 = 1000000
	var seenAfterSetter int64 = -1
	var seenAfterLimit int64 = -1
	var seenAfterReject int64 = -1
	var seenAfterPriority int64 = -1

	decorators := []sdk.AnteDecorator{
		authante.NewSetUpContextDecorator(antedecorators.GetGasMeterSetter(testApp.ParamsKeeper)),
		PrioritySetterDecorator{priority: expectedPriority},
		PriorityCaptureDecorator{captured: &seenAfterSetter},
		func() sdk.AnteDecorator {
			var simLimit sdk.Gas = 1_000_000
			return wasmkeeper.NewLimitSimulationGasDecorator(&simLimit, antedecorators.GetGasMeterSetter(testApp.ParamsKeeper))
		}(),
		PriorityCaptureDecorator{captured: &seenAfterLimit},
		authante.NewRejectExtensionOptionsDecorator(),
		PriorityCaptureDecorator{captured: &seenAfterReject},
		antedecorators.NewPriorityDecorator(),
		PriorityCaptureDecorator{captured: &seenAfterPriority},
	}
	handler := sdk.ChainAnteDecorators(decorators...)

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

	if seenAfterLimit <= 0 || seenAfterReject <= 0 {
		t.Fatalf("expected non zero priority after limit/reject, got %d/%d", seenAfterLimit, seenAfterReject)
	}
	require.Equal(t, expectedPriority, seenAfterPriority)
}
