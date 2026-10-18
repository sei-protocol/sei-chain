package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

// Migrator is a struct for handling in-place store migrations.
type Migrator struct {
	keeper Keeper
}

// NewMigrator returns a Migrator for the given keeper.
func NewMigrator(keeper Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// Migrate1to2 stores the msgExecuteContractDisabled param as false.
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	m.keeper.paramSpace.Set(ctx, types.ParamStoreKeyMsgExecuteContractDisabled, false)
	return nil
}
