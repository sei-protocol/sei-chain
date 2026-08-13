package config_test

import (
	"reflect"
	"testing"

	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/config/registry"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
)

// The second section to enter the registry, and the first whose baseline varies by mode.

// TestTheRegisteredBaselineIsWhatSeidInitWrites is the drift check that did not exist before.
//
// What a mode implies for this section had two definitions: DefaultConfig, which the reader falls back
// to, and a setter in app/params, which seid init applied when writing app.toml. Nothing compared them,
// so they were free to diverge, and the registry would have had to pick one.
//
// There is now one definition and both read it. This holds that, by driving what init applies against
// what the registry resolves, for every mode. A difference means a node's file and a node's resolved
// configuration disagree about what kind of node it is.
func TestTheRegisteredBaselineIsWhatSeidInitWrites(t *testing.T) {
	section, ok := registry.Lookup(evmrpcconfig.SectionName)
	if !ok {
		t.Fatalf("%s did not register, so nothing below measures anything", evmrpcconfig.SectionName)
	}

	for _, mode := range registry.Modes() {
		// What seid init writes into app.toml for this mode.
		fromInit := evmrpcconfig.DefaultConfig
		params.SetEVMConfigByMode(&fromInit, params.NodeMode(mode))

		fromRegistry, isConfig := section.Defaults(mode).(evmrpcconfig.Config)
		if !isConfig {
			t.Fatalf("the baseline for %q is %T, not this package's own type", mode, section.Defaults(mode))
		}

		if fromRegistry.HTTPEnabled != fromInit.HTTPEnabled || fromRegistry.WSEnabled != fromInit.WSEnabled {
			t.Errorf("for %q mode seid init writes http=%v ws=%v and the registry resolves http=%v ws=%v. "+
				"A node's file and its resolved configuration would disagree about what kind of node it is",
				mode, fromInit.HTTPEnabled, fromInit.WSEnabled,
				fromRegistry.HTTPEnabled, fromRegistry.WSEnabled)
		}
		// DeepEqual rather than ==, because this config holds slices and == does not compile for one.
		// Compared whole, so a mode cannot quietly acquire a difference beyond the two keys it is meant
		// to change.
		if !reflect.DeepEqual(fromRegistry, fromInit) {
			t.Errorf("for %q mode the registry's baseline differs from what init writes beyond the two "+
				"keys a mode is meant to change. A mode must not quietly acquire a difference nobody "+
				"declared", mode)
		}
	}
}

// TestTheBaselineActuallyVariesByMode keeps the check above from holding trivially.
//
// If this section's baseline were the same everywhere, the comparison would pass for a registry that
// ignored mode entirely, which is the bug it exists to catch. A validator does not serve the RPC a
// fullnode serves, and that difference is the one being relied on.
func TestTheBaselineActuallyVariesByMode(t *testing.T) {
	section, _ := registry.Lookup(evmrpcconfig.SectionName)

	validator := section.Defaults(registry.ModeValidator).(evmrpcconfig.Config)
	full := section.Defaults(registry.ModeFull).(evmrpcconfig.Config)
	archive := section.Defaults(registry.ModeArchive).(evmrpcconfig.Config)
	seed := section.Defaults(registry.ModeSeed).(evmrpcconfig.Config)

	if validator.HTTPEnabled {
		t.Error("a validator's baseline serves HTTP RPC. A validator does not serve it, which is what " +
			"keeps its RPC ports closed unless an operator opens them")
	}
	if seed.HTTPEnabled {
		t.Error("a seed's baseline serves HTTP RPC")
	}
	if !full.HTTPEnabled || !archive.HTTPEnabled {
		t.Errorf("a fullnode does not serve HTTP RPC in its baseline (full=%v archive=%v). Archive is "+
			"the mode most easily forgotten when this rule is written out by hand",
			full.HTTPEnabled, archive.HTTPEnabled)
	}
	if validator.HTTPEnabled == full.HTTPEnabled {
		t.Error("every mode resolves the same value, so the drift check above would hold for a registry " +
			"that ignored mode")
	}
}

// TestTheDerivedKeysAreTheKeysThisReaderResolves holds the spellings against the live reader.
//
// This section migrated without a new struct because its tags already produce what its reader resolves.
// That is worth checking rather than assuming: a section whose tags and readers disagree declares keys
// nothing reads and leaves the real ones undeclared, which turns the doctor into a check that fails on
// every node.
func TestTheDerivedKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(evmrpcconfig.SectionName)
	if !ok {
		t.Fatalf("%s did not register", evmrpcconfig.SectionName)
	}
	if len(section.Keys) < 50 {
		t.Fatalf("this section derived %d keys, and it reads far more than that. Something is not being "+
			"walked", len(section.Keys))
	}

	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	// A sample of the keys the reader resolves by explicit constant, including the two a mode varies.
	for _, live := range []string{
		"evm.http_enabled",
		"evm.ws_enabled",
		"evm.max_tx_pool_txs",
		"evm.filter_timeout",
		"evm.cors_origins",
	} {
		if !derived[live] {
			t.Errorf("this section's reader resolves %q and the registry does not derive it. An "+
				"operator's value reaches one spelling and not the other", live)
		}
	}
}

// TestRegisteringProducedNoDefect turns a refused registration into a failure here.
//
// A defect leaves the section absent and every one of its keys silently reading the legacy path, which
// looks identical to a section that was never migrated.
func TestRegisteringProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == evmrpcconfig.SectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent, so all of its keys read "+
				"from the legacy path instead", defect.Section, defect.Err)
		}
	}
}
