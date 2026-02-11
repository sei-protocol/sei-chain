package state

import (
	tmstate "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func ABCIResponsesResultsHash(ar *tmstate.ABCIResponses) []byte {
	return types.NewResults(ar.DeliverTxs).Hash()
}
