package sc

import (
	"fmt"
	"math"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

var _ types.Committer = (*CommitStore)(nil)

type CommitStore struct {
	logger logger.Logger
	db     *memiavl.DB
	opts   memiavl.Options

	// WAL for changelog persistence (owned by CommitStore)
	wal wal.GenericWAL[proto.ChangelogEntry]

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

// createWAL creates a new WAL instance for changelog persistence and replay.
func (cs *CommitStore) createWAL() (wal.GenericWAL[proto.ChangelogEntry], error) {
	return wal.NewWAL(
		func(e proto.ChangelogEntry) ([]byte, error) { return e.Marshal() },
		func(data []byte) (proto.ChangelogEntry, error) {
			var e proto.ChangelogEntry
			err := e.Unmarshal(data)
			return e, err
		},
		cs.logger,
		utils.GetChangelogPath(cs.opts.Dir),
		wal.Config{
			DisableFsync:    true,
			ZeroCopy:        true,
			WriteBufferSize: cs.opts.AsyncCommitBuffer,
		},
	)
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
	// Note: we reuse the existing WAL for rollback - memiavl will truncate it
	if cs.wal == nil {
		wal, err := cs.createWAL()
		if err != nil {
			return fmt.Errorf("failed to create WAL: %w", err)
		}
		cs.wal = wal
	}

	options := cs.opts
	options.LoadForOverwriting = true
	options.WAL = cs.wal

	db, err := memiavl.OpenDB(cs.logger, targetVersion, options)
	if err != nil {
		return err
	}
	cs.db = db
	return nil
}

// LoadVersion loads the specified version of the database.
// If copyExisting is true, creates a read-only copy for querying.
func (cs *CommitStore) LoadVersion(targetVersion int64, copyExisting bool) (types.Committer, error) {
	cs.logger.Info(fmt.Sprintf("SeiDB load target memIAVL version %d, copyExisting = %v\n", targetVersion, copyExisting))

	if copyExisting {
		// Create a read-only copy with its own WAL for replay
		newCS := &CommitStore{
			logger: cs.logger,
			opts:   cs.opts,
		}
		newCS.opts.ReadOnly = true
		newCS.opts.CreateIfMissing = false

		// WAL is needed for replay even in read-only mode
		wal, err := newCS.createWAL()
		if err != nil {
			return nil, fmt.Errorf("failed to create WAL: %w", err)
		}
		newCS.wal = wal
		newCS.opts.WAL = wal

		db, err := memiavl.OpenDB(cs.logger, targetVersion, newCS.opts)
		if err != nil {
			_ = wal.Close()
			return nil, err
		}
		newCS.db = db
		return newCS, nil
	}

	// Close existing resources
	if cs.db != nil {
		_ = cs.db.Close()
	}
	if cs.wal != nil {
		_ = cs.wal.Close()
		cs.wal = nil
	}

	// Create WAL for changelog persistence and replay
	wal, err := cs.createWAL()
	if err != nil {
		return nil, fmt.Errorf("failed to create WAL: %w", err)
	}

	// Pass WAL to memiavl via options
	opts := cs.opts
	opts.WAL = wal

	db, err := memiavl.OpenDB(cs.logger, targetVersion, opts)
	if err != nil {
		_ = wal.Close()
		return nil, err
	}

	cs.db = db
	cs.wal = wal
	return cs, nil
}

func (cs *CommitStore) Commit() (int64, error) {
	// Get the next version that will be committed
	nextVersion := cs.db.WorkingCommitInfo().Version

	// Write to WAL first (ensures durability before tree commit)
	if cs.wal != nil {
		cs.pendingLogEntry.Version = nextVersion
		if err := cs.wal.Write(cs.pendingLogEntry); err != nil {
			return 0, fmt.Errorf("failed to write to WAL: %w", err)
		}
		// Check for async write errors
		if err := cs.wal.CheckError(); err != nil {
			return 0, fmt.Errorf("WAL async write error: %w", err)
		}
	}

	// Clear pending entry
	cs.pendingLogEntry = proto.ChangelogEntry{}

	// Now commit to the tree
	version, err := cs.db.Commit()
	if err != nil {
		return 0, err
	}

	// Try to truncate WAL after commit (non-blocking, errors are logged)
	cs.tryTruncateWAL()

	return version, nil
}

// tryTruncateWAL checks if WAL can be truncated based on earliest snapshot version.
// This is safe because we only need WAL entries for versions newer than the earliest snapshot.
// Called after each commit to keep WAL size bounded.
func (cs *CommitStore) tryTruncateWAL() {
	if cs.wal == nil {
		return
	}

	// Get WAL's first index
	firstWALIndex, err := cs.wal.FirstOffset()
	if err != nil {
		cs.logger.Error("failed to get WAL first offset", "err", err)
		return
	}
	if firstWALIndex == 0 {
		return // empty WAL, nothing to truncate
	}

	// Get earliest snapshot version
	earliestSnapshotVersion, err := cs.GetEarliestVersion()
	if err != nil {
		// This can happen if no snapshots exist yet, which is normal
		return
	}

	// Compute WAL's earliest version using delta
	// delta = firstVersion - firstIndex, so firstVersion = firstIndex + delta
	walDelta := cs.db.GetWALIndexDelta()
	walEarliestVersion := int64(firstWALIndex) + walDelta

	// If WAL's earliest version is less than snapshot's earliest version,
	// we can safely truncate those WAL entries
	if walEarliestVersion < earliestSnapshotVersion {
		// Truncate WAL entries with version < earliestSnapshotVersion
		// WAL index for earliestSnapshotVersion = earliestSnapshotVersion - delta
		truncateIndex := earliestSnapshotVersion - walDelta
		if truncateIndex > int64(firstWALIndex) {
			if err := cs.wal.TruncateBefore(uint64(truncateIndex)); err != nil {
				cs.logger.Error("failed to truncate WAL", "err", err, "truncateIndex", truncateIndex)
			} else {
				cs.logger.Debug("truncated WAL", "beforeIndex", truncateIndex, "earliestSnapshotVersion", earliestSnapshotVersion)
			}
		}
	}
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
	var errs []error

	// Close DB first (it may still reference WAL)
	if cs.db != nil {
		errs = append(errs, cs.db.Close())
		cs.db = nil
	}

	// Then close WAL
	if cs.wal != nil {
		errs = append(errs, cs.wal.Close())
		cs.wal = nil
	}

	return errors.Join(errs...)
}
