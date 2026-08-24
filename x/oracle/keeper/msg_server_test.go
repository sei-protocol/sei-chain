package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

func TestDeprecatedMessages(t *testing.T) {
	server := keeper.NewMsgServerImpl(keeper.Keeper{})
	ctx := context.Background()

	voteResponse, err := server.AggregateExchangeRateVote(ctx, &types.MsgAggregateExchangeRateVote{})
	require.ErrorIs(t, err, types.ErrOracleDeprecated)
	require.Nil(t, voteResponse)

	delegationResponse, err := server.DelegateFeedConsent(ctx, &types.MsgDelegateFeedConsent{})
	require.ErrorIs(t, err, types.ErrOracleDeprecated)
	require.Nil(t, delegationResponse)
}
