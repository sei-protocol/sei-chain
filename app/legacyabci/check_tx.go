package legacyabci

import (
	"fmt"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	gometrics "github.com/armon/go-metrics"
	"github.com/sei-protocol/sei-chain/app/ante"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/utils/tracing"
	authkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	bankkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	feegrantkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant/keeper"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	upgradekeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"
	ibckeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/keeper"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
)

var defaultRecoveryMiddleware = newDefaultRecoveryMiddleware()

type CheckTxKeepers struct {
	AccountKeeper  authkeeper.AccountKeeper
	BankKeeper     bankkeeper.Keeper
	FeeGrantKeeper *feegrantkeeper.Keeper
	IBCKeeper      *ibckeeper.Keeper
	OracleKeeper   oraclekeeper.Keeper
	EvmKeeper      *evmkeeper.Keeper
	ParamsKeeper   paramskeeper.Keeper
	UpgradeKeeper  *upgradekeeper.Keeper
}

func CheckTx(
	ctx sdk.Context,
	tx sdk.Tx,
	txConfig client.TxConfig,
	keepers *CheckTxKeepers,
	checksum [32]byte,
	contextCacher func(sdk.Context) (sdk.Context, sdk.CacheMultiStore),
	latestCtxGetter func() sdk.Context,
	tracingInfo *tracing.Info,
) (
	gInfo sdk.GasInfo,
	result *sdk.Result,
	txCtx sdk.Context,
	err error,
) {
	label := "check"
	if ctx.IsReCheckTx() {
		label = "recheck"
	}
	defer telemetry.MeasureThroughputSinceWithLabels(
		telemetry.TxCount,
		[]gometrics.Label{
			telemetry.NewLabel("mode", label),
		},
		time.Now(),
	)
	spanCtx, span := tracingInfo.StartWithContext("CheckTx", ctx.TraceSpanContext())
	defer span.End()
	ctx = ctx.WithTraceSpanContext(spanCtx)
	span.SetAttributes(attribute.String("txHash", fmt.Sprintf("%X", checksum)))
	var gasWanted uint64
	var gasEstimate uint64

	defer func() {
		if r := recover(); r != nil {
			recoveryMW := newOutOfGasRecoveryMiddleware(gasWanted, ctx, defaultRecoveryMiddleware)
			err, result = processRecovery(r, recoveryMW), nil
		}
		gInfo = sdk.GasInfo{GasWanted: gasWanted, GasUsed: ctx.GasMeter().GasConsumed(), GasEstimate: gasEstimate}
	}()

	if tx == nil {
		return sdk.GasInfo{}, nil, ctx, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "tx decode error")
	}

	var anteSpan trace.Span
	// trace AnteHandler
	_, anteSpan = tracingInfo.StartWithContext("AnteHandler", ctx.TraceSpanContext())
	defer anteSpan.End()
	anteCtx, _ := contextCacher(ctx)
	anteCtx = anteCtx.WithEventManager(sdk.NewEventManager())
	var newCtx sdk.Context
	if isEVM, evmerr := evmante.IsEVMMessage(tx); evmerr != nil {
		err = evmerr
	} else if isEVM {
		newCtx, err = ante.EvmCheckTxAnte(anteCtx, txConfig, tx, keepers.UpgradeKeeper, keepers.EvmKeeper, latestCtxGetter)
	} else {
		newCtx, err = ante.CosmosCheckTxAnte(anteCtx, txConfig, tx, keepers.ParamsKeeper, keepers.OracleKeeper, keepers.EvmKeeper, keepers.AccountKeeper, keepers.BankKeeper, keepers.FeeGrantKeeper, keepers.IBCKeeper)
	}
	if !newCtx.IsZero() {
		ctx = newCtx
	}

	if err != nil {
		return gInfo, nil, ctx, err
	}
	// GasMeter expected to be set in AnteHandler
	gasWanted = ctx.GasMeter().Limit()
	gasEstimate = ctx.GasEstimate()
	anteSpan.End()

	return gInfo, &sdk.Result{Events: []abci.Event{}}, ctx, err
}
