package oracle_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/x/oracle"
	"github.com/sei-protocol/sei-chain/x/oracle/keeper/testutils"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

func TestOracleMessagesDeprecated(t *testing.T) {
	input := testutils.CreateTestInput(t)
	handler := oracle.NewHandler(input.OracleKeeper)

	testCases := []struct {
		name string
		msg  sdk.Msg
	}{
		{name: "aggregate exchange rate vote", msg: &types.MsgAggregateExchangeRateVote{}},
		{name: "delegate feed consent", msg: &types.MsgDelegateFeedConsent{}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := handler(input.Ctx, tc.msg)
			require.ErrorIs(t, err, types.ErrOracleDeprecated)
			require.Nil(t, result)
		})
	}
}

func TestOracleHandlerRejectsUnknownMessage(t *testing.T) {
	input := testutils.CreateTestInput(t)
	handler := oracle.NewHandler(input.OracleKeeper)

	result, err := handler(input.Ctx, &banktypes.MsgSend{})
	require.Error(t, err)
	require.Nil(t, result)
}
