package bindings

import "github.com/sei-protocol/sei-chain/x/dex/types"

type SeiDexQuery struct {
	// queries the dex TWAPs
	DexTwaps *types.QueryGetTwapsRequest `json:"dex_twaps,omitempty"`
}
