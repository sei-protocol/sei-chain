package composite

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/backend"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/cosmos"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/pruning"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "state-db", "ss", "composite")

// Compile-time check.
var _ types.StateStore = (*CompositeStateStore)(nil)

// CompositeStateStore routes operations between Cosmos_SS and EVM_SS.
// Both are db_engine.StateStore; the composite itself also implements db_engine.StateStore.
type CompositeStateStore struct {
	cosmosStore    types.StateStore // CosmosStateStore wrapping MVCC DB
	evmStore       types.StateStore // EVMStateStore wrapping sub MVCC DBs (nil if disabled)
	pruningManager *pruning.Manager
	config         config.StateStoreConfig
	closeOnce      sync.Once
	closeErr       error
}

// NewCompositeStateStore creates a new composite state store.
// Backend (PebbleDB or RocksDB) is resolved at compile time via build-tag-gated files in db_engine/backend.
func NewCompositeStateStore(
	ssConfig config.StateStoreConfig,
	homeDir string,
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
	}

	if ssConfig.EVMSplit {
		evmDir := ssConfig.EVMDBDirectory
		if evmDir == "" {
			evmDir = utils.GetEVMStateStorePath(homeDir, ssConfig.Backend)
		}

		// Runs before the DB is opened so a rejection leaves no empty dir behind.
		if err := validateEVMSSDirectory(cosmosStore, evmDir); err != nil {
			_ = cs.cosmosStore.Close()
			return nil, err
		}

		evmStore, err := evm.NewEVMStateStore(evmDir, ssConfig)
		if err != nil {
			_ = cs.cosmosStore.Close()
			return nil, fmt.Errorf("failed to create EVM store: %w", err)
		}
		cs.evmStore = evmStore
		logger.Info("EVM state store enabled",
			"dir", evmDir,
			"separateDBs", ssConfig.SeparateEVMSubDBs,
		)

		// Catches a dir-exists-but-DB-empty case the directory check can't see.
		if err := cs.validateEVMSSPreRecovery(); err != nil {
			_ = cs.Close()
			return nil, err
		}
	}

	changelogPath := utils.GetChangelogPath(dbHome)
	if err := RecoverCompositeStateStore(changelogPath, cs); err != nil {
		_ = cs.Close()
		return nil, fmt.Errorf("failed to recover state store: %w", err)
	}

	// Mismatched earliest versions = DBs from different snapshots; reads would diverge.
	if err := cs.validateEVMSSPostRecovery(); err != nil {
		_ = cs.Close()
		return nil, err
	}

	cs.StartPruning()

	return cs, nil
}

// ssHasData: checks both latest and earliest because state-sync restore only sets earliest.
func ssHasData(ss types.StateStore) bool {
	return ss.GetLatestVersion() > 0 || ss.GetEarliestVersion() > 0
}

// validateEVMSSDirectory rejects enabling evm-ss-split on a populated Cosmos SS
// when the EVM SS dir is missing or empty (i.e. flipping the flag without state sync).
func validateEVMSSDirectory(cosmosStore types.StateStore, evmDir string) error {
	if !ssHasData(cosmosStore) {
		return nil // fresh node, nothing to diverge from
	}

	entries, err := os.ReadDir(evmDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(
				"EVM SS directory %q does not exist but Cosmos SS already has history; state sync before enabling evm-ss-split, or set evm-ss-split=false",
				evmDir,
			)
		}
		return fmt.Errorf("failed to inspect EVM SS directory %q: %w", evmDir, err)
	}
	if len(entries) == 0 {
		return fmt.Errorf(
			"EVM SS directory %q is empty but Cosmos SS already has history; state sync before enabling evm-ss-split, or set evm-ss-split=false",
			evmDir,
		)
	}
	return nil
}

// validateEVMSSPreRecovery rejects an opened-but-empty EVM SS against a populated Cosmos SS.
func (s *CompositeStateStore) validateEVMSSPreRecovery() error {
	if s.evmStore == nil {
		return nil
	}
	if ssHasData(s.cosmosStore) && !ssHasData(s.evmStore) {
		return fmt.Errorf("EVM SS is empty but Cosmos SS already has history; state sync before enabling evm-ss-split, or set evm-ss-split=false")
	}
	return nil
}

// validateEVMSSPostRecovery rejects mismatched earliest versions between the two SS DBs.
func (s *CompositeStateStore) validateEVMSSPostRecovery() error {
	if s.evmStore == nil {
		return nil
	}
	cosmosEarliest := s.cosmosStore.GetEarliestVersion()
	evmEarliest := s.evmStore.GetEarliestVersion()
	if cosmosEarliest != evmEarliest && (cosmosEarliest > 0 || evmEarliest > 0) {
		return fmt.Errorf(
			"EVM SS earliest version %d does not match Cosmos SS earliest version %d: state sync the EVM SS DB, or set evm-ss-split=false",
			evmEarliest, cosmosEarliest,
		)
	}
	return nil
}

func (s *CompositeStateStore) StartPruning() {
	pm := pruning.NewPruningManager(s, int64(s.config.KeepRecent), int64(s.config.PruneIntervalSeconds))
	pm.Start()
	s.pruningManager = pm
}

// evmRouted returns true when the key should be served from the EVM backend.
// If evmStore is open at all, EVMSplit was true at startup and the backend is
// the sole home for EVM data — routing to cosmos would return wrong/empty.
func (s *CompositeStateStore) evmRouted(storeKey string) bool {
	return s.evmStore != nil && storeKey == evm.EVMStoreKey
}

func (s *CompositeStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	if s.evmRouted(storeKey) {
		return s.evmStore.Get(storeKey, version, key)
	}
	return s.cosmosStore.Get(storeKey, version, key)
}

func (s *CompositeStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	if s.evmRouted(storeKey) {
		return s.evmStore.Has(storeKey, version, key)
	}
	return s.cosmosStore.Has(storeKey, version, key)
}

func (s *CompositeStateStore) Iterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	if s.evmRouted(storeKey) {
		return s.evmStore.Iterator(storeKey, version, start, end)
	}
	return s.cosmosStore.Iterator(storeKey, version, start, end)
}

func (s *CompositeStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	if s.evmRouted(storeKey) {
		return s.evmStore.ReverseIterator(storeKey, version, start, end)
	}
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

// GetEarliestVersionForKey reports the earliest version still held locally by
// the store that serves storeKey. Cosmos and EVM prune independently (an EVM
// prune failure is only logged), so pruned-read fallback must consult the
// horizon of the store that will actually serve the read.
func (s *CompositeStateStore) GetEarliestVersionForKey(storeKey string) int64 {
	if s.evmRouted(storeKey) {
		return s.evmStore.GetEarliestVersion()
	}
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
				logger.Error("failed to close EVM store", "error", err)
				lastErr = err
			}
		}
		if err := s.cosmosStore.Close(); err != nil {
			logger.Error("failed to close Cosmos store", "error", err)
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
	if s.evmStore != nil {
		if err := s.evmStore.SetLatestVersion(version); err != nil {
			logger.Error("failed to set EVM store latest version", "error", err)
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
			logger.Error("failed to set EVM store earliest version", "error", err)
		}
	}
	return nil
}

func (s *CompositeStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	if s.evmStore == nil {
		return s.cosmosStore.ApplyChangesetSync(version, changesets)
	}

	evmChangesets := filterEVMChangesets(changesets)
	cosmosChangesets := stripEVMFromChangesets(changesets)

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
	if s.evmStore == nil {
		return s.cosmosStore.ApplyChangesetAsync(version, changesets)
	}

	evmChangesets := filterEVMChangesets(changesets)
	cosmosChangesets := stripEVMFromChangesets(changesets)

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

// convertFlatKVNodes transforms a single FlatKV physical-key snapshot node
// into one or more SS nodes by stripping the module prefix from the key,
// deserializing the vtype metadata from the value, and (for merged account
// rows) splitting into separate nonce and codeHash nodes.
//
// For EVM-specific keys (account, storage, code) the output StoreKey is "evm".
// For legacy keys the original module name is preserved so they route back to
// the correct Cosmos SS module.
func convertFlatKVNodes(node types.SnapshotNode) ([]types.SnapshotNode, error) {
	moduleName, innerKey, err := ktype.StripModulePrefix(node.Key)
	if err != nil {
		return nil, fmt.Errorf("convertFlatKVNodes failed: %w", err)
	}

	kind, strippedKey := keys.ParseEVMKey(innerKey)

	switch kind {
	case keys.EVMKeyNonce:
		acct, err := vtype.DeserializeAccountData(node.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to DeserializeAccountData: %w", err)
		}
		var nodes []types.SnapshotNode
		if nonce := acct.GetNonce(); !acct.IsDelete() {
			nonceBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(nonceBuf, nonce)
			nodes = append(nodes, types.SnapshotNode{
				StoreKey: evm.EVMStoreKey,
				Key:      keys.BuildEVMKey(keys.EVMKeyNonce, strippedKey),
				Value:    nonceBuf,
			})
		}
		if codeHash := acct.GetCodeHash(); *codeHash != (vtype.CodeHash{}) {
			nodes = append(nodes, types.SnapshotNode{
				StoreKey: evm.EVMStoreKey,
				Key:      keys.BuildEVMKey(keys.EVMKeyCodeHash, strippedKey),
				Value:    append([]byte(nil), codeHash[:]...),
			})
		}
		return nodes, nil

	case keys.EVMKeyStorage:
		sd, err := vtype.DeserializeStorageData(node.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to DeserializeStorageData: %w", err)
		}
		return []types.SnapshotNode{
			{StoreKey: evm.EVMStoreKey, Key: innerKey, Value: sd.GetValue()[:]},
		}, nil

	case keys.EVMKeyCode:
		cd, err := vtype.DeserializeCodeData(node.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to DeserializeCodeData: %w", err)
		}
		return []types.SnapshotNode{
			{StoreKey: evm.EVMStoreKey, Key: innerKey, Value: cd.GetBytecode()},
		}, nil

	case keys.EVMKeyLegacy:
		ld, err := vtype.DeserializeLegacyData(node.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to DeserializeLegacyData legacy: %w", err)
		}
		return []types.SnapshotNode{
			{StoreKey: moduleName, Key: innerKey, Value: ld.GetValue()},
		}, nil

	default:
		return nil, fmt.Errorf("got unexpected type of keys when convertFlatKVNodes")
	}
}

func (s *CompositeStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	importToEVM := s.evmStore != nil

	cosmosCh := make(chan types.SnapshotNode, 100)
	var evmCh chan types.SnapshotNode
	if importToEVM {
		evmCh = make(chan types.SnapshotNode, 100)
	}

	done := make(chan struct{})
	var doneOnce sync.Once
	errs := make(chan error, 2)
	var wg sync.WaitGroup

	fail := func(err error) {
		errs <- err
		doneOnce.Do(func() { close(done) })
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.cosmosStore.Import(version, cosmosCh); err != nil {
			fail(err)
		}
	}()
	if importToEVM {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.evmStore.Import(version, evmCh); err != nil {
				fail(err)
			}
		}()
	}

	send := func(dst chan<- types.SnapshotNode, n types.SnapshotNode) bool {
		select {
		case dst <- n:
			return true
		case <-done:
			return false
		}
	}

	var routeErr error
	for node := range ch {
		if routeErr != nil {
			continue
		}

		var nodes []types.SnapshotNode
		if node.StoreKey == keys.FlatKVStoreKey {
			converted, err := convertFlatKVNodes(node)
			if err != nil {
				routeErr = fmt.Errorf("SS import failure: %w", err)
				continue
			}
			nodes = converted
		} else {
			nodes = append(nodes, node)
		}

		for _, n := range nodes {
			if n.StoreKey == evm.EVMStoreKey && importToEVM {
				if !send(evmCh, n) {
					break
				}
			} else {
				if !send(cosmosCh, n) {
					break
				}
			}
		}
	}

	close(cosmosCh)
	if evmCh != nil {
		close(evmCh)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			return err
		}
	}
	return routeErr
}

func (s *CompositeStateStore) Prune(version int64) error {
	if s.evmStore != nil {
		if err := s.evmStore.Prune(version); err != nil {
			logger.Error("failed to prune EVM store", "error", err)
		}
	}
	return s.cosmosStore.Prune(version)
}

// =============================================================================
// Recovery
// =============================================================================

func RecoverCompositeStateStore(
	changelogPath string,
	compositeStore *CompositeStateStore,
) error {
	var cosmosVersion int64
	if compositeStore.cosmosStore != nil {
		cosmosVersion = compositeStore.cosmosStore.GetLatestVersion()
	}

	var evmVersion int64
	if compositeStore.evmStore != nil {
		evmVersion = compositeStore.evmStore.GetLatestVersion()
	}

	startVersion := cosmosVersion
	if compositeStore.evmStore != nil && evmVersion < startVersion {
		startVersion = evmVersion
	}

	evmSplit := compositeStore.evmStore != nil

	logger.Info("Recovering CompositeStateStore",
		"cosmosVersion", cosmosVersion,
		"evmVersion", evmVersion,
		"startVersion", startVersion,
		"changelogPath", changelogPath,
	)

	return ReplayWAL(changelogPath, startVersion, -1, func(entry proto.ChangelogEntry) error {
		if compositeStore.cosmosStore != nil && entry.Version > cosmosVersion {
			changesets := entry.Changesets
			if evmSplit {
				changesets = stripEVMFromChangesets(changesets)
			}
			if err := compositeStore.cosmosStore.ApplyChangesetSync(entry.Version, changesets); err != nil {
				return fmt.Errorf("failed to apply cosmos changeset at version %d: %w", entry.Version, err)
			}
			if err := compositeStore.cosmosStore.SetLatestVersion(entry.Version); err != nil {
				return fmt.Errorf("failed to set cosmos version %d: %w", entry.Version, err)
			}
		}

		if compositeStore.evmStore != nil && entry.Version > evmVersion {
			evmChangesets := filterEVMChangesets(entry.Changesets)
			if len(evmChangesets) > 0 {
				if err := compositeStore.evmStore.ApplyChangesetSync(entry.Version, evmChangesets); err != nil {
					return fmt.Errorf("failed to apply EVM changeset at version %d: %w", entry.Version, err)
				}
			}
			if err := compositeStore.evmStore.SetLatestVersion(entry.Version); err != nil {
				return fmt.Errorf("failed to set EVM version %d: %w", entry.Version, err)
			}
		}

		return nil
	})
}

type WALEntryHandler func(entry proto.ChangelogEntry) error

func ReplayWAL(
	changelogPath string,
	fromVersion int64,
	toVersion int64,
	handler WALEntryHandler,
) error {
	streamHandler, err := wal.NewChangelogWAL(changelogPath, wal.Config{})
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
