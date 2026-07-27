package cmd

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// Log level is one knob with two environment spellings that sit at two different
// precedence tiers, and the difference decides whether a bad value stops the node.
//
// Apply reads log_level out of the server viper. That viper runs AutomaticEnv, so
// the read matches SEID_LOG_LEVEL as well as the --log_level flag, and a non-empty
// result is treated as an explicit flag: it outranks everything and it must parse or
// Apply fails. Separately, the code checks os.Getenv("SEI_LOG_LEVEL") — a different
// variable, read directly — and if that one is set it declines to override at all,
// leaving the level to seilog's own initialization.
//
// So SEID_LOG_LEVEL=bogus fails the boot and SEI_LOG_LEVEL=bogus does not, and
// SEI_LOG_LEVEL=debug suppresses the config.toml value instead of losing to it.
// Neither variable is documented as the other's alternative.

// applyWithLogFlags boots a fixture home the way applyLegacy does, with the two
// logging flags registered on the command.
//
// In production those are persistent flags on the root command; here they are
// registered directly on the start command, because cobra only merges inherited
// persistent flags during Execute and this harness calls Apply directly. The viper
// key is the same either way, so the resolution under test is unchanged — what is
// not reproduced is cobra's flag inheritance, which is not what these rows are
// about.
func applyWithLogFlags(t *testing.T, home *configtest.Home, logLevel, logFormat string) applyResult {
	t.Helper()

	cmd, serverCtx := newApplyCommand(t, home)
	cmd.Flags().String(flags.FlagLogLevel, "", "The logging level")
	cmd.Flags().String(flags.FlagLogFormat, "", "The logging format")

	if logLevel != "" {
		if err := cmd.Flags().Set(flags.FlagLogLevel, logLevel); err != nil {
			t.Fatalf("set --log_level: %v", err)
		}
	}
	if logFormat != "" {
		if err := cmd.Flags().Set(flags.FlagLogFormat, logFormat); err != nil {
			t.Fatalf("set --log_format: %v", err)
		}
	}

	return applyResult{ctx: serverCtx, err: applyThrough(cmd)}
}

// slogAcceptsLevel reports whether the resolved level parses, using the same parser
// Apply uses.
//
// slog is the oracle rather than a hand-written list of spellings, and it has to be:
// UnmarshalText is case-insensitive and also accepts an offset form, so "Info",
// "iNfO" and "INFO+2" are all valid levels. A hand-list misses those and would assert
// that a perfectly good level must fail the boot. slog is not the code under test here,
// Apply's error handling is, so deferring to it is not circular.
func slogAcceptsLevel(level string) bool {
	var lvl slog.Level
	return lvl.UnmarshalText([]byte(level)) == nil
}

// FuzzLogLevelFlagMustParse pins the flag tier: a resolved log_level that slog
// cannot parse fails Apply with the offending value named, rather than falling back
// to a default level.
//
// Failing closed is the right direction here and worth holding: a node that quietly
// started at the wrong verbosity would be diagnosed by its absence of logs.
func FuzzLogLevelFlagMustParse(f *testing.F) {
	f.Add("info")
	f.Add("debug")
	f.Add("warn")
	f.Add("error")
	f.Add("INFO")
	f.Add("bogus")
	f.Add("trace")  // documented in the flag help, but slog does not accept it
	f.Add("fatal")  // likewise
	f.Add("info ")  // not trimmed, so slog rejects it
	f.Add("Info")   // slog is case-insensitive
	f.Add("INFO+2") // slog accepts a name with an offset
	f.Add("warn+3")
	f.Add("DEBUG-1")

	f.Fuzz(func(t *testing.T, level string) {
		if level == "" {
			return // an empty flag is indistinguishable from an unset one
		}
		configtest.Isolate(t)
		home := configtest.NewHome(t)

		got := applyWithLogFlags(t, home, level, "")

		if slogAcceptsLevel(level) {
			if got.err != nil {
				t.Fatalf("--log_level=%q is a level slog accepts and must be applied, got %v", level, got.err)
			}
			return
		}
		if got.err == nil {
			t.Fatalf("--log_level=%q is not a level slog accepts and must fail the boot", level)
		}
		if !containsAll(got.err.Error(), "failed to parse log level", level) {
			t.Fatalf("the failure must name the offending value, got %v", got.err)
		}
	})
}

// TestSeidLogLevelEnvIsTreatedAsAnExplicitFlag pins the higher of the two tiers.
// SEID_LOG_LEVEL reaches the same viper read as the flag, so it is treated as an
// explicit setting and a bad value stops the node — even though the code's own
// comment describes the env tier as ranking below the flag.
func TestSeidLogLevelEnvIsTreatedAsAnExplicitFlag(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)
	setServerEnv(t, flags.FlagLogLevel, "bogus")

	got := applyWithLogFlags(t, home, "", "")
	if got.err == nil {
		t.Fatal("SEID_LOG_LEVEL reaches the log_level viper read through AutomaticEnv, so an " +
			"unparseable value must fail the boot exactly as the flag does")
	}
	if !containsAll(got.err.Error(), "failed to parse log level", "bogus") {
		t.Fatalf("the failure must name the offending value, got %v", got.err)
	}
}

// TestSeiLogLevelEnvSuppressesTheConfigFileValue pins the lower tier, and the
// asymmetry that makes the two variables confusable.
//
// SEI_LOG_LEVEL is read with os.Getenv, not through viper, and its only effect is to
// stop Apply overriding the level at all — seilog picked it up at package init. So
// the same unparseable value that fails the boot under SEID_LOG_LEVEL is accepted
// here, and a valid value silently wins over config.toml rather than losing to it.
func TestSeiLogLevelEnvSuppressesTheConfigFileValue(t *testing.T) {
	configtest.Isolate(t)

	// An unparseable SEI_LOG_LEVEL does not fail the boot, because Apply declines to
	// override rather than parsing it.
	home := configtest.NewHome(t)
	home.WriteConfigTOML(t, []byte("log-level = \"info\"\nmode = \"full\"\n"))
	if err := os.Setenv("SEI_LOG_LEVEL", "bogus"); err != nil {
		t.Fatalf("set SEI_LOG_LEVEL: %v", err)
	}

	got := applyWithLogFlags(t, home, "", "")
	if got.err != nil {
		t.Fatalf("SEI_LOG_LEVEL is read outside viper and must not be parsed by Apply, got %v", got.err)
	}
	if got.ctx.Config.LogLevel != "info" {
		t.Fatalf("config.toml log-level = %q, want info; the file value is still parsed into the "+
			"struct even when the override is declined", got.ctx.Config.LogLevel)
	}

	// And an unparseable value in config.toml *does* fail, when no SEI_LOG_LEVEL
	// suppresses the override — so the variable's presence is what decides whether
	// a bad file value is fatal.
	if err := os.Unsetenv("SEI_LOG_LEVEL"); err != nil {
		t.Fatalf("unset SEI_LOG_LEVEL: %v", err)
	}
	badHome := configtest.NewHome(t)
	badHome.WriteConfigTOML(t, []byte("log-level = \"bogus\"\nmode = \"full\"\n"))
	if bad := applyWithLogFlags(t, badHome, "", ""); bad.err == nil {
		t.Fatal("an unparseable config.toml log-level must fail the boot when no SEI_LOG_LEVEL " +
			"suppresses the override")
	}
}

// TestLogFormatFlagOnlyWarns records that --log_format is accepted and then ignored.
// Apply logs a deprecation warning when the value is non-empty and never applies it;
// the format is fixed at seilog initialization from SEI_LOG_FORMAT. So the flag
// changes nothing, and passing an invalid format is not an error either.
func TestLogFormatFlagOnlyWarns(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	got := applyWithLogFlags(t, home, "info", "not-a-format")
	if got.err != nil {
		t.Fatalf("--log_format is deprecated and inert, so any value must be accepted, got %v", got.err)
	}
	if got.ctx.Config == nil {
		t.Fatal("Apply must still populate the boot channels")
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
