package wasm_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	"github.com/spf13/cast"
)

// The [wasm] section is where the app.toml template and the in-code defaults
// disagree, and the disagreement changes what contracts can do.
//
// query_gas_limit is rendered into a generated app.toml as the literal 300000,
// while DefaultWasmConfig carries 3,000,000 — so a node whose app.toml was generated
// by seid runs smart queries with a tenth of the gas allowance of a node whose
// app.toml has no [wasm] section at all. Neither node is misconfigured by its own
// lights; they simply resolved different values from the same binary.
//
// Two more rows sit alongside it: memory_cache_size is read but never templated, so
// it is reachable only by hand-editing, and the global --trace flag doubles as the
// contract debug-mode switch, so enabling ABCI stack traces also turns on contract
// debugging.

// wasmKeys are the [wasm] read sites whose resolution is a plain guarded checked
// cast. simulation_gas_limit is not among them: it is a pointer field with an
// extra string-shaped guard, so it gets its own target.
var wasmKeys = []configtest.KeySpec{
	{
		Key: "wasm.query_gas_limit", Path: "SmartQueryGasLimit", Cast: configtest.CastUint64,
		Checked: true,
		Why:     "in-code default 3,000,000 vs the template literal 300000",
	},
	{
		Key: "wasm.memory_cache_size", Path: "MemoryCacheSize", Cast: configtest.CastUint32,
		Checked: true,
		Why:     "read but absent from the seid template; settable only by hand, env or flag",
	},
	{
		Key: server.FlagTrace, Path: "ContractDebugMode", Cast: configtest.CastBool,
		Checked: true,
		Why:     "the global --trace flag doubles as the wasm contract debug switch",
	},
}

func readWasm(opts configtest.AppOpts) (any, error) { return wasm.ReadWasmConfig(opts) }

func FuzzReadWasmConfig(f *testing.F) {
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
	seeds.AddRow(uint(0), fuzzing.KindInt64, "", int64(300000), false)
	seeds.AddRow(uint(0), fuzzing.KindNumericString, "", int64(3000000), false)
	seeds.AddRow(uint(0), fuzzing.KindInt64, "", int64(-1), false) // negative into an unsigned cast: rejected
	seeds.AddRow(uint(1), fuzzing.KindInt64, "", int64(256), false)
	seeds.AddRow(uint(2), fuzzing.KindBool, "", int64(0), true)
	seeds.AddRow(uint(2), fuzzing.KindString, "not-a-bool", int64(0), false)
	seeds.AddRow(uint(0), fuzzing.KindNil, "", int64(0), false)
	seeds.AddRow(uint(1), fuzzing.KindMap, "", int64(0), false)

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "wasm", readWasm, wasmKeys, seeds)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(wasmKeys, keyIdx)
		configtest.CheckRow(t, "wasm", readWasm, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// TestReadWasmConfigAbsentKeysKeepDefaults pins the section baseline — the in-code
// defaults, which is what a node with no [wasm] section resolves.
func TestReadWasmConfigAbsentKeysKeepDefaults(t *testing.T) {
	configtest.CheckAbsent(t, "wasm", readWasm, types.DefaultWasmConfig())
}

// TestQueryGasLimitInCodeDefaultStaysAboveTheGeneratedLimit pins this package's half of
// the ten-fold query-gas divergence: what a node with no [wasm] section resolves.
//
// The other half is the literal in seid's app.toml template, and it is deliberately not
// asserted here. The template lives in cmd/seid/cmd, and the only thing this package can
// do with its number is hand it to ReadWasmConfig and watch the same number come back,
// which pins the reader rather than the template and would stay green if the template
// changed. That half is pinned end to end against a materialized app.toml by
// TestGeneratedAppTOMLDivergesFromTheWasmInCodeDefault in cmd/seid/cmd. The constant below
// is a cross-reference for the comparison, not a stand-in for reading the template.
//
// What is asserted here is still load-bearing: the in-code default is what every node
// whose app.toml predates or omits [wasm] runs with, and it must stay above the generated
// limit, since the two are not derived from each other and drift independently.
func TestQueryGasLimitInCodeDefaultStaysAboveTheGeneratedLimit(t *testing.T) {
	// Cross-reference only. cmd/seid/cmd asserts this against the real generated file.
	const generatedLimitSeeElsewhere = 300000

	inCode := types.DefaultWasmConfig().SmartQueryGasLimit
	if inCode == generatedLimitSeeElsewhere {
		t.Fatalf("the in-code default is now %d, matching the generated limit. Closing that "+
			"divergence changes the gas allowance on every node whose app.toml lacks [wasm], so it "+
			"gets recorded here rather than skipped past", inCode)
	}
	if inCode <= generatedLimitSeeElsewhere {
		t.Fatalf("the in-code default (%d) is no longer above the generated limit (%d); the "+
			"direction of the divergence changed", inCode, generatedLimitSeeElsewhere)
	}

	// An app.toml without the section resolves the in-code default, which is the property
	// this package owns.
	fromDefault, err := wasm.ReadWasmConfig(configtest.AppOpts{})
	if err != nil {
		t.Fatalf("ReadWasmConfig: %v", err)
	}
	if fromDefault.SmartQueryGasLimit != inCode {
		t.Fatalf("absent query_gas_limit resolved to %d, want the in-code %d",
			fromDefault.SmartQueryGasLimit, inCode)
	}
}

// TestLruSizeTemplateKeyIsInert records that the template renders a [wasm] key
// nothing reads. lru_size appears in a generated app.toml; ReadWasmConfig reads
// memory_cache_size. Editing the templated key has no effect on the cache.
func TestLruSizeTemplateKeyIsInert(t *testing.T) {
	cfg, err := wasm.ReadWasmConfig(configtest.AppOpts{"wasm.lru_size": uint32(512)})
	if err != nil {
		t.Fatalf("ReadWasmConfig: %v", err)
	}
	if cfg.MemoryCacheSize != types.DefaultWasmConfig().MemoryCacheSize {
		t.Fatalf("wasm.lru_size resolved into MemoryCacheSize (%d); the live key is "+
			"memory_cache_size, and if the two were unified that needs a migration",
			cfg.MemoryCacheSize)
	}
}

// FuzzWasmSimulationGasLimit pins the one [wasm] read with a string-shaped guard.
//
// The field is a pointer, and it is populated only when the raw value is a non-empty
// *string* — a TOML integer leaves it nil, so the same number written unquoted and
// quoted resolve differently. nil means "no simulation limit", so the quoting of the
// value decides whether a bound exists at all.
func FuzzWasmSimulationGasLimit(f *testing.F) {
	f.Add(fuzzing.KindString, "500000", int64(0), false) // a string: adopted
	f.Add(fuzzing.KindInt64, "", int64(500000), false)   // a TOML integer: leaves it nil
	f.Add(fuzzing.KindString, "", int64(0), false)       // an empty string: leaves it nil
	f.Add(fuzzing.KindString, "not-a-number", int64(0), false)
	f.Add(fuzzing.KindNil, "", int64(0), false)

	f.Fuzz(func(t *testing.T, kind uint8, s string, n int64, b bool) {
		value := fuzzing.ConfigValue(kind, s, n, b)
		cfg, err := wasm.ReadWasmConfig(configtest.AppOpts{"wasm.simulation_gas_limit": value})

		raw, isString := value.(string)
		if !isString || raw == "" {
			if err != nil {
				t.Fatalf("a non-string or empty simulation_gas_limit must be ignored, got %v", err)
			}
			if cfg.SimulationGasLimit != nil {
				t.Fatalf("simulation_gas_limit = %#v is not a non-empty string and must leave the "+
					"limit unset, got %d", value, *cfg.SimulationGasLimit)
			}
			return
		}

		// A non-empty string is converted, and an unconvertible one is an error rather
		// than a silently absent limit. The expectation is restated against cast rather
		// than accepting any error, because accepting any error would let a reader that
		// rejected every string value pass while the limit check below never ran.
		want, castErr := cast.ToUint64E(raw)
		if castErr != nil {
			if err == nil {
				t.Fatalf("simulation_gas_limit = %q does not convert to a uint64 (%v), so the read "+
					"must error rather than leaving the limit unset", raw, castErr)
			}
			return
		}
		if err != nil {
			t.Fatalf("simulation_gas_limit = %q converts cleanly and must be accepted, got %v", raw, err)
		}
		if cfg.SimulationGasLimit == nil {
			t.Fatalf("simulation_gas_limit = %q converted cleanly but left the limit unset", raw)
		}
		if *cfg.SimulationGasLimit != want {
			t.Fatalf("simulation_gas_limit = %q resolved to %d, want %d", raw, *cfg.SimulationGasLimit, want)
		}
	})
}

// TestDefaultsMatchTheRecordedValues pins the wasm defaults themselves.
//
// The absent-keys coverage in this file proves the reader returns the declared defaults; it
// cannot prove which values those are, because both sides of that comparison come from the
// same package. This compares them against testdata/wasm.golden, an independent
// recording, so a default that moves shows the new value in a diff instead of passing
// silently.
func TestDefaultsMatchTheRecordedValues(t *testing.T) {
	configtest.CheckDefaults(t, "wasm", types.DefaultWasmConfig())
}

// TestKeyNamesMatchTheRecordedNames pins the three key names themselves.
//
// Two are literals and the third, server.FlagTrace, is the global --trace flag reached
// through the sei-cosmos constant that declares it — a key whose value can be edited in
// another module entirely, where nothing suggests that a wasm contract debug switch rides on
// it. The recorded name is why that edit fails here.
//
// The record holds "trace" without a section prefix for that row, which is correct: the
// third row is not a [wasm] key at all.
func TestKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "wasm", wasmKeys)
}

// TestManifestNamesEveryField enforces the claim wasmKeys makes about itself: that it names
// every key the reader looks up. Left as prose the claim can drift, and it is the artifact a
// replacement implementation reads as this section's contract.
func TestManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "wasm", types.DefaultWasmConfig(), wasmKeys,
		"SimulationGasLimit", // FuzzWasmSimulationGasLimit: the one read with a string-shaped guard
	)
}

// TestWiringMatchesTheRecord pins which checks each of this package's sections is wired to.
//
// Every other check here reports a change to what it asserts. None reports a check being removed, so
// this records the wiring and fails when it thins out.
func TestWiringMatchesTheRecord(t *testing.T) {
	configtest.CheckWiring(t)
}

// TestNoExperimentalKeyShadowsThisSection is this section's half of the experimental collision
// check.
//
// It lives here because a KeySpec manifest is an unexported package-level var in a _test.go file,
// so this is the only test binary that can see both this section's live keys and the experimental
// registry. A test in cmd/seid/cmd cannot reference these vars at all.
//
// A declared experimental name is the path the key occupies after promotion, so a name equal to one
// of these keys would put two declarations on one path. The check compares whole spellings only;
// a semantic duplicate under a different name stays a review question.
func TestNoExperimentalKeyShadowsThisSection(t *testing.T) {
	for _, m := range []struct {
		section string
		specs   []configtest.KeySpec
	}{
		{"wasm", wasmKeys},
	} {
		configtest.CheckNoExperimentalKeyShadowsThisSection(t, m.section, m.specs)
	}
}
