package wasm_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	"github.com/spf13/cast"
	"github.com/stretchr/testify/require"
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
// Two other read sites are worth naming: memory_cache_size is read but never templated,
// so it is reachable only by hand-editing, and the global --trace flag doubles as the
// contract debug-mode switch, so enabling ABCI stack traces also turns on contract
// debugging.

// TestReadWasmConfigAbsentKeysKeepDefaults pins the section baseline — the in-code
// defaults, which is what a node with no [wasm] section resolves. Both sides move
// together when a default changes, so this asserts the reader's behavior rather
// than the values themselves.
func TestReadWasmConfigAbsentKeysKeepDefaults(t *testing.T) {
	cfg, err := wasm.ReadWasmConfig(configtest.AppOpts{})
	require.NoError(t, err, "an absent [wasm] section must read cleanly")
	require.Equal(t, types.DefaultWasmConfig(), cfg,
		"an absent [wasm] section must resolve to the declared defaults")
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
