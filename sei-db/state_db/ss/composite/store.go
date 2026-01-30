package composite

import (
	"fmt"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/pruning"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

// CompositeStateStore routes operations between Cosmos_SS (main state store) and EVM_SS (optimized EVM stores).
// - Reads check EVM_SS first for EVM keys (if EnableRead), then fallback to Cosmos_SS
// - Writes go to both stores for EVM keys (if EnableWrite), only Cosmos_SS for others
type CompositeStateStore struct {
	cosmosStore types.StateStore   // Main MVCC PebbleDB for all modules
	evmStore    *evm.EVMStateStore // Separate EVM DBs with default comparer (nil if disabled)
	ssConfig    config.StateStoreConfig
	evmConfig   *config.EVMStateStoreConfig
	logger      logger.Logger
}

// NewCompositeStateStore creates a new composite state store that manages both Cosmos_SS and EVM_SS.
// It initializes both stores internally and starts pruning on the composite store.
//
// ssConfig: configuration for the main Cosmos state store (required)
// evmConfig: configuration for EVM state stores (optional - if nil or disabled, only Cosmos_SS is used)
// homeDir: base directory for data files
func NewCompositeStateStore(
	ssConfig config.StateStoreConfig,
	evmConfig *config.EVMStateStoreConfig,
	homeDir string,
	log logger.Logger,
) (*CompositeStateStore, error) {
	// Initialize Cosmos store (without pruning - we start pruning on composite)
	dbHome := utils.GetStateStorePath(homeDir, ssConfig.Backend)
	if ssConfig.DBDirectory != "" {
		dbHome = ssConfig.DBDirectory
	}
	cosmosStore, err := mvcc.OpenDB(dbHome, ssConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create cosmos store: %w", err)
	}

	cs := &CompositeStateStore{
		cosmosStore: cosmosStore,
		ssConfig:    ssConfig,
		evmConfig:   evmConfig,
		logger:      log,
	}

	// Initialize EVM stores if enabled
	if evmConfig != nil && evmConfig.Enable {
		evmDir := evmConfig.DBDirectory
		if evmDir == "" {
			evmDir = filepath.Join(homeDir, "data", "evm_ss")
		}

		evmStore, err := evm.NewEVMStateStore(evmDir)
		if err != nil {
			_ = cosmosStore.Close()
			return nil, fmt.Errorf("failed to create EVM store: %w", err)
		}
		cs.evmStore = evmStore
		log.Info("EVM state store enabled", "dir", evmDir, "read", evmConfig.EnableRead, "write", evmConfig.EnableWrite)
	}

	// Recover from WAL if needed
	changelogPath := utils.GetChangelogPath(dbHome)
	if err := recoverFromWAL(log, changelogPath, cs); err != nil {
		_ = cs.Close()
		return nil, fmt.Errorf("failed to recover state store: %w", err)
	}

	// Start pruning on the composite store (prunes both Cosmos_SS and EVM_SS)
	cs.StartPruning()

	return cs, nil
}

// StartPruning starts the pruning manager for this composite store.
// Pruning removes old versions from both Cosmos_SS and EVM_SS.
func (s *CompositeStateStore) StartPruning() *pruning.Manager {
	pm := pruning.NewPruningManager(s.logger, s, int64(s.ssConfig.KeepRecent), int64(s.ssConfig.PruneIntervalSeconds))
	pm.Start()
	return pm
}

// recoverFromWAL replays WAL entries to recover state after crash/restart
func recoverFromWAL(log logger.Logger, changelogPath string, stateStore types.StateStore) error {
	ssLatestVersion := stateStore.GetLatestVersion()
	log.Info(fmt.Sprintf("Recovering from changelog %s with latest SS version %d", changelogPath, ssLatestVersion))

	streamHandler, err := wal.NewChangelogWAL(log, changelogPath, wal.Config{})
	if err != nil {
		return nil // No WAL to recover from
	}

	firstOffset, errFirst := streamHandler.FirstOffset()
	if firstOffset <= 0 || errFirst != nil {
		return nil
	}

	lastOffset, errLast := streamHandler.LastOffset()
	if lastOffset <= 0 || errLast != nil {
		return nil
	}

	lastEntry, errRead := streamHandler.ReadAt(lastOffset)
	if errRead != nil {
		return nil
	}

	// Find replay start offset
	curVersion := lastEntry.Version
	curOffset := lastOffset
	if ssLatestVersion > 0 {
		for curVersion > ssLatestVersion && curOffset > firstOffset {
			curOffset--
			curEntry, err := streamHandler.ReadAt(curOffset)
			if err != nil {
				return err
			}
			curVersion = curEntry.Version
		}
	} else {
		curOffset = firstOffset
	}

	targetStartOffset := curOffset
	log.Info(fmt.Sprintf("Replaying changelog to recover StateStore from offset %d to %d", targetStartOffset, lastOffset))

	if targetStartOffset < lastOffset {
		return streamHandler.Replay(targetStartOffset, lastOffset, func(index uint64, entry proto.ChangelogEntry) error {
			if err := stateStore.ApplyChangesetSync(entry.Version, entry.Changesets); err != nil {
				return err
			}
			return stateStore.SetLatestVersion(entry.Version)
		})
	}
	return nil
}

// Get retrieves a value for a key at a specific version
// For EVM keys: check EVM_SS first (if EnableRead), fallback to Cosmos_SS
// For non-EVM keys: use Cosmos_SS directly
func (s *CompositeStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	// Try EVM store first for EVM keys if read is enabled
	if s.evmStore != nil && s.evmConfig.EnableRead && storeKey == evm.EVMStoreKey {
		val, err := s.evmStore.Get(key, version)
		if err != nil {
			return nil, err
		}
		if val != nil {
			return val, nil
		}
		// Fall through to Cosmos_SS if not found in EVM_SS
	}

	// Fallback to Cosmos store
	return s.cosmosStore.Get(storeKey, version, key)
}

// Has checks if a key exists at a specific version
func (s *CompositeStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	// Try EVM store first for EVM keys if read is enabled
	if s.evmStore != nil && s.evmConfig.EnableRead && storeKey == evm.EVMStoreKey {
		has, err := s.evmStore.Has(key, version)
		if err != nil {
			return false, err
		}
		if has {
			return true, nil
		}
		// Fall through to check Cosmos_SS
	}

	// Fallback to Cosmos store
	return s.cosmosStore.Has(storeKey, version, key)
}

// Iterator returns an iterator over keys in the given range
// For EVM store keys, we use Cosmos_SS iterator (EVM_SS is an optimization layer)
func (s *CompositeStateStore) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	// Use Cosmos store for iteration (source of truth)
	return s.cosmosStore.Iterator(storeKey, version, start, end)
}

// ReverseIterator returns a reverse iterator over keys in the given range
func (s *CompositeStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return s.cosmosStore.ReverseIterator(storeKey, version, start, end)
}

// RawIterate iterates over raw key-value pairs
func (s *CompositeStateStore) RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error) {
	return s.cosmosStore.RawIterate(storeKey, fn)
}

// GetLatestVersion returns the latest version
func (s *CompositeStateStore) GetLatestVersion() int64 {
	return s.cosmosStore.GetLatestVersion()
}

// GetEarliestVersion returns the earliest version
func (s *CompositeStateStore) GetEarliestVersion() int64 {
	return s.cosmosStore.GetEarliestVersion()
}

// GetLatestMigratedKey returns the latest migrated key
func (s *CompositeStateStore) GetLatestMigratedKey() ([]byte, error) {
	return s.cosmosStore.GetLatestMigratedKey()
}

// GetLatestMigratedModule returns the latest migrated module
func (s *CompositeStateStore) GetLatestMigratedModule() (string, error) {
	return s.cosmosStore.GetLatestMigratedModule()
}

// Close closes all underlying stores
func (s *CompositeStateStore) Close() error {
	var lastErr error

	if s.evmStore != nil {
		if err := s.evmStore.Close(); err != nil {
			s.logger.Error("failed to close EVM store", "error", err)
			lastErr = err
		}
	}

	if err := s.cosmosStore.Close(); err != nil {
		s.logger.Error("failed to close Cosmos store", "error", err)
		lastErr = err
	}

	return lastErr
}

// =============================================================================
// Write path methods - delegated to Cosmos store only in this PR
// Full dual-write implementation will be added in the next PR
// =============================================================================

// SetLatestVersion sets the latest version
func (s *CompositeStateStore) SetLatestVersion(version int64) error {
	return s.cosmosStore.SetLatestVersion(version)
}

// SetEarliestVersion sets the earliest version
func (s *CompositeStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	return s.cosmosStore.SetEarliestVersion(version, ignoreVersion)
}

// ApplyChangesetSync applies changeset synchronously (delegates to Cosmos store)
func (s *CompositeStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	// TODO: Add dual-write to EVM_SS in next PR
	return s.cosmosStore.ApplyChangesetSync(version, changesets)
}

// ApplyChangesetAsync applies changeset asynchronously (delegates to Cosmos store)
func (s *CompositeStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	// TODO: Add dual-write to EVM_SS in next PR
	return s.cosmosStore.ApplyChangesetAsync(version, changesets)
}

// Import imports initial state
func (s *CompositeStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	// TODO: Add dual-write to EVM_SS in next PR
	return s.cosmosStore.Import(version, ch)
}

// RawImport imports raw key-value entries
func (s *CompositeStateStore) RawImport(ch <-chan types.RawSnapshotNode) error {
	// TODO: Add dual-write to EVM_SS in next PR
	return s.cosmosStore.RawImport(ch)
}

// Prune removes old versions
func (s *CompositeStateStore) Prune(version int64) error {
	// Prune both stores
	if s.evmStore != nil {
		if err := s.evmStore.Prune(version); err != nil {
			s.logger.Error("failed to prune EVM store", "error", err)
			// Continue to prune Cosmos store
		}
	}
	return s.cosmosStore.Prune(version)
}
