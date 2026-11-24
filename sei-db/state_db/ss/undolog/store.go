// Package undolog implements gigatypes.HistoricalStateStore over the undo-log engine.
package undolog

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

var _ gigatypes.HistoricalStateStore = (*undoLogStore)(nil)

// undoLogStore is a HistoricalStateStore backed by the undo-log engine.
type undoLogStore struct {
	*undo.Database
}

// Open opens the undo log in dataDir, creating it when there is none.
func Open(dataDir string, cfg config.StateStoreConfig) (gigatypes.HistoricalStateStore, error) {
	db, err := undo.OpenDB(dataDir, cfg)
	if err != nil {
		return nil, err
	}
	return &undoLogStore{Database: db}, nil
}

// GetLatestBlock returns the newest block the closed undo log in dataDir holds, or 0 when it holds none.
func GetLatestBlock(dataDir string) (uint64, error) {
	_, head, err := undo.GetLatestBlock(dataDir)
	return head, err
}

// PruneAfter removes every block above target from the closed undo log in dataDir. A missing directory is left
// as it is.
func PruneAfter(dataDir string, cfg config.StateStoreConfig, target uint64) error {
	return undo.DiscardStateAbove(dataDir, cfg, target)
}

func (s *undoLogStore) Resume(version int64, current gigatypes.RawStateView) error {
	return s.Database.Resume(version, current)
}

func (s *undoLogStore) ApplyBlock(version int64, mutations []mutation.Mutation, current gigatypes.RawStateView) {
	for _, m := range mutations {
		if err := checkRecord(m.Key, m.PreviousValue); err != nil {
			current.Close()
			panic(fmt.Errorf("undolog: block %d: key %x: %w", version, m.Key, err))
		}
	}
	s.Database.ApplyBlock(version, mutations, current)
}

func (s *undoLogStore) OpenView(version int64) (gigatypes.RawStateView, bool) {
	view, ok := s.Database.OpenView(version)
	if !ok {
		return nil, false
	}
	return &historicalStateView{view: view, height: version}, true
}
