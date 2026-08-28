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
//   - parseSSConfigs leaves every legacy read unguarded. Each is a bare cast of a
//     possibly-nil value, so an absent key resolves to the zero value and overwrites
//     the default. ss-keep-recent becomes 0 (keep everything, unbounded disk growth),
//     ss-async-write-buffer becomes 0 (synchronous writes), ss-backend becomes ""
//     and ss-enable becomes false. ss-snapshot-enable is the one guarded read, so an
//     app.toml written before SS snapshots existed keeps the in-code default.
//
// Neither reader returns an error, so nothing about the second case is visible at
// boot. It is asserted here as behavior rather than reported as a defect: the
// clobber is what shipped nodes resolve today, and changing it is a migration, not
// a bug fix. Making it executable is what lets the replacement manager prove it
// either reproduces the clobber or diverges from it on purpose.

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
	f.Add(fuzzing.KindInt64, "", int64(0), false)
	f.Add(fuzzing.KindInt64, "", int64(1), false)
	f.Add(fuzzing.KindInt64, "", int64(-1), false)
	f.Add(fuzzing.KindString, "not-a-size", int64(0), false)
	f.Add(fuzzing.KindNumericString, "", int64(1048576), false)
	f.Add(fuzzing.KindNil, "", int64(0), false)
	// A TOML bool where a byte size belongs. cast.ToUint(true) is 1, which clears
	// the > 0 filter, so the node rotates its hash log every single byte rather
	// than falling back to the default. Found by the fuzzer; kept as a seed.
	f.Add(fuzzing.KindBool, "", int64(0), true)

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
	f.Add(fuzzing.KindString, "/var/lib/sei/genesis.json", int64(0), false)
	f.Add(fuzzing.KindNil, "", int64(0), false)
	f.Add(fuzzing.KindInt64, "", int64(1), false)
	f.Add(fuzzing.KindBool, "", int64(0), true)
	f.Add(fuzzing.KindStringSlice, "a b", int64(0), false)
	f.Add(fuzzing.KindMap, "", int64(0), false)

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
// unguarded operator-visible knob has been overwritten with a zero value, including
// the two that change the node's disk behavior without any log line. SnapshotEnable
// is the one field that survives, because its read is guarded.
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
			"if a presence guard was added on purpose, update this test")
	}
}

// TestGuardedSectionsAbsentBaseline pins that the two sections in this package whose reads are
// guarded resolve an omitted key to the default their reader declares.
//
// For light_invariance that value decides whether the supply-conservation check runs at all, so a
// reader that started from a different struct would silently stop running it on an app.toml written
// before the key existed.
//
// The two clobbering sections cannot use this check, because an absent key there does not resolve to
// the declared default. TestParseSCConfigsAbsentBaseline and
// TestParseSSConfigsAbsentBaselineIsZeroClobbered state what they resolve to instead.
func TestGuardedSectionsAbsentBaseline(t *testing.T) {
	// A subtest per section, because each assertion is fatal. Both in one function would let a
	// genesis regression stop the run before light_invariance is read, and light_invariance is the
	// section whose silent downgrade motivated this.
	t.Run("genesis", func(t *testing.T) {
		got, err := ReadGenesisImportConfig(configtest.AppOpts{})
		if err != nil {
			t.Fatalf("an absent [genesis] section must not error, got %v", err)
		}
		if got != DefaultGenesisConfig {
			t.Fatalf("absent [genesis] resolved to %#v, want the declared default %#v",
				got, DefaultGenesisConfig)
		}
	})
	t.Run("light_invariance", func(t *testing.T) {
		got, err := ReadLightInvarianceConfig(configtest.AppOpts{})
		if err != nil {
			t.Fatalf("an absent [light_invariance] section must not error, got %v", err)
		}
		if got != DefaultLightInvarianceConfig {
			t.Fatalf("absent [light_invariance] resolved to %#v, want the declared default %#v",
				got, DefaultLightInvarianceConfig)
		}
	})
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
	// Anything else is rendered rather than dropped. Returning "" here would discard the
	// payload at the one moment it matters, leaving the caller to report that it expected a
	// message and got nothing.
	return fmt.Sprint(r)
}
