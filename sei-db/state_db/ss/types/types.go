package types

import (
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
)

// StateStore is the SS layer contract.
// Extends MvccDB; implemented by CosmosStateStore, EVMStateStore, and CompositeStateStore.
type StateStore interface {
	db_engine.MvccDB
}
