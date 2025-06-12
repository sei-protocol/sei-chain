package utils

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

func IsTxPrioritized(tx sdk.Tx) bool {
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *oracletypes.MsgAggregateExchangeRateVote:
			continue
		case *oracletypes.MsgDelegateFeedConsent:
			continue
		default:
			return false
		}
	}
	return true
}
