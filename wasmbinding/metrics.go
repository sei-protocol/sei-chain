package wasmbinding

import (
	"context"
	"errors"
	"fmt"

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("wasmbinding")

	wasmQueryMetrics = struct {
		associationError metric.Int64Counter
		sdkError         metric.Int64Counter
	}{
		associationError: must(meter.Int64Counter(
			"wasm_query_association_error",
			metric.WithDescription("Association errors during wasm query handling by scenario and address type"),
			metric.WithUnit("{count}"),
		)),
		sdkError: must(meter.Int64Counter(
			"wasm_query_sdk_error",
			metric.WithDescription("SDK errors during wasm query handling by scenario, codespace, and code"),
			metric.WithUnit("{count}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

func recordQueryError(ctx context.Context, scenario string, err error) {
	if err == nil {
		return
	}
	var assocErr evmtypes.AssociationMissingErr
	if errors.As(err, &assocErr) {
		wasmQueryMetrics.associationError.Add(ctx, 1, metric.WithAttributes(
			attribute.String("scenario", scenario),
			attribute.String("type", assocErr.AddressType()),
		))
	} else if codespace, code, _ := sdkerrors.ABCIInfo(err, false); codespace != sdkerrors.UndefinedCodespace {
		wasmQueryMetrics.sdkError.Add(ctx, 1, metric.WithAttributes(
			attribute.String("scenario", scenario),
			attribute.String("codespace", codespace),
			attribute.String("code", fmt.Sprintf("%d", code)),
		))
	}
	// TODO(PLT-343): remove once wasm_query_association_error and wasm_query_sdk_error verified
	metrics.IncrementErrorMetrics(scenario, err)
}
