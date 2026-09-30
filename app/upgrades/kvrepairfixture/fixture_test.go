package kvrepairfixture_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/migration"
	"github.com/sei-protocol/sei-chain/app/upgrades"
	"github.com/sei-protocol/sei-chain/app/upgrades/kvrepair"
	fx "github.com/sei-protocol/sei-chain/app/upgrades/kvrepairfixture"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type appOpts map[string]interface{}

func (o appOpts) Get(key string) interface{} { return o[key] }

func rawKey(slot common.Hash) kvrepair.HexBytes {
	return append(append([]byte{0x03}, fx.Contract.Bytes()...), slot.Bytes()...)
}

func hexPtr(b []byte) *kvrepair.HexBytes {
	h := kvrepair.HexBytes(b)
	return &h
}

func TestSeedDamageRepair(t *testing.T) {
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
	subspace.Get(ctx, migration.KeyNumKeysToMigratePerBlock, &rate)
	require.Equal(t, fx.NumKeysToMigratePerBlock, rate)

	run(fx.DamageHeight)
	require.Equal(t, fx.Sentinel, a.EvmKeeper.GetState(ctx, fx.Contract, fx.Slot(0)))
	require.Equal(t, common.Hash{}, a.EvmKeeper.GetState(ctx, fx.Contract, fx.Slot(2)))
	require.Equal(t, fx.Sentinel, a.EvmKeeper.GetState(ctx, fx.Contract, fx.Slot(fx.ExtraSlot)))

	sentinel := fx.Sentinel.Bytes()
	repair := kvrepair.Repair{Name: "fixture", ChainID: fx.ChainID, Height: fx.DamageHeight + 1, Entries: []kvrepair.Entry{
		{Store: evmtypes.StoreKey, Key: rawKey(fx.Slot(0)), Value: hexPtr(fx.SeededValue(0).Bytes()), Expect: hexPtr(sentinel)},
		{Store: evmtypes.StoreKey, Key: rawKey(fx.Slot(1)), Value: hexPtr(fx.SeededValue(1).Bytes()), Expect: hexPtr(sentinel)},
		{Store: evmtypes.StoreKey, Key: rawKey(fx.Slot(2)), Value: hexPtr(fx.SeededValue(2).Bytes()), ExpectAbsent: true},
		{Store: evmtypes.StoreKey, Key: rawKey(fx.Slot(fx.ExtraSlot)), Expect: hexPtr(sentinel)},
	}}
	h := kvrepair.NewHandler(repair, map[string]*sdk.KVStoreKey{evmtypes.StoreKey: a.GetKey(evmtypes.StoreKey)})
	require.NoError(t, h.ExecuteHandler(ctx))

	for i := int64(0); i < fx.SeededSlots; i++ {
		require.Equal(t, fx.SeededValue(i), a.EvmKeeper.GetState(ctx, fx.Contract, fx.Slot(i)), "slot %d", i)
	}
	require.Equal(t, common.Hash{}, a.EvmKeeper.GetState(ctx, fx.Contract, fx.Slot(fx.ExtraSlot)))
}

func TestDamageNeedsFlag(t *testing.T) {
	a := app.Setup(t, false, false, false)
	m := upgrades.NewHardForkManager(fx.ChainID)
	fx.Register(m, &a.EvmKeeper, a.ParamsKeeper, appOpts{})
	ctx := a.BaseApp.NewContext(false, tmproto.Header{Height: fx.DamageHeight, ChainID: fx.ChainID})
	require.False(t, m.TargetHeightReached(ctx))
}
