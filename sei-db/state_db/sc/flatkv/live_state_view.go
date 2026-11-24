package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

var _ rowReader = liveRows{}

// liveRows reads the rows of one committed block from the views of the stores that hold them. Releasing them
// returns the block's reservation.
type liveRows struct {
	blockView *sview.StoreView
}

func (r liveRows) height() int64 {
	return r.blockView.BlockHeight()
}

func (r liveRows) getRow(store rowStore, physKey []byte) ([]byte, bool) {
	dbView := r.storeView(store)
	value, found, err := dbView.Get(physKey, true)
	if err != nil {
		panic(fmt.Sprintf("flatkv: %s read of key %x at height %d: %v",
			dbView.Name(), physKey, r.blockView.BlockHeight(), err))
	}
	return value, found
}

func (r liveRows) batchGetRows(store rowStore, physKeys [][]byte) map[string][]byte {
	dbView := r.storeView(store)
	found, err := dbView.BatchGet(physKeys)
	if err != nil {
		panic(fmt.Sprintf("flatkv: %s batch read of %d keys at height %d: %v",
			dbView.Name(), len(physKeys), r.blockView.BlockHeight(), err))
	}
	return found
}

func (r liveRows) release() {
	if err := r.blockView.Release(); err != nil {
		panic(fmt.Sprintf("flatkv: close state view at height %d: %v", r.blockView.BlockHeight(), err))
	}
}

// storeView returns the block's view of store.
func (r liveRows) storeView(store rowStore) view.View {
	switch store {
	case accountRows:
		return r.blockView.AccountView()
	case codeRows:
		return r.blockView.CodeView()
	case storageRows:
		return r.blockView.StorageView()
	default:
		return r.blockView.MiscView()
	}
}
