package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	clientkeeper "github.com/cosmos/ibc-go/v3/modules/core/02-client/keeper"
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
// This migration prunes:
// - migrates solo machine client state from protobuf definition v1 to v2
// - prunes solo machine consensus states
// - prunes expired tendermint consensus states
// - adds ProcessedHeight and Iteration keys for unexpired tendermint consensus states
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	clientMigrator := clientkeeper.NewMigrator(m.keeper.ClientKeeper)
	if err := clientMigrator.Migrate1to2(ctx); err != nil {
		return err
	}

	return nil
}
