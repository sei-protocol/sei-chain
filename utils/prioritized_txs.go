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
<<<<<<< HEAD
		case *oracletypes.MsgDelegateFeedConsent:
			continue
=======
>>>>>>> 590aa777 (optimization)
		default:
			return false
		}
	}
	return true
}
