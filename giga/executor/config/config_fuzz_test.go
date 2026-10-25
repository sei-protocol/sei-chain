package config_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/giga/executor/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline: no
// [giga_executor] section means the executor and optimistic concurrency both stay
// on. Both sides move together when a default changes, so this asserts the
// reader's behavior rather than the values themselves.
//
// The defaults matter beyond this section: giga_executor.enabled also decides
// whether app.New flips the process-global atomic that relaxes consensus
// LastResultsHash validation, so a regression that let an absent key resolve to
// false would change consensus behavior without changing app.toml.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	cfg, err := config.ReadConfig(configtest.AppOpts{})
	if err != nil {
		t.Fatalf("an absent [giga_executor] section must read cleanly, got %v", err)
	}
	if cfg != config.DefaultConfig {
		t.Fatalf("an absent [giga_executor] section resolved to %+v, want the declared defaults %+v",
			cfg, config.DefaultConfig)
	}
}
