package utils

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const HardFailPrefix = "hard fail error occurred"

func PanicHandler(recoverCallback func(any)) func() {
	return func() {
		if err := recover(); err != nil {
			if shouldErrorHardFail(fmt.Sprintf("%s", err)) {
				panic(err)
			}
			recoverCallback(err)
		}
	}
}

// LogPanicCallback returns a callback function, given a context and a recovered
// error value, that logs the error and a stack trace.
func LogPanicCallback(ctx sdk.Context, r any) func(any) {
	return func(a any) {
		stackTrace := string(debug.Stack())
		ctx.Logger().Error("recovered panic", "recover_err", r, "recover_type", fmt.Sprintf("%T", r), "stack_trace", stackTrace)
	}
}

func MetricsPanicCallback(err any, ctx sdk.Context, key string) {
	ctx.Logger().Error(fmt.Sprintf("panic %s occurred during order matching for: %s", err, key))
	defer func() {
		if e := recover(); e != nil {
			return
		}
	}()
	telemetry.IncrCounterWithLabels(
		[]string{"panic"},
		1,
		[]metrics.Label{
			telemetry.NewLabel("error", fmt.Sprintf("%s", err)),
			telemetry.NewLabel("module", key),
		},
	)
}

func DecorateHardFailError(err error) error {
	return fmt.Errorf("%s:%s", HardFailPrefix, err.Error())
}

func shouldErrorHardFail(err string) bool {
	// use Contains instead of HasPrefix in case the error is further decorated
	return strings.Contains(err, HardFailPrefix)
}
