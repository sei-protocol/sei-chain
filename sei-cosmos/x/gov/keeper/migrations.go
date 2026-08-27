package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

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
	return nil
}

// Migrate2to3 migrates from version 2 to 3.
func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	return nil
}

// Migrate3to4 creates delegation snapshots and active-proposal indexes for all stored votes.
func (m Migrator) Migrate3to4(ctx sdk.Context) error {
	store := ctx.KVStore(m.keeper.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.VotesKeyPrefix)
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid(); iterator.Next() {
		proposalID, voter := types.SplitKeyVote(iterator.Key())
		m.keeper.initializeVoteDelegationTracking(ctx, proposalID, voter)
	}
	return nil
}
