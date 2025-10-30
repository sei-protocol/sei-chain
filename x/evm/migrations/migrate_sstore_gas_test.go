package migrations_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/evm/migrations"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestMigrateSstoreGas(t *testing.T) {
	a := app.Setup(t, false, false, false)
	k := a.EvmKeeper
	ctx := a.GetContextForDeliverTx([]byte{})

	params := k.GetParams(ctx)
	params.SeiSstoreSetGasEip2200 = 12345
	k.SetParams(ctx, params)

	require.NoError(t, migrations.MigrateSstoreGas(ctx, &k))

	updated := k.GetParams(ctx)
	require.Equal(t, types.DefaultSeiSstoreSetGasEIP2200, updated.SeiSstoreSetGasEip2200)
}
