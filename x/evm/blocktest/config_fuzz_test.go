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
// The section name is worth stating plainly because it exists twice under two
// spellings. The runtime reads eth_blocktest.*, while CustomAppConfig tags the
// struct eth_block_test, so at first-boot generation the generated section header
// is [eth_block_test] and no eth_blocktest.* environment variable or flag seeds
// it. Runtime reads are unaffected — they go through the keys below — but the two
// spellings are why a generated app.toml and a hand-written one behave
// differently.
var ethBlockTestKeys = []configtest.KeySpec{
	{
		Key: "eth_blocktest.eth_blocktest_enabled", Path: "Enabled", Cast: configtest.CastBool,
		Checked: true,
		Why:     "default false; the runtime spelling, distinct from the generated [eth_block_test] section",
	},
	{
		Key: "eth_blocktest.eth_blocktest_test_data_path", Path: "TestDataPath", Cast: configtest.CastString,
		Checked: true,
		Why:     "default is a tilde path, expanded by the consumer rather than here",
	},
}

func readETHBlockTest(opts configtest.AppOpts) (any, error) { return blocktest.ReadConfig(opts) }

func FuzzReadConfig(f *testing.F) {
	f.Add(uint(0), uint8(2), "true", int64(1), true)
	f.Add(uint(1), uint8(1), "~/testdata/", int64(0), false)
	f.Add(uint(0), uint8(1), "on", int64(0), false)
	f.Add(uint(1), uint8(10), "", int64(0), false)
	f.Add(uint(0), uint8(0), "", int64(0), false)

	f.Fuzz(func(t *testing.T, keyIdx uint, kind uint8, s string, n int64, b bool) {
		spec := configtest.Pick(ethBlockTestKeys, keyIdx)
		configtest.CheckRow(t, "eth_blocktest", readETHBlockTest, spec, fuzzing.ConfigValue(kind, s, n, b))
	})
}

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	configtest.CheckAbsent(t, "eth_blocktest", readETHBlockTest, blocktest.DefaultConfig)
}

// TestGeneratedSectionSpellingIsInert records that the section name
// CustomAppConfig generates ([eth_block_test]) is not the one the runtime reads
// ([eth_blocktest]). A value under the generated spelling resolves to nothing.
func TestGeneratedSectionSpellingIsInert(t *testing.T) {
	cfg, err := blocktest.ReadConfig(configtest.AppOpts{
		"eth_block_test.eth_blocktest_enabled": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("the generated [eth_block_test] spelling resolved a value; " +
			"if the two spellings were unified on purpose, update this test and ship a migration")
	}
}
