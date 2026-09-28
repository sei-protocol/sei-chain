package utils

import (
	"github.com/sei-protocol/sei-chain/app/retiredoracle"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func IsTxPrioritized(tx sdk.Tx) bool {
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *retiredoracle.MsgAggregateExchangeRateVote:
			continue
		case *retiredoracle.MsgDelegateFeedConsent:
			continue
		default:
			return false
		}
	}
	return true
}
