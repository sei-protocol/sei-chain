package parquet

import (
	"fmt"
	"math/rand"
	"os"

	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
)

// crashProbability is the probability that any given hook invocation triggers
// a simulated crash. At 1/1000, all 5 hooks give roughly one crash every ~200 blocks.
const crashProbability = 1.0 / 1000.0

// MakeCrashHooksFromEnv returns crash-injection FaultHooks with all 5 hooks enabled.
// Each hook independently samples at crashProbability per invocation.
// On trigger, it logs to stderr and calls os.Exit(1) to simulate an abrupt kill.
// These hooks are installed only AFTER WAL replay completes on startup, so crash
// injection never fires during recovery (which would create an infinite restart loop).
func MakeCrashHooksFromEnv(log dbLogger.Logger) *FaultHooks {
	if os.Getenv("PARQUET_CRASH_TEST") == "" {
		return nil
	}

	if log != nil {
		log.Info("parquet crash testing enabled: rate=1/1000 hook=\"all\"")
	}

	makeCrashFn := func(hookName string) func(blockNumber uint64) error {
		return func(blockNumber uint64) error {
			if rand.Float64() < crashProbability { //nolint:gosec
				fmt.Fprintf(os.Stderr, "PARQUET_CRASH_TEST: crashing at hook=%s block=%d\n", hookName, blockNumber)
				os.Exit(1)
			}
			return nil
		}
	}

	return &FaultHooks{
		AfterWALWrite:     makeCrashFn("AfterWALWrite"),
		BeforeFlush:       makeCrashFn("BeforeFlush"),
		AfterFlush:        makeCrashFn("AfterFlush"),
		AfterCloseWriters: makeCrashFn("AfterCloseWriters"),
		AfterWALClear:     makeCrashFn("AfterWALClear"),
	}
}
