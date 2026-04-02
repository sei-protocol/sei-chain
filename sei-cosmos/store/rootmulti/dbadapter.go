package rootmulti

import (
	"fmt"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/dbadapter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

var commithash = []byte("FAKE_HASH")

//----------------------------------------
// commitDBStoreWrapper should only be used for simulation/debugging,
// as it doesn't compute any commit hash, and it cannot load older state.

// Wrapper type for dbm.Db with implementation of KVStore
type commitDBStoreAdapter struct {
	dbadapter.Store
}

func (cdsa commitDBStoreAdapter) Commit(_ bool) types.CommitID {
	return types.CommitID{
		Version: -1,
		Hash:    commithash,
	}
}

func (cdsa commitDBStoreAdapter) LastCommitID() types.CommitID {
	return types.CommitID{
		Version: -1,
		Hash:    commithash,
	}
}

func (cdsa commitDBStoreAdapter) SetPruning(_ types.PruningOptions) {}

// GetPruning is a no-op as pruning options cannot be directly set on this store.
// They must be set on the root commit multi-store.
func (cdsa commitDBStoreAdapter) GetPruning() types.PruningOptions { return types.PruningOptions{} }

func (cdsa commitDBStoreAdapter) Query(req abci.RequestQuery) abci.ResponseQuery {
	if len(req.Data) == 0 {
		return abci.ResponseQuery{
			Code: 1,
			Log:  "query data must not be empty",
		}
	}

	switch req.Path {
	case "/key":
		val := cdsa.Get(req.Data)
		return abci.ResponseQuery{
			Key:   req.Data,
			Value: val,
		}
	default:
		return abci.ResponseQuery{
			Code: 1,
			Log:  fmt.Sprintf("unexpected query path: %s", req.Path),
		}
	}
}
