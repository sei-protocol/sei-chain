package wasm

import (
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// TestTheWasmSchemaDescribesTheReaderItStandsInFor holds the schema against ReadWasmConfig.
//
// The schema declares the spelling and types.WasmConfig holds the values, and nothing in the code
// connects a schema field to the setting it stands for. This writes a value under each declared key,
// asks the reader which setting changed, and checks the baseline against what the reader leaves that
// setting at when nothing is written.
func TestTheWasmSchemaDescribesTheReaderItStandsInFor(t *testing.T) {
	for _, mode := range registry.Modes() {
		// The section name stays a literal. The wiring record reads it from this call's second
		// argument, so a constant would record every schema check under one placeholder row.
		configtest.CheckSchemaMatchesTheReader(t, "wasm", configtest.SchemaCheck{
			Mode: mode,
			Read: func(opts configtest.AppOpts) (any, error) {
				return ReadWasmConfig(opts)
			},
			Probe: map[string]any{
				flagWasmMemoryCacheSize: uint32(512),
				flagWasmQueryGasLimit:   uint64(9_000_000),
				// A string, because that is the only shape this reader takes for the simulation limit.
				flagWasmSimulationGasLimit: "5000000",
			},
		})
	}
}

// TestTheSimulationLimitIsDeclaredAsTheShapeTheReaderTakes pins why that field is a string.
//
// The reader takes the simulation limit only from a non-empty string and ignores every other written
// shape, a number included. Declaring it as a number would put a value in the configuration that the
// reader drops, and an operator would set a limit that never applied.
func TestTheSimulationLimitIsDeclaredAsTheShapeTheReaderTakes(t *testing.T) {
	asNumber, err := ReadWasmConfig(configtest.AppOpts{flagWasmSimulationGasLimit: uint64(5_000_000)})
	if err != nil {
		t.Fatalf("the reader refused a numeric simulation limit: %v", err)
	}
	if asNumber.SimulationGasLimit != nil {
		t.Fatalf("the reader took %d from a numeric simulation limit. It used to ignore every shape but "+
			"a non-empty string, and if that has changed the schema should declare a number",
			*asNumber.SimulationGasLimit)
	}

	asString, err := ReadWasmConfig(configtest.AppOpts{flagWasmSimulationGasLimit: "5000000"})
	if err != nil {
		t.Fatalf("the reader refused a string simulation limit: %v", err)
	}
	if asString.SimulationGasLimit == nil || *asString.SimulationGasLimit != 5_000_000 {
		t.Fatalf("the reader did not take 5000000 from a string simulation limit, got %v",
			asString.SimulationGasLimit)
	}
}

// TestTheContractDebugModeSettingHasNoKeyInThisSection holds the one field with no key here.
//
// The reader takes it from the node-wide trace setting, so declaring a wasm key for it would be a key
// nothing reads, and a value written under it would be silently discarded.
func TestTheContractDebugModeSettingHasNoKeyInThisSection(t *testing.T) {
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s did not register", SectionName)
	}
	for _, key := range section.Keys {
		if key == SectionName+".contractdebugmode" || key == SectionName+".contract_debug_mode" {
			t.Errorf("the registry declared %q. The reader takes that setting from the node-wide trace "+
				"setting, so a value written here would be discarded", key)
		}
	}
}

func TestTheDerivedKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s did not register", SectionName)
	}
	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	live := []string{flagWasmMemoryCacheSize, flagWasmQueryGasLimit, flagWasmSimulationGasLimit}
	for _, key := range live {
		if !derived[key] {
			t.Errorf("this package's reader resolves %q and the registry derives %v. An operator's "+
				"value reaches one of those spellings and not the other", key, section.Keys)
		}
	}
	if len(section.Keys) != len(live) {
		t.Errorf("the registry derived %d keys and this reader resolves %d wasm settings: %v",
			len(section.Keys), len(live), section.Keys)
	}
}

func TestRegisteringProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == SectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}
