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
	f.Add(uint(0), uint8(3), "", int64(300000), false)
	f.Add(uint(0), uint8(7), "", int64(3000000), false)
	f.Add(uint(0), uint8(3), "", int64(-1), false) // negative into an unsigned cast: rejected
	f.Add(uint(1), uint8(3), "", int64(256), false)
	f.Add(uint(2), uint8(2), "", int64(0), true)
	f.Add(uint(2), uint8(1), "not-a-bool", int64(0), false)
	f.Add(uint(0), uint8(0), "", int64(0), false)
	f.Add(uint(1), uint8(11), "", int64(0), false)

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

// TestQueryGasLimitTemplateLiteralDivergesFromTheInCodeDefault pins the ten-fold
// divergence between the two ways a node can end up with a query gas limit.
//
// The literal in the seid template is not derived from DefaultWasmConfig, so the two
// drift independently. This asserts they are still different, so unifying them is a
// deliberate act with a migration attached rather than a tidy-up: raising a
// generated node's limit to 3,000,000 changes what contract queries succeed.
func TestQueryGasLimitTemplateLiteralDivergesFromTheInCodeDefault(t *testing.T) {
	const generatedTemplateLiteral = 300000

	inCode := types.DefaultWasmConfig().SmartQueryGasLimit
	if inCode == generatedTemplateLiteral {
		t.Fatalf("the in-code default is now %d, matching the template literal. Closing that "+
			"divergence changes the gas allowance on every node whose app.toml lacks [wasm], so it "+
			"gets recorded here rather than skipped past", inCode)
	}

	// A generated app.toml resolves the template literal.
	fromTemplate, err := wasm.ReadWasmConfig(configtest.AppOpts{
		"wasm.query_gas_limit": generatedTemplateLiteral,
	})
	if err != nil {
		t.Fatalf("ReadWasmConfig: %v", err)
	}
	if fromTemplate.SmartQueryGasLimit != generatedTemplateLiteral {
		t.Fatalf("templated query_gas_limit resolved to %d, want %d",
			fromTemplate.SmartQueryGasLimit, generatedTemplateLiteral)
	}

	// An app.toml without the section resolves the in-code default instead.
	fromDefault, err := wasm.ReadWasmConfig(configtest.AppOpts{})
	if err != nil {
		t.Fatalf("ReadWasmConfig: %v", err)
	}
	if fromDefault.SmartQueryGasLimit != inCode {
		t.Fatalf("absent query_gas_limit resolved to %d, want the in-code %d",
			fromDefault.SmartQueryGasLimit, inCode)
	}
	if fromTemplate.SmartQueryGasLimit >= fromDefault.SmartQueryGasLimit {
		t.Fatalf("a generated app.toml (%d) is no longer tighter than an absent section (%d); "+
			"the direction of the divergence changed", fromTemplate.SmartQueryGasLimit, fromDefault.SmartQueryGasLimit)
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
	f.Add(uint8(1), "500000", int64(0), false) // a string: adopted
	f.Add(uint8(3), "", int64(500000), false)  // a TOML integer: leaves it nil
	f.Add(uint8(1), "", int64(0), false)       // an empty string: leaves it nil
	f.Add(uint8(1), "not-a-number", int64(0), false)
	f.Add(uint8(0), "", int64(0), false)

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
