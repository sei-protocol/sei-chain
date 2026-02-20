package legacyabci

import (
	"fmt"
	"time"

	"github.com/armon/go-metrics"
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
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type DeliverTxKeepers struct {
	AccountKeeper  authkeeper.AccountKeeper
	BankKeeper     bankkeeper.Keeper
	FeeGrantKeeper *feegrantkeeper.Keeper
	OracleKeeper   oraclekeeper.Keeper
	EvmKeeper      *evmkeeper.Keeper
	ParamsKeeper   paramskeeper.Keeper
	UpgradeKeeper  *upgradekeeper.Keeper
}

func DeliverTx(
	ctx sdk.Context,
	tx sdk.Tx,
	txConfig client.TxConfig,
	keepers *DeliverTxKeepers,
	checksum [32]byte,
	contextCacher func(sdk.Context) (sdk.Context, sdk.CacheMultiStore),
	msgRunner func(ctx sdk.Context, msgs []sdk.Msg) (*sdk.Result, error), //TODO: remove
	tracingInfo *tracing.Info,
	evmHook func(ctx sdk.Context, tx sdk.Tx, checksum [32]byte, response sdk.DeliverTxHookInput),
) (
	gInfo sdk.GasInfo,
	result *sdk.Result,
	anteEvents []abci.Event,
	txCtx sdk.Context,
	err error,
) {
	defer telemetry.MeasureThroughputSinceWithLabels(
		telemetry.TxCount,
		[]metrics.Label{
			telemetry.NewLabel("mode", "deliver"),
		},
		time.Now(),
	)
	// check for existing parent tracer, and if applicable, use it
	spanCtx, span := tracingInfo.StartWithContext("DeliverTx", ctx.TraceSpanContext())
	defer span.End()
	ctx = ctx.WithTraceSpanContext(spanCtx)
	span.SetAttributes(attribute.String("txHash", fmt.Sprintf("%X", checksum)))
	var gasWanted uint64
	ms := ctx.MultiStore()
	defer func() {
		if r := recover(); r != nil {
			recoveryMW := newOutOfGasRecoveryMiddleware(gasWanted, ctx, defaultRecoveryMiddleware)
			recoveryMW = newOCCAbortRecoveryMiddleware(recoveryMW) // TODO: do we have to wrap with occ enabled check?
			err, result = processRecovery(r, recoveryMW), nil
		}
		gInfo = sdk.GasInfo{GasWanted: gasWanted, GasUsed: ctx.GasMeter().GasConsumed()}
	}()

	if tx == nil {
		return sdk.GasInfo{}, nil, nil, ctx, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "tx decode error")
	}
	var anteSpan trace.Span
	// trace AnteHandler
	_, anteSpan = tracingInfo.StartWithContext("AnteHandler", ctx.TraceSpanContext())
	defer anteSpan.End()
	var (
		anteCtx sdk.Context
		msCache sdk.CacheMultiStore
	)
	anteCtx, msCache = contextCacher(ctx)
	anteCtx = anteCtx.WithEventManager(sdk.NewEventManager())
	var newCtx sdk.Context
	if isEVM, evmerr := evmante.IsEVMMessage(tx); evmerr != nil {
		err = evmerr
	} else if isEVM {
		newCtx, err = ante.EvmDeliverTxAnte(anteCtx, txConfig, tx, keepers.UpgradeKeeper, keepers.EvmKeeper)
		defer func() {
			if newCtx.DeliverTxCallback() != nil {
				newCtx.DeliverTxCallback()(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)))
			}
		}()
	} else {
		newCtx, err = ante.CosmosDeliverTxAnte(anteCtx, txConfig, tx, keepers.ParamsKeeper, keepers.OracleKeeper, keepers.EvmKeeper, keepers.AccountKeeper, keepers.BankKeeper, keepers.FeeGrantKeeper)
	}
	if !newCtx.IsZero() {
		ctx = newCtx.WithMultiStore(ms)
	}

	events := ctx.EventManager().Events()
	if err != nil {
		return gInfo, nil, nil, ctx, err
	}
	gasWanted = ctx.GasMeter().Limit()
	msCache.Write()
	anteEvents = events.ToABCIEvents()
	anteSpan.End()

	runMsgCtx, msCache := contextCacher(ctx)
	// TODO: simplify
	result, err = msgRunner(runMsgCtx, tx.GetMsgs())

	if err == nil {
		msCache.Write()
	}
	// we do this since we will only be looking at result in DeliverTx
	if result != nil && len(anteEvents) > 0 {
		// append the events in the order of occurrence
		result.Events = append(anteEvents, result.Events...)
	}
	// only apply hooks if no error
	if err == nil && (!ctx.IsEVM() || result.EvmError == "") {
		var evmTxInfo *abci.EvmTxInfo
		if ctx.IsEVM() {
			evmTxInfo = &abci.EvmTxInfo{
				SenderAddress: ctx.EVMSenderAddress(),
				Nonce:         ctx.EVMNonce(),
				TxHash:        ctx.EVMTxHash(),
				VmError:       result.EvmError,
			}
		}
		evmHook(ctx, tx, checksum, sdk.DeliverTxHookInput{
			EvmTxInfo: evmTxInfo,
			Events:    result.Events,
		})
	}
	return gInfo, result, anteEvents, ctx, err
}
