package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/version"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	"github.com/spf13/cast"
)

// This file pins the [state-commit], [state-store], [light_invariance] and
// [genesis] read sites — the ones that decide how a node stores state and how it
// loads genesis, and therefore the ones where a silently-wrong value is most
// expensive.
//
// The two SeiDB sections resolve their keys in deliberately different ways, and
// the difference is the single most consequential fact about this surface:
//
//   - parseSCConfigs guards almost every read with `if v := opts.Get(k); v != nil`,
//     so a key absent from an older app.toml keeps its non-zero in-code default.
//   - parseSSConfigs guards nothing. Every read is a bare cast of a possibly-nil
//     value, so an absent key resolves to the zero value and overwrites the
//     default. ss-keep-recent becomes 0 (keep everything, unbounded disk growth),
//     ss-async-write-buffer becomes 0 (synchronous writes), ss-backend becomes ""
//     and ss-enable becomes false.
//
// Neither reader returns an error, so nothing about the second case is visible at
// boot. It is recorded here as behavior rather than reported as a defect: the
// clobber is what shipped nodes resolve today, and changing it is a migration, not
// a bug fix. Making it executable is what lets the replacement manager prove it
// either reproduces the clobber or diverges from it on purpose.

// scKeys is the [state-commit] read-site manifest for the rows whose resolution
// is a plain cast. Every read here is unchecked (cast.ToX, no error return from
// parseSCConfigs at all), so a malformed value resolves to the zero rather than
// failing the boot; Unguarded marks the two rows that additionally cannot tell an
// absent key from a nil one.
var scKeys = []configtest.KeySpec{
	{
		Key: FlagSCEnable, Path: "Enable", Cast: configtest.CastBool, Unguarded: true,
		Why: "absent clobbers the default true to false, and SetupSeiDB then panics rather than booting",
	},
	{
		Key: FlagSCDirectory, Path: "Directory", Cast: configtest.CastString, Unguarded: true,
		Why: "absent clobbers to the empty string, which the store resolves to its own default location",
	},
	{
		Key: FlagSCAsyncCommitBuffer, Path: "MemIAVLConfig.AsyncCommitBuffer", Cast: configtest.CastInt,
		Why: "default 100; a clobber to 0 would make every commit synchronous",
	},
	{Key: FlagSCSnapshotKeepRecent, Path: "MemIAVLConfig.SnapshotKeepRecent", Cast: configtest.CastUint32},
	{
		Key: FlagSCSnapshotInterval, Path: "MemIAVLConfig.SnapshotInterval", Cast: configtest.CastUint32,
		Why: "default 10000; a clobber to 0 would disable snapshots",
	},
	{Key: FlagSCSnapshotMinTimeInterval, Path: "MemIAVLConfig.SnapshotMinTimeInterval", Cast: configtest.CastUint32},
	{Key: FlagSCSnapshotWriterLimit, Path: "MemIAVLConfig.SnapshotWriterLimit", Cast: configtest.CastInt},
	{Key: FlagSCSnapshotPrefetchThreshold, Path: "MemIAVLConfig.SnapshotPrefetchThreshold", Cast: configtest.CastFloat64},
	{Key: FlagSCSnapshotWriteRateMBps, Path: "MemIAVLConfig.SnapshotWriteRateMBps", Cast: configtest.CastInt},
	{
		Key: FlagSCFlatKVReadWriteMetrics, Path: "FlatKVConfig.EnableReadWriteMetrics", Cast: configtest.CastBool,
		Why: "the only [state-commit.flatkv] key the production store reads",
	},
	{Key: FlagSCHistoricalProofMaxInFlight, Path: "HistoricalProofMaxInFlight", Cast: configtest.CastInt},
	{Key: FlagSCHistoricalProofRateLimit, Path: "HistoricalProofRateLimit", Cast: configtest.CastFloat64},
	{Key: FlagSCHistoricalProofBurst, Path: "HistoricalProofBurst", Cast: configtest.CastInt},
	{
		Key: FlagSCHashLoggerEnable, Path: "HashLogger.Enable", Cast: configtest.CastBool,
		Why: "default true; guarded so an absent key does not silently turn hash logging off",
	},
	{Key: FlagSCHashLoggerDirectory, Path: "HashLogger.Directory", Cast: configtest.CastString},
	{
		Key: FlagSCHashLoggerBlocksToRetain, Path: "HashLogger.BlocksToRetain", Cast: configtest.CastUint,
		Why: "0 is a meaningful value (block-count retention disabled) and is taken verbatim",
	},
	{
		Key: FlagSCHashLoggerMaxDiskSize, Path: "HashLogger.MaxDiskSize", Cast: configtest.CastUint,
		Why: "0 is a meaningful value (disk cap disabled) and is taken verbatim",
	},
}

// ssKeys is the [state-store] read-site manifest. Every row is unguarded and
// unchecked — the section has no presence checks at all.
//
// StateStoreConfig also carries KeepLastVersion and UseDefaultComparer, which are
// absent here because parseSSConfigs reads neither: they hold their in-code
// defaults on every node and no app.toml key reaches them. They are named so the
// omission reads as a fact about the section rather than a gap in the table.
var ssKeys = []configtest.KeySpec{
	{
		Key: FlagSSEnable, Path: "Enable", Cast: configtest.CastBool, Unguarded: true,
		Why: "absent clobbers the default true to false",
	},
	{
		Key: FlagSSBackend, Path: "Backend", Cast: configtest.CastString, Unguarded: true,
		Why: "absent clobbers the default \"pebbledb\" to \"\"",
	},
	{
		Key: FlagSSAsyncWriterBuffer, Path: "AsyncWriteBuffer", Cast: configtest.CastInt, Unguarded: true,
		Why: "absent clobbers the default 100 to 0, which means synchronous writes",
	},
	{
		Key: FlagSSKeepRecent, Path: "KeepRecent", Cast: configtest.CastInt, Unguarded: true,
		Why: "absent clobbers the default to 0, which means keep every version and grow without bound",
	},
	{Key: FlagSSPruneInterval, Path: "PruneIntervalSeconds", Cast: configtest.CastInt, Unguarded: true},
	{Key: FlagSSImportNumWorkers, Path: "ImportNumWorkers", Cast: configtest.CastInt, Unguarded: true},
	{Key: FlagSSDirectory, Path: "DBDirectory", Cast: configtest.CastString, Unguarded: true},
	{Key: FlagSSReadWriteMetrics, Path: "EnableReadWriteMetrics", Cast: configtest.CastBool, Unguarded: true},
	{Key: FlagEVMSSDirectory, Path: "EVMDBDirectory", Cast: configtest.CastString, Unguarded: true},
	{Key: FlagEVMSSSeparateDBs, Path: "SeparateEVMSubDBs", Cast: configtest.CastBool, Unguarded: true},
	{Key: FlagEVMSSSplit, Path: "EVMSplit", Cast: configtest.CastBool, Unguarded: true},
}

func readSC(opts configtest.AppOpts) (any, error) { return parseSCConfigs(opts), nil }
func readSS(opts configtest.AppOpts) (any, error) { return parseSSConfigs(opts), nil }

// FuzzParseSCConfigs drives every plain [state-commit] key through arbitrary raw
// values, holding each to its declared cast and guard.
func FuzzParseSCConfigs(f *testing.F) {
	f.Add(uint(0), uint8(2), "", int64(0), true)      // sc-enable as a TOML bool
	f.Add(uint(0), uint8(8), "", int64(0), false)     // sc-enable as the string "false"
	f.Add(uint(2), uint8(3), "", int64(100), false)   // async-commit-buffer
	f.Add(uint(2), uint8(0), "", int64(0), false)     // guarded nil must keep 100
	f.Add(uint(4), uint8(3), "", int64(10000), false) // snapshot-interval
	f.Add(uint(4), uint8(3), "", int64(-1), false)    // negative into an unchecked unsigned cast: resolves 0
	f.Add(uint(7), uint8(6), "", int64(2), false)     // prefetch threshold as a float
	f.Add(uint(1), uint8(1), "/var/lib/sei/sc", int64(0), false)
	f.Add(uint(13), uint8(1), "not-a-bool", int64(0), false) // unchecked: resolves false, no error
	f.Add(uint(15), uint8(3), "", int64(0), false)           // explicit 0 taken verbatim

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(scKeys, keyIdx)
		configtest.CheckRow(t, "state-commit", readSC, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// FuzzParseSSConfigs drives every [state-store] key through arbitrary raw values.
// Because the whole section is unguarded, the property being pinned for a nil
// value is the clobber itself: the resolved field must equal the cast's zero, not
// the in-code default.
func FuzzParseSSConfigs(f *testing.F) {
	f.Add(uint(0), uint8(2), "", int64(0), true)
	f.Add(uint(0), uint8(0), "", int64(0), false) // nil clobbers Enable to false
	f.Add(uint(1), uint8(1), "pebbledb", int64(0), false)
	f.Add(uint(1), uint8(0), "", int64(0), false) // nil clobbers Backend to ""
	f.Add(uint(2), uint8(3), "", int64(100), false)
	f.Add(uint(3), uint8(3), "", int64(200000), false)
	f.Add(uint(3), uint8(0), "", int64(0), false) // nil clobbers KeepRecent to 0
	f.Add(uint(6), uint8(1), "/var/lib/sei/ss", int64(0), false)
	f.Add(uint(10), uint8(8), "", int64(0), true)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(ssKeys, keyIdx)
		configtest.CheckRow(t, "state-store", readSS, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// FuzzSCWriteMode pins the write-mode resolution, which is the one place in
// [state-commit] that both validates and overrides.
//
// Two rules compose, and the order matters. sc-write-mode is always parsed, even
// when it will be ignored, so a typo fails fast with a panic naming the value
// rather than surfacing later from StateCommitConfig.Validate. Then
// sc-write-mode-enable-auto — default true, and only an *explicit* key flips it —
// decides whether the parsed mode is honored at all: with auto on, the effective
// mode is Auto regardless of what was configured.
//
// The combination is what lets a node provisioned by an older binary (explicit
// sc-write-mode = "memiavl_only", no auto key) resolve to auto so a
// governance-driven storage migration can start without an app.toml edit. A
// regression in either half changes which storage engine a validator commits
// through, so the panic is asserted as behavior, not tolerated.
func FuzzSCWriteMode(f *testing.F) {
	f.Add("memiavl_only", true, true)
	f.Add("memiavl_only", true, false)
	f.Add("cosmos_only", true, false) // the retired v6.4/v6.5 spelling for memiavl_only
	f.Add("cosmos_only", false, false)
	f.Add("", false, false)
	f.Add("", true, true)
	f.Add("bogus", false, false)
	f.Add("bogus", true, true)
	f.Add("MEMIAVL_ONLY", false, false)
	f.Add("flatkv_only", false, false)

	f.Fuzz(func(t *testing.T, mode string, setAuto, auto bool) {
		opts := configtest.AppOpts{FlagSCEnable: true, FlagSCWriteMode: mode}
		if setAuto {
			opts[FlagSCWriteModeEnableAuto] = auto
		}

		_, parseErr := config.ParseSCWriteMode(mode)
		wantPanic := mode != "" && parseErr != nil

		if wantPanic {
			// The panic quotes the offending value, so an operator reading a crash
			// log can see exactly what they typed — including trailing whitespace.
			assertPanics(t, fmt.Sprintf("invalid SC write mode %q", mode), func() { parseSCConfigs(opts) })
			return
		}

		cfg := parseSCConfigs(opts)

		// enable-auto defaults to true and only an explicit key changes it.
		effectiveAuto := true
		if setAuto {
			effectiveAuto = auto
		}
		if cfg.WriteModeEnableAuto != effectiveAuto {
			t.Fatalf("sc-write-mode-enable-auto resolved to %v, want %v (set=%v)",
				cfg.WriteModeEnableAuto, effectiveAuto, setAuto)
		}

		want := config.DefaultStateCommitConfig().WriteMode
		if mode != "" {
			parsed, err := config.ParseSCWriteMode(mode)
			if err != nil {
				t.Fatalf("mode %q was expected to parse: %v", mode, err)
			}
			want = parsed
		}
		want = config.ApplyWriteModeAuto(effectiveAuto, want)

		if cfg.WriteMode != want {
			t.Fatalf("sc-write-mode = %q with auto=%v resolved to %v, want %v",
				mode, effectiveAuto, cfg.WriteMode, want)
		}
		if effectiveAuto && cfg.WriteMode != sctypes.Auto {
			t.Fatalf("with sc-write-mode-enable-auto on, the effective mode must be auto, got %v", cfg.WriteMode)
		}
	})
}

// FuzzSCHashLoggerTargetFileSize pins the one guarded read in [state-commit] that
// also filters its value. A hash log file must roll at some positive size, so 0 —
// whether written deliberately or produced by a cast that failed — preserves the
// 16 MB default instead of yielding a rotation size of zero bytes.
func FuzzSCHashLoggerTargetFileSize(f *testing.F) {
	f.Add(uint8(3), "", int64(0), false)
	f.Add(uint8(3), "", int64(1), false)
	f.Add(uint8(3), "", int64(-1), false)
	f.Add(uint8(1), "not-a-size", int64(0), false)
	f.Add(uint8(7), "", int64(1048576), false)
	f.Add(uint8(0), "", int64(0), false)
	// A TOML bool where a byte size belongs. cast.ToUint(true) is 1, which clears
	// the > 0 filter, so the node rotates its hash log every single byte rather
	// than falling back to the default. Found by the fuzzer; kept as a seed.
	f.Add(uint8(2), "", int64(0), true)

	f.Fuzz(func(t *testing.T, kind uint8, s string, n int64, b bool) {
		value := fuzzing.ConfigValue(kind, s, n, b)
		cfg := parseSCConfigs(configtest.AppOpts{
			FlagSCEnable:                   true,
			FlagSCHashLoggerTargetFileSize: value,
		})

		// The rule, restated: cast unconditionally, then adopt only a positive
		// result. Anything that casts to 0 — an absent key, a nil, a negative, an
		// unparseable string — leaves the default standing.
		want := config.DefaultHashLoggerConfig().TargetFileSize
		if converted := cast.ToUint(value); converted > 0 {
			want = converted
		}

		if cfg.HashLogger.TargetFileSize != want {
			t.Fatalf("sc-hash-logger-target-file-size = %#v (%s) resolved to %d, want %d",
				value, fuzzing.ConfigValueKindName(kind), cfg.HashLogger.TargetFileSize, want)
		}
		if cfg.HashLogger.TargetFileSize == 0 {
			t.Fatalf("sc-hash-logger-target-file-size = %#v resolved to 0; a zero rotation size is never valid", value)
		}
	})
}

// FuzzReadLightInvarianceConfig pins the supply-invariance toggle. The default is
// true and the read is guarded and checked, so an absent key keeps the check on
// and a malformed value fails the boot rather than quietly disabling a
// money-conservation assertion that panics the node when it is violated.
func FuzzReadLightInvarianceConfig(f *testing.F) {
	f.Add(uint8(2), "", int64(0), true)
	f.Add(uint8(8), "", int64(0), false)
	f.Add(uint8(0), "", int64(0), false)
	f.Add(uint8(1), "sometimes", int64(0), false)
	f.Add(uint8(11), "", int64(0), false)

	spec := configtest.KeySpec{
		Key: "light_invariance.supply_enabled", Path: "SupplyEnabled",
		Cast: configtest.CastBool, Checked: true,
		Why: "default true; absent must not disable the supply invariance check",
	}
	read := func(opts configtest.AppOpts) (any, error) { return ReadLightInvarianceConfig(opts) }

	f.Fuzz(func(t *testing.T, kind uint8, s string, n int64, b bool) {
		configtest.CheckRow(t, "light_invariance", read, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// FuzzReadGenesisImportConfig pins the [genesis] section, whose two keys resolve
// through different mechanisms.
//
// stream-import is a guarded checked cast. import-file is not: it is an unchecked
// type assertion, `cfg.GenesisStreamFile = v.(string)`, so a non-string value
// panics at app construction with a bare interface-conversion message rather than
// naming the key. That panic is the pinned behavior; the fuzzer's job here is to
// establish that a string is the *only* shape that does not panic, since a TOML
// integer or a mistyped table in this section takes the node down at boot with no
// diagnostic pointing at genesis.
func FuzzReadGenesisImportConfig(f *testing.F) {
	f.Add(uint8(1), "/var/lib/sei/genesis.json", int64(0), false)
	f.Add(uint8(0), "", int64(0), false)
	f.Add(uint8(3), "", int64(1), false)
	f.Add(uint8(2), "", int64(0), true)
	f.Add(uint8(9), "a b", int64(0), false)
	f.Add(uint8(11), "", int64(0), false)

	f.Fuzz(func(t *testing.T, kind uint8, s string, n int64, b bool) {
		value := fuzzing.ConfigValue(kind, s, n, b)
		opts := configtest.AppOpts{"genesis.import-file": value}

		str, isString := value.(string)
		if value != nil && !isString {
			assertPanics(t, "interface conversion", func() {
				_, _ = ReadGenesisImportConfig(opts)
			})
			return
		}

		cfg, err := ReadGenesisImportConfig(opts)
		if err != nil {
			t.Fatalf("genesis.import-file = %#v must not error, got %v", value, err)
		}
		if value == nil {
			if cfg.GenesisStreamFile != DefaultGenesisConfig.GenesisStreamFile {
				t.Fatalf("an absent genesis.import-file must keep the default %q, got %q",
					DefaultGenesisConfig.GenesisStreamFile, cfg.GenesisStreamFile)
			}
			return
		}
		if cfg.GenesisStreamFile != str {
			t.Fatalf("genesis.import-file = %q resolved to %q", str, cfg.GenesisStreamFile)
		}
	})
}

// FuzzReadGenesisStreamImport pins the stream-import toggle, which is a plain
// guarded checked cast.
func FuzzReadGenesisStreamImport(f *testing.F) {
	f.Add(uint8(2), "", int64(0), true)
	f.Add(uint8(8), "", int64(0), false)
	f.Add(uint8(0), "", int64(0), false)
	f.Add(uint8(1), "stream", int64(0), false)

	spec := configtest.KeySpec{
		Key: "genesis.stream-import", Path: "StreamGenesisImport",
		Cast: configtest.CastBool, Checked: true,
	}
	read := func(opts configtest.AppOpts) (any, error) { return ReadGenesisImportConfig(opts) }

	f.Fuzz(func(t *testing.T, kind uint8, s string, n int64, b bool) {
		configtest.CheckRow(t, "genesis", read, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// TestParseSCConfigsAbsentBaseline records what an app.toml with no
// [state-commit] section resolves to. It is not the in-code default: the two
// unguarded reads clobber Enable to false and Directory to "", and the write mode
// resolves to auto because enable-auto defaults to true. The Enable clobber is the
// reason a node whose app.toml predates the section refuses to boot — SetupSeiDB
// panics on !Enable rather than falling back.
func TestParseSCConfigsAbsentBaseline(t *testing.T) {
	want := config.DefaultStateCommitConfig()
	want.Enable = false // unguarded read of an absent key
	want.Directory = "" // unguarded read of an absent key
	want.WriteMode = sctypes.Auto
	want.HashLogger.Version = version.Version // stamped from the build, not from config

	got := parseSCConfigs(configtest.AppOpts{})
	if a, b := configtest.Dump(got), configtest.Dump(want); a != b {
		t.Fatalf("absent [state-commit] resolved unexpectedly\n--- got\n%s\n--- want\n%s", a, b)
	}
}

// TestParseSSConfigsAbsentBaselineIsZeroClobbered records the clobber in full: an
// app.toml with no [state-store] section resolves to a config in which every
// operator-visible knob has been overwritten with a zero value, including the two
// that change the node's disk behavior without any log line.
func TestParseSSConfigsAbsentBaselineIsZeroClobbered(t *testing.T) {
	got := parseSSConfigs(configtest.AppOpts{})

	def := config.DefaultStateStoreConfig()
	want := def
	want.Enable = false           // default true
	want.Backend = ""             // default "pebbledb"
	want.AsyncWriteBuffer = 0     // default 100: synchronous writes
	want.KeepRecent = 0           // keep every version
	want.PruneIntervalSeconds = 0 //
	want.ImportNumWorkers = 0     //
	want.DBDirectory = ""         //
	want.EnableReadWriteMetrics = false
	want.EVMDBDirectory = ""
	want.SeparateEVMSubDBs = false
	want.EVMSplit = false

	if a, b := configtest.Dump(got), configtest.Dump(want); a != b {
		t.Fatalf("absent [state-store] resolved unexpectedly\n--- got\n%s\n--- want\n%s", a, b)
	}
	// State the consequence explicitly so the assertion cannot be "fixed" by
	// updating want without a decision.
	if got.AsyncWriteBuffer == def.AsyncWriteBuffer {
		t.Fatal("ss-async-write-buffer is no longer clobbered by an absent key; " +
			"if a presence guard was added on purpose, update this test and the manifest")
	}
}

// assertPanics runs fn and requires it to panic with a message containing want.
// Several legacy read sites report a bad value by panicking, so the panic is part
// of the contract and needs asserting rather than avoiding.
func assertPanics(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic containing %q, got none", want)
		}
		if msg := panicMessage(r); !strings.Contains(msg, want) {
			t.Fatalf("expected a panic containing %q, got %q", want, msg)
		}
	}()
	fn()
}

func panicMessage(r any) string {
	if err, ok := r.(error); ok {
		return err.Error()
	}
	if s, ok := r.(string); ok {
		return s
	}
	return ""
}
