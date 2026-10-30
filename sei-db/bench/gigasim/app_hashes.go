package gigasim

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
)

// reportExecutedBlockHash reports height's block hash before that block is committed.
func reportExecutedBlockHash(manager *bootstrap.GigaStorageManager, height uint64) error {
	var hash [32]byte
	binary.BigEndian.PutUint64(hash[:8], height)
	// The benchmark cancels its run context to stop, and the block then in flight still reports.
	if err := manager.AppHashBuilder().ReportBlockHash(context.Background(), height, hash); err != nil {
		return fmt.Errorf("report the block hash of block %d: %w", height, err)
	}
	return nil
}
