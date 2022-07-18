package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

/**
 * No `dex` state exists in any public chain at the time this data type update happened.
 * Any new chain (including local ones) should be based on a Sei version newer than this update
 * and therefore doesn't need this migration
 */
func V4ToV5(ctx sdk.Context, storeKey sdk.StoreKey) error {
	ClearStore(ctx, storeKey)
	return nil
}
