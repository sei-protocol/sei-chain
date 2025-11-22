package legacyabci

import (
	"fmt"
	"time"

	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	abci "github.com/tendermint/tendermint/abci/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	gometrics "github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/utils/tracing"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	ibckeeper "github.com/cosmos/ibc-go/v3/modules/core/keeper"
	"github.com/sei-protocol/sei-chain/app/ante"
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
