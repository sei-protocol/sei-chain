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

func (cs *CommitStore) Initialize(initialStores []string) {
	cs.opts.InitialStores = initialStores
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

func (cs *CommitStore) Commit() (int64, error) {
	return cs.db.Commit()
}

func (cs *CommitStore) Version() int64 {
	return cs.db.Version()
}

func (cs *CommitStore) GetLatestVersion() (int64, error) {
	return GetLatestVersion(cs.opts.Dir)
}

func (cs *CommitStore) GetEarliestVersion() (int64, error) {
	return GetEarliestVersion(cs.opts.Dir)
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
	tree := cs.db.TreeByName(name)
	if tree == nil {
		// Return an explicitly nil interface (not a typed-nil *Tree wrapped in an
		// interface), so callers can compare the result against nil.
		return nil
	}
	return tree
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
	if start == nil {
		return nil, fmt.Errorf("start cannot be nil")
	}
	if end == nil {
		return nil, fmt.Errorf("end cannot be nil")
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
