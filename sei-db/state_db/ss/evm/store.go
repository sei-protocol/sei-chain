package evm

import (
	"fmt"
	"path/filepath"
	"sync"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/backend"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

var _ types.StateStore = (*EVMStateStore)(nil)

// EVMStateStore manages either a single MVCC DB for all EVM data or one DB per
// EVM sub-type, depending on config. In both modes, the logical store key and
// key encoding remain unchanged.
type EVMStateStore struct {
	subDBs      map[EVMStoreType]types.StateStore
	managedDBs  []types.StateStore
	dir         string
	separateDBs bool
}

// NewEVMStateStore opens either a single unified MVCC DB for all EVM state
// or one MVCC DB per EVM sub-type.
func NewEVMStateStore(dir string, ssConfig config.StateStoreConfig) (*EVMStateStore, error) {
	opener := backend.ResolveBackend(ssConfig.Backend)

	store := &EVMStateStore{
		subDBs:      make(map[EVMStoreType]types.StateStore, NumEVMStoreTypes),
		dir:         dir,
		separateDBs: ssConfig.SeparateEVMSubDBs,
	}

	if ssConfig.SeparateEVMSubDBs {
		for _, storeType := range AllEVMStoreTypes() {
			dbDir := filepath.Join(dir, StoreTypeName(storeType))
			subCfg := subDBConfig(ssConfig, dbDir)
			db, err := opener(dbDir, subCfg)
			if err != nil {
				_ = store.Close()
				return nil, fmt.Errorf("failed to open EVM MVCC DB for %s: %w", StoreTypeName(storeType), err)
			}
			store.subDBs[storeType] = db
			store.managedDBs = append(store.managedDBs, db)
		}
		return store, nil
	}

	cfg := subDBConfig(ssConfig, dir)
	db, err := opener(dir, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open unified EVM MVCC DB: %w", err)
	}
	store.managedDBs = append(store.managedDBs, db)
	for _, storeType := range AllEVMStoreTypes() {
		store.subDBs[storeType] = db
	}

	return store, nil
}

func subDBConfig(parent config.StateStoreConfig, dbDir string) config.StateStoreConfig {
	cfg := parent
	cfg.DBDirectory = dbDir
	cfg.UseDefaultComparer = true
	return cfg
}

func (s *EVMStateStore) primaryDB() types.StateStore {
	if len(s.managedDBs) == 0 {
		return nil
	}
	return s.managedDBs[0]
}

func (s *EVMStateStore) routeKey(key []byte) types.StateStore {
	storeType, _ := commonevm.ParseEVMKey(key)
	if storeType == StoreEmpty {
		return nil
	}
	return s.subDBs[storeType]
}

func (s *EVMStateStore) Get(_ string, version int64, key []byte) ([]byte, error) {
	db := s.routeKey(key)
	if db == nil {
		return nil, nil
	}
	return db.Get(EVMStoreKey, version, key)
}

func (s *EVMStateStore) Has(_ string, version int64, key []byte) (bool, error) {
	db := s.routeKey(key)
	if db == nil {
		return false, nil
	}
	return db.Has(EVMStoreKey, version, key)
}

func (s *EVMStateStore) Iterator(_ string, _ int64, _, _ []byte) (types.DBIterator, error) {
	return nil, fmt.Errorf("EVMStateStore: cross-type iteration not supported; use Cosmos_SS")
}

func (s *EVMStateStore) ReverseIterator(_ string, _ int64, _, _ []byte) (types.DBIterator, error) {
	return nil, fmt.Errorf("EVMStateStore: cross-type reverse iteration not supported; use Cosmos_SS")
}

func (s *EVMStateStore) RawIterate(_ string, _ func([]byte, []byte, int64) bool) (bool, error) {
	return false, fmt.Errorf("EVMStateStore: RawIterate not supported")
}

func (s *EVMStateStore) GetLatestVersion() int64 {
	var minVersion int64 = -1
	for _, db := range s.managedDBs {
		if v := db.GetLatestVersion(); minVersion < 0 || v < minVersion {
			minVersion = v
		}
	}
	if minVersion < 0 {
		return 0
	}
	return minVersion
}

func (s *EVMStateStore) SetLatestVersion(version int64) error {
	for _, db := range s.managedDBs {
		if err := db.SetLatestVersion(version); err != nil {
			return err
		}
	}
	return nil
}

func (s *EVMStateStore) GetEarliestVersion() int64 {
	var minVersion int64 = -1
	for _, db := range s.managedDBs {
		if v := db.GetEarliestVersion(); minVersion < 0 || v < minVersion {
			minVersion = v
		}
	}
	if minVersion < 0 {
		return 0
	}
	return minVersion
}

func (s *EVMStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	for _, db := range s.managedDBs {
		if err := db.SetEarliestVersion(version, ignoreVersion); err != nil {
			return err
		}
	}
	return nil
}

func (s *EVMStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	if !s.separateDBs {
		db := s.primaryDB()
		if db == nil {
			return nil
		}
		evmChangesets := filterEVMChangesets(changesets)
		if len(evmChangesets) == 0 {
			return nil
		}
		return db.ApplyChangesetSync(version, evmChangesets)
	}

	grouped := s.groupBySubType(changesets)
	if len(grouped) == 0 {
		return nil
	}
	return s.applyGrouped(version, grouped, false)
}

func (s *EVMStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	if !s.separateDBs {
		db := s.primaryDB()
		if db == nil {
			return nil
		}
		evmChangesets := filterEVMChangesets(changesets)
		if len(evmChangesets) == 0 {
			return nil
		}
		return db.ApplyChangesetAsync(version, evmChangesets)
	}

	grouped := s.groupBySubType(changesets)
	if len(grouped) == 0 {
		return nil
	}
	return s.applyGrouped(version, grouped, true)
}

func (s *EVMStateStore) groupBySubType(changesets []*proto.NamedChangeSet) map[EVMStoreType][]*iavl.KVPair {
	grouped := make(map[EVMStoreType][]*iavl.KVPair, NumEVMStoreTypes)
	for _, cs := range changesets {
		if cs.Name != EVMStoreKey {
			continue
		}
		for _, kvPair := range cs.Changeset.Pairs {
			storeType, _ := commonevm.ParseEVMKey(kvPair.Key)
			if storeType == StoreEmpty {
				continue
			}
			grouped[storeType] = append(grouped[storeType], &iavl.KVPair{
				Key:    kvPair.Key,
				Value:  kvPair.Value,
				Delete: kvPair.Delete,
			})
		}
	}
	return grouped
}

func (s *EVMStateStore) applyGrouped(version int64, grouped map[EVMStoreType][]*iavl.KVPair, async bool) error {
	if len(grouped) == 1 {
		for storeType, pairs := range grouped {
			return s.applyToSubDB(storeType, version, pairs, async)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(grouped))

	for storeType, pairs := range grouped {
		wg.Add(1)
		go func(st EVMStoreType, p []*iavl.KVPair) {
			defer wg.Done()
			if err := s.applyToSubDB(st, version, p, async); err != nil {
				errCh <- err
			}
		}(storeType, pairs)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		return err
	}
	return nil
}

func (s *EVMStateStore) applyToSubDB(storeType EVMStoreType, version int64, pairs []*iavl.KVPair, async bool) error {
	db := s.subDBs[storeType]
	if db == nil {
		return nil
	}
	cs := []*proto.NamedChangeSet{
		{
			Name:      EVMStoreKey,
			Changeset: iavl.ChangeSet{Pairs: pairs},
		},
	}
	if async {
		return db.ApplyChangesetAsync(version, cs)
	}
	return db.ApplyChangesetSync(version, cs)
}

func (s *EVMStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	if !s.separateDBs {
		db := s.primaryDB()
		if db == nil {
			return nil
		}
		filtered := make(chan types.SnapshotNode, 100)
		go func() {
			defer close(filtered)
			for node := range ch {
				if node.StoreKey == EVMStoreKey {
					filtered <- node
				}
			}
		}()
		return db.Import(version, filtered)
	}

	const flushThreshold = 10000
	grouped := make(map[EVMStoreType][]*iavl.KVPair, NumEVMStoreTypes)
	pending := 0

	flush := func() error {
		if len(grouped) == 0 {
			return nil
		}
		if err := s.applyGrouped(version, grouped, false); err != nil {
			return err
		}
		grouped = make(map[EVMStoreType][]*iavl.KVPair, NumEVMStoreTypes)
		pending = 0
		return nil
	}

	for node := range ch {
		storeType, _ := commonevm.ParseEVMKey(node.Key)
		if storeType == StoreEmpty {
			continue
		}
		grouped[storeType] = append(grouped[storeType], &iavl.KVPair{
			Key:   node.Key,
			Value: node.Value,
		})
		pending++
		if pending >= flushThreshold {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func (s *EVMStateStore) Prune(version int64) error {
	if len(s.managedDBs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(s.managedDBs))

	for _, db := range s.managedDBs {
		wg.Add(1)
		go func(db types.StateStore) {
			defer wg.Done()
			if err := db.Prune(version); err != nil {
				errCh <- err
			}
		}(db)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		return err
	}
	return nil
}

func (s *EVMStateStore) Close() error {
	var lastErr error
	for _, db := range s.managedDBs {
		if err := db.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func filterEVMChangesets(changesets []*proto.NamedChangeSet) []*proto.NamedChangeSet {
	filtered := make([]*proto.NamedChangeSet, 0, len(changesets))
	for _, cs := range changesets {
		if cs.Name == EVMStoreKey {
			filtered = append(filtered, cs)
		}
	}
	return filtered
}
