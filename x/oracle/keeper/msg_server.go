package keeper

import (
	"context"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

type msgServer struct{}

// NewMsgServerImpl returns an implementation of the oracle MsgServer interface
// that rejects messages because the module is deprecated.
func NewMsgServerImpl(_ Keeper) types.MsgServer {
	return &msgServer{}
}

var _ types.MsgServer = msgServer{}

func (msgServer) AggregateExchangeRateVote(context.Context, *types.MsgAggregateExchangeRateVote) (*types.MsgAggregateExchangeRateVoteResponse, error) {
	return nil, types.ErrOracleDeprecated
}

func (msgServer) DelegateFeedConsent(context.Context, *types.MsgDelegateFeedConsent) (*types.MsgDelegateFeedConsentResponse, error) {
	return nil, types.ErrOracleDeprecated
}
