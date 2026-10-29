package gigasim

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
)

// How long awaitStoredAppHash waits for the vault to store the state version's app hash.
const missingBlockHashTimeout = 10 * time.Second

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

// awaitStoredAppHash waits until the app hash vault has stored the state version.
// It returns nil when the state version is 0.
func awaitStoredAppHash(manager *bootstrap.GigaStorageManager) error {
	if manager == nil || manager.SC() == nil || manager.AppHashVault() == nil {
		return fmt.Errorf("await stored app hash: storage is not open")
	}
	version, err := manager.SC().GetLatestVersion()
	if err != nil {
		return fmt.Errorf("read state height: %w", err)
	}
	if version <= 0 {
		return nil
	}
	//nolint:gosec // state heights are non-negative
	height := uint64(version)
	deadline := time.Now().Add(missingBlockHashTimeout)
	for {
		head, err := manager.AppHashVault().GetLatestBlock()
		if err != nil {
			return fmt.Errorf("read app hash vault head: %w", err)
		}
		if head >= height {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the app hash of block %d was not stored within %s", height, missingBlockHashTimeout)
		}
		time.Sleep(time.Millisecond)
	}
}
