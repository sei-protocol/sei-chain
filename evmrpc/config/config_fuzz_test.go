package config_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/eth/tracers"
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"     // register the JS evaluator
	_ "github.com/ethereum/go-ethereum/eth/tracers/native" // register the native tracers
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

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
// So the property is that every name IsNativeTraceTracer accepts must be non-JS in geth. Adding a
// name to the set that geth does not register would open an in-process JS path on a node whose
// operator only listed a tracer.
//
// This walks the default list, which is the operator-facing half. The set itself is walked by
// TestEveryNativeTracerEntryIsNonJSInGeth, which enumerates nativeTraceTracers from source, so an
// entry added to the map without being added to the defaults is caught there rather than here.
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
}

// TestEveryNativeTracerEntryIsNonJSInGeth holds every entry of nativeTraceTracers to being non-JS,
// by enumerating the set rather than a list written beside it.
//
// TestNativeTracerSetIsNonJSInGeth above walks DefaultTraceAllowedTracers, which is the
// operator-facing half. That list is not the map IsNativeTraceTracer answers from, so an entry added
// to the map without being added to the defaults is reached only here.
//
// The gap is allowlistable and it opens the JS evaluator. Adding "jsStubTracer" to the map leaves
// this package green while IsNativeTraceTracer accepts the name and geth's IsJS reports true for it,
// so an operator who allowlisted only that name would be running request-supplied JavaScript
// in-process. That is the failure this closes.
//
// The set is read at runtime through export_test.go, which is compiled only under test and so widens
// nothing the package ships. Through an accessor rather than a captured var, so a reassignment of the
// map cannot leave this asserting over a set nothing consults.
//
// That runtime read is the stronger observation. It is the same map IsNativeTraceTracer consults, so
// it sees every entry however it arrived, including one added in an init, one added by a helper, one
// spelled as a bare string that no constant names, or the declaration moving to another file.
func TestEveryNativeTracerEntryIsNonJSInGeth(t *testing.T) {
	// An empty set would pass while checking nothing, which is the defect one level up.
	if len(config.NativeTraceTracers()) == 0 {
		t.Fatal("nativeTraceTracers is empty, so this proved nothing about the set and " +
			"IsNativeTraceTracer accepts no name at all")
	}

	for name := range config.NativeTraceTracers() {
		// Cannot fire while IsNativeTraceTracer is a bare lookup in this same map, and that is the
		// point: it holds the accessor to answering from the set and nothing else. A condition added
		// to it later, a feature gate or a build tag, would make the two disagree and land here.
		if !config.IsNativeTraceTracer(name) {
			t.Errorf("%q is in nativeTraceTracers but IsNativeTraceTracer rejects it, so the accessor "+
				"no longer answers from the set alone and an operator's allowlist is filtered by "+
				"something this test cannot see", name)
			continue
		}
		if tracers.DefaultDirectory.IsJS(name) {
			t.Errorf("%q is in nativeTraceTracers but geth resolves it through the JS evaluator. "+
				"IsNativeTraceTracer accepts it, so an operator allowlisting only this name would "+
				"let debug_trace* run JavaScript in-process", name)
		}
	}
}

// resolvedTracers returns the tracer list the named Config field resolved to.
func resolvedTracers(t *testing.T, cfg config.Config, path string) []string {
	t.Helper()
	if path == "TraceAllowedTracers" {
		return cfg.TraceAllowedTracers
	}
	return cfg.TraceBakeTracers
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
	cfg, err := config.ReadConfig(configtest.AppOpts{})
	require.NoError(t, err, "an app.toml with no [evm] section must read cleanly")
	require.Equal(t, config.DefaultConfig, cfg,
		"a key absent from app.toml must keep the in-code default")
}

// TestMachineDependentDefaultsFollowTheirFormula holds the two defaults that are computed from the
// host's CPU count to the formulas that define them.
func TestMachineDependentDefaultsFollowTheirFormula(t *testing.T) {
	require.Equal(t, runtime.NumCPU(), config.DefaultConfig.MaxConcurrentSimulationCalls,
		"MaxConcurrentSimulationCalls must default to runtime.NumCPU()")
	require.Equal(t, min(config.MaxWorkerPoolSize, runtime.NumCPU()*2), config.DefaultConfig.WorkerPoolSize,
		"WorkerPoolSize must default to min(MaxWorkerPoolSize, runtime.NumCPU()*2)")
}
