package evm

import (
	"fmt"
	"path/filepath"
	"sync"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/backend"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// Compile-time check: EVMStateStore implements types.StateStore.
var _ types.StateStore = (*EVMStateStore)(nil)

// EVMStateStore manages 5 MvccDB instances (one per EVM sub-type)
// and implements types.StateStore. Each sub-DB is the raw MVCC DB engine
// (PebbleDB or RocksDB via build tags).
//
// Key parsing (commonevm.ParseEVMKey) and sub-DB routing are encapsulated here,
// so callers (CompositeStateStore) pass standard StateStore calls with raw EVM keys.
type EVMStateStore struct {
	subDBs map[EVMStoreType]db_engine.MvccDB
	dir    string
	logger logger.Logger
}

// NewEVMStateStore opens 5 MvccDB instances (one per EVM sub-type) using the
// backend resolved from ssConfig.Backend (PebbleDB by default, RocksDB with build tag).
func NewEVMStateStore(dir string, ssConfig config.StateStoreConfig, log logger.Logger) (*EVMStateStore, error) {
	opener := backend.ResolveBackend(ssConfig.Backend)

	store := &EVMStateStore{
		subDBs: make(map[EVMStoreType]db_engine.MvccDB, NumEVMStoreTypes),
		dir:    dir,
		logger: log,
	}

	for _, storeType := range AllEVMStoreTypes() {
		dbDir := filepath.Join(dir, StoreTypeName(storeType))
		subCfg := subDBConfig(ssConfig, dbDir)
		db, err := opener(dbDir, subCfg)
		if err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("failed to open EVM MVCC DB for %s: %w", StoreTypeName(storeType), err)
		}
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

func (s *EVMStateStore) routeKey(key []byte) (db_engine.MvccDB, string, []byte) {
	storeType, strippedKey := commonevm.ParseEVMKey(key)
	if storeType == StoreEmpty {
		return nil, "", nil
	}
	db := s.subDBs[storeType]
	return db, StoreTypeName(storeType), strippedKey
}

func (s *EVMStateStore) Get(_ string, version int64, key []byte) ([]byte, error) {
	db, subStoreKey, strippedKey := s.routeKey(key)
	if db == nil {
		return nil, nil
	}
	return db.Get(subStoreKey, version, strippedKey)
}

func (s *EVMStateStore) Has(_ string, version int64, key []byte) (bool, error) {
	db, subStoreKey, strippedKey := s.routeKey(key)
	if db == nil {
		return false, nil
	}
	return db.Has(subStoreKey, version, strippedKey)
}

func (s *EVMStateStore) Iterator(_ string, _ int64, _, _ []byte) (db_engine.DBIterator, error) {
	return nil, fmt.Errorf("EVMStateStore: cross-type iteration not supported; use individual sub-DB or Cosmos_SS")
}

func (s *EVMStateStore) ReverseIterator(_ string, _ int64, _, _ []byte) (db_engine.DBIterator, error) {
	return nil, fmt.Errorf("EVMStateStore: cross-type reverse iteration not supported; use individual sub-DB or Cosmos_SS")
}

func (s *EVMStateStore) RawIterate(_ string, _ func([]byte, []byte, int64) bool) (bool, error) {
	return false, fmt.Errorf("EVMStateStore: RawIterate not supported")
}

func (s *EVMStateStore) GetLatestVersion() int64 {
	var minVersion int64 = -1
	for _, db := range s.subDBs {
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
	for _, db := range s.subDBs {
		if err := db.SetLatestVersion(version); err != nil {
			return err
		}
	}
	return nil
}

func (s *EVMStateStore) GetEarliestVersion() int64 {
	var minVersion int64 = -1
	for _, db := range s.subDBs {
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
	for _, db := range s.subDBs {
		if err := db.SetEarliestVersion(version, ignoreVersion); err != nil {
			return err
		}
	}
	return nil
}

func (s *EVMStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	grouped := s.groupBySubType(changesets)
	if len(grouped) == 0 {
		return nil
	}
	return s.applyGrouped(version, grouped, false)
}

func (s *EVMStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
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
			storeType, strippedKey := commonevm.ParseEVMKey(kvPair.Key)
			if storeType == StoreEmpty {
				continue
			}
			grouped[storeType] = append(grouped[storeType], &iavl.KVPair{
				Key:    strippedKey,
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
	subStoreKey := StoreTypeName(storeType)
	cs := []*proto.NamedChangeSet{
		{
			Name:      subStoreKey,
			Changeset: iavl.ChangeSet{Pairs: pairs},
		},
	}
	if async {
		return db.ApplyChangesetAsync(version, cs)
	}
	return db.ApplyChangesetSync(version, cs)
}

// Import parses incoming snapshot nodes, routes EVM keys to sub-DBs via ApplyChangesetSync.
func (s *EVMStateStore) Import(version int64, ch <-chan db_engine.SnapshotNode) error {
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
		storeType, strippedKey := commonevm.ParseEVMKey(node.Key)
		if storeType == StoreEmpty {
			continue
		}
		grouped[storeType] = append(grouped[storeType], &iavl.KVPair{
			Key:   strippedKey,
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

// Prune removes old versions from all sub-DBs in parallel.
func (s *EVMStateStore) Prune(version int64) error {
	if len(s.subDBs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(s.subDBs))

	for _, db := range s.subDBs {
		wg.Add(1)
		go func(db db_engine.MvccDB) {
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
	for _, db := range s.subDBs {
		if err := db.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}
