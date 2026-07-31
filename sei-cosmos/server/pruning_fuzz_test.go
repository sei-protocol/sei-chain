package server_test

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// Pruning is read from the same appOpts map as every other key, but it is one of
// the few reads whose failure mode depends on the caller rather than the value.
// start's PreRunE calls GetPruningOptionsFromFlags and returns the error; newApp
// calls it and panics. Same key, same value, same function — a legible startup
// error on one path and a stack trace on the other.
//
// The value semantics are worth pinning on their own: the strategy is
// case-folded, only four names are accepted, and "custom" is the one strategy
// whose numbers are validated.

// FuzzGetPruningOptionsFromFlags pins the strategy vocabulary and the custom-mode
// validation.
//
// The four accepted names resolve to fixed option sets and ignore the keep-recent
// / keep-every / interval keys entirely, so an operator who sets pruning-interval
// alongside pruning = "default" gets the preset, not their number. Only "custom"
// reads those keys, and only "custom" can fail validation. Anything else is
// rejected by name.
func FuzzGetPruningOptionsFromFlags(f *testing.F) {
	f.Add("default", uint64(0), uint64(0), uint64(0))
	f.Add("nothing", uint64(0), uint64(0), uint64(0))
	f.Add("everything", uint64(0), uint64(0), uint64(0))
	f.Add("custom", uint64(100), uint64(0), uint64(10))
	f.Add("custom", uint64(0), uint64(0), uint64(0))
	f.Add("CUSTOM", uint64(100), uint64(0), uint64(10)) // case-folded
	f.Add("Default", uint64(0), uint64(0), uint64(0))
	f.Add("", uint64(0), uint64(0), uint64(0))           // rejected by name
	f.Add("aggressive", uint64(0), uint64(0), uint64(0)) // rejected by name
	f.Add("default", uint64(7), uint64(7), uint64(7))    // numbers ignored by a preset

	f.Fuzz(func(t *testing.T, strategy string, keepRecent, keepEvery, interval uint64) {
		opts := configtest.AppOpts{
			server.FlagPruning:           strategy,
			server.FlagPruningKeepRecent: keepRecent,
			server.FlagPruningKeepEvery:  keepEvery,
			server.FlagPruningInterval:   interval,
		}

		got, err := server.GetPruningOptionsFromFlags(opts)

		switch strings.ToLower(strategy) {
		case storetypes.PruningOptionDefault, storetypes.PruningOptionNothing, storetypes.PruningOptionEverything:
			if err != nil {
				t.Fatalf("pruning = %q is a known strategy and must be accepted, got %v", strategy, err)
			}
			// A preset ignores the numeric keys: the resolved options must equal the
			// preset built from the name alone.
			want := storetypes.NewPruningOptionsFromString(strings.ToLower(strategy))
			if a, b := configtest.Dump(got), configtest.Dump(want); a != b {
				t.Fatalf("pruning = %q resolved to options that depend on the numeric keys\n got: %s\nwant: %s",
					strategy, a, b)
			}

		case storetypes.PruningOptionCustom:
			want := storetypes.NewPruningOptions(keepRecent, keepEvery, interval)
			if validateErr := want.Validate(); validateErr != nil {
				if err == nil {
					t.Fatalf("pruning = custom with keep-recent=%d keep-every=%d interval=%d does not "+
						"validate and must be an error", keepRecent, keepEvery, interval)
				}
				if !strings.Contains(err.Error(), "custom pruning") {
					t.Fatalf("the failure must name custom pruning, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("pruning = custom with keep-recent=%d keep-every=%d interval=%d must be "+
					"accepted, got %v", keepRecent, keepEvery, interval, err)
			}
			if a, b := configtest.Dump(got), configtest.Dump(want); a != b {
				t.Fatalf("custom pruning resolved wrongly\n got: %s\nwant: %s", a, b)
			}

		default:
			if err == nil {
				t.Fatalf("pruning = %q is not a known strategy and must be rejected", strategy)
			}
			if !strings.Contains(err.Error(), "unknown pruning strategy") {
				t.Fatalf("the failure must name the unknown strategy, got %v", err)
			}
		}
	})
}

// TestGetPruningOptionsFromFlagsIgnoresNumbersForPresets records the preset
// behavior on its own, because it is the case an operator gets wrong: setting
// pruning-interval while leaving pruning at "default" changes nothing, and nothing
// says so.
func TestGetPruningOptionsFromFlagsIgnoresNumbersForPresets(t *testing.T) {
	withNumbers, err := server.GetPruningOptionsFromFlags(configtest.AppOpts{
		server.FlagPruning:           "default",
		server.FlagPruningKeepRecent: uint64(12345),
		server.FlagPruningInterval:   uint64(999),
	})
	if err != nil {
		t.Fatalf("GetPruningOptionsFromFlags: %v", err)
	}
	bare, err := server.GetPruningOptionsFromFlags(configtest.AppOpts{
		server.FlagPruning: "default",
	})
	if err != nil {
		t.Fatalf("GetPruningOptionsFromFlags: %v", err)
	}
	if a, b := configtest.Dump(withNumbers), configtest.Dump(bare); a != b {
		t.Fatalf("the numeric pruning keys took effect under a preset strategy\n with: %s\nwithout: %s", a, b)
	}
}

// TestGetPruningOptionsFromFlagsAbsentStrategyIsRejected pins what an app.toml with
// no pruning key does. cast.ToString(nil) is "", which matches no strategy, so the
// read fails by name rather than falling back to "default".
func TestGetPruningOptionsFromFlagsAbsentStrategyIsRejected(t *testing.T) {
	_, err := server.GetPruningOptionsFromFlags(configtest.AppOpts{})
	if err == nil {
		t.Fatal("an absent pruning key must be rejected, not defaulted; a silent fallback would " +
			"change the retention of every node whose app.toml omits the key")
	}
	if !strings.Contains(err.Error(), "unknown pruning strategy") {
		t.Fatalf("the failure must name the unknown strategy, got %v", err)
	}
}
