package oracle_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/oracle"
	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestOracleVoteAloneAnteHandler(t *testing.T) {

	testOracleMsg := oracletypes.MsgAggregateExchangeRateVote{}
	testNonOracleMsg := banktypes.MsgSend{}
	testNonOracleMsg2 := banktypes.MsgSend{}

	decorator := oracle.NewOracleVoteAloneDecorator()
	anteHandler, depGen := sdk.ChainAnteDecorators(decorator)
	testCases := []struct {
		name   string
		expErr bool
		tx     sdk.Tx
	}{
		{"only oracle vote", false, app.NewTestTx([]sdk.Msg{&testOracleMsg})},
		{"only non-oracle msgs", false, app.NewTestTx([]sdk.Msg{&testNonOracleMsg, &testNonOracleMsg2})},
		{"mixed messages", true, app.NewTestTx([]sdk.Msg{&testNonOracleMsg, &testOracleMsg, &testNonOracleMsg2})},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := anteHandler(sdk.NewContext(nil, tmproto.Header{}, false, nil), tc.tx, false)
			if tc.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			_, err = depGen([]sdkacltypes.AccessOperation{}, tc.tx, 1)
			require.NoError(t, err)
		})
	}
}

func TestSpammingPreventionAnteHandler(t *testing.T) {
	input, _ := setup(t)

	exchangeRateStr := randomExchangeRate.String() + utils.MicroAtomDenom

	voteMsg := types.NewMsgAggregateExchangeRateVote(exchangeRateStr, keeper.Addrs[0], keeper.ValAddrs[0])
	invalidVoteMsg := types.NewMsgAggregateExchangeRateVote(exchangeRateStr, keeper.Addrs[3], keeper.ValAddrs[2])

	spd := oracle.NewSpammingPreventionDecorator(input.OracleKeeper)
	anteHandler, _ := sdk.ChainAnteDecorators(spd)

	recheckCtx := input.Ctx.WithIsReCheckTx(true)
	_, err := anteHandler(recheckCtx, app.NewTestTx([]sdk.Msg{voteMsg}), false) // should skip the SPD
	require.NoError(t, err)

	ctx := input.Ctx.WithIsCheckTx(true)
	_, err = anteHandler(ctx, app.NewTestTx([]sdk.Msg{voteMsg}), false)
	require.NoError(t, err)

	// invalid because bad feeder val combo
	_, err = anteHandler(ctx, app.NewTestTx([]sdk.Msg{invalidVoteMsg}), false)
	require.Error(t, err)

	// malform feeder
	malformedVote := voteMsg
	malformedVote.Feeder = "seifoobar"
	_, err = anteHandler(ctx, app.NewTestTx([]sdk.Msg{malformedVote}), false)
	require.Error(t, err)

	// malform val
	malformedVote = voteMsg
	malformedVote.Validator = "seivaloperfoobar"
	_, err = anteHandler(ctx, app.NewTestTx([]sdk.Msg{malformedVote}), false)
	require.Error(t, err)

	// set aggregate vote for val 0
	input.OracleKeeper.SetAggregateExchangeRateVote(ctx, keeper.ValAddrs[0], types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{}, keeper.ValAddrs[0]))
	// now should fail
	_, err = anteHandler(ctx, app.NewTestTx([]sdk.Msg{voteMsg}), false)
	require.Error(t, err)
}

func TestSpammingPreventionAnteDeps(t *testing.T) {
	input, _ := setup(t)

	exchangeRateStr := randomExchangeRate.String() + utils.MicroAtomDenom

	voteMsg := types.NewMsgAggregateExchangeRateVote(exchangeRateStr, keeper.Addrs[0], keeper.ValAddrs[0])

	spd := oracle.NewSpammingPreventionDecorator(input.OracleKeeper)
	anteHandler, depGen := sdk.ChainAnteDecorators(spd)

	ctx := input.Ctx.WithIsCheckTx(true)

	// test anteDeps
	msgValidator := sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap)
	ctx = ctx.WithMsgValidator(msgValidator)
	ms := ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	ctx = ctx.WithMultiStore(msCache)
	tx := app.NewTestTx([]sdk.Msg{voteMsg})

	_, err := anteHandler(ctx, tx, false)
	require.NoError(t, err)

	newDeps, err := depGen([]sdkacltypes.AccessOperation{}, tx, 1)
	require.NoError(t, err)

	storeAccessOpEvents := msCache.GetEvents()

	missingAccessOps := ctx.MsgValidator().ValidateAccessOperations(newDeps, storeAccessOpEvents)
	require.Equal(t, 0, len(missingAccessOps))
}
