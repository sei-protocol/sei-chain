package app

import (
	"testing"

	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

// retainedStores are mounted KV stores that no registered module owns. A store
// stays on this list when its history must remain readable at the store level
// after its module is gone; dropping it instead is application-hash breaking
// and needs a StoreUpgrades{Deleted} entry at a specific upgrade height.
var retainedStores = map[string]struct {
	upgrade string
	reason  string
}{
	feegrantModuleName: {
		upgrade: "v6.7",
		reason:  "module removed in v6.7; allowances kept for historical state access",
	},
}

// storeKeyOwners names the owning module for the KV stores whose key differs
// from the module name. Every other mounted store is keyed by its own module.
var storeKeyOwners = map[string]string{
	"acc": "auth",
}

func owningModuleName(storeKey string) string {
	if owner, ok := storeKeyOwners[storeKey]; ok {
		return owner
	}
	return storeKey
}

// Removing a module from the manager does not remove it from the stored module
// version map: SetModuleVersionMap only writes the keys it is given and never
// deletes the ones it is not, so a departing module's entry survives every
// later upgrade unless a handler calls DeleteModuleVersion for it. This asserts
// the whole map rather than the names v6.7 happens to drop, so the next module
// removal that forgets the call fails here instead of leaving a version entry
// on chain forever.
func TestLatestUpgradeLeavesNoOrphanedModuleVersions(t *testing.T) {
	previousUpgrades := upgradesList
	t.Cleanup(func() { upgradesList = previousUpgrades })

	t.Setenv("UPGRADE_VERSION_LIST", LatestUpgrade)
	testApp := Setup(t, false, false, false)
	testApp.RegisterUpgradeHandlers()
	ctx := testApp.NewContext(false, tmproto.Header{})

	registered := make(map[string]struct{}, len(testApp.mm.Modules))
	for name := range testApp.mm.Modules {
		registered[name] = struct{}{}
	}

	// Model a chain that carried every removed module's version across earlier
	// upgrades, which is what a node upgrading into this release straight from
	// v6.6 has in state.
	versionMap := testApp.UpgradeKeeper.GetModuleVersionMap(ctx)
	for name := range retainedStores {
		versionMap[name] = 1
	}
	for _, name := range v68DeletedStores {
		versionMap[name] = 1
	}
	testApp.UpgradeKeeper.SetModuleVersionMap(ctx, versionMap)

	testApp.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{
		Name:   LatestUpgrade,
		Height: ctx.BlockHeight(),
	})

	for name := range testApp.UpgradeKeeper.GetModuleVersionMap(ctx) {
		require.Contains(t, registered, name,
			"module version map still carries %q, which no registered module owns; "+
				"the upgrade handler needs a DeleteModuleVersion call for it", name)
	}
}

// Every mounted KV store should be owned by a registered module or be named on
// the retained list. An unowned store that nobody declared is state the chain
// keeps paying for with no code able to read it, which is how a module removal
// half lands: the manager entry goes, the store stays, and nothing says whether
// that was the intent.
func TestMountedStoresAreOwnedOrExplicitlyRetained(t *testing.T) {
	testApp := Setup(t, false, false, false)

	var unowned []string
	for _, storeKey := range kvStoreKeyNames {
		if _, ok := testApp.mm.Modules[owningModuleName(storeKey)]; ok {
			continue
		}
		if _, ok := retainedStores[storeKey]; ok {
			continue
		}
		unowned = append(unowned, storeKey)
	}

	require.Empty(t, unowned,
		"these KV stores are mounted but no registered module owns them; for each, either "+
			"delete it at an upgrade height, add it to retainedStores with the reason, or "+
			"record its owning module in storeKeyOwners")
}

// A retained store is only worth retaining if it can still be read. This fails
// if a later change drops one from the mount list while leaving it declared
// retained, which would make the declaration a comment rather than a fact.
func TestRetainedStoresRemainMounted(t *testing.T) {
	testApp := Setup(t, false, false, false)

	for storeKey, retained := range retainedStores {
		require.Contains(t, kvStoreKeyNames, storeKey,
			"%q is declared retained (%s) but is not mounted", storeKey, retained.reason)
		require.NotNil(t, testApp.GetKey(storeKey),
			"%q is declared retained (%s) but has no store key", storeKey, retained.reason)
	}
}
