package keeper_test

import (
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"testing"
)

func TestAfterValidatorBonded(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	addrDels := simapp.AddTestAddrsIncremental(app, ctx, 6, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	valAddrs := simapp.ConvertAddrsToValAddrs(addrDels)
	keeper := app.SlashingKeeper
	consAddr := sdk.ConsAddress(addrDels[0])

	keeper.AfterValidatorBonded(ctx, consAddr, valAddrs[0])

	// Verify the updated signing info
	signingInfo, found := keeper.GetValidatorSigningInfo(ctx, consAddr)
	assert.True(t, found)
	assert.Equal(t, ctx.BlockHeight(), signingInfo.StartHeight)
	assert.Equal(t, int64(0), signingInfo.MissedBlocksCounter)
	assert.False(t, signingInfo.Tombstoned)
	assert.Equal(t, false, signingInfo.Tombstoned)
}
