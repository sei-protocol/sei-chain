// Package composite provides a unified commit store that coordinates
// between Cosmos (memiavl) and EVM (flatkv) committers.
package composite

import (
	"context"
	"fmt"
	"math"

	ics23 "github.com/confio/ics23/go"
	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/seilog"
	db "github.com/tendermint/tm-db"
)

var logger = seilog.NewLogger("db", "state-db", "sc", "composite")

// For backward compatibility purpose reuse current interface
var _ types.Committer = (*CompositeCommitStore)(nil)

// CompositeCommitStore manages multiple commit store backends (Cosmos/memiavl and FlatKV)
// and routes operations based on the configured migration strategy.
type CompositeCommitStore struct {
	// The memIAVL backend. Will be nil after all data is migrated to flatkv.
	memIAVL *memiavl.CommitStore

	// The flatKV backend. Will be nil if migration to flatKV has not yet started.
	flatKV flatkv.Store

	// Manages routing of traffic between the memiavl and flatkv backends.
	// Built (and rebuilt) inside LoadVersion against the just-opened
	// backends so that lazily-eager constructors like
	// NewMemiavlMigrationIterator see a non-nil memiavl DB.
	router migration.Router

	// ctx captures the constructor context so LoadVersion can rebuild
	// the router against the opened backends. Section 4 will replace
	// this by removing the context dependency from BuildRouter.
	ctx context.Context

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
) (*CompositeCommitStore, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid state commit config: %w", err)
	}

	var memIAVL *memiavl.CommitStore
	if cfg.WriteMode != config.FlatKVOnly {
		memIAVL = memiavl.NewCommitStore(homeDir, cfg.MemIAVLConfig)

		// TODO instantiate migration store if we are in migration mode!!
	}

	var flatKV flatkv.Store
	if cfg.WriteMode != config.MemiavlOnly {
		cfg.FlatKVConfig.DataDir = utils.GetFlatKVPath(homeDir)
		fkv, err := flatkv.NewCommitStore(ctx, &cfg.FlatKVConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create FlatKV commit store: %w", err)
		}
		flatKV = fkv
	}

	return &CompositeCommitStore{
		memIAVL: memIAVL,
		flatKV:  flatKV,
		homeDir: homeDir,
		config:  cfg,
		ctx:     ctx,
	}, nil
}

// Initialize initializes the store with the given store names
func (cs *CompositeCommitStore) Initialize(initialStores []string) {
	cs.memIAVL.Initialize(initialStores)
}

// CleanupCrashArtifacts removes temporary/orphaned files left by a
// previous process crash (e.g. FlatKV readonly-* working directories).
// Must be called once at process startup, before any read-only clones
// are created. Any writer lock acquired during cleanup is retained for
// the subsequent LoadVersion(..., false) call.
func (cs *CompositeCommitStore) CleanupCrashArtifacts() error {
	if cs.flatKV == nil {
		return nil
	}
	return cs.flatKV.CleanupOrphanedReadOnlyDirs()
}

// SetInitialVersion sets the initial version for the store
func (cs *CompositeCommitStore) SetInitialVersion(initialVersion int64) error {
	return cs.memIAVL.SetInitialVersion(initialVersion)
}

// TODO this method does not properly handle situations when memIAVL is nil...

// LoadVersion opens the database at the given version (0 = latest).
// When readOnly is true an isolated composite store is returned.
func (cs *CompositeCommitStore) LoadVersion(targetVersion int64, readOnly bool) (types.Committer, error) {
	memIAVLSC, err := cs.memIAVL.LoadVersion(targetVersion, readOnly)
	if err != nil {
		return nil, fmt.Errorf("failed to load cosmos version: %w", err)
	}

	memIAVLCommitter, ok := memIAVLSC.(*memiavl.CommitStore)
	if !ok {
		return nil, fmt.Errorf("unexpected committer type from cosmos LoadVersion")
	}

	if readOnly {
		newStore := &CompositeCommitStore{
			memIAVL: memIAVLCommitter,
			homeDir: cs.homeDir,
			config:  cs.config,
		}
		if cs.flatKV != nil {
			evmStore, err := cs.flatKV.LoadVersion(targetVersion, true)
			if err != nil {
				logger.Error("FlatKV unavailable for readonly load, EVM data will not be served",
					"version", targetVersion, "err", err)
			} else {
				newStore.flatKV = evmStore
			}
		}
		return newStore, nil
	}

	cs.memIAVL = memIAVLCommitter
	if cs.flatKV != nil {
		_, err := cs.flatKV.LoadVersion(targetVersion, false)
		if err != nil {
			return nil, fmt.Errorf("failed to load FlatKV version: %w", err)
		}

		// Migration-entry seeding: turning on a non-MemiavlOnly mode on a
		// chain that has been running on MemiavlOnly leaves memiavl at
		// version N while flatkv starts fresh at version 0. Bring flatkv
		// into lockstep so the next composite commit produces matching
		// versions on both backends. Only runs at load-latest; targeted
		// loads stay strict so a mismatch is surfaced loudly.
		if targetVersion == 0 && cs.memIAVL.Version() > 0 && cs.flatKV.Version() == 0 {
			seedTo := cs.memIAVL.Version() + 1
			logger.Info("seeding flatkv initial version to match memiavl",
				"memiavlVersion", cs.memIAVL.Version(), "flatkvInitialVersion", seedTo)
			if err := cs.flatKV.SetInitialVersion(seedTo); err != nil {
				return nil, fmt.Errorf("failed to seed flatkv to memiavl version %d: %w",
					cs.memIAVL.Version(), err)
			}
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

	// Build the router now that memiavl + flatkv are open. The migration
	// iterator-bearing builders (MigrateEVM / MigrateAllButBank /
	// MigrateBank) capture memIAVL.GetDB() eagerly, which is nil before
	// LoadVersion runs. Section 4 will fold this into a single rewrite
	// that also handles router teardown across reloads.
	if err := cs.buildRouter(); err != nil {
		return nil, err
	}

	return cs, nil
}

// buildRouter constructs the migration router against the currently-opened
// backends and assigns it to cs.router. Must be called after memIAVL and
// flatKV (if any) have been opened via LoadVersion.
func (cs *CompositeCommitStore) buildRouter() error {
	router, err := migration.BuildRouter(
		cs.ctx,
		cs.config.WriteMode,
		cs.memIAVL,
		cs.flatKV,
		cs.config.KeysToMigratePerBlock,
	)
	if err != nil {
		return fmt.Errorf("failed to build router: %w", err)
	}
	cs.router = router
	return nil
}

// ApplyChangeSets applies changesets to the appropriate backends based on config.
func (cs *CompositeCommitStore) ApplyChangeSets(changesets []*proto.NamedChangeSet) error {
	if len(changesets) == 0 {
		return nil
	}

	err := cs.router.ApplyChangeSets(changesets)
	if err != nil {
		return fmt.Errorf("failed to apply changesets: %w", err)
	}

	return nil
}

// ApplyUpgrades applies store upgrades (only applicable to memIAVL Cosmos backend). Data in
// flatKV is not affected by this method.
func (cs *CompositeCommitStore) ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error {
	if cs.memIAVL == nil {
		return nil
	}

	return cs.memIAVL.ApplyUpgrades(upgrades)
}

// Commit commits the current state to all active backends
func (cs *CompositeCommitStore) Commit() (int64, error) {
	var cosmosVersion int64 = -1
	if cs.memIAVL != nil {
		var err error
		cosmosVersion, err = cs.memIAVL.Commit()
		if err != nil {
			return 0, fmt.Errorf("failed to commit cosmos: %w", err)
		}
	}

	var flatkvVersion int64 = -1
	if cs.flatKV != nil {
		var err error
		flatkvVersion, err = cs.flatKV.Commit()
		if err != nil {
			return 0, fmt.Errorf("failed to commit flatkv: %w", err)
		}
	}

	if cosmosVersion >= 0 && flatkvVersion >= 0 {
		if cosmosVersion != flatkvVersion {
			return 0, fmt.Errorf("cosmos and flatkv version mismatch after commit: cosmos=%d, flatkv=%d",
				cosmosVersion, flatkvVersion)
		}
		return cosmosVersion, nil
	} else if cosmosVersion >= 0 {
		return cosmosVersion, nil
	} else if flatkvVersion >= 0 {
		return flatkvVersion, nil
	} else {
		return 0, fmt.Errorf("no version committed")
	}
}

// reconcileVersions checks whether the cosmos and EVM backends are at the
// same version after loading latest. A crash between the sequential Commit
// calls can leave one backend one version ahead. When a mismatch is found
// and both backends have committed at least once (version > 0), the ahead
// backend is rolled back to the behind version. Rollback truncates the WAL
// so the correction survives subsequent restarts.
func (cs *CompositeCommitStore) reconcileVersions() error {

	if cs.memIAVL == nil || cs.flatKV == nil {
		// Nothing to reconcile if one of the backends is not present.
		return nil
	}

	cosmosVer := cs.memIAVL.Version()
	evmVer := cs.flatKV.Version()
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
		if err := cs.memIAVL.Rollback(minVer); err != nil {
			return fmt.Errorf("failed to rollback cosmos to reconciled version %d: %w", minVer, err)
		}
	}
	if evmVer > minVer {
		if err := cs.flatKV.Rollback(minVer); err != nil {
			return fmt.Errorf("failed to rollback EVM to reconciled version %d: %w", minVer, err)
		}
	}

	return nil
}

// Version returns the current version
func (cs *CompositeCommitStore) Version() int64 {
	if cs.memIAVL != nil {
		return cs.memIAVL.Version()
	} else if cs.flatKV != nil {
		return cs.flatKV.Version()
	}
	return 0
}

// GetLatestVersion returns the latest version
func (cs *CompositeCommitStore) GetLatestVersion() (int64, error) {
	// TODO how is this different from Version()? How to support with FlatKV?

	// TODO: switch to metadata db
	return cs.memIAVL.GetLatestVersion()
}

// GetEarliestVersion returns the earliest version
func (cs *CompositeCommitStore) GetEarliestVersion() (int64, error) {
	// TODO How to support with FlatKV?

	// TODO: switch to metadata db
	return cs.memIAVL.GetEarliestVersion()
}

// appendEvmLatticeHash returns a new CommitInfo with the EVM lattice hash
// appended, without mutating the original. Returns the original unchanged
// when flatKV is not present.
func (cs *CompositeCommitStore) appendEvmLatticeHash(ci *proto.CommitInfo, evmHash []byte) *proto.CommitInfo {
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
	var ci *proto.CommitInfo
	if cs.memIAVL != nil {
		ci = cs.memIAVL.WorkingCommitInfo()
	} else {
		ci = &proto.CommitInfo{
			Version: cs.Version(),
		}
	}

	if cs.flatKV != nil {
		return cs.appendEvmLatticeHash(ci, cs.flatKV.RootHash())
	}

	return ci
}

// LastCommitInfo returns the last commit info
func (cs *CompositeCommitStore) LastCommitInfo() *proto.CommitInfo {
	var ci *proto.CommitInfo
	if cs.memIAVL != nil {
		ci = cs.memIAVL.LastCommitInfo()
	} else {
		ci = &proto.CommitInfo{
			Version: cs.Version(),
		}
	}

	if cs.flatKV != nil {
		return cs.appendEvmLatticeHash(ci, cs.flatKV.CommittedRootHash())
	}
	return ci
}

// GetChildStoreByName returns the underlying child store by module name.
// This only applies to cosmos committer.
func (cs *CompositeCommitStore) GetChildStoreByName(name string) types.CommitKVStore {
	return migration.NewRouterCommitKVStore(
		cs.router,
		name,
		cs.Version,
	)
}

// Rollback rolls back to the specified version
func (cs *CompositeCommitStore) Rollback(targetVersion int64) error {
	if cs.memIAVL != nil {
		if err := cs.memIAVL.Rollback(targetVersion); err != nil {
			return fmt.Errorf("failed to rollback cosmos commit store: %w", err)
		}
	}

	if cs.flatKV != nil {
		if err := cs.flatKV.Rollback(targetVersion); err != nil {
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

	var memIAVLExporter types.Exporter
	if cs.memIAVL != nil {
		var err error
		memIAVLExporter, err = cs.memIAVL.Exporter(version)
		if err != nil {
			return nil, fmt.Errorf("failed to create cosmos exporter: %w", err)
		}
	}

	var flatkvExporter types.Exporter
	if cs.flatKV != nil {
		var err error
		flatkvExporter, err = cs.flatKV.Exporter(version)
		if err != nil {
			_ = memIAVLExporter.Close()
			return nil, fmt.Errorf("failed to create flatkv exporter: %w", err)
		}
	}

	if memIAVLExporter == nil && flatkvExporter == nil {
		return nil, fmt.Errorf("no exporter created")
	} else if memIAVLExporter == nil {
		return flatkvExporter, nil
	} else if flatkvExporter == nil {
		return memIAVLExporter, nil
	} else {
		return NewExporter(memIAVLExporter, flatkvExporter)
	}
}

// Importer returns an importer for state sync
func (cs *CompositeCommitStore) Importer(version int64) (types.Importer, error) {
	var memIAVLImporter types.Importer
	if cs.memIAVL != nil {
		var err error
		memIAVLImporter, err = cs.memIAVL.Importer(version)
		if err != nil {
			return nil, fmt.Errorf("failed to create cosmos importer: %w", err)
		}
	}

	var flatKVImporter types.Importer
	if cs.flatKV != nil {
		var err error
		flatKVImporter, err = cs.flatKV.Importer(version)
		if err != nil {
			_ = memIAVLImporter.Close()
			return nil, fmt.Errorf("failed to create flatkv importer: %w", err)
		}
	}

	if memIAVLImporter == nil && flatKVImporter == nil {
		return nil, fmt.Errorf("no importer created")
	} else if memIAVLImporter == nil {
		return flatKVImporter, nil
	} else if flatKVImporter == nil {
		return memIAVLImporter, nil
	} else {
		return NewImporter(memIAVLImporter, flatKVImporter), nil
	}
}

// Close closes all backends
func (cs *CompositeCommitStore) Close() error {
	var errs []error

	if cs.memIAVL != nil {
		if err := cs.memIAVL.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close cosmos: %w", err))
		}
	}

	if cs.flatKV != nil {
		if err := cs.flatKV.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close FlatKV: %w", err))
		}
	}

	return commonerrors.Join(errs...)
}

func (cs *CompositeCommitStore) Get(store string, key []byte) (value []byte, ok bool, err error) {
	if store == "" {
		return nil, false, fmt.Errorf("store name cannot be empty")
	}
	if key == nil {
		return nil, false, fmt.Errorf("key cannot be nil")
	}

	value, ok, err = cs.router.Read(store, key)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read value: %w", err)
	}
	return value, ok, nil
}

func (cs *CompositeCommitStore) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	if store == "" {
		return nil, fmt.Errorf("store name cannot be empty")
	}
	if key == nil {
		return nil, fmt.Errorf("key cannot be nil")
	}

	proof, err := cs.router.GetProof(store, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get proof: %w", err)
	}
	return proof, nil
}

func (cs *CompositeCommitStore) Has(store string, key []byte) (bool, error) {
	_, ok, err := cs.Get(store, key)
	if err != nil {
		return false, fmt.Errorf("failed to get value: %w", err)
	}
	return ok, nil
}

func (cs *CompositeCommitStore) Iterator(store string, start []byte, end []byte, ascending bool) (db.Iterator, error) {
	if store == "" {
		return nil, fmt.Errorf("store name cannot be empty")
	}
	if start == nil {
		return nil, fmt.Errorf("start cannot be nil")
	}
	if end == nil {
		return nil, fmt.Errorf("end cannot be nil")
	}
	iterator, err := cs.router.Iterator(store, start, end, ascending)
	if err != nil {
		return nil, fmt.Errorf("failed to get iterator: %w", err)
	}
	return iterator, nil
}
