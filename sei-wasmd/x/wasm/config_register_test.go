package wasm

import (
	"reflect"
	"sort"
	"strconv"
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
	for _, defect := range registry.Defects() {
		if defect.Section == SectionName {
			t.Fatalf("%s was refused: %v", SectionName, defect.Err)
		}
	}
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", SectionName)
	}

	want := []string{flagWasmMemoryCacheSize, flagWasmQueryGasLimit, flagWasmSimulationGasLimit}
	sort.Strings(want)
	if got := section.Keys; !reflect.DeepEqual(got, want) {
		t.Errorf("declared keys are %v, want the flags this module reads, %v", got, want)
	}
}

// TestTheDefaultsAreWhatTheCommandWrites keeps the schema's values from drifting from what a file carries.
//
// The schema restates three settings of types.WasmConfig, so nothing stops those values diverging except
// this. Two of them come from that struct. The third, the query gas limit, comes from what the command
// renders, which is a tenth of what the struct holds, and both halves of that are held below.
func TestTheDefaultsAreWhatTheCommandWrites(t *testing.T) {
	live := types.DefaultWasmConfig()
	for _, mode := range registry.Modes() {
		if got := sectionDefaults(mode); !reflect.DeepEqual(got, sectionDefaults(registry.ModeValidator)) {
			t.Errorf("mode %q resolves differently from the others, and nothing in the module makes "+
				"either setting follow from what kind of node is asking", mode)
		}
	}
	got, ok := sectionDefaults(registry.ModeValidator).(wasmSchema)
	if !ok {
		t.Fatalf("defaults returned %T, want wasmSchema", sectionDefaults(registry.ModeValidator))
	}

	if got.MemoryCacheSize != live.MemoryCacheSize {
		t.Errorf("memory_cache_size resolves to %d, want the live %d", got.MemoryCacheSize, live.MemoryCacheSize)
	}
	// The limit the command writes, not the module's own default. The two differ by a factor of ten and
	// both facts are held: declaring the larger one would have a caller rendering a file that loosens the
	// only bound on what one smart query can ask of a node, and the larger one is still what a node whose
	// file carries no wasm section resolves.
	if got.QueryGasLimit != GeneratedQueryGasLimit {
		t.Errorf("query_gas_limit resolves to %d, want the %d the command writes",
			got.QueryGasLimit, GeneratedQueryGasLimit)
	}
	if GeneratedQueryGasLimit >= live.SmartQueryGasLimit {
		t.Errorf("the limit the command writes, %d, is no longer below this module's own default, %d. "+
			"If they have converged the distinction here is spurious and should go; if the command's "+
			"limit has grown past the module's, a generated file now loosens the bound rather than "+
			"tightening it", GeneratedQueryGasLimit, live.SmartQueryGasLimit)
	}
	// Absent is a meaning of its own here: unset means the consensus block gas limit applies, so an unset
	// live value resolves to no text rather than to a zero, and a set one resolves to its digits.
	want := ""
	if live.SimulationGasLimit != nil {
		want = strconv.FormatUint(*live.SimulationGasLimit, 10)
	}
	if got.SimulationGasLimit != want {
		t.Errorf("simulation_gas_limit resolves to %q, want %q. Unset means the consensus block gas limit "+
			"applies, and a number here claims a limit the node does not apply", got.SimulationGasLimit, want)
	}
}
