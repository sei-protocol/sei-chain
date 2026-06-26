package memiavl

import (
	"fmt"
	"math"

	ics23 "github.com/confio/ics23/go"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ types.Committer = (*CommitStore)(nil)

type CommitStore struct {
	db      *DB
	opts    Options
	homeDir string
}

func NewCommitStore(homeDir string, config Config) *CommitStore {
	commitDBPath := utils.GetCosmosSCStorePath(homeDir)
	opts := Options{
		Config:          config, // Embed the config directly
		Dir:             commitDBPath,
		CreateIfMissing: true,
		ZeroCopy:        false, // Disable zero copy to avoid segfault during historical read
	}
	commitStore := &CommitStore{
		opts:    opts,
		homeDir: homeDir,
	}
	return commitStore
}

func (cs *CommitStore) Initialize(initialStores []string) error {
	cs.opts.InitialStores = initialStores
	return nil
}

func (cs *CommitStore) SetInitialVersion(initialVersion int64) error {
	return cs.db.SetInitialVersion(initialVersion)
}

func (cs *CommitStore) Rollback(targetVersion int64) error {
	// Close existing resources
	if cs.db != nil {
		_ = cs.db.Close()
	}

	options := cs.opts
	options.LoadForOverwriting = true

	db, err := OpenDB(targetVersion, options)
	if err != nil {
		return err
	}
	cs.db = db
	return nil
}

// LoadVersion loads the specified version of the database.
// If copyExisting is true, creates a read-only copy for querying.
func (cs *CommitStore) LoadVersion(targetVersion int64, readOnly bool) (types.Committer, error) {
	logger.Info("SeiDB loading target memIAVL version", "version", targetVersion, "read-only", readOnly)

	if readOnly {
		// Create a read-only copy via NewCommitStore.
		newCS := NewCommitStore(cs.homeDir, cs.opts.Config)
		newCS.opts = cs.opts
		newCS.opts.ReadOnly = true
		newCS.opts.CreateIfMissing = false

		db, err := OpenDB(targetVersion, newCS.opts)
		if err != nil {
			return nil, err
		}
		newCS.db = db
		return newCS, nil
	}

	// Close existing resources
	if cs.db != nil {
		_ = cs.db.Close()
	}

	opts := cs.opts
	db, err := OpenDB(targetVersion, opts)
	if err != nil {
		return nil, err
	}

	cs.db = db
	return cs, nil
}

// SetWriteMode implements types.Committer. The memiavl commit store is a
// single-backend store; its write mode is fixed.
func (cs *CommitStore) SetWriteMode(types.WriteMode) error {
	return fmt.Errorf("memiavl commit store does not support runtime write-mode changes")
}

// SetMigrationBatchSize implements types.Committer. The memiavl commit
// store runs no migration of its own, so this is a no-op.
func (cs *CommitStore) SetMigrationBatchSize(int) error {
	return nil
}

// Copy returns an O(1) memiavl snapshot; COW nodes are shared with the live store.
func (cs *CommitStore) Copy() types.Committer {
	if cs == nil || cs.db == nil {
		return nil
	}
	return &CommitStore{
		db:      cs.db.Copy(),
		opts:    cs.opts,
		homeDir: cs.homeDir,
	}
}

// ReleaseSnapshotRefs releases refs held by a copied in-memory snapshot without
// closing DB-level resources shared with the live store.
func (cs *CommitStore) ReleaseSnapshotRefs() error {
	if cs == nil || cs.db == nil {
		return nil
	}
	err := cs.db.ReleaseSnapshotRefs()
	cs.db = nil
	return err
}

func (cs *CommitStore) Commit() (int64, error) {
	return cs.db.Commit()
}

func (cs *CommitStore) Version() int64 {
	return cs.db.Version()
}

// GetLatestVersion returns the highest version durably written to the
// changelog WAL on disk. Note that with AsyncCommitBuffer > 0,
// wal.Write returns before the entry is durable (see sei-db/wal/wal.go,
// "Do not wait for the write to be durable"), so this value can lag the
// in-memory MultiTree by one or more commits while async writes drain.
// Callers that need the just-committed version should use cs.Version()
// or cs.LastCommitInfo().Version, both of which read the in-memory
// tree. The lag is harmless for current production callers
// (rootmulti.NewStore and rootmulti.LastCommitID), which only invoke
// GetLatestVersion before LoadVersion has opened the DB; in that
// pre-load state nothing is in memory anyway.
func (cs *CommitStore) GetLatestVersion() (int64, error) {
	return GetLatestVersion(cs.opts.Dir)
}

func (cs *CommitStore) ApplyChangeSets(changesets []*proto.NamedChangeSet) error {
	if len(changesets) == 0 {
		return nil
	}

	// Apply to tree
	return cs.db.ApplyChangeSets(changesets)
}

func (cs *CommitStore) ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error {
	if len(upgrades) == 0 {
		return nil
	}

	// Apply to tree
	return cs.db.ApplyUpgrades(upgrades)
}

func (cs *CommitStore) WorkingCommitInfo() *proto.CommitInfo {
	return cs.db.WorkingCommitInfo()
}

func (cs *CommitStore) LastCommitInfo() *proto.CommitInfo {
	return cs.db.LastCommitInfo()
}

func (cs *CommitStore) GetChildStoreByName(name string) types.CommitKVStore {
	// The underlying DB is opened lazily via LoadVersion / Rollback. Reads can
	// arrive before that happens (for example, the mempool reactor invokes
	// CheckTx during state-sync while the snapshot is still being applied),
	// so a typed nil return must be safe.
	if cs == nil || cs.db == nil {
		return nil
	}
	tree := cs.db.TreeByName(name)
	if tree == nil {
		// Return an explicitly nil interface (not a typed-nil *Tree wrapped in an
		// interface), so callers can compare the result against nil.
		return nil
	}
	return tree
}

// IsLoaded reports whether the underlying memiavl DB has been opened. It is
// safe to call on a nil receiver. Callers built on top of CommitStore use this
// to distinguish "store has no committed data yet" (during state-sync, before
// LoadVersion) from "store name is misregistered" (a real config error).
func (cs *CommitStore) IsLoaded() bool {
	return cs != nil && cs.db != nil
}

func (cs *CommitStore) Exporter(version int64) (types.Exporter, error) {
	if version < 0 || version > math.MaxUint32 {
		return nil, fmt.Errorf("version %d out of range", version)
	}
	return NewMultiTreeExporter(cs.opts.Dir, uint32(version), cs.opts.OnlyAllowExportOnSnapshotVersion)
}

func (cs *CommitStore) Importer(version int64) (types.Importer, error) {
	if version < 0 || version > math.MaxUint32 {
		return nil, fmt.Errorf("version %d out of range", version)
	}
	return NewMultiTreeImporter(cs.opts.Dir, uint64(version))
}

func (cs *CommitStore) Close() error {
	var errs []error

	if cs.db != nil {
		errs = append(errs, cs.db.Close())
		cs.db = nil
	}

	return errors.Join(errs...)
}

func (cs *CommitStore) Get(store string, key []byte) (value []byte, ok bool, err error) {
	if store == "" {
		return nil, false, fmt.Errorf("store name cannot be empty")
	}
	if key == nil {
		return nil, false, fmt.Errorf("key cannot be nil")
	}

	childStore := cs.GetChildStoreByName(store)
	if childStore == nil {
		return nil, false, nil
	}

	value = childStore.Get(key)
	if value == nil {
		return nil, false, nil
	}
	return value, true, nil
}

func (cs *CommitStore) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	if store == "" {
		return nil, fmt.Errorf("store name cannot be empty")
	}
	if key == nil {
		return nil, fmt.Errorf("key cannot be nil")
	}

	childStore := cs.GetChildStoreByName(store)
	if childStore == nil {
		return nil, nil
	}

	return childStore.GetProof(key), nil
}

func (cs *CommitStore) Has(store string, key []byte) (bool, error) {
	_, ok, err := cs.Get(store, key)
	if err != nil {
		return false, fmt.Errorf("failed to get value: %w", err)
	}
	return ok, nil
}

func (cs *CommitStore) Iterator(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
	if store == "" {
		return nil, fmt.Errorf("store name cannot be empty")
	}

	childStore := cs.GetChildStoreByName(store)
	if childStore == nil {
		return nil, nil
	}

	return childStore.Iterator(start, end, ascending), nil

}

// Get the underlying memiavl DB.
func (cs *CommitStore) GetDB() *DB {
	return cs.db
}
