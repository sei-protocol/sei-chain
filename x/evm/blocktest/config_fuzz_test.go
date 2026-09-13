package blocktest_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/x/evm/blocktest"
)

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline: no
// [eth_blocktest] section means block testing stays off at the declared test data
// path. Both sides move together when a default changes, so this asserts the
// reader's behavior rather than the values themselves.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	cfg, err := blocktest.ReadConfig(configtest.AppOpts{})
	if err != nil {
		t.Fatalf("an absent [eth_blocktest] section must read cleanly, got %v", err)
	}
	if cfg != blocktest.DefaultConfig {
		t.Fatalf("an absent [eth_blocktest] section resolved to %+v, want the declared defaults %+v",
			cfg, blocktest.DefaultConfig)
	}
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
