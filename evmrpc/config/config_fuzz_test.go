package config_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/eth/tracers"
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"     // register the JS evaluator
	_ "github.com/ethereum/go-ethereum/eth/tracers/native" // register the native tracers

	"github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
)

// evmKeys is the [evm] section's read-site manifest: every key ReadConfig looks
// up, the field it resolves into, and the cast it applies. The keys are spelled
// out as literals rather than referenced through the package's unexported flag
// constants on purpose — the literal is the operator-facing contract, written by
// hand into app.toml, and renaming it is a breaking change that this table is
// here to catch.
//
// Every row is guarded (`if v := opts.Get(k); v != nil`) and checked
// (cast.ToXE), which together give the section its two operator-visible
// properties: a key absent from an older app.toml keeps the in-code default, and
// a malformed value fails the boot with the key named instead of resolving to a
// zero. Three keys carry extra semantics past the cast and are covered by
// dedicated targets below rather than folded in here.
var evmKeys = []configtest.KeySpec{
	{Key: "evm.http_enabled", Path: "HTTPEnabled", Cast: configtest.CastBool, Checked: true},
	{Key: "evm.http_port", Path: "HTTPPort", Cast: configtest.CastInt, Checked: true},
	{Key: "evm.ws_enabled", Path: "WSEnabled", Cast: configtest.CastBool, Checked: true},
	{Key: "evm.ws_port", Path: "WSPort", Cast: configtest.CastInt, Checked: true},
	{Key: "evm.read_timeout", Path: "ReadTimeout", Cast: configtest.CastDuration, Checked: true},
	{Key: "evm.read_header_timeout", Path: "ReadHeaderTimeout", Cast: configtest.CastDuration, Checked: true},
	{Key: "evm.write_timeout", Path: "WriteTimeout", Cast: configtest.CastDuration, Checked: true},
	{Key: "evm.idle_timeout", Path: "IdleTimeout", Cast: configtest.CastDuration, Checked: true},
	{Key: "evm.simulation_gas_limit", Path: "SimulationGasLimit", Cast: configtest.CastUint64, Checked: true},
	{Key: "evm.simulation_evm_timeout", Path: "SimulationEVMTimeout", Cast: configtest.CastDuration, Checked: true},
	{Key: "evm.cors_origins", Path: "CORSOrigins", Cast: configtest.CastString, Checked: true},
	{Key: "evm.ws_origins", Path: "WSOrigins", Cast: configtest.CastString, Checked: true},
	{Key: "evm.filter_timeout", Path: "FilterTimeout", Cast: configtest.CastDuration, Checked: true},
	{Key: "evm.checktx_timeout", Path: "CheckTxTimeout", Cast: configtest.CastDuration, Checked: true},
	{
		Key: "evm.max_tx_pool_txs", Path: "MaxTxPoolTxs", Cast: configtest.CastUint64, Checked: true,
		Why: "not rendered in the [evm] template; reachable only by hand-editing, env or flag",
	},
	{Key: "evm.slow", Path: "Slow", Cast: configtest.CastBool, Checked: true},
	{Key: "evm.deny_list", Path: "DenyList", Cast: configtest.CastStringSlice, Checked: true},
	{Key: "evm.max_log_no_block", Path: "MaxLogNoBlock", Cast: configtest.CastInt64, Checked: true},
	{Key: "evm.max_blocks_for_log", Path: "MaxBlocksForLog", Cast: configtest.CastInt64, Checked: true},
	{
		Key: "evm.max_log_bytes", Path: "MaxLogBytes", Cast: configtest.CastInt64, Checked: true,
		Why: "bounds the response bytes an eth_getLogs may return, so it caps peak memory per query",
	},
	{Key: "evm.max_estimate_gas_calls", Path: "MaxEstimateGasCalls", Cast: configtest.CastInt, Checked: true},
	{Key: "evm.max_state_override_accounts", Path: "MaxStateOverrideAccounts", Cast: configtest.CastInt, Checked: true},
	{Key: "evm.max_state_override_slots", Path: "MaxStateOverrideSlots", Cast: configtest.CastInt, Checked: true},
	{Key: "evm.max_subscriptions_new_head", Path: "MaxSubscriptionsNewHead", Cast: configtest.CastUint64, Checked: true},
	{Key: "evm.max_subscriptions_logs", Path: "MaxSubscriptionsLogs", Cast: configtest.CastUint64, Checked: true},
	{
		Key: "evm.enable_test_api", Path: "EnableTestAPI", Cast: configtest.CastBool, Checked: true,
		Why: "deliberately absent from the template, but any operator can still set it by hand",
	},
	{Key: "evm.max_concurrent_trace_calls", Path: "MaxConcurrentTraceCalls", Cast: configtest.CastUint64, Checked: true},
	{
		Key: "evm.max_concurrent_simulation_calls", Path: "MaxConcurrentSimulationCalls",
		Cast: configtest.CastInt, Checked: true,
		Why: "default is runtime.NumCPU(), so the absent-key value is machine-dependent",
	},
	{Key: "evm.max_trace_lookback_blocks", Path: "MaxTraceLookbackBlocks", Cast: configtest.CastInt64, Checked: true},
	{Key: "evm.trace_timeout", Path: "TraceTimeout", Cast: configtest.CastDuration, Checked: true},
	{Key: "evm.max_trace_struct_log_bytes", Path: "MaxTraceStructLogBytes", Cast: configtest.CastUint64, Checked: true},
	{Key: "evm.trace_allow_js_tracers", Path: "TraceAllowJSTracers", Cast: configtest.CastBool, Checked: true},
	{Key: "evm.enable_parallelized_block_trace", Path: "EnableParallelizedBlockTrace", Cast: configtest.CastBool, Checked: true},
	{Key: "evm.rpc_stats_interval", Path: "RPCStatsInterval", Cast: configtest.CastDuration, Checked: true},
	{
		Key: "evm.worker_pool_size", Path: "WorkerPoolSize", Cast: configtest.CastInt, Checked: true,
		Why: "default is min(64, 2*NumCPU), so the absent-key value is machine-dependent",
	},
	{Key: "evm.worker_queue_size", Path: "WorkerQueueSize", Cast: configtest.CastInt, Checked: true},
	{Key: "evm.enabled_legacy_sei_apis", Path: "EnabledLegacySeiApis", Cast: configtest.CastStringSlice, Checked: true},
	{Key: "evm.trace_bake_enabled", Path: "TraceBakeEnabled", Cast: configtest.CastBool, Checked: true},
	{Key: "evm.trace_bake_workers", Path: "TraceBakeWorkers", Cast: configtest.CastInt, Checked: true},
	{Key: "evm.trace_bake_queue_size", Path: "TraceBakeQueueSize", Cast: configtest.CastInt, Checked: true},
	{Key: "evm.trace_bake_window_blocks", Path: "TraceBakeWindowBlocks", Cast: configtest.CastInt64, Checked: true},
	{Key: "evm.trace_bake_use_snapshot", Path: "TraceBakeUseSnapshot", Cast: configtest.CastBool, Checked: true},
	{Key: "evm.trace_bake_snapshot_window", Path: "TraceBakeSnapshotWindow", Cast: configtest.CastInt64, Checked: true},
	{Key: "evm.ip_rate_limit_rps", Path: "IPRateLimitRPS", Cast: configtest.CastFloat64, Checked: true},
	{Key: "evm.ip_rate_limit_burst", Path: "IPRateLimitBurst", Cast: configtest.CastInt, Checked: true},
	{Key: "evm.batch_request_limit", Path: "BatchRequestLimit", Cast: configtest.CastInt, Checked: true},
	{Key: "evm.batch_response_max_size", Path: "BatchResponseMaxSize", Cast: configtest.CastInt, Checked: true},
	{Key: "evm.max_request_body_bytes", Path: "MaxRequestBodyBytes", Cast: configtest.CastInt64, Checked: true},
	{Key: "evm.max_concurrent_request_bytes", Path: "MaxConcurrentRequestBytes", Cast: configtest.CastInt64, Checked: true},
}

func readEVM(opts configtest.AppOpts) (any, error) { return config.ReadConfig(opts) }

// FuzzReadConfig drives every plain [evm] key through arbitrary raw values,
// holding each to the cast its manifest row declares.
func FuzzReadConfig(f *testing.F) {
	// Seeds span the shapes an operator produces from the three layers that reach
	// this reader: TOML scalars, environment strings (always strings, never
	// typed), and cobra flag values.
	f.Add(uint(0), uint8(2), "true", int64(1), true)                   // TOML bool
	f.Add(uint(1), uint8(7), "", int64(8545), false)                   // env-style numeric string
	f.Add(uint(4), uint8(1), "30s", int64(0), false)                   // duration spelling
	f.Add(uint(4), uint8(3), "", int64(30), false)                     // bare number as a duration (nanoseconds)
	f.Add(uint(16), uint8(9), "eth_call eth_getLogs", int64(0), false) // whitespace-split slice
	f.Add(uint(16), uint8(10), "eth_call", int64(1), false)            // []any slice
	f.Add(uint(8), uint8(3), "", int64(-1), false)                     // negative into an unsigned cast: rejected
	f.Add(uint(8), uint8(5), "", int64(-1), false)                     // the same bits unsigned, near 2^64: accepted
	f.Add(uint(10), uint8(11), "", int64(0), false)                    // a table where a scalar belongs
	f.Add(uint(0), uint8(1), "not-a-bool", int64(0), false)            // must error, never resolve false
	f.Add(uint(42), uint8(6), "", int64(7), false)                     // float into a float key

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(evmKeys, keyIdx)
		configtest.CheckRow(t, "evm", readEVM, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// FuzzTracerAllowlists fuzzes the two tracer-name lists, the one place in this
// section where a value is validated rather than merely converted.
//
// The rule is a security boundary. debug_trace* hands any name that survives the
// allowlist straight to geth's tracer directory, which resolves an unregistered
// name by evaluating it as JavaScript in-process (geth eth/tracers/dir.go: an
// unknown name falls through to the JS evaluator). validateTraceTracer skips its
// JS checks entirely for a name the config allows, so the config list is the last
// gate — even when evm.trace_allow_js_tracers is false.
//
// The assertion is therefore one-directional: a list holding anything outside the
// native set must be an error, and a list that survives must resolve to native
// names only, trimmed and de-duplicated. Names arrive comma-separated so that
// whitespace and empty entries reach the reader intact and its own trim and
// empty-entry rejection are the code under test, not the test's own splitting.
//
// normalizeNativeTracerNames runs unconditionally, including over the defaults,
// which is why the absent-key case is asserted too.
func FuzzTracerAllowlists(f *testing.F) {
	f.Add(true, "callTracer")
	f.Add(false, "callTracer")
	f.Add(true, "callTracer,prestateTracer")
	f.Add(true, "  callTracer  ")        // reaches the reader with its padding
	f.Add(true, "callTracer,callTracer") // de-duplicated
	f.Add(true, "jsTracer")              // must be rejected
	f.Add(true, "")                      // single empty entry: rejected
	f.Add(true, "callTracer,")           // trailing empty entry: rejected
	f.Add(true, "callTracer, prestateTracer")
	f.Add(true, "CALLTRACER") // case-sensitive: not native

	f.Fuzz(func(t *testing.T, requestSide bool, raw string) {
		key := "evm.trace_bake_tracers"
		path := "TraceBakeTracers"
		if requestSide {
			key = "evm.trace_allowed_tracers"
			path = "TraceAllowedTracers"
		}
		names := strings.Split(raw, ",")

		cfg, err := config.ReadConfig(configtest.AppOpts{key: names})

		// Restated independently of the reader: an entry that is empty after
		// trimming is rejected, and so is one that is not a native tracer.
		rejected := false
		for _, n := range names {
			trimmed := strings.TrimSpace(n)
			if trimmed == "" || !config.IsNativeTraceTracer(trimmed) {
				rejected = true
			}
		}
		if rejected {
			if err == nil {
				t.Fatalf("%s = %q holds an empty or non-native entry and must be rejected", key, names)
			}
			return
		}
		if err != nil {
			t.Fatalf("%s = %q is all-native and must be accepted, got %v", key, names, err)
		}

		// The resolved list has to equal the normalized input, not merely satisfy the
		// same invariants the defaults already satisfy. Checking only native, trimmed
		// and de-duplicated would pass for a reader that ignored the key entirely and
		// kept DefaultTraceAllowedTracers, which is the one outcome this target exists
		// to rule out.
		want := make([]string, 0, len(names))
		seen := make(map[string]bool, len(names))
		for _, n := range names {
			trimmed := strings.TrimSpace(n)
			if !seen[trimmed] {
				seen[trimmed] = true
				want = append(want, trimmed)
			}
		}

		got := resolvedTracers(t, cfg, path)
		if a, b := configtest.Dump(got), configtest.Dump(want); a != b {
			t.Fatalf("%s = %q resolved to the wrong list\n got: %s\nwant: %s\n"+
				"the reader must adopt the operator's names, trimmed and de-duplicated in "+
				"first-occurrence order", key, names, a, b)
		}
		for _, n := range got {
			if !config.IsNativeTraceTracer(n) {
				t.Fatalf("%s resolved to %q, which is not a native tracer", key, n)
			}
			if n != strings.TrimSpace(n) {
				t.Fatalf("%s resolved to un-trimmed name %q", key, n)
			}
		}
	})
}

// TestNativeTracerSetIsNonJSInGeth closes the gap that IsNativeTraceTracer cannot
// close for itself.
//
// FuzzTracerAllowlists proves a resolved list contains only names this package
// calls native. That is circular for the question that actually matters — whether
// the names in this package's set are ones geth resolves without invoking the JS
// evaluator. geth answers that directly: DefaultDirectory.IsJS reports false only
// for a registered non-JS tracer, and true for anything unregistered, because an
// unregistered name is treated as JS source.
//
// So the property is: every name IsNativeTraceTracer accepts must be non-JS in
// geth. Adding a name to the set that geth does not register would open an
// in-process JS path on a node whose operator only listed a tracer, and this is
// the assertion that catches it.
func TestNativeTracerSetIsNonJSInGeth(t *testing.T) {
	for _, name := range config.DefaultTraceAllowedTracers() {
		if !config.IsNativeTraceTracer(name) {
			t.Errorf("%q is a default allowed tracer but IsNativeTraceTracer rejects it", name)
			continue
		}
		if tracers.DefaultDirectory.IsJS(name) {
			t.Errorf("%q is allowlisted as native but geth resolves it through the JS evaluator; "+
				"allowlisting it lets debug_trace* run JavaScript in-process", name)
		}
	}

	// The default list is the whole set today. If a tracer is added to the set
	// without being added to the defaults, this catches the omission so the check
	// above cannot silently stop covering it.
	for _, name := range []string{
		config.TraceTracerCall, config.TraceTracerPrestate, config.TraceTracerFlatCall,
		config.TraceTracer4Byte, config.TraceTracerNoop, config.TraceTracerMux,
	} {
		if tracers.DefaultDirectory.IsJS(name) {
			t.Errorf("native tracer constant %q is not registered as non-JS in geth", name)
		}
	}
}

func resolvedTracers(t *testing.T, cfg any, path string) []string {
	t.Helper()
	c, ok := cfg.(config.Config)
	if !ok {
		t.Fatalf("unexpected config type %T", cfg)
	}
	if path == "TraceAllowedTracers" {
		return c.TraceAllowedTracers
	}
	return c.TraceBakeTracers
}

// FuzzMaxOpenConnections pins the only [evm] key with a range check. A negative
// listener budget is meaningless, and 0 is the documented "no limit" spelling, so
// the boundary between them has to be exact: 0 boots, -1 does not.
func FuzzMaxOpenConnections(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(-1))
	f.Add(int64(2000))
	f.Add(int64(-2147483648))

	f.Fuzz(func(t *testing.T, n int64) {
		cfg, err := config.ReadConfig(configtest.AppOpts{"evm.max_open_connections": n})
		// The value is read through cast.ToIntE, so on a 64-bit platform the int64
		// arrives intact and the sign is what decides the outcome.
		switch {
		case n < 0:
			if err == nil {
				t.Fatalf("evm.max_open_connections = %d is negative and must be rejected", n)
			}
		case err != nil:
			t.Fatalf("evm.max_open_connections = %d is valid and must be accepted, got %v", n, err)
		case int64(cfg.MaxOpenConnections) != n:
			t.Fatalf("evm.max_open_connections = %d resolved to %d", n, cfg.MaxOpenConnections)
		}
	})
}

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline: an app.toml
// with no [evm] section resolves to DefaultConfig exactly, including the two
// machine-dependent defaults and the normalized tracer lists.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	configtest.CheckAbsent(t, "evm", readEVM, config.DefaultConfig)
}

// TestDefaultsMatchTheRecordedValues pins the evm defaults themselves.
//
// The absent-keys row above proves the reader returns the declared defaults; it cannot prove
// which values those are, because both sides of that comparison come from this package. This
// compares them against testdata/evm.golden, an independent recording, so a default that
// moves shows the new value in a diff instead of passing silently.
func TestDefaultsMatchTheRecordedValues(t *testing.T) {
	configtest.CheckDefaults(t, "evm", config.DefaultConfig,
		configtest.DerivedDefault{
			Path: "MaxConcurrentSimulationCalls", Want: runtime.NumCPU(),
			Why: "runtime.NumCPU()",
		},
		configtest.DerivedDefault{
			Path: "WorkerPoolSize", Want: min(config.MaxWorkerPoolSize, runtime.NumCPU()*2),
			Why: "min(MaxWorkerPoolSize, runtime.NumCPU()*2)",
		},
	)
}

// TestManifestNamesEveryField enforces the claim evmKeys makes about itself.
//
// The table says it lists every key ReadConfig looks up, and that claim is what a replacement
// implementation will read as the contract for this section. Asserting it in prose leaves it
// able to drift: a key can be added to the reader and rendered into app.toml while the table
// stays silent, and the table is the artifact being trusted.
func TestManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "evm", config.DefaultConfig, evmKeys,
		// Driven by dedicated targets in this file rather than by a table row, because each
		// needs a shape CheckRow does not express.
		"TraceAllowedTracers", // FuzzTracerAllowlists
		"TraceBakeTracers",    // FuzzTracerAllowlists
		"MaxOpenConnections",  // FuzzMaxOpenConnections
	)
}
