package keeper_test

import (
	"encoding/binary"
	"testing"

	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	"github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	gogotypes "github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestMigrate2to3(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 2, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	valAddrs := simapp.ConvertAddrsToValAddrs(addrDels)
	pks := simapp.CreateTestPubKeys(2)
	addr, val := valAddrs[0], pks[0]
	addr2, val2 := valAddrs[1], pks[1]
	tstaking := teststaking.NewHelper(t, ctx, app.StakingKeeper)

	params := app.SlashingKeeper.GetParams(ctx)
	params.SignedBlocksWindow = 10
	app.SlashingKeeper.SetParams(ctx, params)

	ctx = ctx.WithBlockHeight(app.SlashingKeeper.SignedBlocksWindow(ctx) + 1)

	// Validator created
	amt := tstaking.CreateValidatorWithValPower(addr, val, 100, true)
	amt2 := tstaking.CreateValidatorWithValPower(addr2, val2, 100, true)

	staking.EndBlocker(ctx, app.StakingKeeper)
	require.Equal(
		t, app.BankKeeper.GetAllBalances(ctx, sdk.AccAddress(addr)),
		sdk.NewCoins(sdk.NewCoin(app.StakingKeeper.GetParams(ctx).BondDenom, InitTokens.Sub(amt))),
	)
	require.Equal(t, amt, app.StakingKeeper.Validator(ctx, addr).GetBondedTokens())
	require.Equal(
		t, app.BankKeeper.GetAllBalances(ctx, sdk.AccAddress(addr2)),
		sdk.NewCoins(sdk.NewCoin(app.StakingKeeper.GetParams(ctx).BondDenom, InitTokens.Sub(amt))),
	)
	require.Equal(t, amt2, app.StakingKeeper.Validator(ctx, addr2).GetBondedTokens())

	consAddr := sdk.GetConsAddress(val)
	consAddr2 := sdk.GetConsAddress(val2)
	store := ctx.KVStore(app.SlashingKeeper.GetStoreKey())

	signInfo := types.ValidatorSigningInfoLegacyMissedHeights{
		Address:             consAddr.String(),
		StartHeight:         21,
		MissedBlocksCounter: 2,
	}
	signInfo2 := types.ValidatorSigningInfoLegacyMissedHeights{
		Address:             consAddr2.String(),
		StartHeight:         22,
		MissedBlocksCounter: 3,
	}
	oldSignInfo := types.ValidatorSigningInfo{
		Address:             signInfo.Address,
		StartHeight:         signInfo.StartHeight,
		IndexOffset:         5,
		JailedUntil:         signInfo.JailedUntil,
		Tombstoned:          signInfo.Tombstoned,
		MissedBlocksCounter: signInfo.MissedBlocksCounter,
	}
	bz := app.AppCodec().MustMarshal(&oldSignInfo)
	store.Set(types.ValidatorSigningInfoKey(consAddr), bz)

	oldSignInfo2 := types.ValidatorSigningInfo{
		Address:             signInfo2.Address,
		StartHeight:         signInfo2.StartHeight,
		IndexOffset:         5,
		JailedUntil:         signInfo2.JailedUntil,
		Tombstoned:          signInfo2.Tombstoned,
		MissedBlocksCounter: signInfo2.MissedBlocksCounter,
	}
	bz2 := app.AppCodec().MustMarshal(&oldSignInfo2)
	store.Set(types.ValidatorSigningInfoKey(consAddr2), bz2)

	bz = store.Get(types.ValidatorMissedBlockBitArrayKey(consAddr))
	require.Nil(t, bz)
	bz = store.Get(types.ValidatorMissedBlockBitArrayKey(consAddr2))
	require.Nil(t, bz)

	for i := 0; i < 5; i++ {
		keyPrefix := types.ValidatorMissedBlockBitArrayKey(consAddr)
		index := make([]byte, 8)
		binary.LittleEndian.PutUint64(index, uint64(i))
		key := append(keyPrefix, index...)
		bz := app.AppCodec().MustMarshal(&gogotypes.BoolValue{Value: true})
		store.Set(key, bz)

		keyPrefix2 := types.ValidatorMissedBlockBitArrayKey(consAddr2)
		index2 := make([]byte, 8)
		binary.LittleEndian.PutUint64(index2, uint64(i+1))
		key2 := append(keyPrefix2, index2...)
		bz2 := app.AppCodec().MustMarshal(&gogotypes.BoolValue{Value: true})
		store.Set(key2, bz2)
	}

	m := keeper.NewMigrator(app.SlashingKeeper)
	err := m.Migrate2to3(ctx)
	require.NoError(t, err)

	var missedArray types.ValidatorMissedBlockArrayLegacyMissedHeights
	bz = store.Get(types.ValidatorMissedBlockBitArrayKey(consAddr))
	app.AppCodec().MustUnmarshal(bz, &missedArray)
	require.Equal(t, []int64{21, 22, 23, 24, 25}, missedArray.MissedHeights)

	var s types.ValidatorSigningInfoLegacyMissedHeights
	signInfoKey := types.ValidatorSigningInfoKey(consAddr)
	bz = store.Get(signInfoKey)
	app.AppCodec().MustUnmarshal(bz, &s)
	require.Equal(t, signInfo, s)

	var missedArray2 types.ValidatorMissedBlockArrayLegacyMissedHeights
	bz = store.Get(types.ValidatorMissedBlockBitArrayKey(consAddr2))
	app.AppCodec().MustUnmarshal(bz, &missedArray2)
	require.Equal(t, []int64{23, 24, 25, 26, 27}, missedArray2.MissedHeights)

	var s2 types.ValidatorSigningInfoLegacyMissedHeights
	signInfoKey2 := types.ValidatorSigningInfoKey(consAddr2)
	bz = store.Get(signInfoKey2)
	app.AppCodec().MustUnmarshal(bz, &s2)
	require.Equal(t, signInfo2, s2)
}

func TestMigrate2to4(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 2, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	valAddrs := simapp.ConvertAddrsToValAddrs(addrDels)
	pks := simapp.CreateTestPubKeys(2)
	addr, val := valAddrs[0], pks[0]
	addr2, val2 := valAddrs[1], pks[1]
	tstaking := teststaking.NewHelper(t, ctx, app.StakingKeeper)

	params := app.SlashingKeeper.GetParams(ctx)
	params.SignedBlocksWindow = 10
	app.SlashingKeeper.SetParams(ctx, params)

	ctx = ctx.WithBlockHeight(29)

	// Validator created
	amt := tstaking.CreateValidatorWithValPower(addr, val, 100, true)
	amt2 := tstaking.CreateValidatorWithValPower(addr2, val2, 100, true)

	staking.EndBlocker(ctx, app.StakingKeeper)
	require.Equal(
		t, app.BankKeeper.GetAllBalances(ctx, sdk.AccAddress(addr)),
		sdk.NewCoins(sdk.NewCoin(app.StakingKeeper.GetParams(ctx).BondDenom, InitTokens.Sub(amt))),
	)
	require.Equal(t, amt, app.StakingKeeper.Validator(ctx, addr).GetBondedTokens())
	require.Equal(
		t, app.BankKeeper.GetAllBalances(ctx, sdk.AccAddress(addr2)),
		sdk.NewCoins(sdk.NewCoin(app.StakingKeeper.GetParams(ctx).BondDenom, InitTokens.Sub(amt))),
	)
	require.Equal(t, amt2, app.StakingKeeper.Validator(ctx, addr2).GetBondedTokens())

	consAddr := sdk.GetConsAddress(val)
	consAddr2 := sdk.GetConsAddress(val2)
	store := ctx.KVStore(app.SlashingKeeper.GetStoreKey())

	signInfo := types.ValidatorSigningInfoLegacyMissedHeights{
		Address:             consAddr.String(),
		StartHeight:         21,
		MissedBlocksCounter: 2,
	}
	signInfo2 := types.ValidatorSigningInfoLegacyMissedHeights{
		Address:             consAddr2.String(),
		StartHeight:         22,
		MissedBlocksCounter: 3,
	}
	oldSignInfo := types.ValidatorSigningInfo{
		Address:             signInfo.Address,
		StartHeight:         signInfo.StartHeight,
		IndexOffset:         5,
		JailedUntil:         signInfo.JailedUntil,
		Tombstoned:          signInfo.Tombstoned,
		MissedBlocksCounter: signInfo.MissedBlocksCounter,
	}
	bz := app.AppCodec().MustMarshal(&oldSignInfo)
	store.Set(types.ValidatorSigningInfoKey(consAddr), bz)

	oldSignInfo2 := types.ValidatorSigningInfo{
		Address:             signInfo2.Address,
		StartHeight:         signInfo2.StartHeight,
		IndexOffset:         5,
		JailedUntil:         signInfo2.JailedUntil,
		Tombstoned:          signInfo2.Tombstoned,
		MissedBlocksCounter: signInfo2.MissedBlocksCounter,
	}
	bz2 := app.AppCodec().MustMarshal(&oldSignInfo2)
	store.Set(types.ValidatorSigningInfoKey(consAddr2), bz2)

	bz = store.Get(types.ValidatorMissedBlockBitArrayKey(consAddr))
	require.Nil(t, bz)
	bz = store.Get(types.ValidatorMissedBlockBitArrayKey(consAddr2))
	require.Nil(t, bz)

	for i := 0; i < 5; i++ {
		keyPrefix := types.ValidatorMissedBlockBitArrayKey(consAddr)
		index := make([]byte, 8)
		binary.LittleEndian.PutUint64(index, uint64(i))
		key := append(keyPrefix, index...)
		bz := app.AppCodec().MustMarshal(&gogotypes.BoolValue{Value: true})
		store.Set(key, bz)

		keyPrefix2 := types.ValidatorMissedBlockBitArrayKey(consAddr2)
		index2 := make([]byte, 8)
		binary.LittleEndian.PutUint64(index2, uint64(i+1))
		key2 := append(keyPrefix2, index2...)
		bz2 := app.AppCodec().MustMarshal(&gogotypes.BoolValue{Value: true})
		store.Set(key2, bz2)
	}

	m := keeper.NewMigrator(app.SlashingKeeper)
	err := m.Migrate2to3(ctx)
	require.NoError(t, err)

	var missedArray types.ValidatorMissedBlockArrayLegacyMissedHeights
	bz = store.Get(types.ValidatorMissedBlockBitArrayKey(consAddr))
	app.AppCodec().MustUnmarshal(bz, &missedArray)
	require.Equal(t, []int64{21, 22, 23, 24, 25}, missedArray.MissedHeights)

	var s types.ValidatorSigningInfoLegacyMissedHeights
	signInfoKey := types.ValidatorSigningInfoKey(consAddr)
	bz = store.Get(signInfoKey)
	app.AppCodec().MustUnmarshal(bz, &s)
	require.Equal(t, signInfo, s)

	var missedArray2 types.ValidatorMissedBlockArrayLegacyMissedHeights
	bz = store.Get(types.ValidatorMissedBlockBitArrayKey(consAddr2))
	app.AppCodec().MustUnmarshal(bz, &missedArray2)
	require.Equal(t, []int64{23, 24, 25, 26, 27}, missedArray2.MissedHeights)

	var s2 types.ValidatorSigningInfoLegacyMissedHeights
	signInfoKey2 := types.ValidatorSigningInfoKey(consAddr2)
	bz = store.Get(signInfoKey2)
	app.AppCodec().MustUnmarshal(bz, &s2)
	require.Equal(t, signInfo2, s2)

	err = m.Migrate3to4(ctx)
	require.NoError(t, err)

	signInfoNew, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, int64(9), signInfoNew.IndexOffset)

	signInfoNew2, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr2)
	require.True(t, found)
	require.Equal(t, int64(9), signInfoNew2.IndexOffset)

	bg0 := uint64(0)
	bg0 |= 1 << 2
	bg0 |= 1 << 3
	bg0 |= 1 << 4
	bg0 |= 1 << 5
	bg0 |= 1 << 6
	missedInfo, found := app.SlashingKeeper.GetValidatorMissedBlocks(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, []uint64{bg0}, missedInfo.MissedBlocks)
	require.Equal(t, int64(10), missedInfo.WindowSize)

	bg1 := uint64(0)
	bg1 |= 1 << 4
	bg1 |= 1 << 5
	bg1 |= 1 << 6
	bg1 |= 1 << 7
	bg1 |= 1 << 8
	missedInfo2, found := app.SlashingKeeper.GetValidatorMissedBlocks(ctx, consAddr2)
	require.True(t, found)
	require.Equal(t, []uint64{bg1}, missedInfo2.MissedBlocks)
	require.Equal(t, int64(10), missedInfo.WindowSize)

}

func TestMigrate3to4(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 2, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	valAddrs := simapp.ConvertAddrsToValAddrs(addrDels)
	pks := simapp.CreateTestPubKeys(2)
	addr, val := valAddrs[0], pks[0]
	addr2, val2 := valAddrs[1], pks[1]
	tstaking := teststaking.NewHelper(t, ctx, app.StakingKeeper)

	params := app.SlashingKeeper.GetParams(ctx)
	params.SignedBlocksWindow = 10
	app.SlashingKeeper.SetParams(ctx, params)

	ctx = ctx.WithBlockHeight(29)

	// Validator created
	amt := tstaking.CreateValidatorWithValPower(addr, val, 100, true)
	amt2 := tstaking.CreateValidatorWithValPower(addr2, val2, 100, true)

	staking.EndBlocker(ctx, app.StakingKeeper)
	require.Equal(
		t, app.BankKeeper.GetAllBalances(ctx, sdk.AccAddress(addr)),
		sdk.NewCoins(sdk.NewCoin(app.StakingKeeper.GetParams(ctx).BondDenom, InitTokens.Sub(amt))),
	)
	require.Equal(t, amt, app.StakingKeeper.Validator(ctx, addr).GetBondedTokens())
	require.Equal(
		t, app.BankKeeper.GetAllBalances(ctx, sdk.AccAddress(addr2)),
		sdk.NewCoins(sdk.NewCoin(app.StakingKeeper.GetParams(ctx).BondDenom, InitTokens.Sub(amt))),
	)
	require.Equal(t, amt2, app.StakingKeeper.Validator(ctx, addr2).GetBondedTokens())

	consAddr := sdk.GetConsAddress(val)
	consAddr2 := sdk.GetConsAddress(val2)
	store := ctx.KVStore(app.SlashingKeeper.GetStoreKey())

	signInfo := types.ValidatorSigningInfo{
		Address:             consAddr.String(),
		StartHeight:         21,
		MissedBlocksCounter: 2,
	}
	signInfo2 := types.ValidatorSigningInfo{
		Address:             consAddr2.String(),
		StartHeight:         22,
		MissedBlocksCounter: 3,
	}
	oldSignInfo := types.ValidatorSigningInfoLegacyMissedHeights{
		Address:             signInfo.Address,
		StartHeight:         signInfo.StartHeight,
		JailedUntil:         signInfo.JailedUntil,
		Tombstoned:          signInfo.Tombstoned,
		MissedBlocksCounter: signInfo.MissedBlocksCounter,
	}
	bz := app.AppCodec().MustMarshal(&oldSignInfo)
	store.Set(types.ValidatorSigningInfoKey(consAddr), bz)

	oldSignInfo2 := types.ValidatorSigningInfoLegacyMissedHeights{
		Address:             signInfo2.Address,
		StartHeight:         signInfo2.StartHeight,
		JailedUntil:         signInfo2.JailedUntil,
		Tombstoned:          signInfo2.Tombstoned,
		MissedBlocksCounter: signInfo2.MissedBlocksCounter,
	}
	bz2 := app.AppCodec().MustMarshal(&oldSignInfo2)
	store.Set(types.ValidatorSigningInfoKey(consAddr2), bz2)

	missedArray := types.ValidatorMissedBlockArrayLegacyMissedHeights{
		Address:       consAddr.String(),
		MissedHeights: []int64{21, 22, 23, 24, 25},
	}
	bz = app.AppCodec().MustMarshal(&missedArray)
	store.Set(types.ValidatorMissedBlockBitArrayKey(consAddr), bz)

	missedArray2 := types.ValidatorMissedBlockArrayLegacyMissedHeights{
		Address:       consAddr.String(),
		MissedHeights: []int64{23, 24, 25, 26, 27},
	}
	bz = app.AppCodec().MustMarshal(&missedArray2)
	store.Set(types.ValidatorMissedBlockBitArrayKey(consAddr2), bz)

	m := keeper.NewMigrator(app.SlashingKeeper)
	err := m.Migrate3to4(ctx)
	require.NoError(t, err)
	signInfoNew, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, int64(9), signInfoNew.IndexOffset)

	signInfoNew2, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr2)
	require.True(t, found)
	require.Equal(t, int64(9), signInfoNew2.IndexOffset)

	bg0 := uint64(0)
	bg0 |= 1 << 2
	bg0 |= 1 << 3
	bg0 |= 1 << 4
	bg0 |= 1 << 5
	bg0 |= 1 << 6
	missedInfo, found := app.SlashingKeeper.GetValidatorMissedBlocks(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, []uint64{bg0}, missedInfo.MissedBlocks)
	require.Equal(t, int64(10), missedInfo.WindowSize)

	bg1 := uint64(0)
	bg1 |= 1 << 4
	bg1 |= 1 << 5
	bg1 |= 1 << 6
	bg1 |= 1 << 7
	bg1 |= 1 << 8
	missedInfo2, found := app.SlashingKeeper.GetValidatorMissedBlocks(ctx, consAddr2)
	require.True(t, found)
	require.Equal(t, []uint64{bg1}, missedInfo2.MissedBlocks)
	require.Equal(t, int64(10), missedInfo.WindowSize)
}
