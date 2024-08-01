package migrations

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
)

/**
 * No `dex` state exists in any public chain at the time this data type update happened.
 * Any new chain (including local ones) should be based on a Sei version newer than this update
 * and therefore doesn't need this migration
 */
func V4ToV5(ctx sdk.Context, storeKey sdk.StoreKey, paramStore paramtypes.Subspace) error {
	ClearStore(ctx, storeKey)
	if err := migratePriceSnapshotParam(ctx, paramStore); err != nil {
		return err
	}

	// initialize epoch to 0
	store := ctx.KVStore(storeKey)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, 0)
	store.Set([]byte(keeper.EpochKey), bz)
	return nil
}
