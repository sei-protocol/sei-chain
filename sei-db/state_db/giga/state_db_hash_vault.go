package giga

import (
	"context"
	"fmt"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashvault"
)

// hashVaultListener returns the listener that records each block's hash in vault.
func hashVaultListener(vault *hashvault.PebbleHashVault) gigatypes.HashListener {
	return func(ctx context.Context, blockNumber uint64, hash *lthash.BlockHash) error {
		if hash.Global == nil {
			return fmt.Errorf("record the hash of block %d: it carries no global hash", blockNumber)
		}
		checksum := hash.Global.Checksum()
		if err := vault.CommitToHash(ctx, blockNumber, checksum[:]); err != nil {
			return fmt.Errorf("record the hash of block %d in the hash vault: %w", blockNumber, err)
		}
		return nil
	}
}

// recordLoadedBlockHash records, or checks, the hash of the block SC opened on, so that the vault holds
// it once the open returns. SC dispatches that hash while it loads, before the vault is registered, and
// no replay re-dispatches it when SC was already on the WAL's head.
func recordLoadedBlockHash(sc *flatkv.CommitStore, vault *hashvault.PebbleHashVault) error {
	if err := sc.FlushHashes(); err != nil {
		return fmt.Errorf("wait for the replayed blocks to reach the hash vault: %w", err)
	}
	loaded := sc.Version()
	if loaded == 0 {
		// A store that has committed nothing has no block to record, and the first one it commits may be
		// any height the chain starts at.
		return nil
	}
	current, err := sc.RegisterHashListener(nil)
	if err != nil {
		return fmt.Errorf("read the hash of the loaded block %d: %w", loaded, err)
	}
	//nolint:gosec // a committed version is never negative
	if current.BlockNumber != uint64(loaded) {
		return fmt.Errorf("the state commit store is on block %d but last produced the hash of block %d",
			loaded, current.BlockNumber)
	}
	if err := hashVaultListener(vault)(context.Background(), current.BlockNumber, &current); err != nil {
		return fmt.Errorf("record the loaded block's hash: %w", err)
	}
	return nil
}
