package migrations_test

import (
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/migrations"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestMigrateDisableRegisterPointer(t *testing.T) {
	k := testkeeper.EVMTestApp.GigaEvmKeeper
	ctx := testkeeper.EVMTestApp.NewContext(false, tmtypes.Header{})
	migrations.MigrateDisableRegisterPointer(ctx, &k)
	require.NotPanics(t, func() { k.GetParams(ctx) })
}
