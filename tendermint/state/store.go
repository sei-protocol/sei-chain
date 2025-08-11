package state

import (
	tmstate "github.com/sei-protocol/sei-chain/tendermint/proto/tendermint/state"
	"github.com/sei-protocol/sei-chain/tendermint/types"
)

func ABCIResponsesResultsHash(ar *tmstate.ABCIResponses) []byte {
	return types.NewResults(ar.DeliverTxs).Hash()
}
