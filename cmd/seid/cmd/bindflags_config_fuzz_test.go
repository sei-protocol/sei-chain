package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// bindFlags runs at the end of Apply and pushes resolved values *back* into the
// cobra flags they came from: for every bound flag the operator did not pass, if
// viper holds a value, the flag is Set to fmt.Sprintf("%v", value).
//
// Two consequences follow. The reverse flow means a later cmd.Flags().GetString
// returns a value that came from app.toml or the environment rather than the command
// line, and the flag reports itself as Changed — so "the operator passed this" is not
// a question the flag set can answer after Apply. And %v is a lossy rendering for
// anything that is not a scalar, which turns a slice-valued flag into text pflag
// cannot parse back.
//
// The loop runs under a deferred recover that assigns the failure to a named return,
// so a flag whose write-back fails aborts the whole VisitAll and surfaces as an
// error from Apply rather than as a panic.

// FuzzIntSliceFlagInAppTOMLFailsTheBoot pins the sharpest edge in the write-back.
//
// unsafe-skip-upgrades is registered as an IntSlice flag and is also an app.toml key,
// which is the natural place to put it — a skip that has to survive a restart belongs
// in the file, not in a command line. But a TOML array resolves to a Go slice, %v
// renders it as "[100 101]", and pflag's int-slice parser calls strconv.Atoi on that
// text and fails. The panic is swallowed and returned, so Apply fails and the node
// does not start.
//
// The empty array fails too, because "[]" is not a number either. So there is no
// value of this key that an operator can put in app.toml and still boot, and the
// error names pflag rather than the file it came from.
func FuzzIntSliceFlagInAppTOMLFailsTheBoot(f *testing.F) {
	f.Add(0)
	f.Add(1)
	f.Add(2)
	f.Add(5)

	f.Fuzz(func(t *testing.T, count int) {
		if count < 0 || count > 8 {
			return
		}
		configtest.Isolate(t)
		home := configtest.NewHome(t)

		entries := make([]string, 0, count)
		for i := range count {
			entries = append(entries, fmt.Sprintf("%d", 100+i))
		}
		literal := "[" + strings.Join(entries, ", ") + "]"
		home.WriteAppTOML(t, []byte(server.FlagUnsafeSkipUpgrades+" = "+literal+"\n"))

		got := applyLegacy(t, home, nil)
		if got.err == nil {
			t.Fatalf("%s = %s in app.toml booted cleanly. The write-back renders a slice through "+
				"%%v and pflag cannot parse the result, so this has always failed; if the rendering "+
				"was fixed, that is a behavior change for every node carrying this key",
				server.FlagUnsafeSkipUpgrades, literal)
		}
		if !strings.Contains(got.err.Error(), server.FlagUnsafeSkipUpgrades) {
			t.Fatalf("the failure must name the flag it could not set, got %v", got.err)
		}
	})
}

// TestIntSliceFlagOnTheCommandLineIsFine isolates the cause: the value is not the
// problem, the write-back is. The same skip heights passed as a flag are accepted,
// because a flag the operator set is skipped by the write-back entirely.
func TestIntSliceFlagOnTheCommandLineIsFine(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	got := applyLegacy(t, home, map[string]string{server.FlagUnsafeSkipUpgrades: "100,101"})
	if got.err != nil {
		t.Fatalf("--%s=100,101 must be accepted; only the app.toml route fails: %v",
			server.FlagUnsafeSkipUpgrades, got.err)
	}
	if resolved := got.ctx.Viper.GetIntSlice(server.FlagUnsafeSkipUpgrades); len(resolved) != 2 {
		t.Fatalf("--%s resolved to %v, want two heights", server.FlagUnsafeSkipUpgrades, resolved)
	}
}

// TestFlaglessSliceKeyInAppTOMLResolvesIntact is the contrast that shows the failure
// belongs to the flag binding rather than to slices in app.toml.
//
// index-events is read from appOpts exactly like unsafe-skip-upgrades but has no
// cobra flag, so bindFlags never visits it, no %v rendering happens, and the array
// reaches app.New with its entries intact. Whether a slice-valued app.toml key works
// therefore depends on whether someone also registered a flag for it.
func TestFlaglessSliceKeyInAppTOMLResolvesIntact(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)
	home.WriteAppTOML(t, []byte("index-events = [\"message.action\", \"transfer.amount\"]\n"))

	got := applyLegacy(t, home, nil)
	if got.err != nil {
		t.Fatalf("a flag-less slice key must not disturb the boot: %v", got.err)
	}
	resolved := got.ctx.Viper.GetStringSlice(server.FlagIndexEvents)
	want := []string{"message.action", "transfer.amount"}
	if a, b := configtest.Dump(resolved), configtest.Dump(want); a != b {
		t.Fatalf("index-events resolved to %s, want %s; with no flag registered there is no "+
			"write-back to mangle it", a, b)
	}

	// And the flag really is absent, which is the reason.
	if cmd, _ := newApplyCommand(t, home); cmd.Flags().Lookup(server.FlagIndexEvents) != nil {
		t.Fatalf("%s now has a cobra flag, so the write-back applies to it and this key joins "+
			"the failing class above", server.FlagIndexEvents)
	}
}

// TestWriteBackMakesFileValuesLookLikeFlags pins the reverse flow on a scalar, where
// it succeeds and is therefore invisible.
//
// A value that exists only in app.toml comes back from cmd.Flags().GetString after
// Apply, and the flag is marked Changed. Anything downstream that reads the flag set
// to decide whether the operator specified something is wrong for every key present
// in app.toml.
func TestWriteBackMakesFileValuesLookLikeFlags(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)
	home.WriteAppTOML(t, []byte("minimum-gas-prices = \"0.42usei\"\n"))

	cmd, serverCtx := newApplyCommand(t, home)
	if err := applyThrough(cmd, serverCtx); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	flag := cmd.Flags().Lookup(server.FlagMinGasPrices)
	if flag == nil {
		t.Fatalf("%s is not a registered flag", server.FlagMinGasPrices)
	}
	if !flag.Changed {
		t.Fatalf("%s came from app.toml and the write-back must mark the flag Changed; "+
			"downstream code cannot distinguish it from a command-line value", server.FlagMinGasPrices)
	}
	got, err := cmd.Flags().GetString(server.FlagMinGasPrices)
	if err != nil {
		t.Fatalf("read back %s: %v", server.FlagMinGasPrices, err)
	}
	if got != "0.42usei" {
		t.Fatalf("%s read back as %q from the flag set, want the app.toml value 0.42usei",
			server.FlagMinGasPrices, got)
	}
}

// TestWriteBackDoesNotOverrideAnExplicitFlag pins the guard that keeps the reverse
// flow safe: a flag the operator passed is skipped, so a command-line value is never
// replaced by a file value.
func TestWriteBackDoesNotOverrideAnExplicitFlag(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)
	home.WriteAppTOML(t, []byte("minimum-gas-prices = \"0.42usei\"\n"))

	got := applyLegacy(t, home, map[string]string{server.FlagMinGasPrices: "0.99usei"})
	if got.err != nil {
		t.Fatalf("Apply: %v", got.err)
	}
	if resolved := got.ctx.Viper.GetString(server.FlagMinGasPrices); resolved != "0.99usei" {
		t.Fatalf("minimum-gas-prices resolved to %q; an explicit flag must outrank app.toml "+
			"and must not be overwritten by the write-back", resolved)
	}
}
