package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	seiapp "github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/assert"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestAfterValidatorBonded(t *testing.T) {
	app := seiapp.Setup(false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	addrDels := seiapp.AddTestAddrsIncremental(app, ctx, 6, app.StakingKeeper.TokensFromConsensusPower(ctx, 200))
	valAddrs := seiapp.ConvertAddrsToValAddrs(addrDels)
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
