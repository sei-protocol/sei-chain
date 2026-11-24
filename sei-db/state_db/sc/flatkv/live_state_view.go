package flatkv

import (
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

var _ gigatypes.RawStateView = (*liveStateView)(nil)

// liveStateView reads the key-values FlatKV holds as of one committed block.
type liveStateView struct {
	// FlatKV's databases as of the block. Close() releases the reservation they carry.
	dbs *sview.StoreView

	// Guards the release, so a second Close does not release a reservation this view no longer owns.
	closeOnce sync.Once

	// Closed by Close.
	closed utils.CloseMarker[liveStateView]
}

// GetBlockHeight returns the block height of this view.
func (v *liveStateView) GetBlockHeight() int64 {
	return v.dbs.BlockHeight()
}

// Close releases the reservation this view holds. The view must not be read afterwards.
// Idempotent.
func (v *liveStateView) Close() {
	v.closeOnce.Do(func() {
		v.closed.Close(v)
		if err := v.dbs.Release(); err != nil {
			panic(fmt.Sprintf("flatkv: close state view at height %d: %v", v.dbs.BlockHeight(), err))
		}
	})
}

// GetRaw returns the bytes stored under a physical key in this block, and whether it was found. It
// panics on a key under the store's metadata prefix.
func (v *liveStateView) GetRaw(key []byte) ([]byte, bool) {
	dbView, err := v.dbFor(key)
	if err != nil {
		panic(err)
	}
	value, found, err := dbView.Get(key, true)
	if err != nil {
		panic(fmt.Sprintf("flatkv: %s read of key %x at height %d: %v",
			dbView.Name(), key, v.dbs.BlockHeight(), err))
	}
	return value, found
}

// BatchGetRaw returns the bytes stored in this block under each found physical key, keyed by string(key).
// It panics on a key under the store's metadata prefix.
func (v *liveStateView) BatchGetRaw(keys [][]byte) map[string][]byte {
	// There are at most four databases, so a list searched linearly beats a map keyed by database.
	var batches []dbBatch
	for k, key := range keys {
		dbView, err := v.dbFor(key)
		if err != nil {
			panic(err)
		}
		i := slices.IndexFunc(batches, func(b dbBatch) bool { return b.dbView == dbView })
		if i < 0 {
			i = len(batches)
			// Sized for every remaining key, since a batch's keys usually all belong to one database.
			batches = append(batches, dbBatch{dbView: dbView, keys: make([][]byte, 0, len(keys)-k)})
		}
		batches[i].keys = append(batches[i].keys, key)
	}

	// Each database's answer is a map of its own, so the largest one collects the others rather than all of
	// them being copied into a new one. A batch within one database returns that database's answer as is.
	var values map[string][]byte
	for _, batch := range batches {
		found, err := batch.dbView.BatchGet(batch.keys)
		if err != nil {
			panic(fmt.Sprintf("flatkv: %s batch read of %d keys at height %d: %v",
				batch.dbView.Name(), len(batch.keys), v.dbs.BlockHeight(), err))
		}
		if len(found) > len(values) {
			values, found = found, values
		}
		maps.Copy(values, found)
	}
	if values == nil {
		return map[string][]byte{}
	}
	return values
}

// dbBatch is the keys of one BatchGetRaw() call that belong to one database.
type dbBatch struct {
	// The view of the database the keys belong to.
	dbView view.View
	// The keys, in the order they were requested.
	keys [][]byte
}

// dbFor returns the view of the database that holds physKey. It returns an error for a metadata key, since the
// store's own metadata is not part of any block's state, and for a key no database holds.
func (v *liveStateView) dbFor(physKey []byte) (view.View, error) {
	if ktype.IsMetaKey(physKey) {
		return nil, fmt.Errorf("flatkv: raw key %x is store metadata, not block state", physKey)
	}
	db, err := routePhysicalKey(physKey)
	if err != nil {
		return nil, fmt.Errorf("flatkv: route raw key %x at height %d: %w", physKey, v.dbs.BlockHeight(), err)
	}
	switch db {
	case accountDBDir:
		return v.dbs.AccountView(), nil
	case codeDBDir:
		return v.dbs.CodeView(), nil
	case storageDBDir:
		return v.dbs.StorageView(), nil
	case miscDBDir:
		return v.dbs.MiscView(), nil
	default:
		return nil, fmt.Errorf("flatkv: raw key %x routes to unknown database %q", physKey, db)
	}
}
