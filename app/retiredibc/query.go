package retiredibc

import (
	"strings"

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	storekeys "github.com/sei-protocol/sei-chain/sei-db/common/keys"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

var storeNames = map[string]struct{}{
	storekeys.IBCStoreKey:         {},
	storekeys.IBCTransferStoreKey: {},
	storekeys.CapabilityStoreKey:  {},
}

// ErrDeprecated reports that IBC functionality is no longer available.
var ErrDeprecated = sdkerrors.New("ibc", 103, "ibc module is deprecated")

// QueryResponse returns the deprecation response for a retired IBC raw-store query.
func QueryResponse(requestPath string) *abci.ResponseQuery {
	path := strings.Split(strings.TrimPrefix(requestPath, "/"), "/")
	if len(path) != 3 || path[0] != "store" || (path[2] != "key" && path[2] != "subspace") {
		return nil
	}
	if _, retired := storeNames[path[1]]; !retired {
		return nil
	}

	response := sdkerrors.QueryResult(ErrDeprecated)
	return &response
}
