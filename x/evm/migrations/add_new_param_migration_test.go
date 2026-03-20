package migrations_test

import (
	"testing"

	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/migrations"
	"github.com/stretchr/testify/require"
)

func TestAddNewParamsAndSetAllToDefaults(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.NewContext(false, tmtypes.Header{})
	migrations.AddNewParamsAndSetAllToDefaults(ctx, &k)
	require.NotPanics(t, func() { k.GetParams(ctx) })
}
