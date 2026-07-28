package config

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/spf13/viper"
)

// GetConfig is the second parser of app.toml, and the one that feeds api, grpc,
// grpc-web, rosetta, telemetry and state-sync. app/seidb.go parses [state-commit]
// and [state-store] out of the same viper for the store; GetConfig parses them
// again, by a different mechanism (viper.IsSet plus typed getters rather than
// appOpts.Get plus cast), into a Config nobody hands to the store.
//
// Two parsers of one section is the drift risk the manifest names, and the reason
// this file exists: it pins GetConfig's own resolution rules so a change that
// unified the parsers would show up as a diff here rather than as two components
// disagreeing about the same key at runtime.
//
// Everything below drives a bare viper.New, which is what GetConfig takes. That
// deliberately excludes flag binding and env resolution — those belong to Apply and
// are pinned in cmd/seid/cmd. What is left is the parse itself.

// newAppViper returns a viper holding the one key GetConfig unconditionally
// requires, plus whatever the caller adds. telemetry.global-labels has no default
// and no presence guard, so nothing can be parsed without it.
func newAppViper(t testing.TB, keys map[string]any) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.Set("telemetry.global-labels", []any{})
	for k, val := range keys {
		v.Set(k, val)
	}
	return v
}

// FuzzGetConfigGlobalLabels pins the one key that can stop a node booting by being
// absent rather than wrong.
//
// telemetry.global-labels is read as a bare type assertion to []interface{} with no
// presence check, so an app.toml that omits the key entirely fails GetConfig
// outright — a node provisioned before the key existed does not start. Inside, each
// label is asserted to []interface{} and then its two elements to string with no
// checked assertion, so a label list holding a non-string panics rather than
// erroring.
//
// The shape rules are otherwise permissive in a way no operator would guess: a
// label whose length is not exactly 2 is silently dropped, not rejected.
func FuzzGetConfigGlobalLabels(f *testing.F) {
	f.Add(0, 0, "chain")   // no labels
	f.Add(1, 2, "chain")   // one well-formed pair
	f.Add(3, 2, "chain")   // several pairs
	f.Add(1, 1, "chain")   // one element: silently dropped
	f.Add(1, 3, "chain")   // three elements: silently dropped
	f.Add(1, 0, "chain")   // empty label
	f.Add(2, 2, "")        // empty strings are legal label content
	f.Add(1, 2, "a=b,c=d") // punctuation is not special

	f.Fuzz(func(t *testing.T, labelCount, elemsPerLabel int, content string) {
		// Keep the generated document small; the shape rules are what matter.
		if labelCount < 0 || labelCount > 8 || elemsPerLabel < 0 || elemsPerLabel > 4 {
			return
		}

		labels := make([]any, 0, labelCount)
		for i := range labelCount {
			elems := make([]any, 0, elemsPerLabel)
			for j := range elemsPerLabel {
				elems = append(elems, fmt.Sprintf("%s-%d-%d", content, i, j))
			}
			labels = append(labels, elems)
		}

		v := viper.New()
		v.Set("telemetry.global-labels", labels)
		cfg, err := GetConfig(v)
		if err != nil {
			t.Fatalf("a well-typed global-labels list must parse, got %v", err)
		}

		// Only two-element labels survive; the rest vanish without a diagnostic.
		wantKept := 0
		if elemsPerLabel == 2 {
			wantKept = labelCount
		}
		if len(cfg.Telemetry.GlobalLabels) != wantKept {
			t.Fatalf("%d labels of %d elements resolved to %d kept, want %d "+
				"(a label whose length is not exactly 2 is dropped silently)",
				labelCount, elemsPerLabel, len(cfg.Telemetry.GlobalLabels), wantKept)
		}
	})
}

// TestGetConfigRequiresGlobalLabels pins the absent-key failure on its own. This is
// the row that turns a missing telemetry section into a node that will not start.
func TestGetConfigRequiresGlobalLabels(t *testing.T) {
	_, err := GetConfig(viper.New())
	if err == nil {
		t.Fatal("an app.toml with no telemetry.global-labels must fail GetConfig; " +
			"if a presence guard was added, that changes which existing app.toml files boot")
	}
	if !strings.Contains(err.Error(), "global-labels") {
		t.Fatalf("the failure must name the key, got %v", err)
	}
}

// TestGetConfigPanicsOnNonStringLabel records that the inner element assertions are
// unchecked. A label list of the right shape but the wrong element type takes the
// node down with a raw interface-conversion panic rather than an error naming
// telemetry.
func TestGetConfigPanicsOnNonStringLabel(t *testing.T) {
	v := viper.New()
	v.Set("telemetry.global-labels", []any{[]any{1, 2}})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("a non-string label must panic; if it is now an error, the diagnostic " +
				"improved and this row should say so")
		}
	}()
	_, _ = GetConfig(v)
}

// grpcClamp is a duration key GetConfig clamps rather than accepts verbatim.
type grpcClamp struct {
	Key     string
	Path    string
	Default time.Duration
}

var grpcClamps = []grpcClamp{
	{Key: "grpc.max-connection-idle", Path: "GRPC.MaxConnectionIdle", Default: DefaultGRPCMaxConnectionIdle},
	{Key: "grpc.keepalive-time", Path: "GRPC.KeepaliveTime", Default: DefaultGRPCKeepaliveTime},
	{Key: "grpc.keepalive-timeout", Path: "GRPC.KeepaliveTimeout", Default: DefaultGRPCKeepaliveTimeout},
	{Key: "grpc.keepalive-min-time", Path: "GRPC.KeepaliveMinTime", Default: DefaultGRPCKeepaliveMinTime},
	{Key: "grpc.max-connection-age", Path: "GRPC.MaxConnectionAge", Default: DefaultGRPCMaxConnectionAge},
	{
		Key: "grpc.max-connection-age-grace", Path: "GRPC.MaxConnectionAgeGrace",
		Default: DefaultGRPCMaxConnectionAgeGrace,
	},
}

// FuzzGetConfigGRPCDurationClamps pins the negative-duration clamp on the gRPC
// keepalive keys.
//
// gRPC accepts a negative keepalive verbatim and behaves pathologically, so GetConfig
// substitutes the in-code default instead of passing it through. Only a negative is
// clamped, uniformly across all six keys: zero passes through everywhere, which matters
// most on the two age keys where gRPC reads zero as "no limit". Distinguishing negative
// from zero rather than treating both as unset is what this target holds in place.
//
// Each value is driven in two shapes, because a typed time.Duration is not a shape any
// app.toml can produce. A file gives "30s" or a bare integer and an environment variable
// always gives a string, so the typed form skips the cast.ToDuration step that sits between
// the file layer and this comparison. The string spelling is the one an operator actually
// writes, and "-1s" is how they would express the boundary that matters here.
func FuzzGetConfigGRPCDurationClamps(f *testing.F) {
	f.Add(uint(0), int64(0), false)
	f.Add(uint(0), int64(-1), false)
	f.Add(uint(0), int64((30 * time.Second)), false)
	f.Add(uint(4), int64(0), false)
	f.Add(uint(4), int64(-1), false)
	f.Add(uint(1), int64(-1000000000), false)
	f.Add(uint(2), int64((time.Hour)), false)
	// The same values as an operator would write them.
	f.Add(uint(0), int64((30 * time.Second)), true)
	f.Add(uint(0), int64(-1000000000), true) // "-1s"
	f.Add(uint(4), int64(0), true)
	f.Add(uint(2), int64((time.Hour)), true)

	f.Fuzz(func(t *testing.T, keyIdx uint, nanos int64, asString bool) {
		row := grpcClamps[keyIdx%uint(len(grpcClamps))]
		d := time.Duration(nanos)

		// A typed duration and its own String() spelling must resolve identically, since
		// viper.GetDuration casts the text back. Any divergence is in cast, not in the clamp,
		// and it belongs to this row because the file layer only ever produces the text.
		var raw any = d
		if asString {
			// Only a spelling that parses back to the same duration is a faithful stand-in for
			// the typed value. Duration.String has not always round-tripped at the int64
			// boundary, and a spelling the parser rejects would resolve to zero and fail this
			// row with a message about the clamp rather than about the encoding. Declining is
			// the same move fuzzing.TOMLScalar makes for values with no faithful text form.
			spelled := d.String()
			if back, perr := time.ParseDuration(spelled); perr != nil || back != d {
				return // no faithful text spelling; the typed shape already covers this value
			}
			raw = spelled
		}

		cfg, err := GetConfig(newAppViper(t, map[string]any{row.Key: raw}))
		if err != nil {
			t.Fatalf("%s = %#v must parse, got %v", row.Key, raw, err)
		}

		want := d
		if d < 0 {
			want = row.Default
		}
		got, ok := configtest.LeafAt(configtest.Dump(cfg), row.Path)
		if !ok {
			t.Fatalf("%s resolves into %q, which is not in the parsed config", row.Key, row.Path)
		}
		if wantLeaf := configtest.DumpAt(row.Path, want); got != wantLeaf {
			t.Fatalf("%s = %#v resolved wrongly\n got: %s\nwant: %s\n"+
				"a negative duration falls back to the in-code default; zero passes through, and "+
				"the typed and string spellings must agree",
				row.Key, raw, got, wantLeaf)
		}
	})
}

// FuzzGetConfigWriteMode pins GetConfig's own copy of the write-mode resolution.
//
// The rules match app/seidb.go — always parse, then let sc-write-mode-enable-auto
// (default true, flipped only by an explicit key) decide whether the parsed mode is
// honored — but the mechanism differs: GetConfig returns an error where seidb.go
// panics. Both parsers must agree on the resolved mode for a node's store choice
// and its reported config to describe the same thing, so the agreement is asserted
// against the shared helpers rather than restated.
func FuzzGetConfigWriteMode(f *testing.F) {
	f.Add("memiavl_only", true, true)
	f.Add("memiavl_only", true, false)
	f.Add("cosmos_only", false, false)
	f.Add("", false, false)
	f.Add("", true, true)
	f.Add("bogus", false, false)
	f.Add("bogus", true, true)
	f.Add("flatkv_only", false, false)

	f.Fuzz(func(t *testing.T, mode string, setAuto, auto bool) {
		keys := map[string]any{"state-commit.sc-write-mode": mode}
		if setAuto {
			keys["state-commit.sc-write-mode-enable-auto"] = auto
		}

		cfg, err := GetConfig(newAppViper(t, keys))

		_, parseErr := config.ParseSCWriteMode(mode)
		if mode != "" && parseErr != nil {
			if err == nil {
				t.Fatalf("sc-write-mode = %q does not parse and must be an error, not a panic or a fallback", mode)
			}
			if !strings.Contains(err.Error(), "sc-write-mode") {
				t.Fatalf("the failure must name the key, got %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("sc-write-mode = %q must parse, got %v", mode, err)
		}

		effectiveAuto := true
		if setAuto {
			effectiveAuto = auto
		}
		want := config.DefaultStateCommitConfig().WriteMode
		if mode != "" {
			parsed, perr := config.ParseSCWriteMode(mode)
			if perr != nil {
				t.Fatalf("mode %q was expected to parse: %v", mode, perr)
			}
			want = parsed
		}
		want = config.ApplyWriteModeAuto(effectiveAuto, want)

		if cfg.StateCommit.WriteMode != want {
			t.Fatalf("sc-write-mode = %q with auto=%v resolved to %v, want %v",
				mode, effectiveAuto, cfg.StateCommit.WriteMode, want)
		}
		if effectiveAuto && cfg.StateCommit.WriteMode != sctypes.Auto {
			t.Fatalf("with auto on, the effective mode must be auto, got %v", cfg.StateCommit.WriteMode)
		}
	})
}

// guardedKey is a key GetConfig reads only when viper reports it set.
type guardedKey struct {
	Key  string
	Path string
	// Set is a value distinguishable from the default, used to prove the guard
	// admits an explicit value as well as protecting an absent one.
	Set any
	// DefaultIsZero marks a key whose in-code default is already the zero value.
	// The guard still matters there — it is what lets an operator set a non-zero
	// value — but an absent key resolving to zero is correct rather than a clobber,
	// so the two cases need different assertions.
	DefaultIsZero bool
}

// guardedKeys are the GetConfig reads wrapped in viper.IsSet. They are the same
// zero-clobber class app/seidb.go guards, expressed through a different mechanism.
var guardedKeys = []guardedKey{
	{Key: "state-commit.sc-async-commit-buffer", Path: "StateCommit.MemIAVLConfig.AsyncCommitBuffer", Set: 7},
	{Key: "state-commit.sc-keep-recent", Path: "StateCommit.MemIAVLConfig.SnapshotKeepRecent", Set: 9},
	{Key: "state-commit.sc-snapshot-interval", Path: "StateCommit.MemIAVLConfig.SnapshotInterval", Set: 4321},
	{Key: "state-commit.sc-snapshot-min-time-interval", Path: "StateCommit.MemIAVLConfig.SnapshotMinTimeInterval", Set: 11},
	{Key: "state-commit.sc-snapshot-writer-limit", Path: "StateCommit.MemIAVLConfig.SnapshotWriterLimit", Set: 3},
	{Key: "state-commit.sc-snapshot-prefetch-threshold", Path: "StateCommit.MemIAVLConfig.SnapshotPrefetchThreshold", Set: 0.25},
	{Key: "state-commit.flatkv.fsync", Path: "StateCommit.FlatKVConfig.Fsync", Set: true, DefaultIsZero: true},
	{
		Key: "state-commit.flatkv.async-write-buffer", Path: "StateCommit.FlatKVConfig.AsyncWriteBuffer",
		Set: 5, DefaultIsZero: true,
	},
	{Key: "state-commit.flatkv.snapshot-interval", Path: "StateCommit.FlatKVConfig.SnapshotInterval", Set: 777},
	{Key: "state-commit.flatkv.snapshot-keep-recent", Path: "StateCommit.FlatKVConfig.SnapshotKeepRecent", Set: 6},
	{
		Key: "state-commit.flatkv.enable-read-write-metrics", Path: "StateCommit.FlatKVConfig.EnableReadWriteMetrics",
		Set: true, DefaultIsZero: true,
	},
	{Key: "grpc.max-recv-msg-size", Path: "GRPC.MaxRecvMsgSize", Set: 8 * 1024 * 1024},
	{Key: "grpc.max-open-connections", Path: "GRPC.MaxOpenConnections", Set: 123},
	{Key: "grpc-web.max-open-connections", Path: "GRPCWeb.MaxOpenConnections", Set: 456},
}

// FuzzGetConfigGuardedKeysPreserveDefaults pins the guarded half of GetConfig: an
// absent key resolves to the in-code default rather than to the zero value viper's
// typed getters would otherwise return.
//
// The keys that matter most are the bounded ones. grpc.max-recv-msg-size,
// grpc.max-open-connections and grpc-web.max-open-connections all default to a
// finite limit, and an unguarded read of an absent key would resolve 0 — which
// gRPC reads as unlimited. A node upgrading with an older app.toml would go from
// bounded to unbounded connections and message sizes with nothing said about it.
func FuzzGetConfigGuardedKeysPreserveDefaults(f *testing.F) {
	for i := range len(guardedKeys) {
		f.Add(uint(i), false)
		f.Add(uint(i), true)
	}

	f.Fuzz(func(t *testing.T, keyIdx uint, present bool) {
		row := guardedKeys[keyIdx%uint(len(guardedKeys))]

		absent, err := GetConfig(newAppViper(t, nil))
		if err != nil {
			t.Fatalf("parsing with no optional keys must succeed, got %v", err)
		}
		absentLeaf, ok := configtest.LeafAt(configtest.Dump(absent), row.Path)
		if !ok {
			t.Fatalf("%s resolves into %q, which is not in the parsed config", row.Key, row.Path)
		}

		if !present {
			// Whether the guard is doing anything is decided by comparing an absent key
			// against an explicit zero, not against a synthesized zero literal. A
			// synthesized one has to guess the field's Go type, and guessing wrong makes
			// the comparison unsatisfiable and the assertion vacuous — which is exactly
			// what an int-typed literal did for every uint, uint32 and float64 row here,
			// including both gRPC connection bounds. Reading the reader twice needs no
			// type knowledge at all.
			explicitZero, zeroErr := GetConfig(newAppViper(t, map[string]any{row.Key: 0}))
			if zeroErr != nil {
				t.Fatalf("%s = 0 must parse, got %v", row.Key, zeroErr)
			}
			zeroLeaf, ok := configtest.LeafAt(configtest.Dump(explicitZero), row.Path)
			if !ok {
				t.Fatalf("%s resolves into %q, which is not in the parsed config", row.Key, row.Path)
			}

			if row.DefaultIsZero {
				if absentLeaf != zeroLeaf {
					t.Fatalf("%s is marked DefaultIsZero but an absent key (%s) resolves differently "+
						"from an explicit 0 (%s)", row.Key, absentLeaf, zeroLeaf)
				}
				return
			}
			if absentLeaf == zeroLeaf {
				t.Fatalf("%s is absent and resolved to the same value as an explicit 0 (%s); the "+
					"guard that preserves the in-code default is gone", row.Key, absentLeaf)
			}
			return
		}

		set, err := GetConfig(newAppViper(t, map[string]any{row.Key: row.Set}))
		if err != nil {
			t.Fatalf("%s = %#v must parse, got %v", row.Key, row.Set, err)
		}
		setLeaf, ok := configtest.LeafAt(configtest.Dump(set), row.Path)
		if !ok {
			t.Fatalf("%s resolves into %q, which is not in the parsed config", row.Key, row.Path)
		}
		if setLeaf == absentLeaf {
			t.Fatalf("%s = %#v did not change the resolved value (%s); the guard admits an "+
				"explicit value as well as protecting an absent one", row.Key, row.Set, setLeaf)
		}
	})
}

// TestGetConfigGuardedKeyDefaultsMatchTheManifest keeps the DefaultIsZero column
// honest in both directions.
//
// It is what stops the guard assertions above from going vacuous. A key marked
// non-zero whose default moves to zero would make its clobber check meaningless;
// a key marked zero whose default becomes non-zero would leave a real clobber
// unchecked. Either way the manifest, not the assertion, is what needs updating.
//
// "Is the default zero" is answered by resolving the key explicitly as 0 and
// comparing, for the same reason the target above does it that way: a literal 0
// carries Go's int type and would never compare equal to a uint or float64 leaf.
func TestGetConfigGuardedKeyDefaultsMatchTheManifest(t *testing.T) {
	absent, err := GetConfig(newAppViper(t, nil))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	absentDump := configtest.Dump(absent)

	for _, row := range guardedKeys {
		absentLeaf, ok := configtest.LeafAt(absentDump, row.Path)
		if !ok {
			t.Errorf("%s: %q is not in the parsed config", row.Key, row.Path)
			continue
		}
		explicitZero, zeroErr := GetConfig(newAppViper(t, map[string]any{row.Key: 0}))
		if zeroErr != nil {
			t.Errorf("%s = 0 must parse, got %v", row.Key, zeroErr)
			continue
		}
		zeroLeaf, ok := configtest.LeafAt(configtest.Dump(explicitZero), row.Path)
		if !ok {
			t.Errorf("%s: %q is not in the parsed config", row.Key, row.Path)
			continue
		}

		if isZero := absentLeaf == zeroLeaf; isZero != row.DefaultIsZero {
			t.Errorf("%s resolves to %s with no key set and %s with an explicit 0, so "+
				"DefaultIsZero is %v while the manifest says %v; update the row so its guard "+
				"assertion still means something",
				row.Key, absentLeaf, zeroLeaf, isZero, row.DefaultIsZero)
		}
	}
}

// TestGetConfigGenesisKeyDivergesFromTheAppSideKey records that the two genesis
// parsers read different keys for the same value. GetConfig reads
// genesis.genesis-stream-file; app/genesis.go reads genesis.import-file. Setting
// one leaves the other empty, so a stream-import node configured through the key
// the template renders streams from "".
func TestGetConfigGenesisKeyDivergesFromTheAppSideKey(t *testing.T) {
	cfg, err := GetConfig(newAppViper(t, map[string]any{
		"genesis.stream-import": true,
		"genesis.import-file":   "/var/lib/sei/genesis.json",
	}))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !cfg.Genesis.StreamImport {
		t.Fatal("genesis.stream-import must resolve; both parsers agree on this key")
	}
	if cfg.Genesis.GenesisStreamFile != "" {
		t.Fatalf("GetConfig read genesis.import-file (%q); it reads genesis-stream-file, and the "+
			"divergence between the two spellings is the pinned behavior",
			cfg.Genesis.GenesisStreamFile)
	}

	withOwnKey, err := GetConfig(newAppViper(t, map[string]any{
		"genesis.genesis-stream-file": "/var/lib/sei/genesis.json",
	}))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if withOwnKey.Genesis.GenesisStreamFile != "/var/lib/sei/genesis.json" {
		t.Fatalf("genesis.genesis-stream-file resolved to %q", withOwnKey.Genesis.GenesisStreamFile)
	}
}

// TestGetConfigStateStoreReadsAreUnguarded records that GetConfig's [state-store]
// parse has no presence checks, matching app/seidb.go's parseSSConfigs. An absent
// section resolves every field to its zero value, so the reported config says
// ss-enable false and an empty backend on a node whose app.toml simply predates the
// section.
func TestGetConfigStateStoreReadsAreUnguarded(t *testing.T) {
	cfg, err := GetConfig(newAppViper(t, nil))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	def := config.DefaultStateStoreConfig()
	if cfg.StateStore.Enable == def.Enable && def.Enable {
		t.Fatal("state-store.ss-enable is no longer clobbered by an absent key; if a guard was " +
			"added, GetConfig and parseSSConfigs must be changed together or they will disagree")
	}
	if cfg.StateStore.Backend == def.Backend && def.Backend != "" {
		t.Fatalf("state-store.ss-backend resolved to the default %q from an absent key", cfg.StateStore.Backend)
	}
	if cfg.StateStore.AsyncWriteBuffer != 0 || cfg.StateStore.KeepRecent != 0 {
		t.Fatalf("absent [state-store] must resolve to zeros, got buffer=%d keep-recent=%d",
			cfg.StateStore.AsyncWriteBuffer, cfg.StateStore.KeepRecent)
	}
}

// FuzzConfigValidateBasic pins the two conditions that reject an otherwise
// parseable app.toml.
//
// An empty minimum-gas-prices fails, because a validator accepting zero-fee
// transactions is a misconfiguration rather than a choice. And pruning
// "everything" with state-sync snapshots enabled fails, because a node cannot
// serve a snapshot of state it has already pruned. Both are the rare case in this
// surface where a bad combination is refused rather than absorbed.
func FuzzConfigValidateBasic(f *testing.F) {
	f.Add("0.01usei", "default", uint64(0))
	f.Add("", "default", uint64(0))
	f.Add("0.01usei", "everything", uint64(100))
	f.Add("0.01usei", "everything", uint64(0))
	f.Add("", "everything", uint64(100))
	f.Add("0.01usei", "nothing", uint64(100))

	f.Fuzz(func(t *testing.T, minGasPrices, pruning string, snapshotInterval uint64) {
		cfg, err := GetConfig(newAppViper(t, map[string]any{
			"minimum-gas-prices":           minGasPrices,
			"pruning":                      pruning,
			"state-sync.snapshot-interval": snapshotInterval,
		}))
		if err != nil {
			t.Fatalf("GetConfig: %v", err)
		}

		wantErr := minGasPrices == "" || (pruning == "everything" && snapshotInterval > 0)
		got := cfg.ValidateBasic(nil)
		if wantErr && got == nil {
			t.Fatalf("min-gas-prices=%q pruning=%q snapshot-interval=%d must fail ValidateBasic",
				minGasPrices, pruning, snapshotInterval)
		}
		if !wantErr && got != nil {
			t.Fatalf("min-gas-prices=%q pruning=%q snapshot-interval=%d must pass ValidateBasic, got %v",
				minGasPrices, pruning, snapshotInterval, got)
		}
	})
}

// TestDefaultsMatchTheRecordedValues pins the server_config defaults themselves.
//
// The absent-keys coverage in this file proves the reader returns the declared defaults; it
// cannot prove which values those are, because both sides of that comparison come from the
// same package. This compares them against testdata/server_config.golden, an independent
// recording, so a default that moves shows the new value in a diff instead of passing
// silently.
func TestDefaultsMatchTheRecordedValues(t *testing.T) {
	configtest.CheckDefaults(t, "server_config", DefaultConfig(),
		configtest.DerivedDefault{
			Path: "ConcurrencyWorkers", Want: max(10, min(runtime.NumCPU()*2, 128)),
			Why: "max(10, min(runtime.NumCPU()*2, 128))",
		},
	)
}
