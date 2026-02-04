package memiavl

import (
	"fmt"
	"math"

	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ types.Committer = (*CommitStore)(nil)

type CommitStore struct {
	logger  logger.Logger
	db      *DB
	opts    Options
	homeDir string
}

func NewCommitStore(homeDir string, logger logger.Logger, config Config) *CommitStore {
	commitDBPath := utils.GetCommitStorePath(homeDir)
	opts := Options{
		Config:          config, // Embed the config directly
		Dir:             commitDBPath,
		CreateIfMissing: true,
		ZeroCopy:        true,
	}
	commitStore := &CommitStore{
		logger:  logger,
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

	db, err := OpenDB(cs.logger, targetVersion, options)
	if err != nil {
		return err
	}
	cs.db = db
	return nil
}

// LoadVersion loads the specified version of the database.
// If copyExisting is true, creates a read-only copy for querying.
func (cs *CommitStore) LoadVersion(targetVersion int64, readOnly bool) (types.Committer, error) {
	cs.logger.Info(fmt.Sprintf("SeiDB load target memIAVL version %d, readOnly = %v\n", targetVersion, readOnly))

	if readOnly {
		// Create a read-only copy via NewCommitStore.
		newCS := NewCommitStore(cs.homeDir, cs.logger, cs.opts.Config)
		newCS.opts = cs.opts
		newCS.opts.ReadOnly = true
		newCS.opts.CreateIfMissing = false

		db, err := OpenDB(cs.logger, targetVersion, newCS.opts)
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
	db, err := OpenDB(cs.logger, targetVersion, opts)
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
	return cs.db.TreeByName(name)
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
