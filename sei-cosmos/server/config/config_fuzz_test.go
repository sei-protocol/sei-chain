package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/spf13/viper"
)

// GetConfig is the second parser of app.toml, feeding api, rosetta,
// telemetry and state-sync. app/seidb.go parses [state-commit] and [state-store] out
// of the same viper for the store; GetConfig parses them again through a different
// mechanism (viper.IsSet plus typed getters) into a Config the store never sees.
//
// Two parsers of one section is the drift risk this file exists to pin: a change that
// unified them shows up as a diff here rather than as two components disagreeing at
// runtime.
//
// Everything below drives a bare viper.New, which is what GetConfig takes. Flag
// binding and env resolution belong to Apply and are exercised in cmd/seid/cmd.

// newAppViper returns a viper holding the one key GetConfig unconditionally
// requires, plus whatever the caller adds. telemetry.global-labels has no default
// and no presence guard, so nothing parses without it.
func newAppViper(t testing.TB, keys map[string]any) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.Set("telemetry.global-labels", []any{})
	for k, val := range keys {
		v.Set(k, val)
	}
	return v
}

// FuzzGetConfigGlobalLabels pins telemetry.global-labels, the key that stops a node
// booting by being absent rather than wrong.
//
// It is read as a bare type assertion with no presence check, so an app.toml that
// omits it fails GetConfig outright. Each label's elements are asserted to string
// unchecked, so a non-string label panics rather than erroring, and a label whose
// length is not exactly 2 is dropped silently rather than rejected.
func FuzzGetConfigGlobalLabels(f *testing.F) {
	f.Add(0, 0, "chain")   // no labels
	f.Add(1, 2, "chain")   // one well-formed pair
	f.Add(3, 2, "chain")   // several pairs
	f.Add(1, 1, "chain")   // too few elements: silently dropped
	f.Add(1, 3, "chain")   // too many elements: silently dropped
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

// TestGetConfigRequiresGlobalLabels pins the absent-key failure on its own: a missing
// telemetry section is a node that will not start.
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
// unchecked: a label of the right shape but the wrong element type takes the node down
// with a raw interface-conversion panic rather than an error naming telemetry.
func TestGetConfigPanicsOnNonStringLabel(t *testing.T) {
	v := viper.New()
	v.Set("telemetry.global-labels", []any{[]any{1, 2}})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("a non-string label must panic; if it is now an error, the diagnostic " +
				"improved and this assertion should say so")
		}
	}()
	_, _ = GetConfig(v)
}

// FuzzGetConfigWriteMode pins GetConfig's own copy of the write-mode resolution.
//
// The rules match app/seidb.go — always parse, then let sc-write-mode-enable-auto
// (default true, flipped only by an explicit key) decide whether the parsed mode is
// honored — but GetConfig returns an error where seidb.go panics. Both parsers must
// agree on the resolved mode for a node's store choice and its reported config to
// describe the same thing, so the agreement is asserted against the shared helpers
// rather than restated.
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
	// DefaultIsZero marks a key whose in-code default is already the zero value, so an
	// absent key resolving to zero is correct rather than a clobber. The guard still
	// matters there — it is what lets an operator set a non-zero value.
	DefaultIsZero bool
}

// guardedKeys are GetConfig reads wrapped in viper.IsSet: the same zero-clobber class
// app/seidb.go guards, expressed through a different mechanism.
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
		Key: "state-commit.flatkv.max-snapshot-lag-blocks", Path: "StateCommit.FlatKVConfig.MaxSnapshotLagBlocks",
		Set: 32,
	},
	{
		Key: "state-commit.flatkv.enable-read-write-metrics", Path: "StateCommit.FlatKVConfig.EnableReadWriteMetrics",
		Set: true, DefaultIsZero: true,
	},
}

// FuzzGetConfigGuardedKeysPreserveDefaults pins the guarded half of GetConfig: an
// absent key resolves to the in-code default rather than to the zero value viper's
// typed getters would otherwise return.
//
// The bounded gRPC keys matter most. They default to a finite limit, and an unguarded
// read of an absent key would resolve 0 — which gRPC reads as unlimited, so a node
// upgrading with an older app.toml would go from bounded to unbounded connections and
// message sizes with nothing said about it.
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
			// An absent key is compared against an explicit zero rather than a synthesized
			// zero literal: a literal has to guess the field's Go type, and guessing wrong
			// makes the comparison unsatisfiable and the assertion vacuous. Reading the
			// reader twice needs no type knowledge at all.
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

// TestGetConfigGuardedKeyDefaultsMatchTheTable keeps guardedKeys' DefaultIsZero column
// honest in both directions, which is what stops the guard assertions above from going
// vacuous: a key marked non-zero whose default moves to zero makes its clobber check
// meaningless, and a key marked zero whose default becomes non-zero leaves a real
// clobber unchecked.
//
// "Is the default zero" is answered by resolving the key explicitly as 0 and comparing,
// for the same reason the target above does it that way: a literal 0 carries Go's int
// type and would never compare equal to a uint or float64 leaf.
func TestGetConfigGuardedKeyDefaultsMatchTheTable(t *testing.T) {
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
				"DefaultIsZero is %v while the table says %v; update the row so its guard "+
				"assertion still means something",
				row.Key, absentLeaf, zeroLeaf, isZero, row.DefaultIsZero)
		}
	}
}

// TestGetConfigGenesisKeyDivergesFromTheAppSideKey records that the two genesis parsers
// read different keys for the same value: GetConfig reads genesis.genesis-stream-file
// while app/genesis.go reads genesis.import-file. Setting one leaves the other empty, so
// a stream-import node configured through the key the template renders streams from "".
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

// TestGetConfigStateStoreReadsAreUnguarded records that GetConfig's [state-store] parse
// has no presence checks, matching app/seidb.go's parseSSConfigs. An absent section
// resolves every field to its zero value, so the reported config says ss-enable false
// and an empty backend on a node whose app.toml predates the section.
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

// TestGetConfigStateSyncReadsAreUnguarded records the [state-sync] clobber as a divergence
// rather than as two facts that happen to disagree.
//
// snapshot-keep-recent is a registered flag with a non-zero default and DefaultConfig
// declares the same, but GetConfig reads it with a bare v.GetUint32, so a viper that never
// saw the key resolves 0 — which toml.go documents as "keep all". That viper is this file's
// layer and not a booted node's: server/start.go binds the flag in PreRunE ahead of both
// production calls, so there the same read takes the flag's value whenever app.toml is
// silent.
//
// Either side moving fails this assertion — a guard makes the absent read return the
// default, and a default moved to 0 makes the clobber stop being one — so the divergence
// can neither close nor widen without a diff.
func TestGetConfigStateSyncReadsAreUnguarded(t *testing.T) {
	cfg, err := GetConfig(newAppViper(t, nil))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	def := DefaultConfig().StateSync
	if def.SnapshotKeepRecent == 0 {
		t.Fatal("state-sync.snapshot-keep-recent's in-code default is now 0, so an absent key " +
			"clobbers nothing and this no longer describes a divergence. If the default moved " +
			"deliberately, say what a serving node now retains — 0 is keep-all, not keep-none")
	}
	if cfg.StateSync.SnapshotKeepRecent != 0 {
		t.Fatalf("an absent state-sync.snapshot-keep-recent resolved to %d rather than 0, so the "+
			"read is no longer unguarded. That is a fine end state and a no-op for a booted node, "+
			"which takes the bound flag's value either way; what it changes is this assertion, so "+
			"update it in the PR that adds the guard",
			cfg.StateSync.SnapshotKeepRecent)
	}
	if cfg.StateSync.SnapshotInterval != 0 || cfg.StateSync.SnapshotDirectory != "" {
		t.Fatalf("absent [state-sync] must resolve to zeros, got interval=%d directory=%q",
			cfg.StateSync.SnapshotInterval, cfg.StateSync.SnapshotDirectory)
	}
}

// TestParseConfigAndGetConfigDisagree pins the disagreement between this package's two exported
// readers of [state-sync], which is what a guard on GetConfig would close.
//
// Handed the same flagless viper, ParseConfig unmarshals over a DefaultConfig base and keeps
// snapshot-keep-recent at its declared default while GetConfig's bare v.GetUint32 resolves 0.
// Nothing else in the tree states that — TestParseConfig asserts only MinGasPrices.
//
// The disagreement is between the two readers and not between two nodes. GetConfig's production
// calls run with the flag bound, where it takes the declared value, and the one non-test caller
// of ParseConfig is server/util.go's empty-template branch, which no binary in this tree reaches.
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
			"agree, which is the end state — update this test and "+
			"TestGetConfigStateSyncReadsAreUnguarded together. If ParseConfig stopped resolving the "+
			"section over DefaultConfig, there is no implemented guard left to copy. If the in-code "+
			"default moved off 2, update this literal to match it",
			parsed.StateSync.SnapshotKeepRecent, direct.StateSync.SnapshotKeepRecent)
	}
}

// FuzzConfigValidateBasic pins the two conditions that reject an otherwise
// parseable app.toml.
//
// An empty minimum-gas-prices fails, because a validator accepting zero-fee transactions
// is a misconfiguration rather than a choice, and pruning "everything" with state-sync
// snapshots enabled fails, because a node cannot serve a snapshot of state it has already
// pruned. Both are the rare case here where a bad combination is refused rather than
// absorbed.
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

// TestGetConfigAbsentSectionDivergences records every field these sections resolve away from its
// declared default when the section is absent from app.toml.
//
// One table for all of them, because the divergence is one property and a reader comparing sections
// wants them side by side.
//
// The diverges column asserts both directions, so a key is anchored whether or not it moves today; a
// declared default shifting onto the getter's zero, or off it, fails here rather than changing the
// divergence set quietly. Rows set false are the keys whose declared default already equals that
// zero.
//
// Compared with reflect.DeepEqual rather than !=, because != on two any values panics when either
// side holds a slice or a map, as index-events does.
func TestGetConfigAbsentSectionDivergences(t *testing.T) {
	cfg, err := GetConfig(newAppViper(t, nil))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	def := DefaultConfig()

	for _, c := range []struct {
		key              string
		absent, declared any
		diverges         bool
	}{
		{"rosetta.address", cfg.Rosetta.Address, def.Rosetta.Address, true},
		{"rosetta.blockchain", cfg.Rosetta.Blockchain, def.Rosetta.Blockchain, true},
		{"rosetta.network", cfg.Rosetta.Network, def.Rosetta.Network, true},
		{"rosetta.retries", cfg.Rosetta.Retries, def.Rosetta.Retries, true},
		{"telemetry.enabled", cfg.Telemetry.Enabled, def.Telemetry.Enabled, true},
		{"telemetry.prometheus-retention-time",
			cfg.Telemetry.PrometheusRetentionTime, def.Telemetry.PrometheusRetentionTime, false},

		// [api]. The rows set false have a declared default that is already the getter's zero, so
		// nothing about the resolved value distinguishes a guard from its absence.
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
		{"freeze-height", cfg.FreezeHeight, def.FreezeHeight, false},
		{"halt-time", cfg.HaltTime, def.HaltTime, false},
		{"min-retain-blocks", cfg.MinRetainBlocks, def.MinRetainBlocks, false},
		{"compaction-interval", cfg.CompactionInterval, def.CompactionInterval, false},

		// The remaining plain casts, each here for the reason api.enable is: a declared default equal
		// to its getter's zero, anchored so that a default moving off it fails here. This table is
		// the only thing tying these sections' absent-key resolution to their declared defaults.
		{"rosetta.enable", cfg.Rosetta.Enable, def.Rosetta.Enable, false},
		{"rosetta.offline", cfg.Rosetta.Offline, def.Rosetta.Offline, false},
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

		// index-events resolves to a []string, which is why the comparison is reflect.DeepEqual. Both
		// sides are nil today, so it is a false row.
		{"index-events", cfg.IndexEvents, def.IndexEvents, false},
	} {
		if got := !reflect.DeepEqual(c.absent, c.declared); got != c.diverges {
			verb := "no longer diverges from"
			if !c.diverges {
				verb = "now diverges from"
			}
			t.Errorf("%s %s its declared default: absent=%v declared=%v. If a guard was added or "+
				"removed, or a default moved onto the getter's zero, update this table", c.key,
				verb, c.absent, c.declared)
		}
	}
}
