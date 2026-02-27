package composite

import (
	"fmt"
	"path/filepath"
	"sync"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/pruning"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// CompositeStateStore routes operations between Cosmos_SS (main state store) and EVM_SS (optimized EVM stores).
// - Reads check EVM_SS first for EVM keys, then fallback to Cosmos_SS
// - Writes routing controlled by WriteMode (cosmos_only, dual_write, split_write)
// Always created by NewStateStore; when WriteMode==CosmosOnlyWrite && ReadMode==CosmosOnlyRead,
// evmStore is nil and the composite store behaves identically to a plain state store.
type CompositeStateStore struct {
	cosmosStore    types.StateStore   // Main MVCC PebbleDB for all modules
	evmStore       *evm.EVMStateStore // Separate EVM DBs with default comparer (nil if disabled)
	pruningManager *pruning.Manager   // Pruning lifecycle manager (nil if pruning disabled)
	config         config.StateStoreConfig
	logger         logger.Logger
	closeOnce      sync.Once
	closeErr       error
}

// NewCompositeStateStore creates a new composite state store that manages both Cosmos_SS and EVM_SS.
// It initializes both stores internally and starts pruning on the composite store.
// EVM stores are opened when ssConfig.EVMEnabled() returns true (derived from WriteMode/ReadMode).
func NewCompositeStateStore(
	ssConfig config.StateStoreConfig,
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
		config:      ssConfig,
		logger:      log,
	}

	// Initialize EVM stores if WriteMode/ReadMode require them
	if ssConfig.EVMEnabled() {
		evmDir := ssConfig.EVMDBDirectory
		if evmDir == "" {
			evmDir = filepath.Join(homeDir, "data", "evm_ss")
		}

		evmStore, err := evm.NewEVMStateStore(evmDir, log)
		if err != nil {
			_ = cosmosStore.Close()
			return nil, fmt.Errorf("failed to create EVM store: %w", err)
		}
		cs.evmStore = evmStore
		log.Info("EVM state store enabled", "dir", evmDir, "writeMode", ssConfig.WriteMode, "readMode", ssConfig.ReadMode)
	}

	// Recover from WAL if needed (handles EVM_SS being behind Cosmos_SS)
	changelogPath := utils.GetChangelogPath(dbHome)
	if err := RecoverCompositeStateStore(log, changelogPath, cs); err != nil {
		_ = cs.Close()
		return nil, fmt.Errorf("failed to recover state store: %w", err)
	}

	// Start pruning on the composite store (prunes both Cosmos_SS and EVM_SS)
	cs.StartPruning()

	return cs, nil
}

// StartPruning starts the pruning manager for this composite store.
// Pruning removes old versions from both Cosmos_SS and EVM_SS.
func (s *CompositeStateStore) StartPruning() {
	pm := pruning.NewPruningManager(s.logger, s, int64(s.config.KeepRecent), int64(s.config.PruneIntervalSeconds))
	pm.Start()
	s.pruningManager = pm
}

// Get retrieves a value for a key at a specific version
// For EVM keys: check EVM_SS first (if ReadMode allows), fallback to Cosmos_SS
// For non-EVM keys: use Cosmos_SS directly
func (s *CompositeStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	// Try EVM store first for EVM keys if read mode allows
	if s.evmStore != nil && s.config.ReadMode != config.CosmosOnlyRead && storeKey == evm.EVMStoreKey {
		val, err := s.evmStore.Get(key, version)
		if err != nil {
			return nil, err
		}
		if val != nil {
			return val, nil
		}
		// SplitRead: EVM keys come exclusively from EVM_SS, no Cosmos fallback
		if s.config.ReadMode == config.SplitRead {
			return nil, nil
		}
		// EVMFirstRead: fall through to Cosmos_SS if not found in EVM_SS
	}

	// Fallback to Cosmos store
	return s.cosmosStore.Get(storeKey, version, key)
}

// Has checks if a key exists at a specific version
func (s *CompositeStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	// Try EVM store first for EVM keys if read mode allows
	if s.evmStore != nil && s.config.ReadMode != config.CosmosOnlyRead && storeKey == evm.EVMStoreKey {
		has, err := s.evmStore.Has(key, version)
		if err != nil {
			return false, err
		}
		if has {
			return true, nil
		}
		// SplitRead: EVM keys come exclusively from EVM_SS, no Cosmos fallback
		if s.config.ReadMode == config.SplitRead {
			return false, nil
		}
		// EVMFirstRead: fall through to check Cosmos_SS
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

// Close stops the pruning goroutine and then closes all underlying stores.
// This ensures no pruning operations are in progress when the stores are closed.
// Safe to call multiple times (idempotent).
func (s *CompositeStateStore) Close() error {
	s.closeOnce.Do(func() {
		// First, stop the pruning goroutine and wait for it to exit
		if s.pruningManager != nil {
			s.pruningManager.Stop()
		}

		// Then close all underlying stores
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

		s.closeErr = lastErr
	})
	return s.closeErr
}

// =============================================================================
// Write path methods - dual-write to both Cosmos_SS and EVM_SS
// =============================================================================

// SetLatestVersion sets the latest version on both stores.
// Only advances EVM version when EVM writes are active (not CosmosOnlyWrite),
// so that WAL catch-up can later backfill the EVM store if the operator switches modes.
func (s *CompositeStateStore) SetLatestVersion(version int64) error {
	if err := s.cosmosStore.SetLatestVersion(version); err != nil {
		return err
	}
	if s.evmStore != nil && s.config.WriteMode != config.CosmosOnlyWrite {
		if err := s.evmStore.SetLatestVersion(version); err != nil {
			s.logger.Error("failed to set EVM store latest version", "error", err)
			// Non-fatal: EVM store is optimization layer
		}
	}
	return nil
}

// SetEarliestVersion sets the earliest version
func (s *CompositeStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	if err := s.cosmosStore.SetEarliestVersion(version, ignoreVersion); err != nil {
		return err
	}
	if s.evmStore != nil {
		if err := s.evmStore.SetEarliestVersion(version); err != nil {
			s.logger.Error("failed to set EVM store earliest version", "error", err)
		}
	}
	return nil
}

// ApplyChangesetSync applies changeset synchronously and sequentially to both stores.
// Cosmos is written first, then EVM. Both must succeed; caller can retry safely (idempotent).
//
// Write routing by WriteMode:
//   - CosmosOnlyWrite: all data to Cosmos only
//   - DualWrite: full changeset to Cosmos + EVM data to EVM
//   - SplitWrite: non-EVM data to Cosmos, EVM data to EVM only
func (s *CompositeStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	// Fast path: if no EVM store or cosmos-only write mode, just apply to Cosmos
	if s.evmStore == nil || s.config.WriteMode == config.CosmosOnlyWrite {
		return s.cosmosStore.ApplyChangesetSync(version, changesets)
	}

	// Extract EVM changes
	evmChanges := s.extractEVMChanges(changesets)

	// SplitWrite: strip EVM data from Cosmos changeset
	// DualWrite: send full changeset to both stores
	cosmosChangesets := changesets
	if s.config.WriteMode == config.SplitWrite {
		cosmosChangesets = stripEVMFromChangesets(changesets)
	}

	// Write sequentially: Cosmos first, then EVM
	if err := s.cosmosStore.ApplyChangesetSync(version, cosmosChangesets); err != nil {
		return fmt.Errorf("cosmos store failed: %w", err)
	}

	if len(evmChanges) > 0 {
		if err := s.evmStore.ApplyChangeset(version, evmChanges); err != nil {
			return fmt.Errorf("evm store failed: %w", err)
		}
	}
	return nil
}

// ApplyChangesetAsync applies changeset asynchronously to both stores.
// Cosmos changeset is enqueued via cosmosStore.ApplyChangesetAsync.
// EVM changes are enqueued to per-DB channels inside EVMStateStore.ApplyChangesetAsync,
// which returns immediately after routing to the background workers.
// Write routing follows the same rules as ApplyChangesetSync (see WriteMode docs).
func (s *CompositeStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	// Fast path: if no EVM store or cosmos-only write mode, just apply to Cosmos
	if s.evmStore == nil || s.config.WriteMode == config.CosmosOnlyWrite {
		return s.cosmosStore.ApplyChangesetAsync(version, changesets)
	}

	// Extract EVM changes
	evmChanges := s.extractEVMChanges(changesets)

	// SplitWrite: strip EVM data from Cosmos changeset
	cosmosChangesets := changesets
	if s.config.WriteMode == config.SplitWrite {
		cosmosChangesets = stripEVMFromChangesets(changesets)
	}

	// Enqueue Cosmos changeset (non-blocking)
	if err := s.cosmosStore.ApplyChangesetAsync(version, cosmosChangesets); err != nil {
		return fmt.Errorf("cosmos store failed: %w", err)
	}

	// Enqueue EVM changes to per-DB channels (non-blocking)
	if len(evmChanges) > 0 {
		if err := s.evmStore.ApplyChangesetAsync(version, evmChanges); err != nil {
			return fmt.Errorf("evm store async enqueue failed: %w", err)
		}
	}

	return nil
}

// extractEVMChanges extracts EVM-routable changes from changesets
func (s *CompositeStateStore) extractEVMChanges(changesets []*proto.NamedChangeSet) map[evm.EVMStoreType][]*iavl.KVPair {
	evmChanges := make(map[evm.EVMStoreType][]*iavl.KVPair, evm.NumEVMStoreTypes)

	for _, changeset := range changesets {
		if changeset.Name != evm.EVMStoreKey {
			continue
		}
		for _, kvPair := range changeset.Changeset.Pairs {
			evmStoreType, keyBytes := commonevm.ParseEVMKey(kvPair.Key)
			if evmStoreType == evm.StoreEmpty {
				continue // Skip zero-length keys
			}
			// All EVM keys are routed: optimized keys use stripped key, legacy uses full key
			evmChanges[evmStoreType] = append(evmChanges[evmStoreType], &iavl.KVPair{
				Key:    keyBytes,
				Value:  kvPair.Value,
				Delete: kvPair.Delete,
			})
		}
	}

	return evmChanges
}

// stripEVMFromChangesets returns a new changeset slice with the EVM module's
// changeset removed. Used in SplitWrite mode to keep EVM data out of Cosmos_SS.
func stripEVMFromChangesets(changesets []*proto.NamedChangeSet) []*proto.NamedChangeSet {
	stripped := make([]*proto.NamedChangeSet, 0, len(changesets))
	for _, cs := range changesets {
		if cs.Name != evm.EVMStoreKey {
			stripped = append(stripped, cs)
		}
	}
	return stripped
}

// evmImportFlushThreshold is the number of EVM key-value pairs to buffer before
// flushing to the EVM store. Prevents OOM on large state sync imports.
const evmImportFlushThreshold = 10000

// Import imports initial state to both stores.
// WriteMode is respected: CosmosOnlyWrite sends all data to Cosmos only,
// DualWrite fans out to both, SplitWrite routes EVM data exclusively to EVM.
func (s *CompositeStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	if s.evmStore == nil || s.config.WriteMode == config.CosmosOnlyWrite {
		return s.cosmosStore.Import(version, ch)
	}

	splitWrite := s.config.WriteMode == config.SplitWrite

	// Fan out to both stores
	cosmosCh := make(chan types.SnapshotNode, 100)
	evmChanges := make(map[evm.EVMStoreType][]*iavl.KVPair, evm.NumEVMStoreTypes)
	evmPendingCount := 0

	var wg sync.WaitGroup
	var cosmosErr error

	// Start Cosmos import in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		cosmosErr = s.cosmosStore.Import(version, cosmosCh)
	}()

	// flushEVM writes buffered EVM changes to the EVM store and resets the buffer.
	flushEVM := func() {
		if len(evmChanges) > 0 {
			if err := s.evmStore.ApplyChangesetParallel(version, evmChanges); err != nil {
				s.logger.Error("failed to flush EVM import batch", "error", err)
			}
			evmChanges = make(map[evm.EVMStoreType][]*iavl.KVPair, evm.NumEVMStoreTypes)
			evmPendingCount = 0
		}
	}

	// Process incoming nodes
	for node := range ch {
		isEVM := node.StoreKey == evm.EVMStoreKey

		// SplitWrite: skip EVM data for Cosmos; DualWrite: send everything
		if !isEVM || !splitWrite {
			cosmosCh <- node
		}

		// Route EVM keys to EVM-SS
		if isEVM {
			evmStoreType, keyBytes := commonevm.ParseEVMKey(node.Key)
			if evmStoreType != evm.StoreEmpty {
				evmChanges[evmStoreType] = append(evmChanges[evmStoreType], &iavl.KVPair{
					Key:   keyBytes,
					Value: node.Value,
				})
				evmPendingCount++

				// Periodically flush to avoid OOM on large imports
				if evmPendingCount >= evmImportFlushThreshold {
					flushEVM()
				}
			}
		}
	}
	close(cosmosCh)

	// Flush any remaining EVM changes
	flushEVM()

	wg.Wait()
	return cosmosErr
}

// RawImport imports raw key-value entries to both stores.
// WriteMode is respected (same routing as Import).
func (s *CompositeStateStore) RawImport(ch <-chan types.RawSnapshotNode) error {
	if s.evmStore == nil || s.config.WriteMode == config.CosmosOnlyWrite {
		return s.cosmosStore.RawImport(ch)
	}

	splitWrite := s.config.WriteMode == config.SplitWrite

	// Fan out to both stores
	cosmosCh := make(chan types.RawSnapshotNode, 100)
	evmChangesByVersion := make(map[int64]map[evm.EVMStoreType][]*iavl.KVPair)
	evmPendingCount := 0

	var wg sync.WaitGroup
	var cosmosErr error

	// Start Cosmos import in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		cosmosErr = s.cosmosStore.RawImport(cosmosCh)
	}()

	// flushEVM writes all buffered EVM changes (grouped by version) and resets.
	flushEVM := func() {
		for version, evmChanges := range evmChangesByVersion {
			if err := s.evmStore.ApplyChangesetParallel(version, evmChanges); err != nil {
				s.logger.Error("failed to flush EVM raw import batch", "version", version, "error", err)
			}
		}
		evmChangesByVersion = make(map[int64]map[evm.EVMStoreType][]*iavl.KVPair)
		evmPendingCount = 0
	}

	// Process incoming nodes
	for node := range ch {
		isEVM := node.StoreKey == evm.EVMStoreKey

		// SplitWrite: skip EVM data for Cosmos; DualWrite: send everything
		if !isEVM || !splitWrite {
			cosmosCh <- node
		}

		// Route EVM keys to EVM-SS
		if isEVM {
			evmStoreType, keyBytes := commonevm.ParseEVMKey(node.Key)
			if evmStoreType != evm.StoreEmpty {
				if evmChangesByVersion[node.Version] == nil {
					evmChangesByVersion[node.Version] = make(map[evm.EVMStoreType][]*iavl.KVPair, evm.NumEVMStoreTypes)
				}
				evmChangesByVersion[node.Version][evmStoreType] = append(
					evmChangesByVersion[node.Version][evmStoreType],
					&iavl.KVPair{Key: keyBytes, Value: node.Value},
				)
				evmPendingCount++

				// Periodically flush to avoid OOM on large imports
				if evmPendingCount >= evmImportFlushThreshold {
					flushEVM()
				}
			}
		}
	}
	close(cosmosCh)

	// Flush any remaining EVM changes
	flushEVM()

	wg.Wait()
	return cosmosErr
}

// Prune removes old versions from both stores.
// EVM_SS uses its own KeepRecent setting if configured, otherwise uses the same version as Cosmos.
func (s *CompositeStateStore) Prune(version int64) error {
	if s.evmStore != nil {
		if err := s.evmStore.Prune(version); err != nil {
			s.logger.Error("failed to prune EVM store", "error", err)
			// Continue to prune Cosmos store
		}
	}
	return s.cosmosStore.Prune(version)
}

// =============================================================================
// Recovery - WAL replay to sync both stores on startup
// =============================================================================

// RecoverCompositeStateStore recovers the composite state store from WAL in a single pass.
// Both Cosmos_SS and EVM_SS share the same changelog, so we replay once and route appropriately:
//   - Entries that Cosmos needs: dual-write through CompositeStateStore
//   - Entries that only EVM needs (catch-up): write only to EVM store
//   - Entries both stores have: skip
func RecoverCompositeStateStore(
	logger logger.Logger,
	changelogPath string,
	compositeStore *CompositeStateStore,
) error {
	cosmosVersion := compositeStore.cosmosStore.GetLatestVersion()

	// No EVM store - simple case, just replay to Cosmos
	if compositeStore.evmStore == nil {
		return ReplayWAL(logger, changelogPath, cosmosVersion, -1, func(entry proto.ChangelogEntry) error {
			if err := compositeStore.ApplyChangesetSync(entry.Version, entry.Changesets); err != nil {
				return fmt.Errorf("failed to apply changeset at version %d: %w", entry.Version, err)
			}
			return compositeStore.SetLatestVersion(entry.Version)
		})
	}

	evmVersion := compositeStore.evmStore.GetLatestVersion()
	evmWriteActive := compositeStore.config.WriteMode != config.CosmosOnlyWrite

	// Start from whichever store is further behind.
	// In CosmosOnlyWrite mode, ignore EVM version (we won't write to EVM).
	startVersion := cosmosVersion
	if evmWriteActive && evmVersion < cosmosVersion {
		startVersion = evmVersion
	}

	logger.Info("Recovering CompositeStateStore",
		"cosmosVersion", cosmosVersion,
		"evmVersion", evmVersion,
		"startVersion", startVersion,
		"writeMode", compositeStore.config.WriteMode,
		"changelogPath", changelogPath,
	)

	// Single-pass replay: route each entry to the stores that need it
	return ReplayWAL(logger, changelogPath, startVersion, -1, func(entry proto.ChangelogEntry) error {
		needsCosmos := entry.Version > cosmosVersion
		needsEVM := evmWriteActive && entry.Version > evmVersion

		if needsCosmos {
			// Both stores need this entry - dual-write through CompositeStateStore
			// (CompositeStateStore.ApplyChangesetSync routes by WriteMode)
			if err := compositeStore.ApplyChangesetSync(entry.Version, entry.Changesets); err != nil {
				return fmt.Errorf("failed to apply changeset at version %d: %w", entry.Version, err)
			}
			return compositeStore.SetLatestVersion(entry.Version)
		}

		if needsEVM {
			// Only EVM needs this entry (Cosmos already has it) - EVM catch-up
			// Errors here are non-fatal: EVM is an optimization layer
			evmChanges := extractEVMChangesFromChangesets(entry.Changesets)
			if len(evmChanges) > 0 {
				if err := compositeStore.evmStore.ApplyChangesetParallel(entry.Version, evmChanges); err != nil {
					logger.Error("Failed to apply EVM changeset during catch-up, continuing",
						"version", entry.Version, "error", err)
				}
			}
			if err := compositeStore.evmStore.SetLatestVersion(entry.Version); err != nil {
				logger.Error("Failed to set EVM version during catch-up, continuing",
					"version", entry.Version, "error", err)
			}
		}

		// Both stores already have this entry - nothing to do
		return nil
	})
}

// WALEntryHandler processes a single WAL entry during replay
type WALEntryHandler func(entry proto.ChangelogEntry) error

// ReplayWAL replays WAL entries from fromVersion (exclusive) to toVersion (inclusive).
// If toVersion is -1, replays to the end of WAL.
// This is the single consolidated function for all WAL replay operations.
//
// Returns nil if the WAL is empty (no entries to replay).
// Returns an error for actual failures (IO errors, corrupt WAL, read failures).
func ReplayWAL(
	logger logger.Logger,
	changelogPath string,
	fromVersion int64,
	toVersion int64, // -1 means replay to end of WAL
	handler WALEntryHandler,
) error {
	streamHandler, err := wal.NewChangelogWAL(logger, changelogPath, wal.Config{})
	if err != nil {
		return fmt.Errorf("failed to open WAL at %s: %w", changelogPath, err)
	}
	defer func() { _ = streamHandler.Close() }()

	firstOffset, err := streamHandler.FirstOffset()
	if err != nil {
		return fmt.Errorf("failed to read WAL first offset: %w", err)
	}
	if firstOffset <= 0 {
		return nil // Empty WAL, nothing to replay
	}

	lastOffset, err := streamHandler.LastOffset()
	if err != nil {
		return fmt.Errorf("failed to read WAL last offset: %w", err)
	}
	if lastOffset <= 0 {
		return nil // Empty WAL, nothing to replay
	}

	// Check if there's anything to replay
	lastEntry, err := streamHandler.ReadAt(lastOffset)
	if err != nil {
		return fmt.Errorf("failed to read last WAL entry: %w", err)
	}

	// Determine effective end version
	endVersion := toVersion
	if endVersion < 0 {
		endVersion = lastEntry.Version
	}

	// Nothing to replay if WAL doesn't have entries beyond fromVersion
	if lastEntry.Version <= fromVersion {
		return nil
	}

	// Find starting offset
	startOffset, err := findReplayStartOffset(streamHandler, firstOffset, lastOffset, fromVersion)
	if err != nil {
		return fmt.Errorf("failed to find replay start offset: %w", err)
	}

	if startOffset > lastOffset {
		return nil // No entries to replay
	}

	logger.Info("Replaying WAL",
		"fromVersion", fromVersion,
		"toVersion", endVersion,
		"startOffset", startOffset,
		"endOffset", lastOffset,
	)

	return streamHandler.Replay(startOffset, lastOffset, func(index uint64, entry proto.ChangelogEntry) error {
		// Stop if we've reached the end version
		if toVersion >= 0 && entry.Version > toVersion {
			return nil
		}
		return handler(entry)
	})
}

// findReplayStartOffset uses binary search to find the first WAL offset whose
// version is greater than targetVersion. WAL entries have monotonically
// increasing versions, so binary search gives O(log N) instead of O(N).
func findReplayStartOffset(streamHandler wal.ChangelogWAL, firstOffset, lastOffset uint64, targetVersion int64) (uint64, error) {
	lo, hi := firstOffset, lastOffset
	result := lastOffset + 1 // default: nothing to replay

	for lo <= hi {
		mid := lo + (hi-lo)/2
		entry, err := streamHandler.ReadAt(mid)
		if err != nil {
			return 0, fmt.Errorf("failed to read WAL at offset %d: %w", mid, err)
		}
		if entry.Version > targetVersion {
			result = mid // candidate; search left for an earlier one
			if mid == firstOffset {
				break // can't go further left
			}
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	return result, nil
}

func extractEVMChangesFromChangesets(changesets []*proto.NamedChangeSet) map[evm.EVMStoreType][]*iavl.KVPair {
	evmChanges := make(map[evm.EVMStoreType][]*iavl.KVPair, evm.NumEVMStoreTypes)
	for _, changeset := range changesets {
		if changeset.Name != evm.EVMStoreKey {
			continue
		}
		for _, kvPair := range changeset.Changeset.Pairs {
			evmStoreType, keyBytes := commonevm.ParseEVMKey(kvPair.Key)
			if evmStoreType == evm.StoreEmpty {
				continue // Skip zero-length keys
			}
			evmChanges[evmStoreType] = append(evmChanges[evmStoreType], &iavl.KVPair{
				Key:    keyBytes,
				Value:  kvPair.Value,
				Delete: kvPair.Delete,
			})
		}
	}
	return evmChanges
}
