package config

import (
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestDeclaredKeysAreTheOnesItsReaderResolves holds the derived keys against the reader's own constants.
//
// The section registers the struct its reader fills, so a mapstructure tag is the only spelling of these
// keys and there is no second list to fall behind. What remains is the constants ReadConfig looks up,
// which state the same fifty-seven keys again in the same file, and a rename that moves one and not the
// other compiles.
//
// Written out rather than derived from the struct, because a list derived from the same tags would agree
// with itself whatever those tags said.
func TestDeclaredKeysAreTheOnesItsReaderResolves(t *testing.T) {
	want := []string{
		flagHTTPEnabled, flagHTTPPort, flagWSEnabled, flagWSPort,
		flagReadTimeout, flagReadHeaderTimeout, flagWriteTimeout, flagIdleTimeout,
		flagSimulationGasLimit, flagSimulationEVMTimeout, flagCORSOrigins, flagWSOrigins,
		flagFilterTimeout, flagMaxTxPoolTxs, flagCheckTxTimeout, flagSlow,
		flagEnableSimulation, flagDenyList, flagMaxLogNoBlock, flagMaxLogBytes,
		flagMaxBlocksForLog, flagMaxEstimateGasCalls, flagMaxStateOverrideAccounts,
		flagMaxStateOverrideSlots, flagMaxSubscriptionsNewHead, flagMaxSubscriptionsLogs,
		flagEnableTestAPI, flagMaxConcurrentTraceCalls, flagMaxConcurrentSimulationCalls,
		flagMaxTraceLookbackBlocks, flagTraceTimeout, flagMaxTraceStructLogBytes,
		flagTraceAllowedTracers, flagTraceAllowJSTracers, flagEnableParallelizedBlockTrace,
		flagRPCStatsInterval, flagWorkerPoolSize, flagWorkerQueueSize, flagEVMLegacySeiApis,
		flagTraceBakeEnabled, flagTraceBakeWorkers, flagTraceBakeQueueSize, flagTraceBakeTracers,
		flagTraceBakeWindowBlocks, flagTraceBakeUseSnapshot, flagTraceBakeSnapshotWindow,
		flagIPRateLimitRPS, flagIPRateLimitBurst, flagRateLimitingEnabled, flagTrustedProxyCIDRs,
		flagBatchRequestLimit, flagBatchResponseMaxSize, flagMaxRequestBodyBytes,
		flagMaxConcurrentRequestBytes, flagWSAdmissionTimeout, flagMaxOpenConnections,
		flagBodyReadIdleTimeout,
	}
	sort.Strings(want)

	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", SectionName)
	}
	if !reflect.DeepEqual(section.Keys, want) {
		t.Errorf("%s declares %d keys and its reader resolves %d.\ndeclared: %v\nresolved: %v",
			SectionName, len(section.Keys), len(want), section.Keys, want)
	}
}

// TestDefaultsAreTheReaderOwnForEveryMode covers the value side of the same registration.
//
// Unchanged by mode, which is the decision worth pinning. seid init writes the two interface toggles per
// mode, so a validator it provisioned carries them as written values. These are what a node with nothing
// written runs, and no read of these keys consults the node's kind.
func TestDefaultsAreTheReaderOwnForEveryMode(t *testing.T) {
	for _, mode := range registry.Modes() {
		got, ok := defaults(mode).(Config)
		if !ok {
			t.Fatalf("mode %q: defaults returned %T, want the type its reader fills", mode, defaults(mode))
		}
		if !reflect.DeepEqual(got, DefaultConfig) {
			t.Errorf("mode %q resolves to a value other than the reader's own default", mode)
		}
		if !got.HTTPEnabled || !got.WSEnabled {
			t.Errorf("mode %q resolves an interface closed. A node whose file lacks these keys serves "+
				"both, so resolving one closed would take an interface away from a running node", mode)
		}
	}
}

// TestTheTwoHostDerivedValuesDescribeThisHost covers the two defaults that are measurements.
//
// Every other value here is a decision someone wrote down and is the same on any machine. These two are
// the processor count and twice it, so they describe whichever host resolved them. Nothing here can make
// that portable, and stating it is what keeps a caller from rendering them into a file as though it were.
func TestTheTwoHostDerivedValuesDescribeThisHost(t *testing.T) {
	got, ok := defaults(registry.ModeValidator).(Config)
	if !ok {
		t.Fatalf("defaults returned %T, want the type its reader fills", defaults(registry.ModeValidator))
	}
	if got.MaxConcurrentSimulationCalls != runtime.NumCPU() {
		t.Errorf("%s resolves to %d and this host has %d processors",
			flagMaxConcurrentSimulationCalls, got.MaxConcurrentSimulationCalls, runtime.NumCPU())
	}
	if want := min(MaxWorkerPoolSize, runtime.NumCPU()*2); got.WorkerPoolSize != want {
		t.Errorf("%s resolves to %d, want %d on a host with %d processors",
			flagWorkerPoolSize, got.WorkerPoolSize, want, runtime.NumCPU())
	}
}
