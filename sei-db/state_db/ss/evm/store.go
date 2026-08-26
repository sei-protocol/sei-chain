package evm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	dbm "github.com/tendermint/tm-db"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/backend"
	sssnapshot "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/snapshot"
)

var _ types.StateStore = (*EVMStateStore)(nil)
var _ types.ContextIteratorStore = (*EVMStateStore)(nil)

// EVMStateStore manages either a single MVCC DB for all EVM data or one DB per
// EVM sub-type, depending on config. In both modes, the logical store key and
// key encoding remain unchanged.
type EVMStateStore struct {
	subDBs      map[EVMStoreType]types.StateStore
	managedDBs  []types.StateStore
	dir         string
	separateDBs bool
	snapshotMgr *sssnapshot.Manager

	externalPruning bool
}

// NewEVMStateStore opens either a single unified MVCC DB for all EVM state
// or one MVCC DB per EVM sub-type.
func NewEVMStateStore(dir string, ssConfig config.StateStoreConfig) (*EVMStateStore, error) {
	opener := backend.ResolveBackend(ssConfig.Backend)

	store := &EVMStateStore{
		subDBs:          make(map[EVMStoreType]types.StateStore, NumEVMStoreTypes),
		dir:             dir,
		separateDBs:     ssConfig.SeparateEVMSubDBs,
		externalPruning: ssConfig.ExternalPruning,
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

func (s *EVMStateStore) Dir() string {
	return s.dir
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

func (s *EVMStateStore) Iterator(_ string, version int64, start, end []byte) (dbm.Iterator, error) {
	if !s.separateDBs {
		return s.primaryDB().Iterator(EVMStoreKey, version, start, end)
	}
	db := s.routeKey(start)
	if db == nil {
		return nil, fmt.Errorf("EVMStateStore: cannot route iteration for key")
	}
	return db.Iterator(EVMStoreKey, version, start, end)
}

func (s *EVMStateStore) ReverseIterator(_ string, version int64, start, end []byte) (dbm.Iterator, error) {
	if !s.separateDBs {
		return s.primaryDB().ReverseIterator(EVMStoreKey, version, start, end)
	}
	db := s.routeKey(start)
	if db == nil {
		return nil, fmt.Errorf("EVMStateStore: cannot route reverse iteration for key")
	}
	return db.ReverseIterator(EVMStoreKey, version, start, end)
}

func (s *EVMStateStore) IteratorWithContext(ctx context.Context, _ string, version int64, start, end []byte) (dbm.Iterator, error) {
	if !s.separateDBs {
		return types.IterateWithContext(s.primaryDB(), ctx, EVMStoreKey, version, start, end, false)
	}
	db := s.routeKey(start)
	if db == nil {
		return nil, fmt.Errorf("EVMStateStore: cannot route iteration for key")
	}
	return types.IterateWithContext(db, ctx, EVMStoreKey, version, start, end, false)
}

func (s *EVMStateStore) ReverseIteratorWithContext(ctx context.Context, _ string, version int64, start, end []byte) (dbm.Iterator, error) {
	if !s.separateDBs {
		return types.IterateWithContext(s.primaryDB(), ctx, EVMStoreKey, version, start, end, true)
	}
	db := s.routeKey(start)
	if db == nil {
		return nil, fmt.Errorf("EVMStateStore: cannot route reverse iteration for key")
	}
	return types.IterateWithContext(db, ctx, EVMStoreKey, version, start, end, true)
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
	var maxVersion int64
	for _, db := range s.managedDBs {
		if v := db.GetEarliestVersion(); v > maxVersion {
			maxVersion = v
		}
	}
	return maxVersion
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

func (s *EVMStateStore) groupBySubType(changesets []*proto.NamedChangeSet) map[EVMStoreType][]*proto.KVPair {
	grouped := make(map[EVMStoreType][]*proto.KVPair, NumEVMStoreTypes)
	for _, cs := range changesets {
		if cs.Name != EVMStoreKey {
			continue
		}
		for _, kvPair := range cs.Changeset.Pairs {
			storeType, _ := commonevm.ParseEVMKey(kvPair.Key)
			if storeType == StoreEmpty {
				continue
			}
			grouped[storeType] = append(grouped[storeType], &proto.KVPair{
				Key:    kvPair.Key,
				Value:  kvPair.Value,
				Delete: kvPair.Delete,
			})
		}
	}
	return grouped
}

func (s *EVMStateStore) applyGrouped(version int64, grouped map[EVMStoreType][]*proto.KVPair, async bool) error {
	if len(grouped) == 1 {
		for storeType, pairs := range grouped {
			return s.applyToSubDB(storeType, version, pairs, async)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(grouped))

	for storeType, pairs := range grouped {
		wg.Add(1)
		go func(st EVMStoreType, p []*proto.KVPair) {
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

func (s *EVMStateStore) applyToSubDB(storeType EVMStoreType, version int64, pairs []*proto.KVPair, async bool) error {
	db := s.subDBs[storeType]
	if db == nil {
		return nil
	}
	cs := []*proto.NamedChangeSet{
		{
			Name:      EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: pairs},
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
	grouped := make(map[EVMStoreType][]*proto.KVPair, NumEVMStoreTypes)
	pending := 0

	flush := func() error {
		if len(grouped) == 0 {
			return nil
		}
		if err := s.applyGrouped(version, grouped, false); err != nil {
			return err
		}
		grouped = make(map[EVMStoreType][]*proto.KVPair, NumEVMStoreTypes)
		pending = 0
		return nil
	}

	for node := range ch {
		storeType, _ := commonevm.ParseEVMKey(node.Key)
		if storeType == StoreEmpty {
			continue
		}
		grouped[storeType] = append(grouped[storeType], &proto.KVPair{
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

func (s *EVMStateStore) ExternalPruning() bool {
	return s.externalPruning
}

func (s *EVMStateStore) snapshotSourceDirs() []string {
	if !s.separateDBs {
		return []string{s.dir}
	}
	storeTypes := AllEVMStoreTypes()
	dirs := make([]string, 0, len(storeTypes))
	for _, storeType := range storeTypes {
		dirs = append(dirs, subDBPath(s.dir, storeType))
	}
	return dirs
}

// subDBPath returns the directory a sub-DB occupies under base. The live database, a checkpoint, and a
// snapshot source all use this layout.
func subDBPath(base string, storeType EVMStoreType) string {
	return filepath.Join(base, StoreTypeName(storeType))
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

func (s *EVMStateStore) SupportsCheckpoint() bool {
	for _, db := range s.managedDBs {
		if !sssnapshot.SupportsCheckpoint(db) {
			return false
		}
	}
	return len(s.managedDBs) > 0
}

// ScheduleCheckpoint places one barrier on each managed apply queue.
//
// Every sub-DB checkpoints at the same block without the sub-DBs having to agree
// on anything. The caller runs this after it has enqueued the target block on
// every sub-DB and before it enqueues any later block, so each barrier lands at
// the same point in its own queue: after that block and before the next one. A
// sub-DB then checkpoints its own state as of that block. Wall-clock times
// differ, and no lock is shared. A sub-DB that received no change at the target
// block stays at its last version, which is that sub-DB's correct state for the
// block. SetCheckpointVersion afterwards labels every sub-DB with the same
// block, so a reopened snapshot reports one version rather than five.
func (s *EVMStateStore) ScheduleCheckpoint(destDir string, shouldRun func() bool, done func(error)) {
	if !s.separateDBs {
		db := s.primaryDB()
		if db == nil {
			// Unreachable: NewEVMStateStore either opens a managed DB or fails.
			// Reporting success would publish a snapshot with no evm tree in it,
			// which is only discovered by whoever tries to restore from it.
			done(errors.New("EVM state store has no managed DB to checkpoint"))
			return
		}
		sssnapshot.ScheduleCheckpoint(db, destDir, shouldRun, done)
		return
	}

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		done(fmt.Errorf("create EVM checkpoint dir %q: %w", destDir, err))
		return
	}

	storeTypes := AllEVMStoreTypes()
	report := sssnapshot.FanIn(len(storeTypes), done)
	for _, storeType := range storeTypes {
		name := StoreTypeName(storeType)
		sssnapshot.ScheduleCheckpoint(s.subDBs[storeType], subDBPath(destDir, storeType), shouldRun, func(err error) {
			if err != nil {
				err = fmt.Errorf("checkpoint EVM sub-DB %s: %w", name, err)
			}
			report(err)
		})
	}
}

func (s *EVMStateStore) SetCheckpointVersion(destDir string, version int64) error {
	if !s.separateDBs {
		db := s.primaryDB()
		if db == nil {
			return errors.New("EVM state store has no managed DB to stamp")
		}
		return sssnapshot.SetCheckpointVersion(db, destDir, version)
	}
	for _, storeType := range AllEVMStoreTypes() {
		name := StoreTypeName(storeType)
		if err := sssnapshot.SetCheckpointVersion(s.subDBs[storeType], subDBPath(destDir, storeType), version); err != nil {
			return fmt.Errorf("set EVM sub-DB %s checkpoint version: %w", name, err)
		}
	}
	return nil
}

func (s *EVMStateStore) StartSnapshots(
	root string,
	ssConfig config.StateStoreConfig,
	floor *sssnapshot.Floor,
) error {
	manager, err := sssnapshot.Open(sssnapshot.Config{
		Name:            "evm",
		Root:            root,
		SourceDirs:      s.snapshotSourceDirs(),
		Backend:         ssConfig.Backend,
		KeepRecent:      ssConfig.SnapshotKeepRecent,
		ExternalPruning: ssConfig.ExternalPruning,
		Checkpointer:    s,
		Floor:           floor,
	})
	if err != nil {
		return err
	}
	s.snapshotMgr = manager
	s.externalPruning = ssConfig.ExternalPruning
	return nil
}

func (s *EVMStateStore) Snapshots() *sssnapshot.Manager {
	return s.snapshotMgr
}

func (s *EVMStateStore) WaitForPendingWrites() {
	for _, db := range s.managedDBs {
		if w, ok := db.(types.PendingWriteWaiter); ok {
			w.WaitForPendingWrites()
		}
	}
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
