package evm

import (
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
)

var _ types.StateStore = (*EVMStateStore)(nil)

// EVMStateStore manages either a single MVCC DB for all EVM data or one DB per
// EVM sub-type, depending on config. In both modes, the logical store key and
// key encoding remain unchanged.
type EVMStateStore struct {
	subDBs     map[EVMStoreType]types.StateStore
	managedDBs []types.StateStore
	// managedNames labels managedDBs positionally, so an error about one
	// backend can say which of the per-type DBs it came from.
	managedNames []string
	dir          string
	separateDBs  bool
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
			store.managedNames = append(store.managedNames, StoreTypeName(storeType))
		}
		return store, nil
	}

	cfg := subDBConfig(ssConfig, dir)
	db, err := opener(dir, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open unified EVM MVCC DB: %w", err)
	}
	store.managedDBs = append(store.managedDBs, db)
	store.managedNames = append(store.managedNames, "unified")
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

// managedDBName labels the managed DB at index i. In separate-sub-DBs mode the
// label is the EVM sub-type, which is what an operator needs to know when one
// sub-store is the one blocking a rollback.
func (s *EVMStateStore) managedDBName(i int) string {
	if i < len(s.managedNames) {
		return s.managedNames[i]
	}
	return fmt.Sprintf("db-%d", i)
}

func (s *EVMStateStore) CheckRollbackCoverage(targetVersion int64) error {
	for i, db := range s.managedDBs {
		checker, ok := db.(types.RollbackCoverageChecker)
		if !ok {
			return fmt.Errorf("EVM state store %s backend %T does not support rollback coverage checks", s.managedDBName(i), db)
		}
		if err := checker.CheckRollbackCoverage(targetVersion); err != nil {
			return fmt.Errorf("EVM state store %s: %w", s.managedDBName(i), err)
		}
	}
	return nil
}

func (s *EVMStateStore) Rollback(targetVersion int64) error {
	if err := s.CheckRollbackCoverage(targetVersion); err != nil {
		return err
	}
	for i, db := range s.managedDBs {
		rb, ok := db.(types.Rollbackable)
		if !ok {
			return fmt.Errorf("EVM state store %s backend %T does not support rollback", s.managedDBName(i), db)
		}
		if err := rb.Rollback(targetVersion); err != nil {
			return fmt.Errorf("EVM state store %s: %w", s.managedDBName(i), err)
		}
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

// Checkpoint writes a point-in-time snapshot of the EVM store into destDir,
// mirroring the live on-disk layout: the unified DB checkpoints straight into
// destDir; per-type sub-DBs checkpoint into destDir/<type> just as they live
// under the EVM dir. Satisfies types.Checkpointable.
func (s *EVMStateStore) Checkpoint(destDir string) error {
	checkpointDB := func(db types.StateStore, dest string) error {
		cp, ok := db.(types.Checkpointable)
		if !ok {
			return fmt.Errorf("EVM state store backend %T does not support checkpoints", db)
		}
		return cp.Checkpoint(dest)
	}

	if !s.separateDBs {
		db := s.primaryDB()
		if db == nil {
			return nil
		}
		return checkpointDB(db, destDir)
	}

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return fmt.Errorf("create EVM checkpoint dir %q: %w", destDir, err)
	}
	for _, storeType := range AllEVMStoreTypes() {
		if err := checkpointDB(s.subDBs[storeType], filepath.Join(destDir, StoreTypeName(storeType))); err != nil {
			return err
		}
	}
	return nil
}

func (s *EVMStateStore) SupportsCheckpoint() bool {
	for _, db := range s.managedDBs {
		if _, ok := db.(types.Checkpointable); !ok {
			return false
		}
	}
	return len(s.managedDBs) > 0
}

// ScheduleCheckpoint implements types.CheckpointScheduler over however many
// sub-DBs this store spans, into the same layout Checkpoint produces.
//
// Each sub-DB is barriered independently, which is the whole reason this cannot
// be a plain Checkpoint: a block that touches only storage keys is enqueued only
// on the storage DB, so the others never see that version and no amount of
// waiting would tell them it had passed. A barrier per queue pins each sub-DB at
// the last version it was given, and together those are exactly the state as of
// the caller's version.
func (s *EVMStateStore) ScheduleCheckpoint(destDir string, done func(error)) {
	if !s.separateDBs {
		db := s.primaryDB()
		if db == nil {
			done(nil)
			return
		}
		types.ScheduleCheckpoint(db, destDir, done)
		return
	}

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		done(fmt.Errorf("create EVM checkpoint dir %q: %w", destDir, err))
		return
	}

	storeTypes := AllEVMStoreTypes()
	var (
		mu        sync.Mutex
		remaining = len(storeTypes)
		firstErr  error
	)
	// Set up before scheduling: a sub-DB without an apply queue reports back
	// from inside ScheduleCheckpoint, before the loop has finished.
	for _, storeType := range storeTypes {
		dest := filepath.Join(destDir, StoreTypeName(storeType))
		name := StoreTypeName(storeType)
		types.ScheduleCheckpoint(s.subDBs[storeType], dest, func(err error) {
			mu.Lock()
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("checkpoint EVM sub-DB %s: %w", name, err)
			}
			remaining--
			last, outcome := remaining == 0, firstErr
			mu.Unlock()
			if last {
				done(outcome)
			}
		})
	}
}

func (s *EVMStateStore) SuspendChangelogPruning() {
	for _, db := range s.managedDBs {
		if pauser, ok := db.(types.ChangelogPrunePauser); ok {
			pauser.SuspendChangelogPruning()
		}
	}
}

func (s *EVMStateStore) ResumeChangelogPruning() {
	for i := len(s.managedDBs) - 1; i >= 0; i-- {
		if pauser, ok := s.managedDBs[i].(types.ChangelogPrunePauser); ok {
			pauser.ResumeChangelogPruning()
		}
	}
}

// WaitForPendingWrites drains every managed DB's async apply queue when the
// backend exposes a barrier; no-op otherwise.
func (s *EVMStateStore) WaitForPendingWrites() {
	for _, db := range s.managedDBs {
		if w, ok := db.(interface{ WaitForPendingWrites() }); ok {
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
