package keeper

import (
	"github.com/gogo/protobuf/grpc"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// Migrator is a struct for handling in-place store migrations.
type Migrator struct {
	keeper      AccountKeeper
	queryServer grpc.Server
}

// NewMigrator returns a new Migrator.
func NewMigrator(keeper AccountKeeper, queryServer grpc.Server) Migrator {
	return Migrator{keeper: keeper, queryServer: queryServer}
}

// Migrate1to2 migrates from version 1 to 2.
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	var iterErr error

	m.keeper.IterateAccounts(ctx, func(account types.AccountI) (stop bool) {
		return false
	})

	return iterErr
}

func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	defaultParams := types.DefaultParams()
	m.keeper.SetParams(ctx, defaultParams)
	return nil
}
