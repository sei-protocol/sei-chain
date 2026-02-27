package oracle_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/oracle"
	"github.com/sei-protocol/sei-chain/x/oracle/keeper/testutils"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
	"github.com/stretchr/testify/require"
)

func TestOracleVoteAloneAnteHandler(t *testing.T) {

	testOracleMsg := oracletypes.MsgAggregateExchangeRateVote{}
	testNonOracleMsg := banktypes.MsgSend{}
	testNonOracleMsg2 := banktypes.MsgSend{}

	decorator := oracle.NewOracleVoteAloneDecorator()
	anteHandler := sdk.ChainAnteDecorators(decorator)
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
		})
	}
}

func TestSpammingPreventionAnteHandler(t *testing.T) {
	input, _ := setup(t)

	exchangeRateStr := randomExchangeRate.String() + utils.MicroAtomDenom

	voteMsg := types.NewMsgAggregateExchangeRateVote(exchangeRateStr, testutils.Addrs[0], testutils.ValAddrs[0])
	invalidVoteMsg := types.NewMsgAggregateExchangeRateVote(exchangeRateStr, testutils.Addrs[3], testutils.ValAddrs[2])

	spd := oracle.NewSpammingPreventionDecorator(input.OracleKeeper)
	anteHandler := sdk.ChainAnteDecorators(spd)

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
	input.OracleKeeper.SetAggregateExchangeRateVote(ctx, testutils.ValAddrs[0], types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{}, testutils.ValAddrs[0]))
	// now should fail
	_, err = anteHandler(ctx, app.NewTestTx([]sdk.Msg{voteMsg}), false)
	require.Error(t, err)
}
