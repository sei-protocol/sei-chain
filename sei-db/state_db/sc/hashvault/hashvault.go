// Package hashvault records the live state DB's block hashes and refuses to let a recorded hash change,
// so that a node cannot commit to two different states for the same block without human intervention.
package hashvault

import (
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

var logger = seilog.NewLogger("db", "state-db", "sc", "hashvault")

// tableName is the LittDB table the hashes are recorded in.
const tableName = "hashes"

// HashVault records one hash per block for a contiguous range of blocks, and holds each hash fixed once
// it is recorded.
//
// Every method is safe to call from any goroutine.
type HashVault struct {
	// The config the vault was opened with.
	config config.HashVaultConfig

	// Guards db, table, empty, head and closed, and so the table against being replaced while it is read.
	// Commit, Reset and Close take it exclusively; lookups share it.
	mu sync.RWMutex

	// The database the table lives in. Replaced when a mismatch or Reset rewrites the files.
	db litt.DB

	// The table the hashes are recorded in.
	table litt.Table

	// True when the vault holds no hashes.
	empty bool

	// The newest recorded block. Meaningless when empty is true.
	head uint64

	// True once Close has run.
	closed bool

	// The floor PruneBlockHashesBelow() has raised. Only ever rises.
	outerFloor atomic.Uint64

	// The floor the storage garbage collector has raised through PruneHistory(). Only ever rises.
	gcFloor atomic.Uint64
}

// Open opens the vault under cfg.DataDir, creating it if it does not exist, and deletes
// cfg.LegacyPebbleDir if it is present.
func Open(cfg config.HashVaultConfig) (*HashVault, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid hash vault config: %w", err)
	}
	if err := deleteLegacyPebbleVault(cfg.LegacyPebbleDir); err != nil {
		return nil, fmt.Errorf("open the hash vault: %w", err)
	}
	v := &HashVault{config: cfg}
	if err := v.openTable(); err != nil {
		return nil, fmt.Errorf("open the hash vault: %w", err)
	}
	return v, nil
}

// deleteLegacyPebbleVault deletes the Pebble-backed vault this one replaced, if it is present. Its hashes
// are app hashes rather than state hashes, so none of them can be carried over.
//
// This can be deleted once every node that ran the Pebble-backed vault has started on this one.
func deleteLegacyPebbleVault(dir string) error {
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat legacy hash vault dir %q: %w", dir, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete legacy hash vault dir %q: %w", dir, err)
	}
	logger.Info("Deleted the legacy Pebble hash vault; its app hashes cannot be compared with state hashes",
		"dir", dir)
	return nil
}

// littConfig returns the config a vault's LittDB is opened, surveyed and pruned with.
func littConfig(vaultCfg config.HashVaultConfig) (*litt.Config, error) {
	cfg, err := litt.DefaultConfig(vaultCfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("build hash vault littdb config: %w", err)
	}
	cfg.Fsync = vaultCfg.Fsync
	// The table holds a few thousand small keys, so an in-memory keymap rebuilt at open is cheap, and it
	// spares every flush a second database to sync.
	cfg.KeymapType = keymap.MemKeymapType
	cfg.DoubleWriteProtection = true
	return cfg, nil
}

// openTable opens the database and its table, and loads the newest recorded block.
func (v *HashVault) openTable() error {
	cfg, err := littConfig(v.config)
	if err != nil {
		return fmt.Errorf("open the hash vault table: %w", err)
	}
	db, err := littbuilder.NewDB(cfg)
	if err != nil {
		return fmt.Errorf("open hash vault littdb at %q: %w", v.config.DataDir, err)
	}
	tableConfig := litt.DefaultTableConfig(tableName)
	// A single write shard is what makes the writes that survive a crash a prefix of the ones issued, so
	// a crash can shorten the recorded range but never leave a gap in it.
	tableConfig.ShardingFactor = 1
	// A TTL is required for LittDB to collect at all. This one is shorter than any block, so the GC filter
	// alone decides what is deleted.
	tableConfig.TTL = time.Nanosecond
	tableConfig.GCFilter = v.gcFilter
	table, err := db.BuildTable(tableConfig)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("open hash vault table: %w", err)
	}
	v.db = db
	v.table = table
	if err := v.loadRange(); err != nil {
		_ = db.Close()
		return fmt.Errorf("load the hash vault's range: %w", err)
	}
	return nil
}

// loadRange reads the newest recorded block and checks that the recorded blocks are contiguous.
func (v *HashVault) loadRange() error {
	newestKey, found, err := v.table.GetNewestKey()
	if err != nil {
		return fmt.Errorf("read the newest hash vault key: %w", err)
	}
	if !found {
		v.empty = true
		v.head = 0
		return nil
	}
	newest, err := decodeKey(newestKey)
	if err != nil {
		return fmt.Errorf("decode the newest hash vault key: %w", err)
	}
	oldestKey, found, err := v.table.GetOldestKey()
	if err != nil {
		return fmt.Errorf("read the oldest hash vault key: %w", err)
	}
	if !found {
		return fmt.Errorf("hash vault has a newest key but no oldest key")
	}
	oldest, err := decodeKey(oldestKey)
	if err != nil {
		return fmt.Errorf("decode the oldest hash vault key: %w", err)
	}
	if count := v.table.KeyCount(); oldest > newest || newest-oldest+1 != count {
		return fmt.Errorf("hash vault at %q is corrupt: it holds %d hashes for blocks %d to %d, which is not "+
			"one per block", v.config.DataDir, count, oldest, newest)
	}
	v.empty = false
	v.head = newest
	return nil
}

// Head returns the newest recorded block, and false when the vault holds no hashes.
func (v *HashVault) Head() (uint64, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.head, !v.empty
}

// Commit records hash as blockNumber's hash, or checks it against the hash already recorded for that
// block. It returns once the hash is recorded, and the recording is crash durable when cfg.Fsync is set.
//
// An empty vault takes any block, and a vault that is not empty takes a block at or below its newest
// recorded block, or the one after it. A block further ahead is an error. A hash that differs from the
// recorded one, or a block below the oldest recorded one, is an error when cfg.HaltOnMismatch is set;
// otherwise the vault discards its hashes from that block up and records this one in their place.
func (v *HashVault) Commit(blockNumber uint64, hash [32]byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return fmt.Errorf("commit the hash of block %d: the hash vault is closed", blockNumber)
	}

	if v.empty || blockNumber == v.head+1 {
		if err := v.append(blockNumber, hash); err != nil {
			return fmt.Errorf("commit the hash of block %d: %w", blockNumber, err)
		}
		return nil
	}
	if blockNumber > v.head {
		return fmt.Errorf("commit the hash of block %d: the hash vault's newest block is %d, so block %d "+
			"would leave a gap", blockNumber, v.head, blockNumber)
	}

	recorded, found, err := v.read(blockNumber)
	if err != nil {
		return fmt.Errorf("commit the hash of block %d: %w", blockNumber, err)
	}
	if found && recorded == hash {
		return nil
	}
	if err := v.resolveMismatch(blockNumber, hash, recorded, found); err != nil {
		return fmt.Errorf("commit the hash of block %d: %w", blockNumber, err)
	}
	return nil
}

// resolveMismatch handles a hash for blockNumber that differs from the recorded one, or a block below the
// oldest recorded one, which found reports. It halts or replaces the recorded hashes, as
// cfg.HaltOnMismatch selects.
func (v *HashVault) resolveMismatch(blockNumber uint64, hash [32]byte, recorded [32]byte, found bool) error {
	recordedHex := "<pruned>"
	if found {
		recordedHex = hex.EncodeToString(recorded[:])
	}
	fields := []any{
		"blockNumber", blockNumber,
		"recordedHex", recordedHex,
		"incomingHex", hex.EncodeToString(hash[:]),
		"newestRecordedBlock", v.head,
		"hashVaultDir", v.config.DataDir,
	}

	if v.config.HaltOnMismatch {
		logger.Error("HASH VAULT MISMATCH: the node computed a different state hash for a block it already "+
			"recorded, or a block older than any it keeps. Halting. DO NOT RESTART WITHOUT HUMAN "+
			"INVESTIGATION. To continue past this instead, set hash-vault-halt-on-mismatch = false.",
			fields...)
		return fmt.Errorf("hash vault mismatch at block %d: recorded %s, computed %x",
			blockNumber, recordedHex, hash)
	}

	logger.Error("HASH VAULT MISMATCH: the node computed a different state hash for a block it already "+
		"recorded, or a block older than any it keeps. hash-vault-halt-on-mismatch is false, so the "+
		"recorded hashes from this block up are discarded and the new hash replaces them.", fields...)
	if err := v.discardFrom(blockNumber); err != nil {
		return fmt.Errorf("discard the hash vault from block %d after a mismatch: %w", blockNumber, err)
	}
	if err := v.append(blockNumber, hash); err != nil {
		return fmt.Errorf("record the replacing hash of block %d: %w", blockNumber, err)
	}
	return nil
}

// discardFrom closes the database, deletes every hash from blockNumber up, and reopens it. When no hash
// below blockNumber is recorded, the vault is left empty.
func (v *HashVault) discardFrom(blockNumber uint64) error {
	if err := v.db.Close(); err != nil {
		return fmt.Errorf("close the hash vault before pruning it: %w", err)
	}
	if err := pruneFrom(v.config, blockNumber); err != nil {
		return fmt.Errorf("prune the hash vault from block %d: %w", blockNumber, err)
	}
	if err := v.openTable(); err != nil {
		return fmt.Errorf("reopen the hash vault after pruning it: %w", err)
	}
	return nil
}

// Reset deletes every recorded hash and records hash as blockNumber's, leaving it the only one.
func (v *HashVault) Reset(blockNumber uint64, hash [32]byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return fmt.Errorf("reset the hash vault to block %d: the hash vault is closed", blockNumber)
	}
	if err := v.db.Close(); err != nil {
		return fmt.Errorf("close the hash vault before resetting it: %w", err)
	}
	if err := os.RemoveAll(v.config.DataDir); err != nil {
		return fmt.Errorf("delete the hash vault at %q: %w", v.config.DataDir, err)
	}
	if err := v.openTable(); err != nil {
		return fmt.Errorf("reopen the hash vault after deleting it: %w", err)
	}
	logger.Info("Reset the hash vault", "blockNumber", blockNumber, "hashVaultDir", v.config.DataDir)
	if err := v.append(blockNumber, hash); err != nil {
		return fmt.Errorf("record the hash of block %d after a reset: %w", blockNumber, err)
	}
	return nil
}

// append records hash as blockNumber's hash and flushes it. blockNumber must be the block after the
// newest recorded one, or any block when the vault is empty.
func (v *HashVault) append(blockNumber uint64, hash [32]byte) error {
	if err := v.table.Put(encodeKey(blockNumber), encodeValue(hash)); err != nil {
		return fmt.Errorf("record the hash of block %d: %w", blockNumber, err)
	}
	if err := v.table.Flush(); err != nil {
		return fmt.Errorf("flush the hash of block %d: %w", blockNumber, err)
	}
	v.empty = false
	v.head = blockNumber
	return nil
}

// read returns the hash recorded for blockNumber, and false when none is.
func (v *HashVault) read(blockNumber uint64) ([32]byte, bool, error) {
	value, found, err := v.table.Get(encodeKey(blockNumber))
	if err != nil {
		return [32]byte{}, false, fmt.Errorf("read the hash of block %d: %w", blockNumber, err)
	}
	if !found {
		return [32]byte{}, false, nil
	}
	hash, err := decodeValue(value)
	if err != nil {
		return [32]byte{}, false, fmt.Errorf("decode the hash of block %d: %w", blockNumber, err)
	}
	return hash, true, nil
}

// Get returns the hash recorded for blockNumber, without blocking.
func (v *HashVault) Get(blockNumber uint64) ([32]byte, gigatypes.BlockHashStatus, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.closed {
		return [32]byte{}, gigatypes.BlockHashStatusError,
			fmt.Errorf("get the hash of block %d: the hash vault is closed", blockNumber)
	}
	if v.empty || blockNumber > v.head {
		return [32]byte{}, gigatypes.BlockHashStatusNotReady, nil
	}
	hash, found, err := v.read(blockNumber)
	if err != nil {
		return [32]byte{}, gigatypes.BlockHashStatusError,
			fmt.Errorf("get the hash of block %d: %w", blockNumber, err)
	}
	if !found {
		// The recorded blocks are contiguous up to head, so a block at or below it that is missing has
		// been pruned.
		return [32]byte{}, gigatypes.BlockHashStatusTooOld, nil
	}
	return hash, gigatypes.BlockHashStatusFound, nil
}

// Close closes the vault. Every later call fails.
func (v *HashVault) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return nil
	}
	v.closed = true
	if err := v.db.Close(); err != nil {
		return fmt.Errorf("close the hash vault: %w", err)
	}
	return nil
}
