package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func MigrateBlockBloom(ctx sdk.Context, k *keeper.Keeper) error {
	k.SetLegacyBlockBloomCutoffHeight(ctx)

	prefsToDelete := [][]byte{}
	k.IterateAll(ctx, types.BlockBloomPrefix, func(key, _ []byte) bool {
		if len(key) > 0 {
			prefsToDelete = append(prefsToDelete, key)
		}
		return false
	})
	store := k.PrefixStore(ctx, types.BlockBloomPrefix)
	for _, pref := range prefsToDelete {
		store.Delete(pref)
	}

	return nil
}
