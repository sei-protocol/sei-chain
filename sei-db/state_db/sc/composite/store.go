// Package composite provides a unified commit store that coordinates
// between Cosmos (memiavl) and EVM (flatkv) committers.
package composite

import (
	"context"
	"errors"
	"fmt"
	"math"

	ics23 "github.com/confio/ics23/go"
	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
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

	// ctx is the constructor's context. Each invocation of buildRouter
	// derives a per-router child context from it and stores the
	// corresponding cancel function in routerCancel; cancelling that
	// child stops any background goroutines owned by the current
	// router (today: the MigrationMetrics boundary-snapshot loop)
	// without affecting any unrelated work that shares cs.ctx.
	ctx context.Context

	// routerCancel cancels the child context handed to the current
	// router. Called before installing a new router on reload, and on
	// Close. Nil before the first LoadVersion and after Close.
	routerCancel context.CancelFunc

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

// Initialize records the set of child store names that should exist on
// the memiavl backend the first time it is opened. In mixed-DB modes
// names must be members of keys.MemIAVLStoreKeys.
func (cs *CompositeCommitStore) Initialize(initialStores []string) error {
	if err := validateInitialStores(cs.config.WriteMode, initialStores); err != nil {
		return err
	}
	if cs.memIAVL == nil {
		return nil
	}
	return cs.memIAVL.Initialize(initialStores)
}

// validateInitialStores enforces the rules described on Initialize.
func validateInitialStores(mode config.WriteMode, initialStores []string) error {
	for _, s := range initialStores {
		if s == migration.MigrationStore {
			return fmt.Errorf(
				"composite.Initialize: reserved store name %q is owned by the composite store",
				migration.MigrationStore,
			)
		}
	}
	if mode == config.MemiavlOnly || mode == config.FlatKVOnly {
		return nil
	}
	known := make(map[string]struct{}, len(keys.MemIAVLStoreKeys))
	for _, k := range keys.MemIAVLStoreKeys {
		known[k] = struct{}{}
	}
	var unknown []string
	for _, s := range initialStores {
		if _, ok := known[s]; !ok {
			unknown = append(unknown, s)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf(
			"composite.Initialize: store names not routable by router: %v "+
				"(allowed set is keys.MemIAVLStoreKeys)", unknown,
		)
	}
	return nil
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

// SetInitialVersion seeds every active backend so that the next Commit
// produces initialVersion. Called from cosmos-sdk BaseApp.InitChain on
// fresh genesis.
func (cs *CompositeCommitStore) SetInitialVersion(initialVersion int64) error {
	if cs.memIAVL != nil {
		if err := cs.memIAVL.SetInitialVersion(initialVersion); err != nil {
			return fmt.Errorf("memiavl SetInitialVersion: %w", err)
		}
	}
	if cs.flatKV != nil {
		if err := cs.flatKV.SetInitialVersion(initialVersion); err != nil {
			return fmt.Errorf("flatkv SetInitialVersion: %w", err)
		}
	}
	return nil
}

// LoadVersion opens the database at the given version (0 = latest).
// When readOnly is true an isolated composite store is returned.
func (cs *CompositeCommitStore) LoadVersion(targetVersion int64, readOnly bool) (types.Committer, error) {
	var memIAVLCommitter *memiavl.CommitStore
	if cs.memIAVL != nil {
		memIAVLSC, err := cs.memIAVL.LoadVersion(targetVersion, readOnly)
		if err != nil {
			return nil, fmt.Errorf("failed to load cosmos version: %w", err)
		}
		var ok bool
		memIAVLCommitter, ok = memIAVLSC.(*memiavl.CommitStore)
		if !ok {
			return nil, fmt.Errorf("unexpected committer type from cosmos LoadVersion")
		}
	}

	var flatKVStore flatkv.Store
	if cs.flatKV != nil {
		fkv, err := cs.flatKV.LoadVersion(targetVersion, readOnly)
		if err != nil {
			return nil, fmt.Errorf("failed to load FlatKV version: %w", err)
		}
		flatKVStore = fkv
	}

	if readOnly {
		// Build a per-handle composite with its own router. Without
		// this the read-only handle has cs.router == nil and every
		// read-side method nil-dereferences on first call. The new
		// composite inherits cs.ctx so cancellation of the parent
		// context cascades, but buildRouter installs its own child
		// cancel so closing this handle does not affect the parent.
		ro := &CompositeCommitStore{
			memIAVL: memIAVLCommitter,
			flatKV:  flatKVStore,
			homeDir: cs.homeDir,
			config:  cs.config,
			ctx:     cs.ctx,
		}
		if err := ro.buildRouter(); err != nil {
			return nil, fmt.Errorf("failed to build router for read-only handle: %w", err)
		}
		return ro, nil
	}

	// Reassign the freshly-loaded backends. flatkv.Store.LoadVersion
	// is documented to return the receiver on the writable path, but
	// the field is an interface (tests inject mocks via cs.flatKV =
	// mock); honoring the return value future-proofs against an
	// implementation that returns a swapped instance.
	if memIAVLCommitter != nil {
		cs.memIAVL = memIAVLCommitter
	}
	if flatKVStore != nil {
		cs.flatKV = flatKVStore
	}

	if cs.memIAVL != nil && cs.flatKV != nil {
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

	// In migration modes the router probes a "migration" tree on memiavl
	// during BuildRouter to learn the on-disk migration version. Add it
	// here on the writable path if it isn't already present;
	// memiavl.ApplyUpgrades rejects duplicate names rather than skipping
	// them, so we guard with a presence check. Read-only handles inherit
	// the tree from the on-disk state.
	if cs.memIAVL != nil && cs.config.WriteMode.IsMigrationMode() &&
		cs.memIAVL.GetChildStoreByName(migration.MigrationStore) == nil {
		if err := cs.memIAVL.ApplyUpgrades([]*proto.TreeNameUpgrade{
			{Name: migration.MigrationStore},
		}); err != nil {
			return nil, fmt.Errorf("add migration store tree to memiavl: %w", err)
		}
	}

	if err := cs.buildRouter(); err != nil {
		return nil, err
	}

	return cs, nil
}

// buildRouter constructs the migration router against the currently-opened
// backends and assigns it to cs.router. Must be called after memIAVL and
// flatKV (if any) have been opened via LoadVersion.
func (cs *CompositeCommitStore) buildRouter() error {
	routerCtx, cancel := context.WithCancel(cs.ctx)
	router, err := migration.BuildRouter(
		routerCtx, cs.config.WriteMode, cs.memIAVL, cs.flatKV, cs.config.KeysToMigratePerBlock)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to build router: %w", err)
	}
	if cs.routerCancel != nil {
		cs.routerCancel()
	}
	cs.router = router
	cs.routerCancel = cancel
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

// GetLatestVersion returns the highest version persisted to disk across
// all configured backends. When both backends are configured their
// answers must agree; a mismatch is surfaced as an error rather than
// silently picking one. Returns 0 when no backend has any prior commit.
func (cs *CompositeCommitStore) GetLatestVersion() (int64, error) {
	switch {
	case cs.memIAVL != nil && cs.flatKV != nil:
		memVer, err := cs.memIAVL.GetLatestVersion()
		if err != nil {
			return 0, fmt.Errorf("memiavl: %w", err)
		}
		flatVer, err := cs.flatKV.GetLatestVersion()
		if err != nil {
			return 0, fmt.Errorf("flatkv: %w", err)
		}
		if memVer != flatVer {
			return 0, fmt.Errorf(
				"backend latest version mismatch: memiavl=%d flatkv=%d",
				memVer, flatVer,
			)
		}
		return memVer, nil
	case cs.memIAVL != nil:
		return cs.memIAVL.GetLatestVersion()
	case cs.flatKV != nil:
		return cs.flatKV.GetLatestVersion()
	default:
		return 0, errors.New("no backend configured")
	}
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

// Copy returns an in-memory snapshot, or nil when flatkv is engaged
// (no in-memory primitive; a partial snapshot would miss EVM state).
func (cs *CompositeCommitStore) Copy() types.Committer {
	if cs == nil || cs.memIAVL == nil || cs.flatKV != nil {
		return nil
	}
	cosmosCopy, ok := cs.memIAVL.Copy().(*memiavl.CommitStore)
	if !ok || cosmosCopy == nil {
		return nil
	}
	snap := &CompositeCommitStore{
		memIAVL: cosmosCopy,
		homeDir: cs.homeDir,
		config:  cs.config,
		ctx:     cs.ctx,
	}
	if err := snap.buildRouter(); err != nil {
		if releaseErr := cosmosCopy.ReleaseSnapshotRefs(); releaseErr != nil {
			logger.Warn("failed to release memiavl snapshot refs after router build error",
				"buildErr", err, "releaseErr", releaseErr)
		}
		logger.Warn("failed to build router for SC snapshot", "err", err)
		return nil
	}
	return snap
}

// ReleaseSnapshotRefs releases refs held by a copied in-memory snapshot without
// closing DB-level resources shared with the live store.
func (cs *CompositeCommitStore) ReleaseSnapshotRefs() error {
	if cs == nil {
		return nil
	}
	if cs.routerCancel != nil {
		cs.routerCancel()
		cs.routerCancel = nil
	}
	cs.router = nil
	if cs.memIAVL == nil {
		return nil
	}
	err := cs.memIAVL.ReleaseSnapshotRefs()
	cs.memIAVL = nil
	return err
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

	if cs.routerCancel != nil {
		cs.routerCancel()
		cs.routerCancel = nil
	}
	cs.router = nil

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
