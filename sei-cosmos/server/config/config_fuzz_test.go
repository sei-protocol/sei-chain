package config

import (
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
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
			// row with a message about the clamp rather than about the encoding, so a value
			// with no faithful text form is declined instead.
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

// stateSyncKeys is the [state-sync] manifest as GetConfig resolves it.
//
// The section is read three ways and this describes one of them — counted by mechanism, because
// a count of readers goes stale: simd is a fourth call site (sei-ibc-go/testing/simapp/simd/cmd/
// root.go:273-274) and a second instance of the first mechanism, not a fourth way. NewApp reads
// the same three keys out of an AppOpts and hands them to a baseapp (cmd/seid/cmd/root.go:255
// and :304-306), which no row can predict. ParseConfig (toml.go:272-277) unmarshals them by mapstructure tag
// over a DefaultConfig base, so an absent snapshot-keep-recent keeps 2 where this reader
// returns 0 — a second describable reader, undescribed. GetConfig reads them as literals
// through one unguarded typed getter each, which is the shape CheckRow holds a reader to.
//
// All three reads are unguarded and one of them clobbers, which is why this section gets no
// CheckAbsent: an empty viper does not resolve to DefaultConfig's [state-sync].
// TestGetConfigStateSyncReadsAreUnguarded pins that divergence directly.
var stateSyncKeys = []configtest.KeySpec{
	{
		Key: "state-sync.snapshot-interval", Path: "SnapshotInterval",
		Cast: configtest.CastUint64, Unguarded: true,
		Why: "0 is both the in-code default and \"disabled\", so what this row protects is an " +
			"explicit interval reaching the field rather than an absent one: it is the only " +
			"[state-sync] key with a consumer inside this Config (ValidateBasic, config.go:655), " +
			"and a serving node's cadence is NewApp's read (root.go:304)",
	},
	{
		Key: "state-sync.snapshot-keep-recent", Path: "SnapshotKeepRecent",
		Cast: configtest.CastUint32, Unguarded: true,
		Why: "the in-code default is 2 and toml.go:78 documents 0 as keep all, so the clobber " +
			"inverts the declared retention; what a node retains is NewApp's read (root.go:305)",
	},
	{
		Key: "state-sync.snapshot-directory", Path: "SnapshotDirectory",
		Cast: configtest.CastString, Unguarded: true,
		Why: "toml.go:82 documents empty as store under the home directory, and the fallback that " +
			"implements it is NewApp's read (root.go:255-257) rather than this one",
	},
}

// readStateSync drives GetConfig from an AppOpts, which is what lets the manifest engine
// describe a viper-based reader.
//
// The two transports differ by an adapter and not by a wall: newAppViper already takes the
// map[string]any an AppOpts is, so a row's key and raw value reach v.Set and then the same
// typed getter an app.toml value reaches. It returns the section rather than the whole
// Config so a row's Path names its field the way every other section's rows do, which is
// also what lets CheckManifestCoversEveryField point at StateSyncConfig alone.
func readStateSync(t testing.TB) func(configtest.AppOpts) (any, error) {
	return func(opts configtest.AppOpts) (any, error) {
		cfg, err := GetConfig(newAppViper(t, opts))
		if err != nil {
			return nil, err
		}
		return cfg.StateSync, nil
	}
}

// FuzzGetConfigStateSync drives the [state-sync] manifest.
//
// Two of the three keys had no assertion on a resolved value anywhere in the tree: cmd/seid/cmd
// records snapshot-keep-recent's and snapshot-directory's spelling and cannot predict what NewApp
// does with them, and the whole-Config defaults golden says what DefaultConfig declares rather
// than what a parse returns. snapshot-interval was already held twice — FuzzConfigValidateBasic
// below drives it through GetConfig with a wantErr that is a function of its resolved value, and
// appKeys[5] in cmd/seid/cmd's FuzzApplyPrecedenceApp holds it to a resolved value and Go type
// across every layer combination that sets it — so for that key this target adds arbitrary values
// rather than a first assertion.
func FuzzGetConfigStateSync(f *testing.F) {
	read := readStateSync(f)
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)

	// Rows 0 and 1 get three seeds and row 2 gets two, and only the first seed of each row
	// discriminates. An unguarded read resolves an absent key to its cast's zero, and every one
	// of these rows resolves to that same zero from an absent key, so a nil seed lands on the
	// absent-key value and so does a value the cast rejects: those pin the clobber and, on the
	// two numeric rows, the swallowed conversion. Row 2 stops at two because a string cast has
	// no malformed input, so there is no swallowed conversion to pin there. A value that
	// converts to something else is what states the key is read at all.
	seeds.AddRow(uint(0), fuzzing.KindInt64, "", int64(1000), false)
	seeds.AddRow(uint(0), fuzzing.KindNil, "", int64(0), false)
	seeds.AddRow(uint(0), fuzzing.KindString, "not-a-number", int64(0), false)
	seeds.AddRow(uint(1), fuzzing.KindInt64, "", int64(10), false)
	seeds.AddRow(uint(1), fuzzing.KindNil, "", int64(0), false)
	seeds.AddRow(uint(1), fuzzing.KindString, "not-a-number", int64(0), false)
	seeds.AddRow(uint(2), fuzzing.KindString, "/var/lib/sei/snapshots", int64(0), false)
	seeds.AddRow(uint(2), fuzzing.KindNil, "", int64(0), false)

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "state-sync", read, stateSyncKeys, seeds)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(stateSyncKeys, keyIdx)
		configtest.CheckRow(t, "state-sync", readStateSync(t), spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// TestGetConfigStateSyncReadsAreUnguarded records the [state-sync] clobber as a divergence
// rather than as two facts that happen to disagree.
//
// snapshot-keep-recent is a registered flag defaulting to 2 (server/start.go:234) and
// DefaultConfig declares 2, but GetConfig reads it with a bare v.GetUint32, so a viper that
// never saw the key resolves 0 — which toml.go:78 documents as "keep all". That viper is this
// file's layer and not a booted node's: start.go:117 binds the flag in PreRunE, ahead of both
// production calls (start.go:168 and :303), so there the same read takes the flag's 2 whenever
// app.toml is silent.
//
// It is recorded and not repaired, because a characterization PR does not change readers. The
// guard is not hypothetical: ParseConfig already resolves this section that way and returns 2 for
// the absent key this read returns 0 for. The guard would not change the fleet either: with the
// flag bound IsSet is false, so a guarded read falls back to the in-code 2 the unguarded read
// already takes from the flag default. The point of the assertion is that either side moving fails
// it — a guard makes the absent read return 2, and a default moved to 0 makes the clobber stop
// being one — so the divergence can neither close nor widen without a diff.
func TestGetConfigStateSyncReadsAreUnguarded(t *testing.T) {
	cfg, err := GetConfig(newAppViper(t, nil))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	def := DefaultConfig().StateSync
	if def.SnapshotKeepRecent == 0 {
		t.Fatal("state-sync.snapshot-keep-recent's in-code default is now 0, so an absent key " +
			"clobbers nothing and this recording no longer describes a divergence. If the default " +
			"moved deliberately, say what a serving node now retains — 0 is keep-all, not keep-none")
	}
	if cfg.StateSync.SnapshotKeepRecent != 0 {
		t.Fatalf("an absent state-sync.snapshot-keep-recent resolved to %d rather than 0, so the "+
			"read is no longer unguarded. That is a fine end state and a no-op for a booted node, "+
			"which takes 2 from the bound flag either way (start.go:117 and :234); what it changes "+
			"is this recording, so update the row and this assertion in the PR that adds the guard",
			cfg.StateSync.SnapshotKeepRecent)
	}
	if cfg.StateSync.SnapshotInterval != 0 || cfg.StateSync.SnapshotDirectory != "" {
		t.Fatalf("absent [state-sync] must resolve to zeros, got interval=%d directory=%q",
			cfg.StateSync.SnapshotInterval, cfg.StateSync.SnapshotDirectory)
	}
}

// TestParseConfigAndGetConfigDisagree pins the disagreement between this package's two exported
// readers of [state-sync], which is what the guard above would close.
//
// Handed the same flagless viper, ParseConfig unmarshals over a DefaultConfig base and keeps
// snapshot-keep-recent at 2 while GetConfig's bare v.GetUint32 resolves 0. That is what makes the
// deferral above a deferral rather than an open question: the guard is not a retention anyone has
// to choose, it is ParseConfig's resolution moved into GetConfig, and this states the behavior
// being moved. Nothing else in the tree does — TestParseConfig (config_test.go:358) asserts only
// MinGasPrices.
//
// The disagreement is between the two readers and not between two nodes. GetConfig's production
// calls (start.go:168 and :303) run with the flag bound, where it takes the same 2, and the one
// non-test caller of ParseConfig is server/util.go:308's empty-template branch, which no binary in
// this tree reaches. So what fails when either side moves is the rationale, which is the point.
func TestParseConfigAndGetConfigDisagree(t *testing.T) {
	v := newAppViper(t, nil)
	parsed, err := ParseConfig(v)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	direct, err := GetConfig(v)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if parsed.StateSync.SnapshotKeepRecent != 2 || direct.StateSync.SnapshotKeepRecent != 0 {
		t.Fatalf("an absent state-sync.snapshot-keep-recent resolved to %d through ParseConfig and "+
			"%d through GetConfig, want 2 and 0. If GetConfig gained the guard the two readers now "+
			"agree, which is the end state — update this test, the row and the deferral note "+
			"together. If ParseConfig stopped resolving the section over DefaultConfig, the deferral "+
			"note above is now wrong about there being an implemented guard to copy, and whoever "+
			"adds one is choosing a retention rather than matching one. A third cause is that the "+
			"in-code default moved off 2, which the defaults golden names and this literal does "+
			"not: a default of 3 reaches here rather than the unguarded-read check, whose guard "+
			"fires only at exactly 0",
			parsed.StateSync.SnapshotKeepRecent, direct.StateSync.SnapshotKeepRecent)
	}
}

// TestKeyNamesMatchTheRecordedNames records the [state-sync] spellings GetConfig looks up.
//
// The rows name them as literals, so a rename here fails the row assertions as well; the
// record is what makes the diff name the old and the new operator-facing key rather than a
// resolved value. cmd/seid/cmd holds its own state-sync record for NewApp's two constants,
// and the two files are independent because the readers they describe are.
func TestKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "state-sync", stateSyncKeys)
}

// TestManifestNamesEveryField enforces the claim stateSyncKeys makes about itself.
//
// StateSyncConfig is the one struct in this file's surface with a single reader populating
// it, so the check costs no exemptions: a fourth [state-sync] key added to GetConfig fails
// here until it has a row. The [state-commit] and [state-store] structs are shared with
// app/seidb.go and are not assertable this way — see app/config_fuzz_test.go.
func TestManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "state-sync", DefaultConfig().StateSync, stateSyncKeys)
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
	// [state-sync] has its own manifest and its own struct, so it gets its own record. The three
	// values are also inside server_config.golden, which does catch a change to them, so this is for
	// discoverability rather than detection. A reader asking what [state-sync] defaults to reads three
	// lines here instead of finding them among two hundred. Regenerating one of the two records without
	// the other leaves that other one red.
	configtest.CheckDefaults(t, "state-sync", DefaultConfig().StateSync)

	configtest.CheckDefaults(t, "server_config", DefaultConfig(),
		configtest.DerivedDefault{
			Path: "ConcurrencyWorkers", Want: max(10, min(runtime.NumCPU()*2, 128)),
			Why: "max(10, min(runtime.NumCPU()*2, 128))",
		},
	)
}

// apiKeys covers the [api] keys GetConfig reads.
//
// Every read is a bare viper getter with no IsSet guard (config.go:579-586), so an absent key
// resolves to that getter's zero rather than to what DefaultConfig declares. Unlike [state-sync],
// no api.* flag is registered anywhere in this tree, so nothing supplies a fallback and the zero
// is what a node gets. TestGetConfigAbsentSectionDivergences records which fields that changes.
var apiKeys = []configtest.KeySpec{
	{
		Key: "api.enable", Path: "Enable", Cast: configtest.CastBool, Unguarded: true,
		Why: "whether the node serves the REST API at all; false is also the declared default, so " +
			"this row states the key is read rather than recording a divergence",
	},
	{
		Key: "api.swagger", Path: "Swagger", Cast: configtest.CastBool, Unguarded: true,
		Why: "the declared default is true and an absent key resolves false, so a node whose " +
			"app.toml lacks the section serves the API without its documentation",
	},
	{
		Key: "api.enabled-unsafe-cors", Path: "EnableUnsafeCORS", Cast: configtest.CastBool,
		Unguarded: true,
		Why: "cross-origin access to the REST API; false either way, so the clobber cannot turn " +
			"this on, which is the direction that would matter",
	},
	{
		Key: "api.address", Path: "Address", Cast: configtest.CastString, Unguarded: true,
		Why: "the declared default is tcp://0.0.0.0:1317 and an absent key resolves empty, so the " +
			"listener address a node binds comes from the file or from nowhere",
	},
	{
		Key: "api.max-open-connections", Path: "MaxOpenConnections", Cast: configtest.CastUint,
		Unguarded: true,
		Why: "the declared default is 1000 and an absent key resolves 0, so the connection ceiling " +
			"a node enforces is whichever of those the server treats as a limit",
	},
	{
		Key: "api.rpc-read-timeout", Path: "RPCReadTimeout", Cast: configtest.CastUint,
		Unguarded: true,
		Why: "the declared default is 10 seconds and an absent key resolves 0, so a node whose " +
			"app.toml lacks the section reads a request body with no deadline",
	},
	{
		Key: "api.rpc-write-timeout", Path: "RPCWriteTimeout", Cast: configtest.CastUint,
		Unguarded: true,
		Why: "0 is both the declared default and what an absent key resolves to, so this row " +
			"states the key is read rather than recording a divergence",
	},
	{
		Key: "api.rpc-max-body-bytes", Path: "RPCMaxBodyBytes", Cast: configtest.CastUint,
		Unguarded: true,
		Why: "the declared default is 1000000 and an absent key resolves 0, so the response body " +
			"ceiling a node applies is whichever of those the server treats as a limit",
	},
}

func readAPI(t testing.TB) func(configtest.AppOpts) (any, error) {
	return sectionOfGetConfig(t, func(c Config) any { return c.API })
}

// FuzzAPIConfig drives every [api] row.
//
// Three seeds per row, uniformly. Every row here is unguarded, so an absent key, a nil value and a
// value the cast rejects all land on the same zero, and only a value that converts to something else
// states the key is read at all. The nil and malformed pair comes from seedEveryRow, the same shape
// the other five sections use.
func FuzzAPIConfig(f *testing.F) {
	read := readAPI(f)
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
	seedEveryRow(seeds, len(apiKeys))

	// The discriminating value per row, each chosen away from the value an absent key resolves to, which
	// is what CheckEveryRowHasADiscriminatingSeed holds them to. For a bool whose declared default is
	// not the zero that is the default itself, since a bool has only the two.
	seeds.AddRow(uint(0), fuzzing.KindBool, "", int64(0), true) // enable
	seeds.AddRow(uint(1), fuzzing.KindBool, "", int64(0), true) // swagger
	seeds.AddRow(uint(2), fuzzing.KindBool, "", int64(0), true) // unsafe CORS
	seeds.AddRow(uint(3), fuzzing.KindString, "tcp://127.0.0.1:11317", int64(0), false)
	seeds.AddRow(uint(4), fuzzing.KindInt64, "", int64(250), false)     // max-open-connections
	seeds.AddRow(uint(5), fuzzing.KindInt64, "", int64(30), false)      // rpc-read-timeout
	seeds.AddRow(uint(6), fuzzing.KindInt64, "", int64(45), false)      // rpc-write-timeout
	seeds.AddRow(uint(7), fuzzing.KindInt64, "", int64(2000000), false) // rpc-max-body-bytes

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "api", read, apiKeys, seeds)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(apiKeys, keyIdx)
		configtest.CheckRow(t, "api", readAPI(t), spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// TestAPIKeyNamesMatchTheRecordedNames pins the operator-facing spelling of the eight [api] keys.
func TestAPIKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "api", apiKeys)
}

// TestAPIManifestNamesEveryField enforces the claim apiKeys makes about itself, that it names every
// key the reader looks up.
func TestAPIManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "api", DefaultConfig().API, apiKeys)
}

// rosetta covers the [rosetta] keys GetConfig reads. Every read is a bare viper getter
// (config.go:589-594), so an absent key resolves to that getter's zero.
var rosettaKeys = []configtest.KeySpec{
	{
		Key: "rosetta.enable", Path: "Enable", Cast: configtest.CastBool, Unguarded: true,
		Why: "whether the node serves the Rosetta API; false either way, so this row states the " +
			"key is read rather than recording a divergence",
	},
	{
		Key: "rosetta.address", Path: "Address", Cast: configtest.CastString, Unguarded: true,
		Why: "the declared default is :8080 and an absent key resolves empty, so the listener " +
			"address comes from the file or from nowhere",
	},
	{
		Key: "rosetta.blockchain", Path: "Blockchain", Cast: configtest.CastString, Unguarded: true,
		Why: "the declared default is app and an absent key resolves empty, so the blockchain name " +
			"Rosetta reports identifies nothing",
	},
	{
		Key: "rosetta.network", Path: "Network", Cast: configtest.CastString, Unguarded: true,
		Why: "the declared default is network and an absent key resolves empty, so the network name " +
			"Rosetta reports identifies nothing",
	},
	{
		Key: "rosetta.retries", Path: "Retries", Cast: configtest.CastInt, Unguarded: true,
		Why: "the declared default is 3 and an absent key resolves 0, so a node retries a failed " +
			"Rosetta operation as many times as its file says and no more",
	},
	{
		Key: "rosetta.offline", Path: "Offline", Cast: configtest.CastBool, Unguarded: true,
		Why: "whether Rosetta runs without a live node; false either way, so this row states the " +
			"key is read rather than recording a divergence",
	},
}

// grpcWebKeys covers the [grpc-web] keys GetConfig reads.
//
// Three of the four are bare getters. max-open-connections is not: config.go:514-517 reads it
// behind v.IsSet and falls back to the in-code default, with a comment saying the guard is there so
// a node upgrading with an older app.toml stays bounded. That is the same hazard
// api.max-open-connections and api.rpc-max-body-bytes carry unguarded, which is why this section is
// worth reading beside apiKeys rather than on its own.
var grpcWebKeys = []configtest.KeySpec{
	{
		Key: "grpc-web.enable", Path: "Enable", Cast: configtest.CastBool, Unguarded: true,
		Why: "the declared default is true and an absent key resolves false, so a node whose " +
			"app.toml lacks the section serves no gRPC-Web",
	},
	{
		Key: "grpc-web.address", Path: "Address", Cast: configtest.CastString, Unguarded: true,
		Why: "the declared default is 0.0.0.0:9091 and an absent key resolves empty",
	},
	{
		Key: "grpc-web.enable-unsafe-cors", Path: "EnableUnsafeCORS", Cast: configtest.CastBool,
		Unguarded: true,
		Why: "cross-origin access to gRPC-Web; false either way, so the clobber cannot turn this " +
			"on, which is the direction that would matter",
	},
	{
		Key: "grpc-web.max-open-connections", Path: "MaxOpenConnections", Cast: configtest.CastUint,
		Why: "the one guarded read in this section (config.go:514-517), so an absent key keeps the " +
			"declared 1000 rather than resolving 0; the guard exists so an upgrading node stays bounded",
	},
}

// telemetryKeys covers the [telemetry] keys GetConfig reads as scalars.
//
// global-labels is not a row. It is read as a bare type assertion whose absence fails GetConfig
// outright and whose shape rules are their own subject, so it has dedicated targets above
// (FuzzGetConfigGlobalLabels, TestGetConfigRequiresGlobalLabels, TestGetConfigPanicsOnNonStringLabel)
// and is recorded by name rather than driven as a row.
var telemetryKeys = []configtest.KeySpec{
	{
		Key: "telemetry.service-name", Path: "ServiceName", Cast: configtest.CastString,
		Unguarded: true,
		Why:       "empty either way, so this row states the key is read rather than recording a divergence",
	},
	{
		Key: "telemetry.enabled", Path: "Enabled", Cast: configtest.CastBool, Unguarded: true,
		Why: "the declared default is true and an absent key resolves false, so a node whose " +
			"app.toml lacks the section emits no telemetry",
	},
	{
		Key: "telemetry.enable-hostname", Path: "EnableHostname", Cast: configtest.CastBool,
		Unguarded: true,
		Why:       "false either way, so this row states the key is read",
	},
	{
		Key: "telemetry.enable-hostname-label", Path: "EnableHostnameLabel",
		Cast: configtest.CastBool, Unguarded: true,
		Why: "false either way, so this row states the key is read",
	},
	{
		Key: "telemetry.enable-service-label", Path: "EnableServiceLabel", Cast: configtest.CastBool,
		Unguarded: true,
		Why:       "false either way, so this row states the key is read",
	},
	{
		Key: "telemetry.prometheus-retention-time", Path: "PrometheusRetentionTime",
		Cast: configtest.CastInt64, Unguarded: true,
		Why: "the declared default is 7200 seconds and an absent key resolves 0, which telemetry " +
			"reads as retaining nothing, so a scrape finds an empty store",
	},
}

// telemetryKeysWithTargetsOfTheirOwn is global-labels, recorded for its name because its behaviour
// is driven by targets rather than by a row.
var telemetryKeysWithTargetsOfTheirOwn = []configtest.KeyName{"telemetry.global-labels"}

func readRosetta(t testing.TB) func(configtest.AppOpts) (any, error) {
	return sectionOfGetConfig(t, func(c Config) any { return c.Rosetta })
}

func readGRPCWeb(t testing.TB) func(configtest.AppOpts) (any, error) {
	return sectionOfGetConfig(t, func(c Config) any { return c.GRPCWeb })
}

func readTelemetry(t testing.TB) func(configtest.AppOpts) (any, error) {
	return sectionOfGetConfig(t, func(c Config) any { return c.Telemetry })
}

// sectionOfGetConfig adapts GetConfig to the reader shape the checks take, for one section.
//
// One helper rather than a function per section, because every one of these differs only in which
// field it returns, and a per-section copy is a place for the newAppViper call to drift.
func sectionOfGetConfig(t testing.TB, section func(Config) any) func(configtest.AppOpts) (any, error) {
	return func(opts configtest.AppOpts) (any, error) {
		cfg, err := GetConfig(newAppViper(t, opts))
		if err != nil {
			return nil, err
		}
		return section(cfg), nil
	}
}

// seedEveryRow gives each row a nil and a malformed seed, which is the pair every unguarded section
// needs so an ordinary go test run reaches the clobber and the swallowed conversion.
func seedEveryRow(seeds *configtest.Seeds, rows int) {
	for i := range rows {
		seeds.AddRow(uint(i), fuzzing.KindNil, "", int64(0), false)
		seeds.AddRow(uint(i), fuzzing.KindString, "not-a-value", int64(0), false)
	}
}

func FuzzRosettaConfig(f *testing.F) {
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
	seedEveryRow(seeds, len(rosettaKeys))

	// One discriminating value per row, away from the value an absent key resolves to.
	seeds.AddRow(uint(0), fuzzing.KindBool, "", int64(0), true)
	seeds.AddRow(uint(1), fuzzing.KindString, ":18080", int64(0), false)
	seeds.AddRow(uint(2), fuzzing.KindString, "sei-app", int64(0), false)
	seeds.AddRow(uint(3), fuzzing.KindString, "sei-network", int64(0), false)
	seeds.AddRow(uint(4), fuzzing.KindInt64, "", int64(9), false)
	seeds.AddRow(uint(5), fuzzing.KindBool, "", int64(0), true)

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "rosetta", readRosetta(f), rosettaKeys, seeds)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(rosettaKeys, keyIdx)
		configtest.CheckRow(t, "rosetta", readRosetta(t), spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

func FuzzGRPCWebConfig(f *testing.F) {
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
	seedEveryRow(seeds, len(grpcWebKeys))

	seeds.AddRow(uint(0), fuzzing.KindBool, "", int64(0), true)
	seeds.AddRow(uint(1), fuzzing.KindString, "127.0.0.1:19091", int64(0), false)
	seeds.AddRow(uint(2), fuzzing.KindBool, "", int64(0), true)
	seeds.AddRow(uint(3), fuzzing.KindInt64, "", int64(250), false)

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "grpc-web", readGRPCWeb(f), grpcWebKeys, seeds)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(grpcWebKeys, keyIdx)
		configtest.CheckRow(t, "grpc-web", readGRPCWeb(t), spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

func FuzzTelemetryConfig(f *testing.F) {
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
	seedEveryRow(seeds, len(telemetryKeys))

	seeds.AddRow(uint(0), fuzzing.KindString, "sei-node", int64(0), false)
	seeds.AddRow(uint(1), fuzzing.KindBool, "", int64(0), true)
	seeds.AddRow(uint(2), fuzzing.KindBool, "", int64(0), true)
	seeds.AddRow(uint(3), fuzzing.KindBool, "", int64(0), true)
	seeds.AddRow(uint(4), fuzzing.KindBool, "", int64(0), true)
	seeds.AddRow(uint(5), fuzzing.KindInt64, "", int64(3600), false)

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "telemetry", readTelemetry(f), telemetryKeys,
		seeds, telemetryKeysWithTargetsOfTheirOwn...)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(telemetryKeys, keyIdx)
		configtest.CheckRow(t, "telemetry", readTelemetry(t), spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

func TestRosettaKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "rosetta", rosettaKeys)
}

func TestGRPCWebKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "grpc-web", grpcWebKeys)
}

func TestTelemetryKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "telemetry", telemetryKeys, telemetryKeysWithTargetsOfTheirOwn...)
}

func TestRosettaManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "rosetta", DefaultConfig().Rosetta, rosettaKeys)
}

func TestGRPCWebManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "grpc-web", DefaultConfig().GRPCWeb, grpcWebKeys)
}

func TestTelemetryManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "telemetry", DefaultConfig().Telemetry, telemetryKeys,
		// FuzzGetConfigGlobalLabels drives this field; it is not a plain guarded cast.
		"GlobalLabels",
	)
}

// TestGetConfigAbsentSectionDivergences records every field these sections resolve away from its
// declared default when the section is absent from app.toml.
//
// One table for all of them, because the divergence is one property and a reader comparing sections
// wants them side by side. It puts api.max-open-connections and api.rpc-max-body-bytes beside the
// guarded grpc-web.max-open-connections, which is the contrast worth seeing.
//
// The diverges column asserts both directions, so a key is anchored whether or not it moves today. A
// declared default later shifting onto the getter's zero, or off it, fails here rather than changing
// the divergence set quietly. The rows set false are the keys whose declared default already equals
// that zero.
//
// Every plain cast across the six sections is here. None of these sections is wired to CheckAbsent,
// so this table is the only thing tying their absent-key resolution to their declared defaults.
//
// Compared with reflect.DeepEqual rather than !=, because != on two any values panics rather than
// reporting when either side holds a slice or a map. index-events already holds a []string, and a
// field type changing to one later would otherwise turn this table into a panic.
func TestGetConfigAbsentSectionDivergences(t *testing.T) {
	cfg, err := GetConfig(newAppViper(t, nil))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	def := DefaultConfig()

	covered := map[string]bool{}

	for _, c := range []struct {
		key              string
		absent, declared any
		diverges         bool
	}{
		{"rosetta.address", cfg.Rosetta.Address, def.Rosetta.Address, true},
		{"rosetta.blockchain", cfg.Rosetta.Blockchain, def.Rosetta.Blockchain, true},
		{"rosetta.network", cfg.Rosetta.Network, def.Rosetta.Network, true},
		{"rosetta.retries", cfg.Rosetta.Retries, def.Rosetta.Retries, true},
		{"grpc-web.enable", cfg.GRPCWeb.Enable, def.GRPCWeb.Enable, true},
		{"grpc-web.address", cfg.GRPCWeb.Address, def.GRPCWeb.Address, true},
		// The [grpc] keys read as plain casts. keepalive-permit-without-stream is the third, further
		// down with the other rows whose default is already the zero. Its remaining eight are guarded
		// or clamped and held by TestGetConfigGRPCAbsentReads.
		{"grpc.enable", cfg.GRPC.Enable, def.GRPC.Enable, true},
		{"grpc.address", cfg.GRPC.Address, def.GRPC.Address, true},
		{"telemetry.enabled", cfg.Telemetry.Enabled, def.Telemetry.Enabled, true},
		{
			"telemetry.prometheus-retention-time",
			cfg.Telemetry.PrometheusRetentionTime, def.Telemetry.PrometheusRetentionTime, true,
		},

		// [api]. Five diverge. The three set false have a declared default that is already the
		// getter's zero, so nothing about the resolved value distinguishes a guard from its absence.
		{"api.swagger", cfg.API.Swagger, def.API.Swagger, true},
		{"api.address", cfg.API.Address, def.API.Address, true},
		{"api.max-open-connections", cfg.API.MaxOpenConnections, def.API.MaxOpenConnections, true},
		{"api.rpc-read-timeout", cfg.API.RPCReadTimeout, def.API.RPCReadTimeout, true},
		{"api.rpc-max-body-bytes", cfg.API.RPCMaxBodyBytes, def.API.RPCMaxBodyBytes, true},
		{"api.enable", cfg.API.Enable, def.API.Enable, false},
		{"api.enabled-unsafe-cors", cfg.API.EnableUnsafeCORS, def.API.EnableUnsafeCORS, false},
		{"api.rpc-write-timeout", cfg.API.RPCWriteTimeout, def.API.RPCWriteTimeout, false},

		// The top-level keys, written with no section header. occ-enabled resolving false runs a node
		// without optimistic concurrency control, and minimum-gas-prices resolving empty is the
		// spelling for accepting a transaction at any fee.
		{"minimum-gas-prices", cfg.MinGasPrices, def.MinGasPrices, true},
		{"inter-block-cache", cfg.InterBlockCache, def.InterBlockCache, true},
		{"pruning", cfg.Pruning, def.Pruning, true},
		{"pruning-keep-recent", cfg.PruningKeepRecent, def.PruningKeepRecent, true},
		{"pruning-interval", cfg.PruningInterval, def.PruningInterval, true},
		{"concurrency-workers", cfg.ConcurrencyWorkers, def.ConcurrencyWorkers, true},
		{"occ-enabled", cfg.OccEnabled, def.OccEnabled, true},
		{"halt-height", cfg.HaltHeight, def.HaltHeight, false},
		{"halt-time", cfg.HaltTime, def.HaltTime, false},
		{"min-retain-blocks", cfg.MinRetainBlocks, def.MinRetainBlocks, false},
		{"compaction-interval", cfg.CompactionInterval, def.CompactionInterval, false},

		// The remaining plain casts. Every row from here down has a declared default equal to its
		// getter's zero, so none diverges today, and each is here for the reason api.enable is. This
		// table is the only thing tying these sections' absent-key resolution to their declared
		// defaults, since none of them is wired to CheckAbsent.
		{"rosetta.enable", cfg.Rosetta.Enable, def.Rosetta.Enable, false},
		{"rosetta.offline", cfg.Rosetta.Offline, def.Rosetta.Offline, false},
		{"grpc-web.enable-unsafe-cors", cfg.GRPCWeb.EnableUnsafeCORS, def.GRPCWeb.EnableUnsafeCORS, false},
		{
			"grpc.keepalive-permit-without-stream",
			cfg.GRPC.KeepalivePermitWithoutStream, def.GRPC.KeepalivePermitWithoutStream, false,
		},
		{"telemetry.service-name", cfg.Telemetry.ServiceName, def.Telemetry.ServiceName, false},
		{"telemetry.enable-hostname", cfg.Telemetry.EnableHostname, def.Telemetry.EnableHostname, false},
		{
			"telemetry.enable-hostname-label",
			cfg.Telemetry.EnableHostnameLabel, def.Telemetry.EnableHostnameLabel, false,
		},
		{
			"telemetry.enable-service-label",
			cfg.Telemetry.EnableServiceLabel, def.Telemetry.EnableServiceLabel, false,
		},

		// index-events resolves to a []string, which is why the comparison is reflect.DeepEqual rather
		// than !=. Both sides are nil today, so it is a false row.
		{"index-events", cfg.IndexEvents, def.IndexEvents, false},
		// The guarded read. Its absent value is the declared default, which is the property the
		// guard exists to provide.
		{
			"grpc-web.max-open-connections",
			cfg.GRPCWeb.MaxOpenConnections, def.GRPCWeb.MaxOpenConnections, false,
		},
	} {
		covered[c.key] = true
		if got := !reflect.DeepEqual(c.absent, c.declared); got != c.diverges {
			verb := "no longer diverges from"
			if !c.diverges {
				verb = "now diverges from"
			}
			t.Errorf("%s %s its declared default: absent=%v declared=%v. If a guard was added or "+
				"removed, or a default moved onto the getter's zero, update the row and this table "+
				"in the same PR", c.key, verb, c.absent, c.declared)
		}
	}

	requireEveryManifestRowIsAnchored(t, covered)
}

// requireEveryManifestRowIsAnchored holds the table above to the manifests it is meant to anchor.
//
// The rows are hand-listed, because the diverges column is a judgement about each key that nothing
// derives. What can be derived is which keys need a row at all, and this does that: a key added to any
// of the six manifests gets a row, a seed and a name record from the checks already wired, and would
// otherwise get no absent-key entry while this table is stated to be the only thing tying these
// sections to their declared defaults.
func requireEveryManifestRowIsAnchored(t *testing.T, covered map[string]bool) {
	t.Helper()

	var missing []string
	for _, manifest := range [][]configtest.KeySpec{
		apiKeys, rosettaKeys, grpcWebKeys, telemetryKeys, grpcKeys, baseConfigKeys,
	} {
		for _, spec := range manifest {
			if !covered[spec.Key] {
				missing = append(missing, spec.Key)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("these manifest rows have no entry in the absent-key table above, so nothing ties "+
			"their absent-key resolution to the declared defaults:\n  %s\n"+
			"Add a row with the diverges value the key actually has, which is a decision rather than "+
			"something this can fill in.", strings.Join(missing, "\n  "))
	}
}

// baseConfigKeys covers the twelve keys GetConfig reads at the top level of app.toml, the ones
// written without a section header (config.go:555-568). Every one is a bare viper getter.
var baseConfigKeys = []configtest.KeySpec{
	{
		Key: "minimum-gas-prices", Path: "MinGasPrices", Cast: configtest.CastString,
		Unguarded: true,
		Why: "the declared default is 0.01usei and an absent key resolves empty, which is the " +
			"spelling for accepting a transaction at any fee",
	},
	{
		Key: "inter-block-cache", Path: "InterBlockCache", Cast: configtest.CastBool,
		Unguarded: true,
		Why: "the declared default is true and an absent key resolves false, so the node reads " +
			"every store access from disk",
	},
	{
		Key: "pruning", Path: "Pruning", Cast: configtest.CastString, Unguarded: true,
		Why: "the declared default is nothing, meaning keep all history, and an absent key resolves " +
			"empty, which is not one of the strategy names",
	},
	{
		Key: "pruning-keep-recent", Path: "PruningKeepRecent", Cast: configtest.CastString,
		Unguarded: true,
		Why:       "the declared default is the string 0 and an absent key resolves empty",
	},
	{
		Key: "pruning-interval", Path: "PruningInterval", Cast: configtest.CastString,
		Unguarded: true,
		Why:       "the declared default is the string 0 and an absent key resolves empty",
	},
	{
		Key: "halt-height", Path: "HaltHeight", Cast: configtest.CastUint64, Unguarded: true,
		Why: "0 is both the declared default and the spelling for never halting, so this row states " +
			"the key is read rather than recording a divergence",
	},
	{
		Key: "halt-time", Path: "HaltTime", Cast: configtest.CastUint64, Unguarded: true,
		Why: "0 is both the declared default and the spelling for never halting",
	},
	{
		Key: "index-events", Path: "IndexEvents", Cast: configtest.CastStringSlice, Unguarded: true,
		Why: "which events the node indexes; nil either way, and the only slice-cast row here, so " +
			"it is where a value the cast turns into a one-element slice would show up",
	},
	{
		Key: "min-retain-blocks", Path: "MinRetainBlocks", Cast: configtest.CastUint64,
		Unguarded: true,
		Why:       "0 is both the declared default and the spelling for retaining everything",
	},
	{
		Key: "compaction-interval", Path: "CompactionInterval", Cast: configtest.CastUint64,
		Unguarded: true,
		Why:       "0 is both the declared default and the spelling for never compacting",
	},
	{
		Key: "concurrency-workers", Path: "ConcurrencyWorkers", Cast: configtest.CastInt,
		Unguarded: true,
		Why: "the declared default is derived from the machine and an absent key resolves 0, so the " +
			"worker count a node runs with comes from the file or is nothing",
	},
	{
		Key: "occ-enabled", Path: "OccEnabled", Cast: configtest.CastBool, Unguarded: true,
		Why: "the declared default is true and an absent key resolves false, so a node whose " +
			"app.toml lacks the key executes without optimistic concurrency control",
	},
}

func readBaseConfig(t testing.TB) func(configtest.AppOpts) (any, error) {
	return sectionOfGetConfig(t, func(c Config) any { return c.BaseConfig })
}

func FuzzBaseConfig(f *testing.F) {
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
	seedEveryRow(seeds, len(baseConfigKeys))

	// One discriminating value per row, away from the value an absent key resolves to.
	seeds.AddRow(uint(0), fuzzing.KindString, "0.5usei", int64(0), false)
	seeds.AddRow(uint(1), fuzzing.KindBool, "", int64(0), true)
	seeds.AddRow(uint(2), fuzzing.KindString, "everything", int64(0), false)
	seeds.AddRow(uint(3), fuzzing.KindString, "500", int64(0), false)
	seeds.AddRow(uint(4), fuzzing.KindString, "17", int64(0), false)
	seeds.AddRow(uint(5), fuzzing.KindInt64, "", int64(9000000), false)
	seeds.AddRow(uint(6), fuzzing.KindInt64, "", int64(1893456000), false)
	seeds.AddRow(uint(7), fuzzing.KindString, "message.action", int64(0), false)
	seeds.AddRow(uint(8), fuzzing.KindInt64, "", int64(200000), false)
	seeds.AddRow(uint(9), fuzzing.KindInt64, "", int64(1000), false)
	seeds.AddRow(uint(10), fuzzing.KindInt64, "", int64(7), false)
	seeds.AddRow(uint(11), fuzzing.KindBool, "", int64(0), true)

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "base_config", readBaseConfig(f),
		baseConfigKeys, seeds)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(baseConfigKeys, keyIdx)
		configtest.CheckRow(t, "base_config", readBaseConfig(t), spec,
			fuzzing.ConfigValue(kind, s, n, b))
	})
}

func TestBaseConfigKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "base_config", baseConfigKeys)
}

// TestBaseConfigManifestNamesEveryField enforces the manifest's claim, and records the one field
// that has no key.
//
// PruningKeepEvery carries a mapstructure tag of pruning-keep-every and a declared default of "0",
// and GetConfig never reads it. So no app.toml value reaches it through this reader, and the
// exemption below is the record of that rather than a gap in the manifest. It is the shape of thing
// a replacement manager would otherwise try to map a key onto.
func TestBaseConfigManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "base_config", DefaultConfig().BaseConfig,
		baseConfigKeys,
		"PruningKeepEvery",
	)
}

// grpcKeys covers the three [grpc] keys read as plain casts.
//
// The section is where the guarding in this reader is most complete, which is why only three keys
// are rows. Eight others are read behind v.IsSet or through clampNonNegativeDuration and so resolve
// an absent key to the in-code default rather than to a zero; CheckRow would predict the wrong
// resolution for each, so they are driven by FuzzGetConfigGRPCDurationClamps and
// TestGetConfigGRPCAbsentReads and recorded by name below.
//
// Read this beside apiKeys. Both sections expose a listener with a connection ceiling and a message
// size ceiling, and here every ceiling is guarded while there none is.
var grpcKeys = []configtest.KeySpec{
	{
		Key: "grpc.enable", Path: "Enable", Cast: configtest.CastBool, Unguarded: true,
		Why: "the declared default is true and an absent key resolves false, so a node whose " +
			"app.toml lacks the section serves no gRPC",
	},
	{
		Key: "grpc.address", Path: "Address", Cast: configtest.CastString, Unguarded: true,
		Why: "the declared default is 0.0.0.0:9090 and an absent key resolves empty",
	},
	{
		Key: "grpc.keepalive-permit-without-stream", Path: "KeepalivePermitWithoutStream",
		Cast: configtest.CastBool, Unguarded: true,
		Why: "whether a client may ping with no active stream; false either way, so this row states " +
			"the key is read rather than recording a divergence",
	},
}

// grpcKeysWithTargetsOfTheirOwn are the [grpc] keys whose resolution a row cannot describe, recorded
// for their names alone.
//
// Six are read behind v.IsSet, so an absent key keeps the in-code default. The other two,
// max-connection-age and max-connection-age-grace, are read unconditionally through the clamp
// (config.go:551-552), and their absent value matches the declared default only because both
// defaults are 0. The clamp rescues a negative value and does nothing for an absent one, so they are
// unguarded reads whose clobber is invisible. TestGetConfigGRPCAbsentReads holds the two groups
// apart for that reason.
//
// What the record adds is narrower than it looks, and worth stating exactly. Each of these keys is a
// literal at its read site, so renaming one already reddens its clamp target. The record puts the
// operator-facing spelling in a reviewable diff, and it is what would catch the rename if any of
// these moved to a shared constant the way twenty-eight of app's thirty rows have, since then the
// row and the read site would move together and the behavioural target would stay green.
var grpcKeysWithTargetsOfTheirOwn = []configtest.KeyName{
	"grpc.max-recv-msg-size",
	"grpc.max-open-connections",
	"grpc.max-connection-idle",
	"grpc.max-connection-age",
	"grpc.max-connection-age-grace",
	"grpc.keepalive-time",
	"grpc.keepalive-timeout",
	"grpc.keepalive-min-time",
}

func readGRPC(t testing.TB) func(configtest.AppOpts) (any, error) {
	return sectionOfGetConfig(t, func(c Config) any { return c.GRPC })
}

func FuzzGRPCConfig(f *testing.F) {
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
	seedEveryRow(seeds, len(grpcKeys))

	seeds.AddRow(uint(0), fuzzing.KindBool, "", int64(0), true)
	seeds.AddRow(uint(1), fuzzing.KindString, "127.0.0.1:19090", int64(0), false)
	seeds.AddRow(uint(2), fuzzing.KindBool, "", int64(0), true)

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "grpc", readGRPC(f), grpcKeys, seeds,
		grpcKeysWithTargetsOfTheirOwn...)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(grpcKeys, keyIdx)
		configtest.CheckRow(t, "grpc", readGRPC(t), spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// TestGRPCKeyNamesMatchTheRecordedNames pins all eleven [grpc] key names, the three rows and the
// eight driven elsewhere.
//
// The eight had no record before this, because their target carries a local struct rather than a
// KeySpec table, so nothing held their spelling. That is the gap this closes.
func TestGRPCKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "grpc", grpcKeys, grpcKeysWithTargetsOfTheirOwn...)
}

func TestGRPCManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "grpc", DefaultConfig().GRPC, grpcKeys,
		// Guarded reads, so an absent key keeps the in-code default rather than clobbering it.
		"MaxRecvMsgSize",
		"MaxOpenConnections",
		// Clamped reads: a negative resolves to the in-code default rather than passing through.
		"MaxConnectionIdle",
		"MaxConnectionAge",
		"MaxConnectionAgeGrace",
		"KeepaliveTime",
		"KeepaliveTimeout",
		"KeepaliveMinTime",
	)
}

// TestGetConfigGRPCAbsentReads records what an absent [grpc] key resolves to, holding the guarded
// reads apart from the two that only look guarded.
//
// This is the assertion the exemptions in TestGRPCManifestNamesEveryField rest on. Without it that
// list would claim those fields are covered elsewhere with nothing checking it, and a guard removed
// from any of them would pass every check in this file.
//
// The split matters because the two groups fail for different reasons and a reader needs the right
// one. Six keys are read behind v.IsSet, so a guard is what returns the declared default and losing
// it is the failure. max-connection-age and max-connection-age-grace have no guard at all: they are
// read unconditionally and clamped, so an absent key resolves 0 and that happens to equal their
// declared default. Moving either default off 0 turns them into a visible clobber, which is a
// different event from a guard disappearing.
func TestGetConfigGRPCAbsentReads(t *testing.T) {
	cfg, err := GetConfig(newAppViper(t, nil))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	def := DefaultConfig().GRPC
	got := cfg.GRPC

	// Read behind v.IsSet, so the guard is what restores the declared default.
	for _, c := range []struct {
		key              string
		absent, declared any
	}{
		{"grpc.max-recv-msg-size", got.MaxRecvMsgSize, def.MaxRecvMsgSize},
		{"grpc.max-open-connections", got.MaxOpenConnections, def.MaxOpenConnections},
		{"grpc.max-connection-idle", got.MaxConnectionIdle, def.MaxConnectionIdle},
		{"grpc.keepalive-time", got.KeepaliveTime, def.KeepaliveTime},
		{"grpc.keepalive-timeout", got.KeepaliveTimeout, def.KeepaliveTimeout},
		{"grpc.keepalive-min-time", got.KeepaliveMinTime, def.KeepaliveMinTime},
	} {
		if c.absent != c.declared {
			t.Errorf("an absent %s resolved to %v rather than the declared %v, so its v.IsSet guard "+
				"is gone. That is the failure the guard exists to prevent, and config.go:519-521 says "+
				"why: a node upgrading with an older app.toml stays bounded", c.key, c.absent, c.declared)
		}
	}

	// Read unconditionally and clamped. Nothing guards these, so the assertion is on the coincidence
	// itself: the declared default is the getter's zero, which is why an absent key looks correct.
	for _, c := range []struct {
		key      string
		absent   time.Duration
		declared time.Duration
	}{
		{"grpc.max-connection-age", got.MaxConnectionAge, def.MaxConnectionAge},
		{"grpc.max-connection-age-grace", got.MaxConnectionAgeGrace, def.MaxConnectionAgeGrace},
	} {
		if c.declared != 0 {
			t.Errorf("%s's declared default is now %v rather than 0. It is read unconditionally and "+
				"only clamped, so an absent key still resolves 0, which is now a clobber the manifest "+
				"should carry as a row rather than an exemption", c.key, c.declared)
			continue
		}
		if c.absent != 0 {
			t.Errorf("an absent %s resolved to %v rather than 0. That means a guard was added or the "+
				"clamp changed, which is a fine end state and moves this key into the guarded group "+
				"above", c.key, c.absent)
		}
	}
}
