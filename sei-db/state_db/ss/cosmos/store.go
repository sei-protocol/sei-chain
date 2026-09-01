package cosmos

import (
	"context"
	"fmt"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	sssnapshot "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/snapshot"
)

// Compile-time check: CosmosStateStore implements db_engine.StateStore.
var _ types.StateStore = (*CosmosStateStore)(nil)
var _ types.ContextIteratorStore = (*CosmosStateStore)(nil)

// CosmosStateStore wraps a single StateStore (MVCC DB) and satisfies db_engine.StateStore.
// It is the SS-layer adapter for the main Cosmos state (all non-EVM modules).
type CosmosStateStore struct {
	db              types.StateStore
	snapshotMgr     *sssnapshot.Manager
	externalPruning bool
}

// NewCosmosStateStore wraps an existing StateStore as a CosmosStateStore.
func NewCosmosStateStore(db types.StateStore) *CosmosStateStore {
	return &CosmosStateStore{db: db}
}

func (s *CosmosStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	return s.db.Get(storeKey, version, key)
}

func (s *CosmosStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	return s.db.Has(storeKey, version, key)
}

func (s *CosmosStateStore) Iterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	return s.db.Iterator(storeKey, version, start, end)
}

func (s *CosmosStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	return s.db.ReverseIterator(storeKey, version, start, end)
}

func (s *CosmosStateStore) IteratorWithContext(ctx context.Context, storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	return types.IterateWithContext(s.db, ctx, storeKey, version, start, end, false)
}

func (s *CosmosStateStore) ReverseIteratorWithContext(ctx context.Context, storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	return types.IterateWithContext(s.db, ctx, storeKey, version, start, end, true)
}

func (s *CosmosStateStore) RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error) {
	return s.db.RawIterate(storeKey, fn)
}

func (s *CosmosStateStore) GetLatestVersion() int64 {
	return s.db.GetLatestVersion()
}

func (s *CosmosStateStore) SetLatestVersion(version int64) error {
	return s.db.SetLatestVersion(version)
}

func (s *CosmosStateStore) GetEarliestVersion() int64 {
	return s.db.GetEarliestVersion()
}

func (s *CosmosStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	return s.db.SetEarliestVersion(version, ignoreVersion)
}

func (s *CosmosStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	return s.db.ApplyChangesetSync(version, changesets)
}

func (s *CosmosStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	return s.db.ApplyChangesetAsync(version, changesets)
}

func (s *CosmosStateStore) Prune(version int64) error {
	return s.db.Prune(version)
}

func (s *CosmosStateStore) ExternalPruning() bool {
	return s.externalPruning
}

func (s *CosmosStateStore) SetExternalPruning(enabled bool) {
	s.externalPruning = enabled
}

func (s *CosmosStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	return s.db.Import(version, ch)
}

func (s *CosmosStateStore) Close() error {
	return s.db.Close()
}

func (s *CosmosStateStore) SupportsCheckpoint() bool {
	return sssnapshot.SupportsCheckpoint(s.db)
}

func (s *CosmosStateStore) ScheduleCheckpoint(destDir string, shouldRun func() bool, done func(error)) {
	sssnapshot.ScheduleCheckpoint(s.db, destDir, shouldRun, done)
}

func (s *CosmosStateStore) SetCheckpointVersion(destDir string, version int64) error {
	return sssnapshot.SetCheckpointVersion(s.db, destDir, version)
}

func (s *CosmosStateStore) StartSnapshots(
	root string,
	sourceDirs []string,
	ssConfig config.StateStoreConfig,
	floor *sssnapshot.Floor,
) error {
	manager, err := sssnapshot.Open(sssnapshot.Config{
		Name:            "cosmos",
		Root:            root,
		SourceDirs:      sourceDirs,
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

func (s *CosmosStateStore) Snapshots() *sssnapshot.Manager {
	return s.snapshotMgr
}

func (s *CosmosStateStore) WaitForPendingWrites() {
	if w, ok := s.db.(types.PendingWriteWaiter); ok {
		w.WaitForPendingWrites()
	}
}

func (s *CosmosStateStore) PruneWALBeforeVersion(version int64) error {
	if p, ok := s.db.(types.SnapshotWALPruner); ok {
		return p.PruneWALBeforeVersion(version)
	}
	return nil
}

// WALVersionsAfter fails rather than reporting an empty changelog when the
// engine keeps none: a caller asking what the changelog covers cannot act on
// "nothing" and "no such thing" as the same answer.
func (s *CosmosStateStore) WALVersionsAfter(version int64) (oldest int64, next int64, err error) {
	r, ok := s.db.(types.SnapshotWALReader)
	if !ok {
		return 0, 0, fmt.Errorf("%T does not keep a changelog WAL", s.db)
	}
	return r.WALVersionsAfter(version)
}
