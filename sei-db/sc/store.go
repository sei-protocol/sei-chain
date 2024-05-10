package sc

import (
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/sc/memiavl"
	"github.com/sei-protocol/sei-db/sc/types"
)

var _ types.Committer = (*CommitStore)(nil)

type CommitStore struct {
	logger logger.Logger
	db     *memiavl.DB
	opts   memiavl.Options
}

func NewCommitStore(homeDir string, logger logger.Logger, config config.StateCommitConfig) *CommitStore {
	scDir := homeDir
	if config.Directory != "" {
		scDir = config.Directory
	}
	opts := memiavl.Options{
		Dir:                 utils.GetCommitStorePath(scDir),
		ZeroCopy:            config.ZeroCopy,
		AsyncCommitBuffer:   config.AsyncCommitBuffer,
		SnapshotInterval:    config.SnapshotInterval,
		SnapshotKeepRecent:  config.SnapshotKeepRecent,
		SnapshotWriterLimit: config.SnapshotWriterLimit,
		CacheSize:           config.CacheSize,
		CreateIfMissing:     true,
	}
	commitStore := &CommitStore{
		logger: logger,
		opts:   opts,
	}
	return commitStore
}

func (cs *CommitStore) Initialize(initialStores []string) error {
	options := cs.opts
	options.InitialStores = initialStores
	db, err := memiavl.OpenDB(cs.logger, 0, options)
	if err != nil {
		return err
	}
	cs.db = db
	return nil
}

func (cs *CommitStore) SetInitialVersion(initialVersion int64) error {
	return cs.db.SetInitialVersion(initialVersion)
}

func (cs *CommitStore) Rollback(targetVersion int64) error {
	options := cs.opts
	options.LoadForOverwriting = true
	if cs.db != nil {
		cs.db.Close()
	}
	db, err := memiavl.OpenDB(cs.logger, targetVersion, options)
	if err != nil {
		return err
	}
	cs.db = db
	return nil
}

// copyExisting is for creating new memiavl object given existing folder
func (cs *CommitStore) LoadVersion(targetVersion int64, copyExisting bool) (types.Committer, error) {
	if copyExisting {
		opts := cs.opts
		opts.ReadOnly = copyExisting
		opts.CreateIfMissing = false
		db, err := memiavl.OpenDB(cs.logger, targetVersion, opts)
		if err != nil {
			return nil, err
		}
		return &CommitStore{
			logger: cs.logger,
			db:     db,
			opts:   opts,
		}, nil
	} else {
		if cs.db != nil {
			cs.db.Close()
		}
		db, err := memiavl.OpenDB(cs.logger, targetVersion, cs.opts)
		if err != nil {
			return nil, err
		}
		cs.db = db
	}
	return cs, nil
}

func (cs *CommitStore) Commit() (int64, error) {
	return cs.db.Commit()
}

func (cs *CommitStore) Version() int64 {
	return cs.db.Version()
}

func (cs *CommitStore) GetLatestVersion() (int64, error) {
	return memiavl.GetLatestVersion(cs.opts.Dir)
}

func (cs *CommitStore) ApplyChangeSets(changesets []*proto.NamedChangeSet) error {
	return cs.db.ApplyChangeSets(changesets)
}

func (cs *CommitStore) ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error {
	return cs.db.ApplyUpgrades(upgrades)
}

func (cs *CommitStore) WorkingCommitInfo() *proto.CommitInfo {
	return cs.db.WorkingCommitInfo()
}

func (cs *CommitStore) LastCommitInfo() *proto.CommitInfo {
	return cs.db.LastCommitInfo()
}

func (cs *CommitStore) GetTreeByName(name string) types.Tree {
	return cs.db.TreeByName(name)
}

func (cs *CommitStore) Exporter(version int64) (types.Exporter, error) {
	exporter, err := memiavl.NewMultiTreeExporter(cs.opts.Dir, uint32(version), true)
	if err != nil {
		return nil, err
	}
	return exporter, nil
}

func (cs *CommitStore) Importer(version int64) (types.Importer, error) {
	treeImporter, err := memiavl.NewMultiTreeImporter(cs.opts.Dir, uint64(version))
	if err != nil {
		return nil, err
	}
	return treeImporter, nil
}

func (cs *CommitStore) Close() error {
	return cs.db.Close()
}
