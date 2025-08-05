package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/slashing/types"
)

func TestGetSetValidatorSigningInfo(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 1, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))

	info, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, sdk.ConsAddress(addrDels[0]))
	require.False(t, found)
	newInfo := types.NewValidatorSigningInfo(
		sdk.ConsAddress(addrDels[0]),
		int64(4),
		int64(3),
		time.Unix(2, 0),
		false,
		int64(10),
	)
	app.SlashingKeeper.SetValidatorSigningInfo(ctx, sdk.ConsAddress(addrDels[0]), newInfo)
	info, found = app.SlashingKeeper.GetValidatorSigningInfo(ctx, sdk.ConsAddress(addrDels[0]))
	require.True(t, found)
	require.Equal(t, info.StartHeight, int64(4))
	require.Equal(t, info.IndexOffset, int64(3))
	require.Equal(t, info.JailedUntil, time.Unix(2, 0).UTC())
	require.Equal(t, info.MissedBlocksCounter, int64(10))
}

func TestTombstoned(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 1, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))

	require.Panics(t, func() { app.SlashingKeeper.Tombstone(ctx, sdk.ConsAddress(addrDels[0])) })
	require.False(t, app.SlashingKeeper.IsTombstoned(ctx, sdk.ConsAddress(addrDels[0])))

	newInfo := types.NewValidatorSigningInfo(
		sdk.ConsAddress(addrDels[0]),
		int64(4),
		int64(3),
		time.Unix(2, 0),
		false,
		int64(10),
	)
	app.SlashingKeeper.SetValidatorSigningInfo(ctx, sdk.ConsAddress(addrDels[0]), newInfo)

	require.False(t, app.SlashingKeeper.IsTombstoned(ctx, sdk.ConsAddress(addrDels[0])))
	app.SlashingKeeper.Tombstone(ctx, sdk.ConsAddress(addrDels[0]))
	require.True(t, app.SlashingKeeper.IsTombstoned(ctx, sdk.ConsAddress(addrDels[0])))
	require.Panics(t, func() { app.SlashingKeeper.Tombstone(ctx, sdk.ConsAddress(addrDels[0])) })
}

func TestJailUntil(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 1, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))

	require.Panics(t, func() { app.SlashingKeeper.JailUntil(ctx, sdk.ConsAddress(addrDels[0]), time.Now()) })

	newInfo := types.NewValidatorSigningInfo(
		sdk.ConsAddress(addrDels[0]),
		int64(4),
		int64(3),
		time.Unix(2, 0),
		false,
		int64(10),
	)
	app.SlashingKeeper.SetValidatorSigningInfo(ctx, sdk.ConsAddress(addrDels[0]), newInfo)
	app.SlashingKeeper.JailUntil(ctx, sdk.ConsAddress(addrDels[0]), time.Unix(253402300799, 0).UTC())

	info, ok := app.SlashingKeeper.GetValidatorSigningInfo(ctx, sdk.ConsAddress(addrDels[0]))
	require.True(t, ok)
	require.Equal(t, time.Unix(253402300799, 0).UTC(), info.JailedUntil)
}

func TestParseBitGroupsAndBoolArrays(t *testing.T) {
	app := simapp.Setup(false)
	bitGroup0 := uint64(0)
	bitGroup0 |= 1 << 1
	bitGroup0 |= 1 << 33
	bitGroup0 |= 1 << 63

	bitGroup1 := uint64(0)
	bitGroup1 |= 1 << 2
	bitGroup1 |= 1 << 32
	bitGroup1 |= 1 << 62 // should be ignores

	bitGroups := []uint64{bitGroup0, bitGroup1}

	boolArray := app.SlashingKeeper.ParseBitGroupsToBoolArray(bitGroups, 100)
	require.Equal(t, 100, len(boolArray))
	fmt.Println(boolArray)
	for i, val := range boolArray {
		if i == 1 || i == 33 || i == 63 || i == 66 || i == 96 {
			require.Equal(t, true, val)
		} else {
			require.Equal(t, false, val)
		}
	}

	expectedBitGroup1 := uint64(0)
	expectedBitGroup1 |= 1 << 2
	expectedBitGroup1 |= 1 << 32

	parsedBitGroups := app.SlashingKeeper.ParseBoolArrayToBitGroups(boolArray)
	require.Equal(t, 2, len(bitGroups))
	expectedBitGroups := []uint64{bitGroup0, expectedBitGroup1}
	require.Equal(t, expectedBitGroups, parsedBitGroups)
}

func TestGetSetValidatorMissedArrayBit(t *testing.T) {
	app := simapp.Setup(false)

	bg0 := uint64(0)
	bg0 |= 1 << 2
	bg0 |= 1 << 23
	bg1 := uint64(0)
	bg1 |= 1 << 4
	bitGroups := []uint64{bg0, bg1}

	require.True(t, app.SlashingKeeper.GetBooleanFromBitGroups(bitGroups, 2))
	require.True(t, app.SlashingKeeper.GetBooleanFromBitGroups(bitGroups, 23))
	require.True(t, app.SlashingKeeper.GetBooleanFromBitGroups(bitGroups, 68))
	require.False(t, app.SlashingKeeper.GetBooleanFromBitGroups(bitGroups, 69))

	bitGroups = app.SlashingKeeper.SetBooleanInBitGroups(bitGroups, 69, true)
	bitGroups = app.SlashingKeeper.SetBooleanInBitGroups(bitGroups, 68, false)
	bitGroups = app.SlashingKeeper.SetBooleanInBitGroups(bitGroups, 23, false)

	require.False(t, app.SlashingKeeper.GetBooleanFromBitGroups(bitGroups, 23))
	require.False(t, app.SlashingKeeper.GetBooleanFromBitGroups(bitGroups, 68))
	require.True(t, app.SlashingKeeper.GetBooleanFromBitGroups(bitGroups, 69))
}
