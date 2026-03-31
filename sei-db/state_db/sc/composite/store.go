// Package composite provides a unified commit store that coordinates
// between Cosmos (memiavl) and EVM (flatkv) committers.
package composite

import (
	"context"
	"fmt"
	"math"

	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "state-db", "sc", "composite")

// EVMStoreName is the module name for the EVM store in memiavl.
const EVMStoreName = commonevm.EVMStoreKey

// EVMFlatKVStoreName is the module name used when exporting/importing
// EVM data from the FlatKV backend. Treated as a separate module in
// state-sync snapshots so that import routes data exclusively to FlatKV.
const EVMFlatKVStoreName = commonevm.EVMFlatKVStoreKey

// For backward compatibility purpose reuse current interface
var _ types.Committer = (*CompositeCommitStore)(nil)

// CompositeCommitStore manages multiple commit store backends (Cosmos/memiavl and FlatKV)
// and routes operations based on the configured migration strategy.
type CompositeCommitStore struct {

	// cosmosCommitter is the Cosmos (memiavl) backend - always initialized
	cosmosCommitter *memiavl.CommitStore

	// evmCommitter is the FlatKV backend - may be nil if not enabled
	evmCommitter flatkv.Store

	// homeDir is the base directory for the store
	homeDir string

	// config holds the store configuration
	config config.StateCommitConfig
}

// NewCompositeCommitStore creates a new composite commit store.
// Note: The store is NOT opened yet. Call LoadVersion to open and initialize the DBs.
// This matches the memiavl.NewCommitStore pattern.
func NewCompositeCommitStore(
	ctx context.Context,
	homeDir string,
	cfg config.StateCommitConfig,
) *CompositeCommitStore {
	if err := cfg.Validate(); err != nil {
		panic(fmt.Sprintf("invalid state commit config: %s", err))
	}

	// Always initialize the Cosmos backend (creates struct only, not opened)
	cosmosCommitter := memiavl.NewCommitStore(homeDir, cfg.MemIAVLConfig)

	store := &CompositeCommitStore{
		cosmosCommitter: cosmosCommitter,
		homeDir:         homeDir,
		config:          cfg,
	}

	// Initialize FlatKV store struct if write mode requires it
	// Note: DB is NOT opened here, will be opened in LoadVersion
	if cfg.WriteMode == config.DualWrite || cfg.WriteMode == config.SplitWrite {
		flatkvPath := utils.GetFlatKVPath(homeDir)
		store.evmCommitter = flatkv.NewCommitStore(ctx, flatkvPath, cfg.FlatKVConfig)
	}

	return store
}

// Initialize initializes the store with the given store names
func (cs *CompositeCommitStore) Initialize(initialStores []string) {
	cs.cosmosCommitter.Initialize(initialStores)
}

// CleanupCrashArtifacts removes temporary/orphaned files left by a
// previous process crash (e.g. FlatKV readonly-* working directories).
// Must be called once at process startup, before any read-only clones
// are created. Any writer lock acquired during cleanup is retained for
// the subsequent LoadVersion(..., false) call.
func (cs *CompositeCommitStore) CleanupCrashArtifacts() error {
	if fkv, ok := cs.evmCommitter.(*flatkv.CommitStore); ok {
		if err := fkv.CleanupOrphanedReadOnlyDirs(); err != nil {
			return err
		}
	}
	return nil
}

// SetInitialVersion sets the initial version for the store
func (cs *CompositeCommitStore) SetInitialVersion(initialVersion int64) error {
	return cs.cosmosCommitter.SetInitialVersion(initialVersion)
}

// LoadVersion opens the database at the given version (0 = latest).
// When readOnly is true an isolated composite store is returned.
func (cs *CompositeCommitStore) LoadVersion(targetVersion int64, readOnly bool) (types.Committer, error) {
	cosmosSC, err := cs.cosmosCommitter.LoadVersion(targetVersion, readOnly)
	if err != nil {
		return nil, fmt.Errorf("failed to load cosmos version: %w", err)
	}

	cosmosCommitter, ok := cosmosSC.(*memiavl.CommitStore)
	if !ok {
		return nil, fmt.Errorf("unexpected committer type from cosmos LoadVersion")
	}

	if readOnly {
		newStore := &CompositeCommitStore{
			cosmosCommitter: cosmosCommitter,
			homeDir:         cs.homeDir,
			config:          cs.config,
		}
		if cs.evmCommitter != nil {
			evmStore, err := cs.evmCommitter.LoadVersion(targetVersion, true)
			if err != nil {
				logger.Error("FlatKV unavailable for readonly load, EVM data will not be served",
					"version", targetVersion, "err", err)
			} else {
				newStore.evmCommitter = evmStore
			}
		}
		return newStore, nil
	}

	cs.cosmosCommitter = cosmosCommitter
	if cs.evmCommitter != nil {
		_, err := cs.evmCommitter.LoadVersion(targetVersion, false)
		if err != nil {
			return nil, fmt.Errorf("failed to load FlatKV version: %w", err)
		}

		// When loading latest (targetVersion==0), a crash between the
		// sequential cosmos and EVM commits can leave the backends at
		// different versions. Detect the mismatch and roll the ahead
		// backend back so both restart from a consistent point.
		if targetVersion == 0 {
			if err := cs.reconcileVersions(); err != nil {
				return nil, err
			}
		}
	}

	return cs, nil
}

// ApplyChangeSets applies changesets to the appropriate backends based on config.
func (cs *CompositeCommitStore) ApplyChangeSets(changesets []*proto.NamedChangeSet) error {
	if len(changesets) == 0 {
		return nil
	}

	// Separate EVM and cosmos changesets
	var evmChangeset []*proto.NamedChangeSet
	var cosmosChangeset []*proto.NamedChangeSet

	for _, changeset := range changesets {
		if changeset.Name == EVMStoreName {
			evmChangeset = append(evmChangeset, changeset)
		} else {
			cosmosChangeset = append(cosmosChangeset, changeset)
		}
	}

	// Handle write mode routing
	switch cs.config.WriteMode {
	case config.CosmosOnlyWrite:
		// All data goes to cosmos
		cosmosChangeset = changesets
		evmChangeset = nil
	case config.DualWrite:
		// EVM data goes to both, non-EVM only to cosmos
		cosmosChangeset = changesets
		// evmChangeset already filtered above
	case config.SplitWrite:
		// EVM goes to EVM store, non-EVM to cosmos (already filtered above)
	}

	// Cosmos changesets always goes to cosmos commit store
	if len(cosmosChangeset) > 0 {
		if err := cs.cosmosCommitter.ApplyChangeSets(cosmosChangeset); err != nil {
			return fmt.Errorf("failed to apply cosmos changesets: %w", err)
		}
	}

	if cs.evmCommitter != nil && len(evmChangeset) > 0 {
		if err := cs.evmCommitter.ApplyChangeSets(evmChangeset); err != nil {
			return fmt.Errorf("failed to apply EVM changesets: %w", err)
		}
	}

	return nil
}

// ApplyUpgrades applies store upgrades (only applicable to Cosmos backend)
func (cs *CompositeCommitStore) ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error {
	return cs.cosmosCommitter.ApplyUpgrades(upgrades)
}

// Commit commits the current state to all active backends
func (cs *CompositeCommitStore) Commit() (int64, error) {
	// Always commit to Cosmos
	cosmosVersion, err := cs.cosmosCommitter.Commit()
	if err != nil {
		return 0, fmt.Errorf("failed to commit cosmos: %w", err)
	}

	// Commit to FlatKV as well if enabled
	if cs.evmCommitter != nil {
		evmVersion, err := cs.evmCommitter.Commit()
		if err != nil {
			return 0, fmt.Errorf("failed to commit to EVM store: %w", err)
		}
		if cosmosVersion != evmVersion {
			return 0, fmt.Errorf("cosmos and EVM version mismatch after commit: cosmos=%d, evm=%d", cosmosVersion, evmVersion)
		}
	}

	return cosmosVersion, nil
}

// reconcileVersions checks whether the cosmos and EVM backends are at the
// same version after loading latest. A crash between the sequential Commit
// calls can leave one backend one version ahead. When a mismatch is found
// and both backends have committed at least once (version > 0), the ahead
// backend is rolled back to the behind version. Rollback truncates the WAL
// so the correction survives subsequent restarts.
func (cs *CompositeCommitStore) reconcileVersions() error {
	cosmosVer := cs.cosmosCommitter.Version()
	evmVer := cs.evmCommitter.Version()
	if cosmosVer == evmVer {
		return nil
	}

	// Skip reconciliation when either backend is at version 0 (fresh
	// initialization / migration), since that is not a crash artifact.
	if cosmosVer == 0 || evmVer == 0 {
		return nil
	}

	minVer := cosmosVer
	if evmVer < minVer {
		minVer = evmVer
	}

	logger.Warn("version mismatch between cosmos and EVM after loading latest, rolling back to consistent version",
		"cosmosVersion", cosmosVer, "evmVersion", evmVer, "reconciledVersion", minVer)

	if cosmosVer > minVer {
		if err := cs.cosmosCommitter.Rollback(minVer); err != nil {
			return fmt.Errorf("failed to rollback cosmos to reconciled version %d: %w", minVer, err)
		}
	}
	if evmVer > minVer {
		if err := cs.evmCommitter.Rollback(minVer); err != nil {
			return fmt.Errorf("failed to rollback EVM to reconciled version %d: %w", minVer, err)
		}
	}

	return nil
}

// Version returns the current version
func (cs *CompositeCommitStore) Version() int64 {
	if cs.cosmosCommitter != nil {
		return cs.cosmosCommitter.Version()
	} else if cs.evmCommitter != nil {
		return cs.evmCommitter.Version()
	}
	return 0
}

// GetLatestVersion returns the latest version
func (cs *CompositeCommitStore) GetLatestVersion() (int64, error) {
	// TODO: switch to metadata db
	return cs.cosmosCommitter.GetLatestVersion()
}

// GetEarliestVersion returns the earliest version
func (cs *CompositeCommitStore) GetEarliestVersion() (int64, error) {
	// TODO: switch to metadata db
	return cs.cosmosCommitter.GetEarliestVersion()
}

// appendEvmLatticeHash returns a new CommitInfo with the EVM lattice hash
// appended, without mutating the original. Returns the original unchanged
// when lattice hashing is disabled.
func (cs *CompositeCommitStore) appendEvmLatticeHash(ci *proto.CommitInfo, evmHash []byte) *proto.CommitInfo {
	if !cs.config.EnableLatticeHash {
		return ci
	}
	combined := make([]proto.StoreInfo, len(ci.StoreInfos)+1)
	copy(combined, ci.StoreInfos)
	combined[len(combined)-1] = proto.StoreInfo{
		Name: "evm_lattice",
		CommitId: proto.CommitID{
			Version: ci.Version,
			Hash:    evmHash,
		},
	}
	return &proto.CommitInfo{
		Version:    ci.Version,
		StoreInfos: combined,
	}
}

// WorkingCommitInfo returns the working commit info
func (cs *CompositeCommitStore) WorkingCommitInfo() *proto.CommitInfo {
	ci := cs.cosmosCommitter.WorkingCommitInfo()
	if cs.evmCommitter != nil {
		return cs.appendEvmLatticeHash(ci, cs.evmCommitter.RootHash())
	}
	return ci
}

// LastCommitInfo returns the last commit info
func (cs *CompositeCommitStore) LastCommitInfo() *proto.CommitInfo {
	ci := cs.cosmosCommitter.LastCommitInfo()
	if cs.evmCommitter != nil {
		return cs.appendEvmLatticeHash(ci, cs.evmCommitter.CommittedRootHash())
	}
	return ci
}

// GetChildStoreByName returns the underlying child store by module name.
// This only applies to cosmos committer.
func (cs *CompositeCommitStore) GetChildStoreByName(name string) types.CommitKVStore {
	return cs.cosmosCommitter.GetChildStoreByName(name)
}

// Rollback rolls back to the specified version
func (cs *CompositeCommitStore) Rollback(targetVersion int64) error {
	if err := cs.cosmosCommitter.Rollback(targetVersion); err != nil {
		return fmt.Errorf("failed to rollback cosmos commit store: %w", err)
	}

	if cs.evmCommitter != nil {
		if err := cs.evmCommitter.Rollback(targetVersion); err != nil {
			return fmt.Errorf("failed to rollback evm commit store: %w", err)
		}
	}

	return nil
}

// Exporter returns an exporter for state sync
func (cs *CompositeCommitStore) Exporter(version int64) (types.Exporter, error) {
	if version < 0 || version > math.MaxUint32 {
		return nil, fmt.Errorf("version %d out of range", version)
	}

	cosmosExporter, err := cs.cosmosCommitter.Exporter(version)
	if err != nil {
		return nil, fmt.Errorf("failed to create cosmos exporter: %w", err)
	}

	var evmExporter types.Exporter
	if cs.evmCommitter != nil && (cs.config.WriteMode == config.SplitWrite || cs.config.WriteMode == config.DualWrite) {
		evmExporter, err = cs.evmCommitter.Exporter(version)
		if err != nil {
			_ = cosmosExporter.Close()
			return nil, fmt.Errorf("failed to create evm exporter: %w", err)
		}
	}

	return NewExporter(cosmosExporter, evmExporter)
}

// Importer returns an importer for state sync
func (cs *CompositeCommitStore) Importer(version int64) (types.Importer, error) {
	cosmosImporter, err := cs.cosmosCommitter.Importer(version)
	if err != nil {
		return nil, err
	}
	var evmImporter types.Importer
	if cs.evmCommitter != nil {
		evmImporter, err = cs.evmCommitter.Importer(version)
		if err != nil {
			_ = cosmosImporter.Close()
			return nil, fmt.Errorf("failed to create evm importer: %w", err)
		}
	}
	compositeImporter := NewImporter(cosmosImporter, evmImporter)
	return compositeImporter, nil
}

// Close closes all backends
func (cs *CompositeCommitStore) Close() error {
	var errs []error

	if cs.cosmosCommitter != nil {
		if err := cs.cosmosCommitter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close cosmos: %w", err))
		}
	}

	if cs.evmCommitter != nil {
		if err := cs.evmCommitter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close FlatKV: %w", err))
		}
	}

	return commonerrors.Join(errs...)
}
