package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"strings"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

const KeySeparator = "|"

// Migrator is a struct for handling in-place store migrations.
type Migrator struct {
	keeper Keeper
}

// NewMigrator returns a new Migrator.
func NewMigrator(keeper Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// Migrate1to2 migrates from version 1 to 2.
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	// Reset params after removing the denom creation fee param
	defaultParams := types.DefaultParams()
	m.keeper.paramSpace.SetParamSet(ctx, &defaultParams)

	// We remove the denom creation fee whitelist in this migration
	store := ctx.KVStore(m.keeper.storeKey)
	oldCreateDenomFeeWhitelistKey := "createdenomfeewhitelist"

	oldCreateDenomFeeWhitelistPrefix := []byte(strings.Join([]string{oldCreateDenomFeeWhitelistKey, ""}, KeySeparator))
	iter := sdk.KVStorePrefixIterator(store, oldCreateDenomFeeWhitelistPrefix)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		store.Delete(iter.Key())
	}
	return nil
}
