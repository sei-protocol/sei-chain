package cmd

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app/params"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
)

// TestTheRenderedConfigStillDiscardsTheStateStoreRules is the half of a departure that lives here.
//
// A section elsewhere declares what this command writes for a kind of node, and it departs from this command
// on three values because of one thing this file does: the type rendered here declares a state store field
// of its own and fills it from the mode-blind default, so every rule applied to the server configuration is
// applied and then thrown away.
//
// That section cannot measure this. It cannot import this package, because that direction is the cycle, so
// it compares against the same default this file reads rather than against what this file produces. A change
// that made this keep the rules would leave it green and its record stale. This is the test that fails then.
func TestTheRenderedConfigStillDiscardsTheStateStoreRules(t *testing.T) {
	blind := seidbconfig.DefaultStateStoreConfig()

	for _, tc := range []struct {
		mode  params.NodeMode
		named string
		ruled any
		got   func(CustomAppConfig) any
		blind any
	}{
		{params.NodeModeValidator, "ss-enable", false,
			func(c CustomAppConfig) any { return c.StateStore.Enable }, blind.Enable},
		{params.NodeModeSeed, "ss-enable", false,
			func(c CustomAppConfig) any { return c.StateStore.Enable }, blind.Enable},
		{params.NodeModeArchive, "ss-keep-recent", 0,
			func(c CustomAppConfig) any { return c.StateStore.KeepRecent }, blind.KeepRecent},
	} {
		base := srvconfig.DefaultConfig()
		params.SetAppConfigByMode(base, tc.mode)

		// What the rules put on the server configuration, before this file renders anything.
		switch tc.named {
		case "ss-enable":
			if base.StateStore.Enable != tc.ruled {
				t.Errorf("the rules for %s no longer set %s to %v, so this row measures nothing",
					tc.mode, tc.named, tc.ruled)
				continue
			}
		case "ss-keep-recent":
			if base.StateStore.KeepRecent != tc.ruled {
				t.Errorf("the rules for %s no longer set %s to %v, so this row measures nothing",
					tc.mode, tc.named, tc.ruled)
				continue
			}
		}

		rendered := NewCustomAppConfig(base, evmrpcconfig.DefaultConfig)
		if got := tc.got(rendered); got != tc.blind {
			t.Errorf("%s for %s renders %v, and the mode-blind default is %v. This command has stopped "+
				"discarding the rules, so the section that declares these values no longer departs from "+
				"it and its record should go", tc.named, tc.mode, got, tc.blind)
		}
	}
}
