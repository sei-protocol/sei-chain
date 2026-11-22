package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
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
	defaultGenesis := types.DefaultGenesis()
	m.keeper.SetCosmosGasParams(ctx, defaultGenesis.CosmosGasParams)
	return nil
}

// Migrate2to3 stores an empty list of disabled message type URLs.
func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	m.keeper.SetDisabledMsgTypeURLs(ctx, []string{})
	return nil
}
