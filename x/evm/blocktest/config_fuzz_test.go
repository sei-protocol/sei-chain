package blocktest_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	"github.com/sei-protocol/sei-chain/x/evm/blocktest"
)

// ethBlockTestKeys is the [eth_blocktest] section's read-site manifest. Both keys
// are guarded and checked.
//
// This section's name exists twice under two spellings, and the harmless one is the
// generated file. The app.toml template hardcodes the header [eth_blocktest] and both
// eth_blocktest_* key names as literal text, resolving only the values through
// {{ .ETHBlockTest.Field }}, which text/template walks by Go field name. So a generated
// app.toml carries exactly the spellings the reader below looks up and round-trips
// correctly.
//
// The divergent spelling is the mapstructure tag on CustomAppConfig.ETHBlockTest,
// eth_block_test, which names a section the template never writes and the reader never
// reads. It is inert rather than dangerous, and it is worth recording because it is the
// spelling a manager that drove generation from the struct tags — the obvious way to
// build one — would emit, producing a file this reader ignores entirely.
var ethBlockTestKeys = []configtest.KeySpec{
	{
		Key: "eth_blocktest.eth_blocktest_enabled", Path: "Enabled", Cast: configtest.CastBool,
		Checked: true,
		Why:     "default false; the spelling both the template and the reader use",
	},
	{
		Key: "eth_blocktest.eth_blocktest_test_data_path", Path: "TestDataPath", Cast: configtest.CastString,
		Checked: true,
		Why:     "default is a tilde path, expanded by the consumer rather than here",
	},
}

func readETHBlockTest(opts configtest.AppOpts) (any, error) { return blocktest.ReadConfig(opts) }

func FuzzReadConfig(f *testing.F) {
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
	seeds.AddRow(uint(0), fuzzing.KindBool, "true", int64(1), true)
	seeds.AddRow(uint(1), fuzzing.KindString, "~/testdata/", int64(0), false)
	seeds.AddRow(uint(0), fuzzing.KindString, "on", int64(0), false)
	seeds.AddRow(uint(1), fuzzing.KindAnySlice, "", int64(0), false)
	seeds.AddRow(uint(0), fuzzing.KindNil, "", int64(0), false)

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "eth_blocktest", readETHBlockTest, ethBlockTestKeys, seeds)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(ethBlockTestKeys, keyIdx)
		configtest.CheckRow(t, "eth_blocktest", readETHBlockTest, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	configtest.CheckAbsent(t, "eth_blocktest", readETHBlockTest, blocktest.DefaultConfig)
}

// TestStructTagSpellingIsInert records that the mapstructure spelling on
// CustomAppConfig.ETHBlockTest (eth_block_test) is not the one the reader looks up
// (eth_blocktest), so a value written under it resolves to nothing.
//
// The generated app.toml is not affected: the template writes the reader's spelling. What
// this pins is that the struct tag cannot be treated as the section name, which is the
// mistake a manager generating config from the tags would make.
func TestStructTagSpellingIsInert(t *testing.T) {
	cfg, err := blocktest.ReadConfig(configtest.AppOpts{
		"eth_block_test.eth_blocktest_enabled": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("the eth_block_test spelling resolved a value; if the tag and the read prefix " +
			"were unified on purpose, update this test and ship a migration")
	}
}

// TestDefaultsMatchTheRecordedValues pins the eth_blocktest defaults themselves.
//
// The absent-keys row above proves the reader returns the declared defaults; it cannot prove
// which values those are, because both sides of that comparison come from this package. This
// compares them against testdata/eth_blocktest.golden, an independent recording, so a default that
// moves shows the new value in a diff instead of passing silently.
func TestDefaultsMatchTheRecordedValues(t *testing.T) {
	configtest.CheckDefaults(t, "eth_blocktest", blocktest.DefaultConfig)
}

// TestKeyNamesMatchTheRecordedNames pins the two key names themselves.
//
// The section already exists under two spellings — the reader's eth_blocktest and the
// mapstructure tag's eth_block_test — and TestStructTagSpellingIsInert records which one
// resolves. The record holds the same answer for the keys, so unifying the two cannot be done
// by editing the reader's prefix without the change appearing as a diff.
func TestKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "eth_blocktest", ethBlockTestKeys)
}

// TestManifestNamesEveryField enforces the claim ethBlockTestKeys makes about itself: that it names
// every key the reader looks up. Left as prose the claim can drift, and it is the artifact a
// replacement implementation reads as this section's contract.
func TestManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "eth_blocktest", blocktest.DefaultConfig, ethBlockTestKeys)
}

// TestWiringMatchesTheRecord pins which checks each of this package's sections is wired to.
//
// Every other check here reports a change to what it asserts. None reports a check being removed, so
// this records the wiring and fails when it thins out.
func TestWiringMatchesTheRecord(t *testing.T) {
	configtest.CheckWiring(t)
}

// TestNoExperimentalKeyShadowsThisSection is this section's half of the experimental collision
// check.
//
// It lives here because a KeySpec manifest is an unexported package-level var in a _test.go file,
// so this is the only test binary that can see both this section's live keys and the experimental
// registry. A test in cmd/seid/cmd cannot reference these vars at all.
//
// A declared experimental name is the path the key occupies after promotion, so a name equal to one
// of these keys would put two declarations on one path. The check compares whole spellings only;
// a semantic duplicate under a different name stays a review question.
func TestNoExperimentalKeyShadowsThisSection(t *testing.T) {
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, "eth_blocktest", ethBlockTestKeys)
}
