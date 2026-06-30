package rootmulti

import (
	"fmt"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// App-level hash logger categories owned by rootmulti. No backend can compute these:
//   - appHashType:        the root application hash returned to consensus.
//   - blockHashType:      the Tendermint block hash, supplied by baseapp via SetNextBlockHash.
//   - resultHashType:     the result hash (merkle root over the block's deterministic tx results),
//     supplied by baseapp via SetNextResultHash. Equals the next block's header.LastResultsHash.
//   - memIAVLRootHashType: the simple-merkle root over memIAVL's per-module hashes (requires the cosmos
//     hashing utilities, which sei-db cannot reach), computed here via convertCommitInfo(...).Hash().
const (
	appHashType         = "appHash"
	blockHashType       = "blockHash"
	resultHashType      = "resultHash"
	memIAVLRootHashType = "memIAVL/root"
)

// hashReportingStore is the subset of the SC store that owns and reports its own hash categories. The
// composite commit store implements it. Type-asserting keeps these methods out of the broad
// sctypes.Committer interface. The caller registers the categories returned by HashCategories.
type hashReportingStore interface {
	HashCategories() []string
	RecordHashes(hashlog.HashLogger, uint64) error
	MemIAVLCommitInfo() *proto.CommitInfo
}

// SetNextBlockHash stashes the Tendermint block hash for the block currently being committed. baseapp
// calls it just before Commit; rootmulti records it (under the committed version) inside Commit so every
// hash for a block shares one block number. No-op when hash logging is disabled.
func (rs *Store) SetNextBlockHash(blockHash []byte) {
	if rs.hashLoggerDisabled {
		return
	}
	rs.nextBlockHash = append([]byte(nil), blockHash...)
}

// SetNextResultHash stashes the result hash (merkle root over the block's deterministic tx results)
// for the block currently being committed. baseapp calls it just before Commit; rootmulti records it
// (under the committed version) inside Commit so every hash for a block shares one block number.
// No-op when hash logging is disabled.
func (rs *Store) SetNextResultHash(resultHash []byte) {
	if rs.hashLoggerDisabled {
		return
	}
	rs.nextResultHash = append([]byte(nil), resultHash...)
}

// hashLogDir returns the directory hash log files are written to, defaulting to a "hash.log" directory
// under the state-commit store's data directory (sibling of committer.db / receipt.db). The ".log"
// suffix mirrors the data/ naming convention (.db, .wal); the files inside keep the .hlog format.
func (rs *Store) hashLogDir() string {
	if rs.hashLoggerConfig.Directory != "" {
		return rs.hashLoggerConfig.Directory
	}
	return filepath.Join(rs.scDir, "data", "hash.log")
}

// desiredHashCategories computes the full caller-reported category set for the current backend state:
// the app-level categories plus whatever the live backends report (and memIAVL/root when memIAVL is
// present). The backend set is dynamic (memIAVL departs and flatKV arrives during migration), so this
// is recomputed each block and used to detect when the logger's column set must change.
func (rs *Store) desiredHashCategories() map[string]struct{} {
	categories := map[string]struct{}{
		appHashType:    {},
		blockHashType:  {},
		resultHashType: {},
	}
	if h, ok := rs.scStore.(hashReportingStore); ok {
		for _, category := range h.HashCategories() {
			categories[category] = struct{}{}
		}
		if h.MemIAVLCommitInfo() != nil {
			categories[memIAVLRootHashType] = struct{}{}
		}
	}
	return categories
}

// openHashLogger constructs the logger once. It starts with no caller columns (just the changeset
// column); syncHashCategories then registers the live categories, which the logger handles as runtime
// column changes (each new column rotates to a fresh file, but the empty initial files are dropped and
// their indexes reused, so the first file with data starts at index 0).
func (rs *Store) openHashLogger() error {
	loggerVersion := rs.hashLoggerConfig.Version
	if loggerVersion == "" {
		loggerVersion = "unknown"
	}
	cfg := hashlog.DefaultHashLoggerConfig(rs.hashLogDir(), loggerVersion)
	// Propagate the operator-configured retention tunables verbatim. A configured 0 must reach the logger
	// (where it disables that dimension); the old `if > 0` guards swallowed it. Defaults are applied at
	// config construction (config.DefaultHashLoggerConfig), so these always carry a meaningful value.
	cfg.BlocksToRetain = rs.hashLoggerConfig.BlocksToRetain
	cfg.TargetFileSize = rs.hashLoggerConfig.TargetFileSize
	cfg.MaxDiskSize = rs.hashLoggerConfig.MaxDiskSize

	hl, err := hashlog.NewHashLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create hash logger: %w", err)
	}
	rs.hashLogger = hl
	return nil
}

// syncHashCategories brings the logger's column set in line with the desired set for the current backend
// state, registering newly-present categories and unregistering departed ones. The logger rotates files
// as needed on each change; a no-op when the set is unchanged (the common case after the first block).
func (rs *Store) syncHashCategories() {
	desired := rs.desiredHashCategories()
	for category := range rs.hashCategories {
		if _, ok := desired[category]; !ok {
			if err := rs.hashLogger.UnregisterHashType(category); err != nil {
				logger.Error("failed to unregister hash category", "category", category, "err", err)
			}
		}
	}
	for category := range desired {
		if _, ok := rs.hashCategories[category]; !ok {
			if err := rs.hashLogger.RegisterHashType(category); err != nil {
				logger.Error("failed to register hash category", "category", category, "err", err)
			}
		}
	}
	rs.hashCategories = desired
}

// HashLoggingEnabled reports whether the store is actively recording hashes (config-enabled and not
// disabled by a prior fatal error). baseapp uses it to skip per-block hash computation when off.
func (rs *Store) HashLoggingEnabled() bool {
	return !rs.hashLoggerDisabled
}

// disableHashLogger turns hash logging off after a fatal error, closing the logger if it is open.
func (rs *Store) disableHashLogger() {
	rs.hashLoggerDisabled = true
	if rs.hashLogger != nil {
		_ = rs.hashLogger.Close()
		rs.hashLogger = nil
	}
}

// recordBlockHashes reports every hash for the just-committed block at the given version. It opens the
// logger on first use and keeps its column set in sync with the live backends. On open failure it
// disables hash logging rather than disrupting commit. Must be called with rs.mtx held (from Commit).
func (rs *Store) recordBlockHashes(version int64) {
	if rs.hashLoggerDisabled {
		return
	}

	if rs.hashLogger == nil {
		if err := rs.openHashLogger(); err != nil {
			logger.Error("failed to open hash logger; disabling hash logging", "err", err)
			rs.disableHashLogger()
			return
		}
	}
	rs.syncHashCategories()

	blockNumber := uint64(version) //nolint:gosec // commit versions are non-negative

	// Changeset: the aggregate of all modules' changes for this block, captured (sorted) in flush.
	rs.hashLogger.ReportChangeset(blockNumber, rs.blockChangeSets)
	rs.blockChangeSets = nil

	// appHash: the root application hash returned to consensus.
	if rs.lastCommitInfo != nil {
		appHash := append([]byte(nil), rs.lastCommitInfo.CommitID().Hash...)
		if err := rs.hashLogger.ReportHash(blockNumber, appHashType, appHash); err != nil {
			logger.Error("failed to report app hash", "err", err)
		}
	}

	// blockHash: supplied by baseapp before Commit. nil if it was not provided for this block.
	if err := rs.hashLogger.ReportHash(blockNumber, blockHashType, rs.nextBlockHash); err != nil {
		logger.Error("failed to report block hash", "err", err)
	}
	rs.nextBlockHash = nil

	// resultHash: supplied by baseapp before Commit. nil if it was not provided for this block.
	if err := rs.hashLogger.ReportHash(blockNumber, resultHashType, rs.nextResultHash); err != nil {
		logger.Error("failed to report result hash", "err", err)
	}
	rs.nextResultHash = nil

	if h, ok := rs.scStore.(hashReportingStore); ok {
		if memInfo := h.MemIAVLCommitInfo(); memInfo != nil {
			root := convertCommitInfo(memInfo).Hash()
			if err := rs.hashLogger.ReportHash(blockNumber, memIAVLRootHashType, root); err != nil {
				logger.Error("failed to report memIAVL root hash", "err", err)
			}
		}
		if err := h.RecordHashes(rs.hashLogger, blockNumber); err != nil {
			logger.Error("failed to record backend hashes", "err", err)
		}
	}
}
