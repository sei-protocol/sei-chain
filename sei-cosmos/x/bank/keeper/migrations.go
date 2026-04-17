package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// Migrator is a struct for handling in-place store migrations.
type Migrator struct {
	keeper BaseKeeper
}

// NewMigrator returns a new Migrator.
func NewMigrator(keeper BaseKeeper) Migrator {
	return Migrator{keeper: keeper}
}

// Migrate1to2 migrates from version 1 to 2.
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	return nil
}
