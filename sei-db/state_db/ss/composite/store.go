package composite

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/backend"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/cosmos"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/pruning"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

// Compile-time check.
var _ types.StateStore = (*CompositeStateStore)(nil)

// CompositeStateStore routes operations between Cosmos_SS and EVM_SS.
// Both are db_engine.StateStore; the composite itself also implements db_engine.StateStore.
type CompositeStateStore struct {
	cosmosStore    types.StateStore // CosmosStateStore wrapping MVCC DB
	evmStore       types.StateStore // EVMStateStore wrapping sub MVCC DBs (nil if disabled)
	pruningManager *pruning.Manager
	config         config.StateStoreConfig
	logger         logger.Logger
	closeOnce      sync.Once
	closeErr       error
}

// NewCompositeStateStore creates a new composite state store.
// Backend (PebbleDB or RocksDB) is resolved at compile time via build-tag-gated files in db_engine/backend.
func NewCompositeStateStore(
	ssConfig config.StateStoreConfig,
	homeDir string,
	log logger.Logger,
) (*CompositeStateStore, error) {
	dbHome := utils.GetStateStorePath(homeDir, ssConfig.Backend)
	if ssConfig.DBDirectory != "" {
		dbHome = ssConfig.DBDirectory
	}

	mvccDB, err := backend.ResolveBackend(ssConfig.Backend)(dbHome, ssConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create cosmos MVCC DB: %w", err)
	}
	cosmosStore := cosmos.NewCosmosStateStore(mvccDB)

	cs := &CompositeStateStore{
		cosmosStore: cosmosStore,
		config:      ssConfig,
		logger:      log,
	}

	if ssConfig.EVMEnabled() {
		evmDir := ssConfig.EVMDBDirectory
		if evmDir == "" {
			evmDir = filepath.Join(homeDir, "data", "evm_ss")
		}

		evmStore, err := evm.NewEVMStateStore(evmDir, ssConfig, log)
		if err != nil {
			_ = cs.cosmosStore.Close()
			return nil, fmt.Errorf("failed to create EVM store: %w", err)
		}
		cs.evmStore = evmStore
		log.Info("EVM state store enabled", "dir", evmDir, "writeMode", ssConfig.WriteMode, "readMode", ssConfig.ReadMode)
	}

	changelogPath := utils.GetChangelogPath(dbHome)
	if err := RecoverCompositeStateStore(log, changelogPath, cs); err != nil {
		_ = cs.Close()
		return nil, fmt.Errorf("failed to recover state store: %w", err)
	}

	cs.StartPruning()

	return cs, nil
}

func (s *CompositeStateStore) StartPruning() {
	pm := pruning.NewPruningManager(s.logger, s, int64(s.config.KeepRecent), int64(s.config.PruneIntervalSeconds))
	pm.Start()
	s.pruningManager = pm
}

func (s *CompositeStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	if s.evmStore != nil && s.config.ReadMode != config.CosmosOnlyRead && storeKey == evm.EVMStoreKey {
		val, err := s.evmStore.Get(storeKey, version, key)
		if err != nil {
			return nil, err
		}
		if val != nil {
			return val, nil
		}
		if s.config.ReadMode == config.SplitRead {
			return nil, nil
		}
	}
	return s.cosmosStore.Get(storeKey, version, key)
}

func (s *CompositeStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	if s.evmStore != nil && s.config.ReadMode != config.CosmosOnlyRead && storeKey == evm.EVMStoreKey {
		has, err := s.evmStore.Has(storeKey, version, key)
		if err != nil {
			return false, err
		}
		if has {
			return true, nil
		}
		if s.config.ReadMode == config.SplitRead {
			return false, nil
		}
	}
	return s.cosmosStore.Has(storeKey, version, key)
}

func (s *CompositeStateStore) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return s.cosmosStore.Iterator(storeKey, version, start, end)
}

func (s *CompositeStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return s.cosmosStore.ReverseIterator(storeKey, version, start, end)
}

func (s *CompositeStateStore) RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error) {
	return s.cosmosStore.RawIterate(storeKey, fn)
}

func (s *CompositeStateStore) GetLatestVersion() int64 {
	return s.cosmosStore.GetLatestVersion()
}

func (s *CompositeStateStore) GetEarliestVersion() int64 {
	return s.cosmosStore.GetEarliestVersion()
}

func (s *CompositeStateStore) Close() error {
	s.closeOnce.Do(func() {
		if s.pruningManager != nil {
			s.pruningManager.Stop()
		}
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
// Write path
// =============================================================================

func (s *CompositeStateStore) SetLatestVersion(version int64) error {
	if err := s.cosmosStore.SetLatestVersion(version); err != nil {
		return err
	}
	if s.evmStore != nil && s.config.WriteMode != config.CosmosOnlyWrite {
		if err := s.evmStore.SetLatestVersion(version); err != nil {
			s.logger.Error("failed to set EVM store latest version", "error", err)
		}
	}
	return nil
}

func (s *CompositeStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	if err := s.cosmosStore.SetEarliestVersion(version, ignoreVersion); err != nil {
		return err
	}
	if s.evmStore != nil {
		if err := s.evmStore.SetEarliestVersion(version, ignoreVersion); err != nil {
			s.logger.Error("failed to set EVM store earliest version", "error", err)
		}
	}
	return nil
}

func (s *CompositeStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	if s.evmStore == nil || s.config.WriteMode == config.CosmosOnlyWrite {
		return s.cosmosStore.ApplyChangesetSync(version, changesets)
	}

	evmChangesets := filterEVMChangesets(changesets)
	cosmosChangesets := changesets
	if s.config.WriteMode == config.SplitWrite {
		cosmosChangesets = stripEVMFromChangesets(changesets)
	}

	if err := s.cosmosStore.ApplyChangesetSync(version, cosmosChangesets); err != nil {
		return fmt.Errorf("cosmos store failed: %w", err)
	}
	if len(evmChangesets) > 0 {
		if err := s.evmStore.ApplyChangesetSync(version, evmChangesets); err != nil {
			return fmt.Errorf("evm store failed: %w", err)
		}
	}
	return nil
}

func (s *CompositeStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	if s.evmStore == nil || s.config.WriteMode == config.CosmosOnlyWrite {
		return s.cosmosStore.ApplyChangesetAsync(version, changesets)
	}

	evmChangesets := filterEVMChangesets(changesets)
	cosmosChangesets := changesets
	if s.config.WriteMode == config.SplitWrite {
		cosmosChangesets = stripEVMFromChangesets(changesets)
	}

	if err := s.cosmosStore.ApplyChangesetAsync(version, cosmosChangesets); err != nil {
		return fmt.Errorf("cosmos store failed: %w", err)
	}
	if len(evmChangesets) > 0 {
		if err := s.evmStore.ApplyChangesetAsync(version, evmChangesets); err != nil {
			return fmt.Errorf("evm store async enqueue failed: %w", err)
		}
	}
	return nil
}

func filterEVMChangesets(changesets []*proto.NamedChangeSet) []*proto.NamedChangeSet {
	var evmCS []*proto.NamedChangeSet
	for _, cs := range changesets {
		if cs.Name == evm.EVMStoreKey {
			evmCS = append(evmCS, cs)
		}
	}
	return evmCS
}

func stripEVMFromChangesets(changesets []*proto.NamedChangeSet) []*proto.NamedChangeSet {
	stripped := make([]*proto.NamedChangeSet, 0, len(changesets))
	for _, cs := range changesets {
		if cs.Name != evm.EVMStoreKey {
			stripped = append(stripped, cs)
		}
	}
	return stripped
}

func (s *CompositeStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	if s.evmStore == nil || s.config.WriteMode == config.CosmosOnlyWrite {
		return s.cosmosStore.Import(version, ch)
	}

	splitWrite := s.config.WriteMode == config.SplitWrite

	cosmosCh := make(chan types.SnapshotNode, 100)
	evmCh := make(chan types.SnapshotNode, 100)

	var wg sync.WaitGroup
	var cosmosErr, evmErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		cosmosErr = s.cosmosStore.Import(version, cosmosCh)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		evmErr = s.evmStore.Import(version, evmCh)
	}()

	for node := range ch {
		isEVM := node.StoreKey == evm.EVMStoreKey
		if !isEVM || !splitWrite {
			cosmosCh <- node
		}
		if isEVM {
			evmCh <- node
		}
	}
	close(cosmosCh)
	close(evmCh)

	wg.Wait()
	if cosmosErr != nil {
		return cosmosErr
	}
	return evmErr
}

func (s *CompositeStateStore) Prune(version int64) error {
	if s.evmStore != nil {
		if err := s.evmStore.Prune(version); err != nil {
			s.logger.Error("failed to prune EVM store", "error", err)
		}
	}
	return s.cosmosStore.Prune(version)
}

// =============================================================================
// Recovery
// =============================================================================

func RecoverCompositeStateStore(
	logger logger.Logger,
	changelogPath string,
	compositeStore *CompositeStateStore,
) error {
	// TODO: Remove this piece once cosmos is fully deprecated
	if compositeStore.cosmosStore != nil {
		cosmosVersion := compositeStore.cosmosStore.GetLatestVersion()
		logger.Info("Recovering Cosmos state store",
			"cosmosVersion", cosmosVersion,
			"changelogPath", changelogPath,
		)
		if err := ReplayWAL(logger, changelogPath, cosmosVersion, -1, func(entry proto.ChangelogEntry) error {
			changesets := entry.Changesets
			if compositeStore.config.WriteMode == config.SplitWrite {
				changesets = stripEVMFromChangesets(changesets)
			}
			if err := compositeStore.cosmosStore.ApplyChangesetSync(entry.Version, changesets); err != nil {
				return fmt.Errorf("failed to apply cosmos changeset at version %d: %w", entry.Version, err)
			}
			return compositeStore.cosmosStore.SetLatestVersion(entry.Version)
		}); err != nil {
			return fmt.Errorf("failed to recover cosmos state store: %w", err)
		}
	}

	// TODO: consider combine the replay together to avoid double read WAL
	if compositeStore.evmStore != nil {
		evmVersion := compositeStore.evmStore.GetLatestVersion()
		if evmVersion <= 0 {
			return nil
		}
		logger.Info("Recovering EVM state store",
			"evmVersion", evmVersion,
			"changelogPath", changelogPath,
		)
		if err := ReplayWAL(logger, changelogPath, evmVersion, -1, func(entry proto.ChangelogEntry) error {
			evmChangesets := filterEVMChangesets(entry.Changesets)
			if len(evmChangesets) > 0 {
				if err := compositeStore.evmStore.ApplyChangesetSync(entry.Version, evmChangesets); err != nil {
					return fmt.Errorf("failed to apply EVM changeset at version %d: %w", entry.Version, err)
				}
			}
			return compositeStore.evmStore.SetLatestVersion(entry.Version)
		}); err != nil {
			return fmt.Errorf("failed to recover EVM state store: %w", err)
		}
	}

	return nil
}

type WALEntryHandler func(entry proto.ChangelogEntry) error

func ReplayWAL(
	logger logger.Logger,
	changelogPath string,
	fromVersion int64,
	toVersion int64,
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
		return nil
	}

	lastOffset, err := streamHandler.LastOffset()
	if err != nil {
		return fmt.Errorf("failed to read WAL last offset: %w", err)
	}
	if lastOffset <= 0 {
		return nil
	}

	lastEntry, err := streamHandler.ReadAt(lastOffset)
	if err != nil {
		return fmt.Errorf("failed to read last WAL entry: %w", err)
	}

	endVersion := toVersion
	if endVersion < 0 {
		endVersion = lastEntry.Version
	}

	if lastEntry.Version <= fromVersion {
		return nil
	}

	startOffset, err := findReplayStartOffset(streamHandler, firstOffset, lastOffset, fromVersion)
	if err != nil {
		return fmt.Errorf("failed to find replay start offset: %w", err)
	}

	if startOffset > lastOffset {
		return nil
	}

	logger.Info("Replaying WAL",
		"fromVersion", fromVersion,
		"toVersion", endVersion,
		"startOffset", startOffset,
		"endOffset", lastOffset,
	)

	return streamHandler.Replay(startOffset, lastOffset, func(index uint64, entry proto.ChangelogEntry) error {
		if toVersion >= 0 && entry.Version > toVersion {
			return nil
		}
		return handler(entry)
	})
}

func findReplayStartOffset(streamHandler wal.ChangelogWAL, firstOffset, lastOffset uint64, targetVersion int64) (uint64, error) {
	lo, hi := firstOffset, lastOffset
	result := lastOffset + 1

	for lo <= hi {
		mid := lo + (hi-lo)/2
		entry, err := streamHandler.ReadAt(mid)
		if err != nil {
			return 0, fmt.Errorf("failed to read WAL at offset %d: %w", mid, err)
		}
		if entry.Version > targetVersion {
			result = mid
			if mid == firstOffset {
				break
			}
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	return result, nil
}
