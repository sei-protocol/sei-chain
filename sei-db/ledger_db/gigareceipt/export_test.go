package gigareceipt

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"

	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
)

// PruneLittIdx runs a synchronous prune on a littidx store, removing every
// block below cutoff. Test-only hook so prune behavior can be asserted without
// waiting on the background interval.
func PruneLittIdx(store receipt.ReceiptStore, cutoff uint64) error {
	return store.(*littReceiptStore).pruneBlocksBelow(cutoff)
}

// LoseIndexAbove simulates a crash that lost the index commits above keep: it deletes every index entry
// above keep and sets the index head to keep, leaving litt untouched. The store must be closed.
func LoseIndexAbove(dir string, keep uint64) (err error) {
	indexCfg := pebbledb.DefaultConfig()
	indexCfg.DataDir = filepath.Join(dir, littIndexDirName)
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, index.Close()) }()
	return rollBackIndex(index, keep)
}

// LoseLittAbove simulates a crash that lost the litt writes above keep: it deletes every receipt in litt
// above keep, leaving the index untouched. The store must be closed.
func LoseLittAbove(dir string, keep uint64) error {
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = dir
	return rollBackLitt(cfg, keep)
}
