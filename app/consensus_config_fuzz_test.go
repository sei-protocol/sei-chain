package app

import (
	"slices"
	"strings"
	"testing"

	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"golang.org/x/mod/semver"
)

// The configuration reads pinned elsewhere in this package resolve values. The two
// pinned here decide consensus behavior, and they do it by a route the config
// tests cannot see: an environment variable that replaces the upgrade-handler list
// outright, and an app.toml bool that lands in a process-global atomic relaxing
// block validation.
//
// A reader test proves the value resolved. Only these prove the value reached the
// thing it controls.

// FuzzUpgradeVersionListOverride pins how UPGRADE_VERSION_LIST is parsed and how
// completely it takes over.
//
// The variable replaces the semver-sorted list embedded in the binary — the list
// that decides which upgrade heights a node will honor. There is no allowlist, no
// live-chain check, and no log line: any string that survives the split becomes an
// upgrade name, on any node where the variable happens to be set, in a production
// binary. The parse is two rules deep (split on newlines and commas, then
// semver.Sort), and the target pins both plus the total-replacement semantics.
//
// The fuzzer's job here is the unvalidated part. Names that are not semver at all
// sort in a defined but unobvious order, and pinning that order is what makes the
// absence of validation visible rather than theoretical.
func FuzzUpgradeVersionListOverride(f *testing.F) {
	f.Add("")
	f.Add("2.0.0")
	f.Add("2.0.0,2.1.0,2.2.0")
	f.Add("2.2.0,2.0.0,2.1.0") // order is normalized, not preserved
	f.Add("2.0.0\n2.1.0\n")    // newlines split like commas
	f.Add("not-a-version")     // accepted with no complaint
	f.Add(",,,")               // every field empty
	f.Add("v1.2.3,1.2.3")      // the v-prefixed and bare spellings coexist
	f.Add("  2.0.0  ")         // surrounding space is not trimmed

	f.Fuzz(func(t *testing.T, envValue string) {
		if !configtest.EnvValueIsSettable(envValue) {
			return
		}
		embedded := upgradesList
		t.Cleanup(func() { upgradesList = embedded })

		t.Setenv("UPGRADE_VERSION_LIST", envValue)
		upgradesList = embedded
		overrideList()

		if envValue == "" {
			if !slices.Equal(upgradesList, embedded) {
				t.Fatalf("an unset UPGRADE_VERSION_LIST must leave the embedded list intact, got %v", upgradesList)
			}
			return
		}

		// A non-empty value replaces the embedded list outright — it does not merge
		// with it, so a node given a single name honors only that upgrade.
		want := strings.FieldsFunc(envValue, func(r rune) bool { return r == '\n' || r == ',' })
		semver.Sort(want)
		if !slices.Equal(upgradesList, want) {
			t.Fatalf("UPGRADE_VERSION_LIST = %q parsed to %v, want %v", envValue, upgradesList, want)
		}

		// FieldsFunc drops empty fields, so a value of only separators yields an
		// empty list rather than a list of empty names. An empty list means the node
		// honors no upgrade at all, which is the state to notice.
		if len(want) == 0 && len(upgradesList) != 0 {
			t.Fatalf("UPGRADE_VERSION_LIST = %q produced %v, want an empty list", envValue, upgradesList)
		}
	})
}

// TestUpgradeVersionListOverrideAcceptsAnyName records the absence of validation
// as its own assertion, so removing every seed above could not hide it. A name
// that is not a version, and could never match a governance-scheduled upgrade,
// replaces the embedded list without an error or a warning.
func TestUpgradeVersionListOverrideAcceptsAnyName(t *testing.T) {
	embedded := upgradesList
	t.Cleanup(func() { upgradesList = embedded })

	t.Setenv("UPGRADE_VERSION_LIST", "definitely-not-a-release")
	upgradesList = embedded
	overrideList()

	if !slices.Equal(upgradesList, []string{"definitely-not-a-release"}) {
		t.Fatalf("upgrade list = %v, want the unvalidated override verbatim", upgradesList)
	}
	if len(embedded) == 0 {
		t.Fatal("the embedded upgrade list is empty, so this fixture no longer shows a replacement")
	}
}

// TestGigaExecutorEnabledDrivesLastResultsHashValidation pins the linkage from an
// app.toml bool to consensus block validation.
//
// giga_executor.enabled is read like any other config key, but app.New then stores
// it into tmtypes.SkipLastResultsHashValidation — a process-global atomic that
// suppresses the LastResultsHash comparison in block validation and in evidence
// verification. So enabling the executor relaxes a consensus check as a side
// effect, and the reader tests in this package cannot observe that: they prove the
// bool resolved, not that it reached the atomic.
//
// This is the one row where the config surface touches consensus safety directly,
// which is why it is asserted through a real app construction rather than inferred.
func TestGigaExecutorEnabledDrivesLastResultsHashValidation(t *testing.T) {
	// This row builds real apps, and app.New calls RegisterUpgradeHandlers, which calls
	// overrideList, which replaces the package-level upgradesList from UPGRADE_VERSION_LIST
	// with no restore. Isolate takes the variable out of the environment for the row's own
	// app constructions, and the list is saved and restored so the row leaves it exactly as
	// found, which is the discipline the two rows above already keep.
	//
	// This does not make the binary hermetic with respect to that list, and it is worth
	// being exact about why rather than implying otherwise. testutil/keeper's package init
	// calls app.SetupWithDefaultHome, so an app is constructed, and overrideList runs,
	// before any test body in this binary executes. An ambient UPGRADE_VERSION_LIST is
	// therefore already applied by the time any row here could pin the environment, and no
	// change inside this file can prevent it. Closing that needs testutil/keeper to stop
	// building an app at init, which is a much wider change than this suite.
	configtest.Isolate(t)
	embedded := upgradesList
	t.Cleanup(func() { upgradesList = embedded })

	// The atomic is process-global and other tests in this package write it, so it
	// is restored regardless of outcome.
	original := tmtypes.SkipLastResultsHashValidation.Load()
	t.Cleanup(func() { tmtypes.SkipLastResultsHashValidation.Store(original) })

	for _, enabled := range []bool{true, false} {
		tmtypes.SkipLastResultsHashValidation.Store(!enabled) // force a change to observe
		app := SetupWithSc(t, true, false, TestAppOpts{UseSc: true, EnableGiga: enabled})
		t.Cleanup(func() { _ = app.Close() })

		if app.GigaExecutorEnabled != enabled {
			t.Fatalf("giga_executor.enabled = %v resolved to GigaExecutorEnabled = %v",
				enabled, app.GigaExecutorEnabled)
		}
		if got := tmtypes.SkipLastResultsHashValidation.Load(); got != enabled {
			t.Fatalf("giga_executor.enabled = %v left SkipLastResultsHashValidation = %v; "+
				"an app.toml bool must still drive the consensus LastResultsHash bypass", enabled, got)
		}
	}
}
