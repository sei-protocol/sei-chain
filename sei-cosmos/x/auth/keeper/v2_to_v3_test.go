package keeper_test

import (
	"github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMigrate2to3(t *testing.T) {
	app, ctx := createTestApp(true)

	prevParams := types.Params{
		MaxMemoCharacters:      types.DefaultMaxMemoCharacters,
		TxSigLimit:             types.DefaultTxSigLimit,
		TxSizeCostPerByte:      types.DefaultTxSizeCostPerByte,
		SigVerifyCostED25519:   types.DefaultSigVerifyCostED25519,
		SigVerifyCostSecp256k1: types.DefaultSigVerifyCostSecp256k1,
	}

	app.AccountKeeper.SetParams(ctx, prevParams)
	// migrate to default params
	m := keeper.NewMigrator(app.AccountKeeper, app.GRPCQueryRouter())
	err := m.Migrate2to3(ctx)
	require.NoError(t, err)
	params := app.AccountKeeper.GetParams(ctx)
	require.Equal(t, params.DisableSeqnoCheck, false)
}
