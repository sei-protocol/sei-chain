package config_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/giga/executor/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
)

// gigaKeys is the [giga_executor] section's read-site manifest. Both keys are
// guarded and checked, so an absent key keeps the in-code default (true for
// both) and a value that will not convert to a bool is a boot error rather than a
// silent false.
//
// The defaults matter beyond this section: giga_executor.enabled also decides
// whether app.New flips the process-global atomic that relaxes consensus
// LastResultsHash validation, so a regression that let an absent or malformed key
// resolve to false would change consensus behavior without changing app.toml.
var gigaKeys = []configtest.KeySpec{
	{
		Key: config.FlagEnabled, Path: "Enabled", Cast: configtest.CastBool,
		Checked: true,
		Why:     "default true; also gates the SkipLastResultsHashValidation atomic in app.New",
	},
	{
		Key: config.FlagOCCEnabled, Path: "OCCEnabled", Cast: configtest.CastBool,
		Checked: true,
		Why:     "default true; absent must not silently disable optimistic concurrency",
	},
}

func readGiga(opts configtest.AppOpts) (any, error) { return config.ReadConfig(opts) }

// FuzzReadConfig drives every [giga_executor] key through arbitrary raw values
// and holds ReadConfig to its contract: an absent or nil key keeps the default,
// a convertible value is adopted verbatim, and an inconvertible one errors
// instead of resolving to false.
func FuzzReadConfig(f *testing.F) {
	// Seeds cover the shapes an operator can actually produce: a TOML bool, the
	// string spellings viper hands back from an environment variable, a numeric
	// spelling cast accepts, and the malformed value that must not become false.
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
	seeds.AddRow(uint(0), fuzzing.KindBool, "true", int64(1), true)
	seeds.AddRow(uint(1), fuzzing.KindBoolString, "false", int64(0), false)
	seeds.AddRow(uint(0), fuzzing.KindString, "maybe", int64(0), false)
	seeds.AddRow(uint(1), fuzzing.KindString, "1", int64(1), true)
	seeds.AddRow(uint(0), fuzzing.KindNil, "", int64(0), false)
	seeds.AddRow(uint(1), fuzzing.KindMap, "nested", int64(0), false)

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "giga_executor", readGiga, gigaKeys, seeds)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(gigaKeys, keyIdx)
		configtest.CheckRow(t, "giga_executor", readGiga, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// TestReadConfigAbsentKeysKeepDefaults pins the section's baseline: an app.toml
// with no [giga_executor] section at all resolves to the in-code defaults, not to
// the zero value.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	configtest.CheckAbsent(t, "giga_executor", readGiga, config.DefaultConfig)
}

// TestDefaultsMatchTheRecordedValues pins the giga_executor defaults themselves.
//
// The absent-keys row above proves the reader returns the declared defaults; it cannot prove
// which values those are, because both sides of that comparison come from this package. This
// compares them against testdata/giga_executor.golden, an independent recording, so a default that
// moves shows the new value in a diff instead of passing silently.
func TestDefaultsMatchTheRecordedValues(t *testing.T) {
	configtest.CheckDefaults(t, "giga_executor", config.DefaultConfig)
}

// TestKeyNamesMatchTheRecordedNames pins the two key names themselves.
//
// Both rows above reach their key through config.FlagEnabled and config.FlagOCCEnabled, the
// same constants ReadConfig passes to opts.Get. Editing one of those values renames an
// operator-facing app.toml key and moves the row with it, so every assertion in this file
// keeps passing against a key no node carries. testdata/giga_executor.keys.golden is the
// copy that does not move, and giga_executor.enabled is worth that: it also gates the
// SkipLastResultsHashValidation atomic, so a node resolving it to its default because the
// key it was configured under stopped being read changes consensus behavior.
func TestKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "giga_executor", gigaKeys)
}

// TestManifestNamesEveryField enforces the claim gigaKeys makes about itself: that it names every
// key the reader looks up. Left as prose the claim can drift, and it is the artifact a replacement
// implementation reads as this section's contract.
//
// The exemption list is empty and has to stay reviewed rather than assumed: Config carries exactly
// the two fields the manifest names, so a third one arriving without a row fails here.
func TestManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "giga_executor", config.DefaultConfig, gigaKeys)
}

// TestWiringMatchesTheRecord pins which checks each of this package's sections is wired to.
//
// Every other check here reports a change to what it asserts. None reports a check being removed, so
// this records the wiring and fails when it thins out.
func TestWiringMatchesTheRecord(t *testing.T) {
	configtest.CheckWiring(t)
}
