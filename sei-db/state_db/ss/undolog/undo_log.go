// Package undolog implements gigatypes.HistoricalStateStore over the undo-log engine.
package undolog

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

var _ gigatypes.HistoricalStateStore = (*undoLog)(nil)

// undoLog is a HistoricalStateStore that keeps recent history as a bucketed undo log.
type undoLog struct {
	*undo.Database
}

// Open opens the undo log in dataDir, creating it when there is none.
func Open(dataDir string, cfg config.StateStoreConfig) (gigatypes.HistoricalStateStore, error) {
	db, err := undo.OpenDB(dataDir, cfg)
	if err != nil {
		return nil, err
	}
	return &undoLog{Database: db}, nil
}

func (l *undoLog) Resume(version int64, current gigatypes.StateView) error {
	return l.Database.Resume(version, current)
}

func (l *undoLog) ApplyBlock(version int64, changes *gigatypes.StateChangeSet, current gigatypes.StateView) {
	l.Database.ApplyBlock(version, priorsOf(changes), current)
}

func (l *undoLog) OpenView(version int64) (gigatypes.StateView, bool) {
	view, ok := l.Database.OpenView(version)
	if !ok {
		return nil, false
	}
	return &historicalStateView{height: version, read: view.Get, release: view.Close}, true
}
