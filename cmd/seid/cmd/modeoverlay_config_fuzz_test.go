package cmd

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app/params"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
)

// TestNodeModeStateStoreOverlayIsDiscarded pins that a node's mode does not reach its state-store
// settings, which is what happens today and is not what the code reads as intending.
//
// SetAppConfigByMode assigns per mode: validator and seed turn the state store off
// (app/params/config.go:146), and archive sets KeepRecent to 0 with the comment "keep all state
// history" (:173). NewCustomAppConfig then assigns StateStore from the sei-db defaults, and because
// CustomAppConfig embeds srvconfig.Config the outer field shadows the embedded one. So the assignment
// the mode made survives at Config.StateStore and is read from nowhere, while the value a node runs on
// is the default.
//
// The consequence is operator-visible and the reverse of the intent: an archive node, whose purpose is
// retaining history, prunes state at the default KeepRecent instead of keeping all of it.
//
// Asserted rather than repaired. Changing which value wins would change what every existing archive
// node prunes on its next restart, so this suite records the behaviour and the repair belongs to a
// change that can be rolled out deliberately. What this stops is the behaviour changing by accident in
// either direction: removing the overwrite makes the modes take effect, and nothing reported that.
func TestNodeModeStateStoreOverlayIsDiscarded(t *testing.T) {
	defaults := seidbconfig.DefaultStateStoreConfig()

	// What each mode assigns, written down here rather than read back from the code that assigns it.
	// Deriving the expectation from SetAppConfigByMode's own output would compare the code against
	// itself: a mode that stopped assigning would move both sides together and the assertion would hold.
	// That is the defect this suite exists to find, and the first version of this test had it.
	for _, c := range []struct {
		mode   params.NodeMode
		assert func(t *testing.T, embedded seidbconfig.StateStoreConfig)
	}{
		{params.NodeModeValidator, func(t *testing.T, e seidbconfig.StateStoreConfig) {
			if e.Enable {
				t.Error("validator mode no longer disables the state store (app/params/config.go:146)")
			}
		}},
		{params.NodeModeSeed, func(t *testing.T, e seidbconfig.StateStoreConfig) {
			if e.Enable {
				t.Error("seed mode no longer disables the state store; it shares validator's assignments")
			}
		}},
		{params.NodeModeFull, func(t *testing.T, e seidbconfig.StateStoreConfig) {
			if !e.Enable {
				t.Error("full mode no longer leaves the state store enabled")
			}
		}},
		{params.NodeModeArchive, func(t *testing.T, e seidbconfig.StateStoreConfig) {
			if e.KeepRecent != 0 {
				t.Errorf("archive mode no longer sets KeepRecent to 0 for keeping all state history, "+
					"got %d (app/params/config.go:173)", e.KeepRecent)
			}
		}},
	} {
		t.Run(string(c.mode), func(t *testing.T) {
			base := srvconfig.DefaultConfig()
			params.SetAppConfigByMode(base, c.mode)
			got := NewCustomAppConfig(base, evmrpcconfig.DefaultConfig)

			if got.StateStore != defaults {
				t.Errorf("the state-store config a %s node runs on is no longer the sei-db default.\n"+
					"got:  %+v\nwant: %+v\n"+
					"If the mode overlay now reaches the value a node reads, that is a behaviour change "+
					"for every node of this mode on its next restart, and it needs recording here "+
					"deliberately rather than arriving as a passing test.", c.mode, got.StateStore, defaults)
			}

			// The mode's assignment survives on the embedded copy, which is where it goes to be
			// ignored. Checked against what the mode is supposed to assign, so a mode that stopped
			// assigning is caught rather than absorbed.
			c.assert(t, got.Config.StateStore)
		})
	}
}
