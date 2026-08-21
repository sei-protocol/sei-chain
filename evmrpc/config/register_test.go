package config

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestDeclaredKeysAreTheOnesItsReaderResolves holds the derived keys against the reader's own constants.
//
// The section registers the struct its reader fills, so a mapstructure tag is the only spelling of these
// keys and there is no second list to fall behind. What remains is the constants ReadConfig looks up,
// which state the same keys again in the same file, and a rename that moves one and not the other
// compiles.
//
// Written out rather than derived from the struct, because a list derived from the same tags would agree
// with itself whatever those tags said.
func TestDeclaredKeysAreTheOnesItsReaderResolves(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == SectionName {
			t.Fatalf("%s was refused, so none of its keys is declared: %v", SectionName, defect.Err)
		}
	}
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
	declared := map[string]bool{}
	for _, key := range section.Keys {
		declared[key] = true
	}
	for _, key := range want {
		if !declared[key] {
			t.Errorf("the reader resolves %s and no tag declares it", key)
		}
		delete(declared, key)
	}
	for key := range declared {
		t.Errorf("%s is declared and no constant in this file resolves it", key)
	}
}

// TestEachKindOfNodeResolvesTheInterfacesItIsFor is the mode-varying part of this section.
//
// A full node and an archive node serve queries, which is what these two interfaces are for. A validator
// and a seed serve none, and an open interface on the node that holds a signing key is a public request
// surface on the one node meant to expose the least. The values are written out here rather than taken
// from the same rule the section reads, so a change to that rule fails this and gets looked at.
func TestEachKindOfNodeResolvesTheInterfacesItIsFor(t *testing.T) {
	serving := map[registry.Mode]bool{
		registry.ModeValidator: false,
		registry.ModeSeed:      false,
		registry.ModeFull:      true,
		registry.ModeArchive:   true,
	}
	for _, mode := range registry.Modes() {
		want, named := serving[mode]
		if !named {
			t.Fatalf("mode %q has no expectation here, so a mode was added and this was not revisited", mode)
		}
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		for _, key := range []string{flagHTTPEnabled, flagWSEnabled} {
			if got := resolved.Values[key]; got != want {
				t.Errorf("mode %q: %s resolves to %v, want %v", mode, key, got, want)
			}
		}
	}
}

// TestEachKeyResolvesToTheValueItsFieldHolds covers the binding a key set cannot show.
//
// Resolving carries the key a tag produced together with the value that tag's field held. Comparing the
// defaults struct against itself does not: two tags on each other's fields leave the key set identical and
// every field still holding the value it always did, so a list and a URL change places unnoticed.
func TestEachKeyResolvesToTheValueItsFieldHolds(t *testing.T) {
	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{})
	if err != nil {
		t.Fatalf("%v", err)
	}
	for key, want := range map[string]any{
		flagCORSOrigins:         DefaultConfig.CORSOrigins,
		flagDenyList:            DefaultConfig.DenyList,
		flagTraceAllowedTracers: DefaultConfig.TraceAllowedTracers,
		flagEVMLegacySeiApis:    DefaultConfig.EnabledLegacySeiApis,
		flagTrustedProxyCIDRs:   DefaultConfig.TrustedProxyCIDRs,
		flagReadTimeout:         DefaultConfig.ReadTimeout,
		flagHTTPPort:            DefaultConfig.HTTPPort,
		flagIPRateLimitRPS:      DefaultConfig.IPRateLimitRPS,
		flagMaxLogBytes:         DefaultConfig.MaxLogBytes,
	} {
		if got := resolved.Values[key]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s resolves to %#v (%T), want %#v (%T)", key, got, got, want, want)
		}
	}
}
