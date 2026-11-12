package keeper

import (
	"github.com/gogo/protobuf/grpc"
	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

var logger = seilog.NewLogger("cosmos", "x", "auth", "keeper")

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

// Migrate3to4 starts the rewrite of every account stored under a type of the removed vesting module
// as the BaseAccount it embeds, which RewriteLegacyVestingAccounts carries out a batch per block from
// the upgrade block on. Balances are not touched, so coins that were still locked become spendable.
func (m Migrator) Migrate3to4(ctx sdk.Context) error {
	m.keeper.startLegacyVestingRewrite(ctx)
	return nil
}
