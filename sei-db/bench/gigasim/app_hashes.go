package gigasim

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
)

// How long reportMissingBlockHashes waits for the vault to store the state version's app hash.
const missingBlockHashTimeout = 10 * time.Second

// blockHash is the block hash the benchmark reports for height. It is a function of the height alone,
// so a block reported again after a restart carries the same hash.
func blockHash(height uint64) [32]byte {
	var hash [32]byte
	binary.BigEndian.PutUint64(hash[:8], height)
	return hash
}

// reportMissingBlockHashes reports a block hash for each committed height the app hash vault does
// not yet hold, then waits until the vault stores the state version's app hash. It returns nil when
// the state version is 0.
func reportMissingBlockHashes(manager *bootstrap.GigaStorageManager) error {
	if manager == nil || manager.SC() == nil || manager.AppHashBuilder() == nil || manager.AppHashVault() == nil {
		return fmt.Errorf("report missing block hashes: storage is not open")
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
	stored, err := manager.AppHashVault().GetLatestBlock()
	if err != nil {
		return fmt.Errorf("read app hash vault head: %w", err)
	}
	for n := stored + 1; n <= height; n++ {
		if err := manager.AppHashBuilder().ReportBlockHash(context.Background(), n, blockHash(n)); err != nil {
			return fmt.Errorf("report the block hash of block %d: %w", n, err)
		}
	}
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
