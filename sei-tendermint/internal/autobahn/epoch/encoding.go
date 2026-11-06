package epoch

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
)

// MarshalEpoch returns the canonical encoding of e.
func MarshalEpoch(e *types.Epoch) []byte {
	return hashable.MarshalCanonical(types.EpochConv.Encode(e))
}
