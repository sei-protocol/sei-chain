package wasm

import (
	"reflect"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

// TestTheDeclaredKeysAreTheFlagsThisModuleReads holds the schema against the module.
//
// The schema exists because types.WasmConfig carries no mapstructure tags, so the keys cannot be derived
// from it. That makes the schema a second statement of the same key set, and a second statement is only
// safe while something holds it against the first. These are the flag constants the module registers and
// reads.
func TestTheDeclaredKeysAreTheFlagsThisModuleReads(t *testing.T) {
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", SectionName)
	}

	want := []string{flagWasmMemoryCacheSize, flagWasmQueryGasLimit, flagWasmSimulationGasLimit}
	if got := section.Keys; !reflect.DeepEqual(got, want) {
		t.Errorf("declared keys are %v, want the flags this module reads, %v", got, want)
	}
}

// TestTheDefaultsCarryTheLiveWasmConfig keeps the schema's values from drifting from the real ones.
//
// The schema restates three settings of types.WasmConfig, so nothing stops those values diverging from
// what DefaultWasmConfig returns except this.
func TestTheDefaultsCarryTheLiveWasmConfig(t *testing.T) {
	live := types.DefaultWasmConfig()
	got, ok := defaults(registry.ModeValidator).(wasmSchema)
	if !ok {
		t.Fatalf("defaults returned %T, want wasmSchema", defaults(registry.ModeValidator))
	}

	if got.MemoryCacheSize != live.MemoryCacheSize {
		t.Errorf("memory_cache_size resolves to %d, want the live %d", got.MemoryCacheSize, live.MemoryCacheSize)
	}
	if got.QueryGasLimit != live.SmartQueryGasLimit {
		t.Errorf("query_gas_limit resolves to %d, want the live %d", got.QueryGasLimit, live.SmartQueryGasLimit)
	}
	// Absent is a meaning of its own here: unset means the consensus block gas limit applies, so an unset
	// live value has to resolve to no text rather than to a zero.
	if live.SimulationGasLimit == nil && got.SimulationGasLimit != "" {
		t.Errorf("simulation_gas_limit resolves to %q where the live value is unset. A number here claims "+
			"a limit the node does not apply", got.SimulationGasLimit)
	}
}
