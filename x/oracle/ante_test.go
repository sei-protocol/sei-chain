package oracle_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/oracle"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestOracleVoteAloneAnteHandler(t *testing.T) {

	testOracleMsg := oracletypes.MsgAggregateExchangeRateVote{}
	testNonOracleMsg := banktypes.MsgSend{}
	testNonOracleMsg2 := banktypes.MsgSend{}

	decorator := oracle.NewOracleVoteAloneDecorator()
	anteHandler, _ := sdk.ChainAnteDecorators(decorator)

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
