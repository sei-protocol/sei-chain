package types

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	store "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// UpgradeStoreLoader is used to prepare baseapp with a fixed StoreLoader
// pattern. This is useful for custom upgrade loading logic.
func UpgradeStoreLoader(upgradeHeight int64, storeUpgrades *store.StoreUpgrades) baseapp.StoreLoader {
	return func(ms sdk.CommitMultiStore) error {
		if upgradeHeight == ms.LastCommitID().Version+1 {
			// Check if the current commit version and upgrade height matches
			if len(storeUpgrades.Renamed) > 0 || len(storeUpgrades.Deleted) > 0 || len(storeUpgrades.Added) > 0 {
				return ms.LoadLatestVersionAndUpgrade(storeUpgrades)
			}
		}

		// Otherwise load default store loader
		return baseapp.DefaultStoreLoader(ms)
	}
}
