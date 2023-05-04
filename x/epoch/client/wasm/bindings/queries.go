package bindings

import "github.com/sei-protocol/sei-chain/x/epoch/types"

type SeiEpochQuery struct {
	// queries the current Epoch
	Epoch *types.QueryEpochRequest `json:"epoch,omitempty"`
}
