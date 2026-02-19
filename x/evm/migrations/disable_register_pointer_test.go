package migrations_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigrateDisableRegisterPointer(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.NewContext(false, sdk.Header{})
	migrations.MigrateDisableRegisterPointer(ctx, &k)
	require.NotPanics(t, func() { k.GetParams(ctx) })
}
