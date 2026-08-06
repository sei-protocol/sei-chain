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
// SetAppConfigByMode assigns per mode. setValidatorTypeAppConfig turns the state store off, for seed
// as well as validator, and setArchiveTypeAppConfig sets KeepRecent to 0 with the comment "keep all
// state history". NewCustomAppConfig then assigns StateStore from the sei-db defaults, and because
// CustomAppConfig embeds srvconfig.Config the outer field shadows the embedded one. So the assignment
// the mode made survives at Config.StateStore and is read from nowhere, while the value a node runs on
// is the default.
//
// The consequence is operator-visible and the reverse of the intent. An archive node, whose purpose is
// retaining history, prunes state at the default KeepRecent instead of keeping all of it.
//
// It fails partially rather than wholly, which is worth knowing before reading a failure here.
// Shadowing only reaches the fields CustomAppConfig redeclares, so MinRetainBlocks and API.Enable and
// GRPCWeb.Enable sit on the embedded struct and do take effect. Archive mode works for those and not
// for its state store.
//
// The blast radius is narrower than it first looks, and stating it wrongly is how a repair gets
// deprioritised. SetAppConfigByMode has one non-test caller, cmd/seid/cmd/init.go, which runs during
// seid init and hands its result to WriteConfigFile. initAppConfig in root.go builds the default
// template without consulting the mode. So the overlay only ever reaches the app.toml that seid init
// renders once. An existing node reads the app.toml already on its disk, so repairing the ordering
// would change what a newly initialised node gets rather than what a running fleet prunes on restart.
//
// Asserted rather than repaired all the same, and tracked as PLT-955. The standing decision for this
// suite is to pin how configuration resolves today and to correct it in the versioned manager, so that
// a correction arrives as a deliberate change rather than inside a test PR. What this test stops
// meanwhile is the behaviour moving by accident in either direction. Removing the overwrite makes the
// modes take effect, and nothing reported that.
func TestNodeModeStateStoreOverlayIsDiscarded(t *testing.T) {
	defaults := seidbconfig.DefaultStateStoreConfig()

	// What each mode assigns, written down here rather than read back from the code that assigns it.
	// Deriving the expectation from SetAppConfigByMode's own output would compare the code against
	// itself. A mode that stopped assigning would move both sides together and the assertion would hold.
	// That is the defect this suite exists to find, and the first version of this test had it.
	for _, c := range []struct {
		mode   params.NodeMode
		assert func(t *testing.T, embedded seidbconfig.StateStoreConfig)
	}{
		{params.NodeModeValidator, func(t *testing.T, e seidbconfig.StateStoreConfig) {
			if e.Enable {
				t.Error("validator mode no longer disables the state store in setValidatorTypeAppConfig")
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
				t.Errorf("setArchiveTypeAppConfig no longer sets KeepRecent to 0 for keeping all "+
					"state history, got %d", e.KeepRecent)
			}
		}},
	} {
		t.Run(string(c.mode), func(t *testing.T) {
			base := srvconfig.DefaultConfig()
			params.SetAppConfigByMode(base, c.mode)
			got := NewCustomAppConfig(base, evmrpcconfig.DefaultConfig)

			if got.StateStore != defaults {
				t.Errorf("the state-store config seid init serialises for a %s node is no longer the "+
					"sei-db default.\ngot:  %+v\nwant: %+v\n"+
					"If the mode overlay now reaches the rendered value, that is a change to the app.toml "+
					"every newly initialised node of this mode gets, and it needs recording here "+
					"deliberately rather than arriving as a passing test.", c.mode, got.StateStore, defaults)
			}

			// The mode's assignment survives on the embedded copy, which is where it goes to be
			// ignored. Checked against what the mode is supposed to assign, so a mode that stopped
			// assigning is caught rather than absorbed.
			c.assert(t, got.Config.StateStore)
		})
	}
}
