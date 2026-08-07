package cmd

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app/params"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/spf13/cast"
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
// The blast radius is narrow, and worth stating precisely, because it sets what a repair costs.
// SetAppConfigByMode has one non-test caller, cmd/seid/cmd/init.go, which runs during
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
	// That is the defect this suite exists to find.
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
		// Full mode's overlay is identical to the sei-db default, so nothing about the resolved value
		// distinguishes the assignment from its absence. Deleting setFullnodeTypeAppConfig's two
		// StateStore lines leaves this case green.
		//
		// The literals below are still worth asserting, and it is worth being exact about which
		// failures they reach. A changed assignment reddens. So does the sei-db default drifting away
		// from what this mode intends while the assignment is missing, because these numbers are
		// written here rather than read back from the default. Deletion on its own is what cannot be
		// caught, and that is a property of the two values coinciding rather than of the assertion.
		{params.NodeModeFull, func(t *testing.T, e seidbconfig.StateStoreConfig) {
			if !e.Enable {
				t.Error("full mode no longer leaves the state store enabled")
			}
			if e.KeepRecent != 100000 {
				t.Errorf("full mode no longer keeps 100000 recent versions for queries, got %d",
					e.KeepRecent)
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

			// And the other half of the claim, that shadowing reaches only the fields CustomAppConfig
			// redeclares. It redeclares StateStore and ten siblings; MinRetainBlocks and API sit on the
			// embedded struct untouched, so their mode values do reach a node. Adding MinRetainBlocks to
			// CustomAppConfig's own fields would discard it too, taking the failure from partial to
			// total, and this is what reports that.
			assertModeOverlaySurvives(t, c.mode, got)
		})
	}
}

// assertModeOverlaySurvives pins the fields a node mode reaches, as opposed to the state-store fields
// it does not.
//
// Only the arms that discriminate are asserted, and the ones that cannot are named rather than written
// as assertions that would pass whatever the code did. srvconfig.DefaultConfig carries API.Enable false
// and MinRetainBlocks 0, and validator and seed assign exactly those, so neither mode's survival is
// observable from the result. Full assigns API.Enable true and MinRetainBlocks 100000, both away from
// the default, and archive assigns API.Enable true, so those three arms are real.
//
// Archive's MinRetainBlocks is 0, which is the default too, so it is left out for the same reason.
func assertModeOverlaySurvives(t *testing.T, mode params.NodeMode, got CustomAppConfig) {
	t.Helper()

	switch mode {
	case params.NodeModeFull:
		if !got.API.Enable {
			t.Error("full mode's API.Enable no longer reaches the resolved config, so shadowing now " +
				"covers a field it did not: the discarded overlay is wider than this file claims")
		}
		if got.MinRetainBlocks != 100000 {
			t.Errorf("full mode's MinRetainBlocks no longer reaches the resolved config, got %d, want "+
				"100000. Shadowing now covers a field on the embedded struct, so the discarded overlay "+
				"is wider than this file claims", got.MinRetainBlocks)
		}
	case params.NodeModeArchive:
		if !got.API.Enable {
			t.Error("archive mode's API.Enable no longer reaches the resolved config, so shadowing now " +
				"covers a field it did not: the discarded overlay is wider than this file claims")
		}
	case params.NodeModeValidator, params.NodeModeSeed:
		// Both assign what the default already holds, so nothing here can tell survival from
		// coincidence. Named rather than asserted.
	}
}

// TestNewCustomAppConfigKeepsOnlyWhatItIsHanded pins which sections of a generated app.toml can carry
// a caller's values at all.
//
// NewCustomAppConfig builds a struct literal. Two of its fields come from its arguments, the embedded
// srvconfig.Config and EVM, and every other section is a hardcoded default or a hardcoded literal. So
// the mode overlay reaches EVM and is discarded for the rest, which is the same fact
// TestNodeModeStateStoreOverlayIsDiscarded records from the state-store side.
//
// Asserted from both sides here because the contrast is the useful part. A change that started
// forwarding StateStore would fail the other test, and a change that stopped forwarding EVM would fail
// this one, so neither direction moves quietly.
func TestNewCustomAppConfigKeepsOnlyWhatItIsHanded(t *testing.T) {
	base := srvconfig.DefaultConfig()
	base.API.Enable = !base.API.Enable // an embedded field, which must survive

	evm := evmrpcconfig.DefaultConfig
	evm.HTTPEnabled = !evm.HTTPEnabled // an EVM field, which must survive
	evm.HTTPPort = 18545

	got := NewCustomAppConfig(base, evm)

	if got.API.Enable != base.API.Enable {
		t.Error("the embedded srvconfig.Config no longer carries the caller's values, so nothing a " +
			"mode sets on it reaches a generated app.toml")
	}
	if got.EVM.HTTPEnabled != evm.HTTPEnabled || got.EVM.HTTPPort != evm.HTTPPort {
		t.Errorf("the EVM section no longer carries the caller's values, got HTTPEnabled=%v "+
			"HTTPPort=%d. SetEVMConfigByMode writes into that struct during seid init, so this would "+
			"mean a node mode no longer decides whether the EVM server is enabled",
			got.EVM.HTTPEnabled, got.EVM.HTTPPort)
	}

	// And the sections it hardcodes. Each is compared against the default it is built from, so a
	// section that started forwarding a caller value fails here and gets a decision.
	if got.StateStore != seidbconfig.DefaultStateStoreConfig() {
		t.Error("the state-store section is no longer the sei-db default, so it now carries " +
			"something the caller supplied; see TestNodeModeStateStoreOverlayIsDiscarded")
	}
	if got.StateCommit != seidbconfig.DefaultStateCommitConfig() {
		t.Error("the state-commit section is no longer the sei-db default")
	}
	if got.ReceiptStore != seidbconfig.DefaultReceiptStoreConfig() {
		t.Error("the receipt-store section is no longer the sei-db default")
	}
}

// TestGeneratedAppTOMLLruSizeDisagreesWithTheStruct pins a key two generators disagree about.
//
// The app.toml template renders `lru_size = 0` as a bare literal (root.go), and NewCustomAppConfig's
// WASM struct sets LruSize to 1. A generated file therefore carries 0 while the struct that names the
// section says 1, and no first-party reader consumes the key at all, so neither value has an effect
// today.
//
// It is worth pinning rather than fixing because the disagreement is what makes it safe to ignore. If
// a reader is wired later it will resolve 0 from a generated file and 1 from the struct depending on
// which path it takes, and this assertion is what forces that decision into the open instead of
// letting one of the two silently become the answer.
//
// That no first-party code reads the key was established by inspection and is not asserted here, so
// the name says what this holds rather than claiming the absence of a reader. Wiring one would leave
// this row green and correct.
func TestGeneratedAppTOMLLruSizeDisagreesWithTheStruct(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	// The struct side, stated here and compared against the constructor.
	const fromStruct = uint64(1)

	got := NewCustomAppConfig(srvconfig.DefaultConfig(), evmrpcconfig.DefaultConfig)
	if got.WASM.LruSize != fromStruct {
		t.Errorf("NewCustomAppConfig now sets wasm.lru_size to %d rather than %d. If that was to "+
			"match the template, update the template side of this row too", got.WASM.LruSize, fromStruct)
	}

	// The template side, read out of a real generated file rather than restated as a constant. A
	// constant here would leave the template free to move with this row still green, which is the
	// defect this suite exists to find.
	applied := applyLegacy(t, home, nil)
	if applied.err != nil {
		t.Fatalf("Apply: %v", applied.err)
	}
	if !home.Exists("app.toml") {
		t.Fatal("Apply did not materialize app.toml, so this row is not reading a generated file")
	}
	raw := applied.ctx.Viper.Get("wasm.lru_size")
	if raw == nil {
		t.Fatal("a generated app.toml no longer carries wasm.lru_size, so the two generators no " +
			"longer disagree and this row should say what replaced them")
	}
	fromTemplate, castErr := cast.ToUint64E(raw)
	if castErr != nil {
		t.Fatalf("wasm.lru_size = %#v does not convert to uint64: %v", raw, castErr)
	}

	// Both sides pinned, not just their inequality. Asserting only that they differ would leave the
	// template free to render any other value with this row still green, and the doc above claims it
	// renders 0.
	const templateLiteral = uint64(0)
	if fromTemplate != templateLiteral {
		t.Errorf("a generated app.toml renders wasm.lru_size as %d rather than %d. The template "+
			"literal in root.go moved, so update this row and say whether anything reads the key yet",
			fromTemplate, templateLiteral)
	}
	if fromTemplate == fromStruct {
		t.Fatalf("the template and NewCustomAppConfig now agree on wasm.lru_size at %d, so this row "+
			"no longer describes a divergence. Closing it is a fine end state; say which value won "+
			"and whether anything reads the key yet", fromTemplate)
	}
}
