package utils

import (
	"fmt"

	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func PanicHandler(recoverCallback func(any)) func() {
	return func() {
		if err := recover(); err != nil {
			recoverCallback(err)
		}
	}
}

func MetricsPanicCallback(err any, ctx sdk.Context, key string) {
	ctx.Logger().Error(fmt.Sprintf("panic %s occurred during order matching for: %s", err, key))
	telemetry.IncrCounterWithLabels(
		[]string{"endblockpanic"},
		1,
		[]metrics.Label{
			telemetry.NewLabel("error", fmt.Sprintf("%s", err)),
			telemetry.NewLabel("module", key),
		},
	)
}
