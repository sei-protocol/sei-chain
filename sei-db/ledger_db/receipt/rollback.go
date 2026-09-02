package receipt

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/rollback"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// Rollback drops every receipt above target. The store must not be open.
func Rollback(cfg dbconfig.ReceiptStoreConfig, target int64) error {
	if target < 0 {
		return fmt.Errorf("invalid receipt rollback target %d", target)
	}
	if normalizeReceiptBackend(cfg.Backend) != receiptBackendLittIdx {
		return fmt.Errorf("receipt store rollback is not supported for backend %q", cfg.Backend)
	}
	if err := rollbackLittBodies(cfg, uint64(target)); err != nil { //nolint:gosec // target >= 0
		return err
	}
	return rewindReceiptIndex(cfg, uint64(target)) //nolint:gosec // target >= 0
}

func rollbackLittBodies(cfg dbconfig.ReceiptStoreConfig, target uint64) error {
	littDir := filepath.Join(cfg.DBDirectory, "littdb")
	if _, err := os.Stat(littDir); os.IsNotExist(err) {
		return nil
	}
	return rollback.RollbackLittDB([]string{littDir}, func(_ string, key []byte, isPrimary bool) (bool, error) {
		if !isPrimary || len(key) != blockNumLen+littPartCountLen {
			return false, nil
		}
		return binary.BigEndian.Uint64(key[:blockNumLen]) <= target, nil
	})
}

func rewindReceiptIndex(cfg dbconfig.ReceiptStoreConfig, target uint64) error {
	indexCfg := pebbledb.DefaultConfig()
	indexCfg.DataDir = filepath.Join(cfg.DBDirectory, "log-index")
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	if err != nil {
		return fmt.Errorf("open receipt log index for rollback: %w", err)
	}
	defer func() { _ = index.Close() }()

	rd, ok := index.(rangeDeleter)
	if !ok {
		return fmt.Errorf("receipt index %T does not support range delete", index)
	}
	if err := rd.DeleteRange(littTagBlockKey(target+1), []byte{littTagKeyPrefix + 1}, dbtypes.WriteOptions{}); err != nil {
		return fmt.Errorf("drop receipt index keys above %d: %w", target, err)
	}
	if err := index.Set(receiptLatestVersionKey, encodeBlockNumber(target), dbtypes.WriteOptions{}); err != nil {
		return fmt.Errorf("rewind receipt head to %d: %w", target, err)
	}
	return nil
}
