package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogotypes "github.com/gogo/protobuf/types"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

// Migrator is a struct for handling in-place store migrations.
type Migrator struct {
	keeper Keeper
}

// NewMigrator returns a new Migrator.
func NewMigrator(keeper Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// Migrate2to3 migrates from version 2 to 3.
func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	store := ctx.KVStore(m.keeper.storeKey)

	iter := sdk.KVStorePrefixIterator(store, types.ExchangeRateKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		dp := sdk.DecProto{}
		m.keeper.cdc.MustUnmarshal(iter.Value(), &dp)
		// create proto for new data value
		// because we don't have a lastUpdate, we set it to 0
		rate := types.OracleExchangeRate{ExchangeRate: dp.Dec, LastUpdate: sdk.ZeroInt()}
		bz := m.keeper.cdc.MustMarshal(&rate)
		store.Set(iter.Key(), bz)
	}

	return nil
}

// Migrate3to4 migrates from version 3 to 4
func (m Migrator) Migrate3to4(ctx sdk.Context) error {
	// we need to migrate the miss counters to be stored as VotePenaltyCounter to introduce abstain count logic
	store := ctx.KVStore(m.keeper.storeKey)

	// previously the data was stored as uint64, now it is VotePenaltyCounter proto
	iter := sdk.KVStorePrefixIterator(store, types.VotePenaltyCounterKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var missCounter gogotypes.UInt64Value
		m.keeper.cdc.MustUnmarshal(iter.Value(), &missCounter)
		// create proto for new data value
		// because we don't have a lastUpdate, we set it to 0
		votePenaltyCounter := types.VotePenaltyCounter{MissCount: missCounter.Value, AbstainCount: 0}
		bz := m.keeper.cdc.MustMarshal(&votePenaltyCounter)
		store.Set(iter.Key(), bz)
	}

	return nil
}

// Migrate3to4 migrates from version 4 to 5
func (m Migrator) Migrate4to5(ctx sdk.Context) error {
	// we remove the prevotes from store in this migration
	store := ctx.KVStore(m.keeper.storeKey)

	oldPrevoteKey := []byte{0x04}
	iter := sdk.KVStorePrefixIterator(store, oldPrevoteKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		store.Delete(iter.Key())
	}
	return nil
}

func (m Migrator) Migrate5To6(ctx sdk.Context) error {
	// Do a one time backfill for success count in the vote penalty counter
	store := ctx.KVStore(m.keeper.storeKey)

	// previously the data was stored as uint64, now it is VotePenaltyCounter proto
	iter := sdk.KVStorePrefixIterator(store, types.VotePenaltyCounterKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var votePenaltyCounter types.VotePenaltyCounter
		m.keeper.cdc.MustUnmarshal(iter.Value(), &votePenaltyCounter)
		slashWindow := m.keeper.GetParams(ctx).SlashWindow
		totalPenaltyCount := votePenaltyCounter.MissCount + votePenaltyCounter.AbstainCount
		successCount := ((uint64)(ctx.BlockHeight()) % slashWindow) - totalPenaltyCount
		newVotePenaltyCounter := types.VotePenaltyCounter{
			MissCount:    votePenaltyCounter.MissCount,
			AbstainCount: votePenaltyCounter.AbstainCount,
			SuccessCount: successCount,
		}
		bz := m.keeper.cdc.MustMarshal(&newVotePenaltyCounter)
		store.Set(iter.Key(), bz)
	}
	return nil
}
