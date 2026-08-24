package keeper

import "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/05-port/types"

// Keeper defines the IBC connection keeper
type Keeper struct {
	Router *types.Router
}

// NewKeeper creates a new IBC connection Keeper instance
func NewKeeper() Keeper {
	return Keeper{}
}
