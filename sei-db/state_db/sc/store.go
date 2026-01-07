package sc

import (
	"fmt"
	"math"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ types.Committer = (*CommitStore)(nil)

type CommitStore struct {
	logger logger.Logger
	db     *memiavl.DB
	opts   memiavl.Options

	// pending changes to be written to WAL on next Commit
	pendingLogEntry proto.ChangelogEntry
}

func NewCommitStore(homeDir string, logger logger.Logger, config config.StateCommitConfig) *CommitStore {
	scDir := homeDir
	if config.Directory != "" {
		scDir = config.Directory
	}
	opts := memiavl.Options{
		Dir:                              utils.GetCommitStorePath(scDir),
		ZeroCopy:                         config.ZeroCopy,
		AsyncCommitBuffer:                config.AsyncCommitBuffer,
		SnapshotInterval:                 config.SnapshotInterval,
		SnapshotKeepRecent:               config.SnapshotKeepRecent,
		SnapshotMinTimeInterval:          time.Duration(config.SnapshotMinTimeInterval) * time.Second,
		SnapshotWriterLimit:              config.SnapshotWriterLimit,
		PrefetchThreshold:                config.SnapshotPrefetchThreshold,
		CreateIfMissing:                  true,
		OnlyAllowExportOnSnapshotVersion: config.OnlyAllowExportOnSnapshotVersion,
	}
	commitStore := &CommitStore{
		logger: logger,
		opts:   opts,
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
	options := cs.opts
	options.LoadForOverwriting = true
	if cs.db != nil {
		_ = cs.db.Close()
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
	cs.logger.Info(fmt.Sprintf("SeiDB load target memIAVL version %d, copyExisting = %v\n", targetVersion, copyExisting))
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
	}
	if cs.db != nil {
		_ = cs.db.Close()
	}
	db, err := memiavl.OpenDB(cs.logger, targetVersion, cs.opts)
	if err != nil {
		return nil, err
	}
	cs.db = db
	return cs, nil
}

func (cs *CommitStore) Commit() (int64, error) {
	// Get the next version that will be committed
	nextVersion := cs.db.WorkingCommitInfo().Version

	// Write to WAL first (ensures durability before tree commit)
	wal := cs.db.GetWAL()
	if wal != nil {
		cs.pendingLogEntry.Version = nextVersion
		if err := wal.Write(cs.pendingLogEntry); err != nil {
			return 0, fmt.Errorf("failed to write to WAL: %w", err)
		}
		// Check for async write errors
		if err := wal.CheckError(); err != nil {
			return 0, fmt.Errorf("WAL async write error: %w", err)
		}
	}

	// Clear pending entry
	cs.pendingLogEntry = proto.ChangelogEntry{}

	// Now commit to the tree
	return cs.db.Commit()
}

func (cs *CommitStore) Version() int64 {
	return cs.db.Version()
}

func (cs *CommitStore) GetLatestVersion() (int64, error) {
	return memiavl.GetLatestVersion(cs.opts.Dir)
}

func (cs *CommitStore) GetEarliestVersion() (int64, error) {
	return memiavl.GetEarliestVersion(cs.opts.Dir)
}

func (cs *CommitStore) ApplyChangeSets(changesets []*proto.NamedChangeSet) error {
	if len(changesets) == 0 {
		return nil
	}

	// Store in pending log entry for WAL
	cs.pendingLogEntry.Changesets = changesets

	// Apply to tree
	return cs.db.ApplyChangeSets(changesets)
}

func (cs *CommitStore) ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error {
	if len(upgrades) == 0 {
		return nil
	}

	// Store in pending log entry for WAL
	cs.pendingLogEntry.Upgrades = append(cs.pendingLogEntry.Upgrades, upgrades...)

	// Apply to tree
	return cs.db.ApplyUpgrades(upgrades)
}

func (cs *CommitStore) WorkingCommitInfo() *proto.CommitInfo {
	return cs.db.WorkingCommitInfo()
}

func (cs *CommitStore) LastCommitInfo() *proto.CommitInfo {
	return cs.db.LastCommitInfo()
}

func (cs *CommitStore) GetModuleByName(name string) types.ModuleStore {
	return cs.db.TreeByName(name)
}

func (cs *CommitStore) Exporter(version int64) (types.Exporter, error) {
	if version < 0 || version > math.MaxUint32 {
		return nil, fmt.Errorf("version %d out of range", version)
	}
	return memiavl.NewMultiTreeExporter(cs.opts.Dir, uint32(version), cs.opts.OnlyAllowExportOnSnapshotVersion)
}

func (cs *CommitStore) Importer(version int64) (types.Importer, error) {
	if version < 0 || version > math.MaxUint32 {
		return nil, fmt.Errorf("version %d out of range", version)
	}
	return memiavl.NewMultiTreeImporter(cs.opts.Dir, uint64(version))
}

func (cs *CommitStore) Close() error {
	if cs.db != nil {
		err := cs.db.Close()
		cs.db = nil
		return err
	}
	return nil
}
