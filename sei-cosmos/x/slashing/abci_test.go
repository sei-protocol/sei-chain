package slashing_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	"github.com/cosmos/cosmos-sdk/x/slashing/testslashing"
	"github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func TestBeginBlocker(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	pks := simapp.CreateTestPubKeys(5)
	simapp.AddTestAddrsFromPubKeys(app, ctx, pks, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	tstaking := teststaking.NewHelper(t, ctx, app.StakingKeeper)

	trueVotes := []abci.VoteInfo{}
	falseVotes := []abci.VoteInfo{}

	for i := 0; i < 5; i++ {
		addr, pk := sdk.ValAddress(pks[i].Address()), pks[i]
		// bond the validator
		power := int64(100)
		amt := tstaking.CreateValidatorWithValPower(addr, pk, power, true)
		staking.EndBlocker(ctx, app.StakingKeeper)
		require.Equal(
			t, app.BankKeeper.GetAllBalances(ctx, sdk.AccAddress(addr)),
			sdk.NewCoins(sdk.NewCoin(app.StakingKeeper.GetParams(ctx).BondDenom, InitTokens.Sub(amt))),
		)
		require.Equal(t, amt, app.StakingKeeper.Validator(ctx, addr).GetBondedTokens())
		val := abci.Validator{
			Address: pk.Address(),
			Power:   power,
		}
		trueVotes = append(trueVotes, abci.VoteInfo{
			Validator:       val,
			SignedLastBlock: true,
		})
		falseVotes = append(falseVotes, abci.VoteInfo{
			Validator:       val,
			SignedLastBlock: i != 0,
		})
	}

	params := app.SlashingKeeper.GetParams(ctx)
	params.SignedBlocksWindow = 10000
	params.MinSignedPerWindow = sdk.MustNewDecFromStr("0.5")
	app.SlashingKeeper.SetParams(ctx, params)

	// mark the validator as having signed
	req := abci.RequestBeginBlock{
		LastCommitInfo: abci.LastCommitInfo{
			Votes: trueVotes,
		},
	}
	slashing.BeginBlocker(ctx, req, app.SlashingKeeper)

	for i := 0; i < 5; i++ {
		info, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, sdk.ConsAddress(pks[i].Address()))
		require.True(t, found)
		require.Equal(t, ctx.BlockHeight(), info.StartHeight)
		require.Equal(t, time.Unix(0, 0).UTC(), info.JailedUntil)
		require.Equal(t, int64(0), info.MissedBlocksCounter)
	}

	height := int64(0)

	// for 10000 blocks, mark the validator as having signed
	for ; height < app.SlashingKeeper.SignedBlocksWindow(ctx); height++ {
		ctx = ctx.WithBlockHeight(height)
		req = abci.RequestBeginBlock{
			LastCommitInfo: abci.LastCommitInfo{
				Votes: trueVotes,
			},
		}

		slashing.BeginBlocker(ctx, req, app.SlashingKeeper)
	}

	// for 5000 blocks, mark the validator as having not signed
	for ; height < ((app.SlashingKeeper.SignedBlocksWindow(ctx) * 2) - app.SlashingKeeper.MinSignedPerWindow(ctx) + 1); height++ {
		ctx = ctx.WithBlockHeight(height)
		req = abci.RequestBeginBlock{
			LastCommitInfo: abci.LastCommitInfo{
				Votes: falseVotes,
			},
		}

		slashing.BeginBlocker(ctx, req, app.SlashingKeeper)
	}

	// end block
	staking.EndBlocker(ctx, app.StakingKeeper)

	// validator should be jailed
	for i := 0; i < 5; i++ {
		validator, found := app.StakingKeeper.GetValidatorByConsAddr(ctx, sdk.GetConsAddress(pks[i]))
		require.True(t, found)
		if i == 0 {
			require.Equal(t, stakingtypes.Unbonding, validator.GetStatus())
		} else {
			require.Equal(t, stakingtypes.Bonded, validator.GetStatus())
		}
	}
}

func TestResizeTrimResetValidatorMissedBlocksArray(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 1, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	valAddrs := simapp.ConvertAddrsToValAddrs(addrDels)

	pks := simapp.CreateTestPubKeys(1)

	tstaking := teststaking.NewHelper(t, ctx, app.StakingKeeper)

	addr, val := valAddrs[0], pks[0]
	tstaking.CreateValidatorWithValPower(addr, val, 200, true)

	staking.EndBlocker(ctx, app.StakingKeeper)

	consAddr := sdk.GetConsAddress(val)

	newInfo := types.NewValidatorSigningInfo(
		consAddr,
		int64(4),
		int64(7),
		time.Unix(2, 0),
		false,
		int64(3),
	)
	app.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, newInfo)

	bitGroup := uint64(0)
	bitGroup |= 1 << 5
	bitGroup |= 1 << 7
	bitGroup |= 1 << 9
	tooLargeArray := types.ValidatorMissedBlockArray{
		Address:      consAddr.String(),
		WindowSize:   10,
		MissedBlocks: []uint64{bitGroup},
	}
	app.SlashingKeeper.SetValidatorMissedBlocks(ctx, consAddr, tooLargeArray)

	params := app.SlashingKeeper.GetParams(ctx)
	params.SignedBlocksWindow = 8
	app.SlashingKeeper.SetParams(ctx, params)

	ctx = ctx.WithBlockHeight(18)
	slashing.BeginBlocker(ctx, testslashing.CreateBeginBlockReq(val.Address(), 200, true), app.SlashingKeeper)

	bitGroup = uint64(0)
	missedInfo, found := app.SlashingKeeper.GetValidatorMissedBlocks(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, 1, len(missedInfo.MissedBlocks))
	require.Equal(t, int64(8), missedInfo.WindowSize)
	require.Equal(t, []uint64{bitGroup}, missedInfo.MissedBlocks)

	info, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, int64(0), info.MissedBlocksCounter)
	require.Equal(t, int64(1), info.IndexOffset)

}

func TestResizeExpandValidatorMissedBlocksArray(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 1, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	valAddrs := simapp.ConvertAddrsToValAddrs(addrDels)

	pks := simapp.CreateTestPubKeys(1)

	tstaking := teststaking.NewHelper(t, ctx, app.StakingKeeper)

	addr, val := valAddrs[0], pks[0]
	tstaking.CreateValidatorWithValPower(addr, val, 200, true)

	staking.EndBlocker(ctx, app.StakingKeeper)

	params := app.SlashingKeeper.GetParams(ctx)
	params.SignedBlocksWindow = 10
	app.SlashingKeeper.SetParams(ctx, params)

	consAddr := sdk.GetConsAddress(val)

	newInfo := types.NewValidatorSigningInfo(
		consAddr,
		int64(4),
		int64(3),
		time.Unix(2, 0),
		false,
		int64(2),
	)
	app.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, newInfo)

	bitGroup := uint64(0)
	bitGroup |= 1 << 1
	bitGroup |= 1 << 3
	tooSmallArray := types.ValidatorMissedBlockArray{
		Address:      consAddr.String(),
		WindowSize:   8,
		MissedBlocks: []uint64{bitGroup},
	}
	app.SlashingKeeper.SetValidatorMissedBlocks(ctx, consAddr, tooSmallArray)

	ctx = ctx.WithBlockHeight(39)
	slashing.BeginBlocker(ctx, testslashing.CreateBeginBlockReq(val.Address(), 200, true), app.SlashingKeeper)

	bitGroup = uint64(0)
	bitGroup |= 1 << 1
	bitGroup |= 1 << 5
	missedInfo, found := app.SlashingKeeper.GetValidatorMissedBlocks(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, 1, len(missedInfo.MissedBlocks))
	require.Equal(t, []uint64{bitGroup}, missedInfo.MissedBlocks)

	info, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, int64(2), info.MissedBlocksCounter)
	require.Equal(t, int64(4), info.IndexOffset)

}

func TestResizeExpandShiftValidatorMissedBlocksArrayMultipleBitGroups(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 1, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	valAddrs := simapp.ConvertAddrsToValAddrs(addrDels)

	pks := simapp.CreateTestPubKeys(1)

	tstaking := teststaking.NewHelper(t, ctx, app.StakingKeeper)

	addr, val := valAddrs[0], pks[0]
	tstaking.CreateValidatorWithValPower(addr, val, 200, true)

	staking.EndBlocker(ctx, app.StakingKeeper)

	params := app.SlashingKeeper.GetParams(ctx)
	params.SignedBlocksWindow = 66
	app.SlashingKeeper.SetParams(ctx, params)

	consAddr := sdk.GetConsAddress(val)

	newInfo := types.NewValidatorSigningInfo(
		consAddr,
		int64(0),
		int64(61),
		time.Unix(2, 0),
		false,
		int64(4),
	)
	app.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, newInfo)

	bg0 := uint64(0)
	bg0 |= 1 << 0
	bg0 |= 1 << 20
	bg0 |= 1 << 61
	bg0 |= 1 << 62
	tooSmallArray := types.ValidatorMissedBlockArray{
		Address:      consAddr.String(),
		WindowSize:   64,
		MissedBlocks: []uint64{bg0},
	}
	app.SlashingKeeper.SetValidatorMissedBlocks(ctx, consAddr, tooSmallArray)

	ctx = ctx.WithBlockHeight(2053)
	slashing.BeginBlocker(ctx, testslashing.CreateBeginBlockReq(val.Address(), 200, true), app.SlashingKeeper)

	bg0 = uint64(0)
	bg0 |= 1 << 0
	bg0 |= 1 << 20
	bg0 |= 1 << 63
	bg1 := uint64(0)
	bg1 |= 1 << 0
	missedInfo, found := app.SlashingKeeper.GetValidatorMissedBlocks(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, 2, len(missedInfo.MissedBlocks))
	require.Equal(t, int64(66), missedInfo.WindowSize)
	require.Equal(t, []uint64{bg0, bg1}, missedInfo.MissedBlocks)

	info, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, int64(4), info.MissedBlocksCounter)
	require.Equal(t, int64(62), info.IndexOffset)
}

func TestResizeExpandShiftValidatorMissedBlocksArrayMultipleBitGroupsBeforeAndAfter(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 1, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	valAddrs := simapp.ConvertAddrsToValAddrs(addrDels)

	pks := simapp.CreateTestPubKeys(1)

	tstaking := teststaking.NewHelper(t, ctx, app.StakingKeeper)

	addr, val := valAddrs[0], pks[0]
	tstaking.CreateValidatorWithValPower(addr, val, 200, true)

	staking.EndBlocker(ctx, app.StakingKeeper)

	params := app.SlashingKeeper.GetParams(ctx)
	params.SignedBlocksWindow = 100
	app.SlashingKeeper.SetParams(ctx, params)

	consAddr := sdk.GetConsAddress(val)

	newInfo := types.NewValidatorSigningInfo(
		consAddr,
		int64(0),
		int64(5),
		time.Unix(2, 0),
		false,
		int64(7),
	)
	app.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, newInfo)

	bg0 := uint64(0)
	bg0 |= 1 << 0
	bg0 |= 1 << 20
	bg0 |= 1 << 35
	bg0 |= 1 << 36
	bg1 := uint64(0)
	bg1 |= 1 << 3
	bg1 |= 1 << 4
	bg1 |= 1 << 7
	tooSmallArray := types.ValidatorMissedBlockArray{
		Address:      consAddr.String(),
		WindowSize:   72,
		MissedBlocks: []uint64{bg0, bg1},
	}
	app.SlashingKeeper.SetValidatorMissedBlocks(ctx, consAddr, tooSmallArray)

	ctx = ctx.WithBlockHeight(509)
	slashing.BeginBlocker(ctx, testslashing.CreateBeginBlockReq(val.Address(), 200, true), app.SlashingKeeper)

	bg0 = uint64(0)
	bg0 |= 1 << 0
	bg0 |= 1 << 48
	bg0 |= 1 << 63
	bg1 = uint64(0)
	bg1 |= 1 << 0
	bg1 |= 1 << 31
	bg1 |= 1 << 32
	bg1 |= 1 << 35
	missedInfo, found := app.SlashingKeeper.GetValidatorMissedBlocks(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, 2, len(missedInfo.MissedBlocks))
	require.Equal(t, int64(100), missedInfo.WindowSize)
	require.Equal(t, []uint64{bg0, bg1}, missedInfo.MissedBlocks)

	info, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, int64(7), info.MissedBlocksCounter)
	require.Equal(t, int64(6), info.IndexOffset)
}

func TestResizeTrimValidatorMissedBlocksArrayMultipleBitGroups(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 1, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	valAddrs := simapp.ConvertAddrsToValAddrs(addrDels)

	pks := simapp.CreateTestPubKeys(1)

	tstaking := teststaking.NewHelper(t, ctx, app.StakingKeeper)

	addr, val := valAddrs[0], pks[0]
	tstaking.CreateValidatorWithValPower(addr, val, 200, true)

	staking.EndBlocker(ctx, app.StakingKeeper)

	params := app.SlashingKeeper.GetParams(ctx)
	params.SignedBlocksWindow = 72
	app.SlashingKeeper.SetParams(ctx, params)

	consAddr := sdk.GetConsAddress(val)

	newInfo := types.NewValidatorSigningInfo(
		consAddr,
		int64(0),
		int64(5),
		time.Unix(2, 0),
		false,
		int64(8),
	)
	app.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, newInfo)

	bg0 := uint64(0)
	bg0 |= 1 << 5
	bg0 |= 1 << 6
	bg0 |= 1 << 33
	bg0 |= 1 << 34
	bg1 := uint64(0)
	bg1 |= 1 << 23
	bg1 |= 1 << 24
	bg1 |= 1 << 32
	bg1 |= 1 << 35
	tooSmallArray := types.ValidatorMissedBlockArray{
		Address:      consAddr.String(),
		WindowSize:   100,
		MissedBlocks: []uint64{bg0, bg1},
	}
	app.SlashingKeeper.SetValidatorMissedBlocks(ctx, consAddr, tooSmallArray)

	ctx = ctx.WithBlockHeight(509)
	slashing.BeginBlocker(ctx, testslashing.CreateBeginBlockReq(val.Address(), 200, false), app.SlashingKeeper)

	missedInfo, found := app.SlashingKeeper.GetValidatorMissedBlocks(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, 2, len(missedInfo.MissedBlocks))
	require.Equal(t, int64(72), missedInfo.WindowSize)
	require.Equal(t, []uint64{uint64(1), uint64(0)}, missedInfo.MissedBlocks)

	info, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, int64(1), info.MissedBlocksCounter)
	require.Equal(t, int64(1), info.IndexOffset)
}

func TestResizeTrimValidatorMissedBlocksArrayEliminateBitGroup(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 1, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	valAddrs := simapp.ConvertAddrsToValAddrs(addrDels)

	pks := simapp.CreateTestPubKeys(1)

	tstaking := teststaking.NewHelper(t, ctx, app.StakingKeeper)

	addr, val := valAddrs[0], pks[0]
	tstaking.CreateValidatorWithValPower(addr, val, 200, true)

	staking.EndBlocker(ctx, app.StakingKeeper)

	params := app.SlashingKeeper.GetParams(ctx)
	params.SignedBlocksWindow = 128
	app.SlashingKeeper.SetParams(ctx, params)

	consAddr := sdk.GetConsAddress(val)

	newInfo := types.NewValidatorSigningInfo(
		consAddr,
		int64(0),
		int64(447),
		time.Unix(2, 0),
		false,
		int64(6),
	)
	app.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, newInfo)

	bg0 := uint64(0)
	bg0 |= 1 << 0
	bg0 |= 1 << 63
	bg1 := uint64(0)
	bg1 |= 1 << 1
	bg1 |= 1 << 62
	bg2 := uint64(0)
	bg2 |= 1 << 2
	bg2 |= 1 << 61
	tooSmallArray := types.ValidatorMissedBlockArray{
		Address:      consAddr.String(),
		WindowSize:   192,
		MissedBlocks: []uint64{bg0, bg1, bg2},
	}
	app.SlashingKeeper.SetValidatorMissedBlocks(ctx, consAddr, tooSmallArray)

	ctx = ctx.WithBlockHeight(509)
	slashing.BeginBlocker(ctx, testslashing.CreateBeginBlockReq(val.Address(), 200, true), app.SlashingKeeper)

	missedInfo, found := app.SlashingKeeper.GetValidatorMissedBlocks(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, 2, len(missedInfo.MissedBlocks))
	require.Equal(t, int64(128), missedInfo.WindowSize)
	require.Equal(t, []uint64{uint64(0), uint64(0)}, missedInfo.MissedBlocks)

	info, found := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	require.True(t, found)
	require.Equal(t, int64(0), info.MissedBlocksCounter)
	require.Equal(t, int64(1), info.IndexOffset)
}
