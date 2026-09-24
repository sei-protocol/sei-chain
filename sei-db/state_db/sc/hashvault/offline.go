package hashvault

import (
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/offline"
)

// StoredRange returns the oldest and newest blocks the closed vault under cfg.DataDir records, and false
// when it records none. A vault that has never been created records none.
func StoredRange(cfg config.HashVaultConfig) (oldest uint64, newest uint64, recorded bool, err error) {
	exists, err := vaultExists(cfg)
	if err != nil {
		return 0, 0, false, fmt.Errorf("read the hash vault's range: %w", err)
	}
	if !exists {
		return 0, 0, false, nil
	}
	oldest, recorded, err = firstStoredBlock(cfg, false)
	if err != nil {
		return 0, 0, false, fmt.Errorf("read the oldest hash vault block: %w", err)
	}
	if !recorded {
		return 0, 0, false, nil
	}
	newest, _, err = firstStoredBlock(cfg, true)
	if err != nil {
		return 0, 0, false, fmt.Errorf("read the newest hash vault block: %w", err)
	}
	return oldest, newest, true, nil
}

// PruneAfter deletes every hash the closed vault under cfg.DataDir records above blockNumber.
func PruneAfter(cfg config.HashVaultConfig, blockNumber uint64) error {
	if blockNumber == math.MaxUint64 {
		return nil
	}
	if err := pruneFrom(cfg, blockNumber+1); err != nil {
		return fmt.Errorf("prune the hash vault after block %d: %w", blockNumber, err)
	}
	return nil
}

// pruneFrom deletes every hash the closed vault under cfg.DataDir records at or above blockNumber. A
// vault left with no hash is left empty.
func pruneFrom(cfg config.HashVaultConfig, blockNumber uint64) error {
	exists, err := vaultExists(cfg)
	if err != nil {
		return fmt.Errorf("prune the hash vault: %w", err)
	}
	if !exists {
		return nil
	}
	littCfg, err := littConfig(cfg)
	if err != nil {
		return fmt.Errorf("prune the hash vault: %w", err)
	}
	// The rollback walks from the newest hash down and keeps everything from the first one this accepts.
	keep := func(_ string, key []byte, _ bool) (bool, error) {
		recordedBlock, err := decodeKey(key)
		if err != nil {
			return false, fmt.Errorf("decode a hash vault key: %w", err)
		}
		return recordedBlock < blockNumber, nil
	}
	if err := offline.RollbackLittDB(littCfg, keep); err != nil {
		return fmt.Errorf("prune the hash vault from block %d: %w", blockNumber, err)
	}
	return nil
}

// firstStoredBlock returns the first block the closed vault yields in the direction reverse selects: the
// newest when reverse is true, the oldest otherwise. It returns false when the vault records none.
func firstStoredBlock(cfg config.HashVaultConfig, reverse bool) (blockNumber uint64, found bool, err error) {
	littCfg, err := littConfig(cfg)
	if err != nil {
		return 0, false, fmt.Errorf("iterate the hash vault offline: %w", err)
	}
	iterator, err := offline.NewIterator(littCfg, tableName, reverse)
	if err != nil {
		return 0, false, fmt.Errorf("open an offline iterator over the hash vault: %w", err)
	}
	defer func() {
		if closeErr := iterator.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close the offline hash vault iterator: %w", closeErr))
		}
	}()
	found, err = iterator.Next()
	if err != nil {
		return 0, false, fmt.Errorf("iterate the hash vault offline: %w", err)
	}
	if !found {
		return 0, false, nil
	}
	key, _, err := iterator.GetKey()
	if err != nil {
		return 0, false, fmt.Errorf("read a hash vault key: %w", err)
	}
	blockNumber, err = decodeKey(key)
	if err != nil {
		return 0, false, fmt.Errorf("decode a hash vault key: %w", err)
	}
	return blockNumber, true, nil
}

// vaultExists reports whether the vault's directory exists.
func vaultExists(cfg config.HashVaultConfig) (bool, error) {
	if _, err := os.Stat(cfg.DataDir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat the hash vault dir %q: %w", cfg.DataDir, err)
	}
	return true, nil
}
