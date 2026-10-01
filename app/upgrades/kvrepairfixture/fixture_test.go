package kvrepairfixture_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/migration"
	"github.com/sei-protocol/sei-chain/app/upgrades"
	fx "github.com/sei-protocol/sei-chain/app/upgrades/kvrepairfixture"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

type appOpts map[string]interface{}

func (o appOpts) Get(key string) interface{} { return o[key] }

func TestSeedAndDamage(t *testing.T) {
	a := app.Setup(t, false, false, false)
	m := upgrades.NewHardForkManager(fx.ChainID)
	fx.Register(m, &a.EvmKeeper, a.ParamsKeeper, appOpts{fx.FlagDamage: true})

	run := func(height int64) {
		ctx := a.BaseApp.NewContext(false, tmproto.Header{Height: height, ChainID: fx.ChainID})
		require.True(t, m.TargetHeightReached(ctx))
		m.ExecuteForTargetHeight(ctx)
	}
	ctx := a.BaseApp.NewContext(false, tmproto.Header{ChainID: fx.ChainID})

	run(fx.SeedHeight)
	var rate uint64
	subspace, _ := a.ParamsKeeper.GetSubspace(migration.SubspaceName)
	subspace.GetIfExists(ctx, migration.KeyNumKeysToMigratePerBlock, &rate)
	require.Zero(t, rate, "the migration starts after the seed")
	run(fx.MigrationStartHeight)
	subspace.Get(ctx, migration.KeyNumKeysToMigratePerBlock, &rate)
	require.Equal(t, fx.NumKeysToMigratePerBlock, rate)
	require.Equal(t, fx.SeededValue(fx.SeededSlots-1), a.EvmKeeper.GetState(ctx, fx.Contract, fx.Slot(fx.SeededSlots-1)))
	require.Equal(t, fx.SeededCode, a.EvmKeeper.GetCode(ctx, fx.CodeContract))
	require.Equal(t, uint64(fx.SeededNonce), a.EvmKeeper.GetNonce(ctx, fx.NonceAccount))

	run(fx.DamageHeight)
	for _, i := range fx.DamagedSlots.Set {
		require.Equal(t, fx.Sentinel, a.EvmKeeper.GetState(ctx, fx.Contract, fx.Slot(i)))
	}
	for _, i := range fx.DamagedSlots.Delete {
		require.Equal(t, common.Hash{}, a.EvmKeeper.GetState(ctx, fx.Contract, fx.Slot(i)))
	}
	require.Equal(t, fx.Sentinel, a.EvmKeeper.GetState(ctx, fx.Contract, fx.Slot(fx.ExtraSlot)))
	require.Equal(t, fx.DamagedCode, a.EvmKeeper.GetCode(ctx, fx.CodeContract))
	require.Equal(t, uint64(fx.SeededNonce+4), a.EvmKeeper.GetNonce(ctx, fx.NonceAccount))
	require.Equal(t, uint64(0), a.EvmKeeper.GetNonce(ctx, fx.ZeroedNonceAccount))
}

func TestDamageNeedsFlag(t *testing.T) {
	a := app.Setup(t, false, false, false)
	m := upgrades.NewHardForkManager(fx.ChainID)
	fx.Register(m, &a.EvmKeeper, a.ParamsKeeper, appOpts{})
	ctx := a.BaseApp.NewContext(false, tmproto.Header{Height: fx.DamageHeight, ChainID: fx.ChainID})
	require.False(t, m.TargetHeightReached(ctx))
}
