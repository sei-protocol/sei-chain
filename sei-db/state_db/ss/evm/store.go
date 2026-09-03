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
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "state-db", "ss", "evm")

var _ types.StateStore = (*EVMStateStore)(nil)
var _ types.ContextIteratorStore = (*EVMStateStore)(nil)

// EVMStateStore manages either a single MVCC DB for all EVM data or one DB per
// EVM sub-type, depending on config. In both modes, the logical store key and
// key encoding remain unchanged.
type EVMStateStore struct {
	subDBs      map[EVMStoreType]types.StateStore
	managedDBs  []types.StateStore
	dir         string
	ssConfig    config.StateStoreConfig
	separateDBs bool
	snapshotMgr *sssnapshot.Manager

	// checkpoint holds the schedule this store takes its snapshot heights from, and tracks the
	// snapshots in flight so Close can wait for them.
	checkpoint checkpointState

	externalPruning bool
}

// NewEVMStateStore opens either a single unified MVCC DB for all EVM state
// or one MVCC DB per EVM sub-type.
func NewEVMStateStore(dir string, ssConfig config.StateStoreConfig) (*EVMStateStore, error) {
	store := &EVMStateStore{
		subDBs:          make(map[EVMStoreType]types.StateStore, NumEVMStoreTypes),
		dir:             dir,
		ssConfig:        ssConfig,
		separateDBs:     ssConfig.SeparateEVMSubDBs,
		externalPruning: ssConfig.ExternalPruning,
	}
	if err := store.openDBs(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func (s *EVMStateStore) openDBs() error {
	opener := backend.ResolveBackend(s.ssConfig.Backend)
	s.subDBs = make(map[EVMStoreType]types.StateStore, NumEVMStoreTypes)
	s.managedDBs = nil

	if s.separateDBs {
		for _, storeType := range AllEVMStoreTypes() {
			dbDir := filepath.Join(s.dir, StoreTypeName(storeType))
			db, err := opener(dbDir, subDBConfig(s.ssConfig, dbDir))
			if err != nil {
				return fmt.Errorf("failed to open EVM MVCC DB for %s: %w", StoreTypeName(storeType), err)
			}
			s.subDBs[storeType] = db
			s.managedDBs = append(s.managedDBs, db)
		}
		return nil
	}

	db, err := opener(s.dir, subDBConfig(s.ssConfig, s.dir))
	if err != nil {
		return fmt.Errorf("failed to open unified EVM MVCC DB: %w", err)
	}
	s.managedDBs = append(s.managedDBs, db)
	for _, storeType := range AllEVMStoreTypes() {
		s.subDBs[storeType] = db
	}
	return nil
}

func (s *EVMStateStore) closeDBs() error {
	var lastErr error
	for _, db := range s.managedDBs {
		if err := db.Close(); err != nil {
			lastErr = err
		}
	}
	s.managedDBs = nil
	s.subDBs = make(map[EVMStoreType]types.StateStore, NumEVMStoreTypes)
	return lastErr
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

// CommitBlock records a committed block and offers its version to the checkpoint schedule. It is the
// commit path's entry point: the apply methods are raw writes and take no snapshot.
func (s *EVMStateStore) CommitBlock(version int64, changesets []*proto.NamedChangeSet) error {
	evmChangesets := filterEVMChangesets(changesets)

	// Separate-DB mode parses and groups the block's pairs exactly once: the grouping both routes the
	// apply and names the sub-DBs the block never reached. A unified store routes nothing by sub-type —
	// its one database takes the whole changeset and stamps the version marker in the same batch, so
	// only a block that wrote nothing leaves that marker to be moved on its own.
	var unwritten []types.StateStore
	switch {
	case s.separateDBs:
		grouped := s.groupBySubType(evmChangesets)
		if err := s.ApplyChangesetAsyncGrouped(version, grouped); err != nil {
			return err
		}
		unwritten = s.dbsWithoutWrites(grouped)
	case len(evmChangesets) > 0:
		if err := s.ApplyChangesetAsync(version, evmChangesets); err != nil {
			return err
		}
	default:
		unwritten = s.managedDBs
	}

	if err := s.advanceUnwrittenHeads(version, unwritten); err != nil {
		return err
	}
	s.scheduleSnapshot(version)
	return nil
}

// advanceUnwrittenHeads moves each given database's version marker to version, at that database's own
// place in its write order.
//
// A database that took a batch is not among these: the batch carries the marker alongside the data.
// The rest would keep the marker of whichever block last routed to them, and the head is the minimum
// across all of them, so one left behind holds the head — and the GC floor taken from it — below the
// committed height. Going through the queue rather than around it keeps the marker from overtaking
// blocks still queued behind it, which stamp their own lower version as they drain.
func (s *EVMStateStore) advanceUnwrittenHeads(version int64, dbs []types.StateStore) error {
	for _, db := range dbs {
		barrier, ok := db.(types.DrainBarrier)
		if !ok {
			if err := db.SetLatestVersion(version); err != nil {
				return err
			}
			continue
		}
		// Runs on the database's apply goroutine, which a panic would take down with it.
		barrier.ScheduleAtDrain(func() {
			if err := db.SetLatestVersion(version); err != nil {
				logger.Error("failed to advance EVM state store version marker", "version", version, "err", err)
			}
		})
	}
	return nil
}

// dbsWithoutWrites returns the sub-DBs that grouped routes no keys to.
//
// Separate-DB mode only. A unified store keys every sub-type to the same database, so it would report
// that one database once per sub-type the block missed.
func (s *EVMStateStore) dbsWithoutWrites(grouped map[EVMStoreType][]*proto.KVPair) []types.StateStore {
	unwritten := make([]types.StateStore, 0, len(s.managedDBs))
	for storeType, db := range s.subDBs {
		if len(grouped[storeType]) == 0 {
			unwritten = append(unwritten, db)
		}
	}
	return unwritten
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

	return s.ApplyChangesetAsyncGrouped(version, s.groupBySubType(changesets))
}

// ApplyChangesetAsyncGrouped applies pairs already grouped by sub-type, skipping the filtering and
// grouping ApplyChangesetAsync does for itself. It is for callers that already hold a grouping and
// would otherwise pay to parse every key twice — CommitBlock builds one to find the sub-DBs a block
// did not write, and applies through here.
//
// Separate-DB mode only. A unified store keys every sub-type to the same database, so applying a
// grouping to one would split a block into a batch per sub-type.
func (s *EVMStateStore) ApplyChangesetAsyncGrouped(
	version int64,
	grouped map[EVMStoreType][]*proto.KVPair,
) error {
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
	// A snapshot being published reads and stamps these databases, so it has to finish before they
	// close rather than race the shutdown.
	s.stopCheckpoints()
	return s.closeDBs()
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
