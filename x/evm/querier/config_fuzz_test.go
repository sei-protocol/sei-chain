package querier_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/x/evm/querier"
)

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline: no [evm_query]
// section means EVM state queries run under the declared gas limit rather than a
// limit of 0, which would fail every such query on a node that came up clean.
// Both sides move together when a default changes, so this asserts the reader's
// behavior rather than the value itself.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	cfg, err := querier.ReadConfig(configtest.AppOpts{})
	if err != nil {
		t.Fatalf("an absent [evm_query] section must read cleanly, got %v", err)
	}
	if cfg != querier.DefaultConfig {
		t.Fatalf("an absent [evm_query] section resolved to %+v, want the declared defaults %+v",
			cfg, querier.DefaultConfig)
	}
}
