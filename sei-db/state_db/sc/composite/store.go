// Package composite provides a unified commit store that coordinates
// between Cosmos (memiavl) and EVM (flatkv) committers.
package composite

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"sync/atomic"

	ics23 "github.com/confio/ics23/go"
	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
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

// sto558Trace gates verbose diagnostic logs used to investigate the STO-558
// account_number divergence and the rollback AppHash mismatch in
// migrate_evm mode. Enabled by setting STO558_TRACE=1.
var sto558Trace = os.Getenv("STO558_TRACE") == "1"

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

	// latticeAppendLatched is a sticky one-way flag: once it transitions
	// to true, LastCommitInfo and WorkingCommitInfo unconditionally
	// append the evm_lattice StoreInfo without consulting the on-disk
	// migration metadata again. The flag protects the AppHash continuity
	// invariant for a live MemiavlOnly -> MigrateEVM transition: while the
	// migration boundary on flatkv is still NotStarted, the lattice must
	// be suppressed so the post-restart LastCommitInfo matches the
	// pre-restart memiavl-only AppHash at the same height. Once the
	// boundary advances (or the migration completes), the gate latches
	// and subsequent calls skip the flatkv read. See shouldAppendLatticeHash.
	latticeAppendLatched atomic.Bool

	// migrationAdvancedThisCommit gates per-block migration progress
	// against rootmulti.Store's double-flush pattern. rootmulti calls
	// flush() once inside GetWorkingHash (whose result is the AppHash
	// returned to Tendermint) and once inside Commit. In migration
	// modes we forward every flush to the router, but only the first
	// ApplyChangeSets call in a commit cycle is marked firstBatchInBlock
	// so the MigrationManager advances at most one batch per block.
	// Otherwise a second flush with empty or non-empty changesets could
	// advance another migration batch, perturb the working commit info,
	// and persist a hash that differs from the one already returned to
	// Tendermint.
	//
	// Set on the first ApplyChangeSets of a block; reset by Commit
	// after both backend commits succeed. See ApplyChangeSets + Commit
	// for the wiring and the rootmulti integration test
	// TestRootMultiMigrateEVM_DoubleFlushAppHashStable for the pinned
	// invariant.
	migrationAdvancedThisCommit bool
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
func (cs *CompositeCommitStore) LoadVersion(targetVersion int64, readOnly bool) (committer types.Committer, retErr error) {
	var memIAVLCommitter *memiavl.CommitStore
	var flatKVStore flatkv.Store

	defer func() {
		if !readOnly || retErr == nil {
			return
		}
		if memIAVLCommitter != nil {
			_ = memIAVLCommitter.Close()
		}
		if flatKVStore != nil {
			_ = flatKVStore.Close()
		}
	}()

	if cs.memIAVL != nil {
		memIAVLSC, err := cs.memIAVL.LoadVersion(targetVersion, readOnly)
		if err != nil {
			return nil, fmt.Errorf("failed to load cosmos version: %w", err)
		}
		var ok bool
		memIAVLCommitter, ok = memIAVLSC.(*memiavl.CommitStore)
		if !ok {
			// Defensive: in practice memiavl always returns
			// *CommitStore, but if some future implementation does not,
			// close whatever was returned so we do not leak it.
			if closer, isCloser := memIAVLSC.(interface{ Close() error }); isCloser {
				_ = closer.Close()
			}
			return nil, fmt.Errorf("unexpected committer type from cosmos LoadVersion")
		}
	}

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
//
// Forwarding rules:
//   - Non-migration modes: empty changesets are a no-op (nothing to apply).
//   - Migration modes: every flush is forwarded so caller writes always
//     reach the backends and empty blocks can still advance migration.
//     The firstBatchInBlock flag tells the MigrationManager whether this
//     call may advance the boundary; second and later flushes in the same
//     commit cycle forward writes only.
func (cs *CompositeCommitStore) ApplyChangeSets(changesets []*proto.NamedChangeSet) error {
	if cs.config.WriteMode.IsMigrationMode() {
		firstBatchInBlock := !cs.migrationAdvancedThisCommit
		if err := cs.router.ApplyChangeSets(changesets, firstBatchInBlock); err != nil {
			return fmt.Errorf("failed to apply changesets: %w", err)
		}
		cs.migrationAdvancedThisCommit = true
		return nil
	} else if len(changesets) == 0 {
		return nil
	}

	err := cs.router.ApplyChangeSets(changesets, false)
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

	// Reset the per-block migration-advance gate so the next block's
	// first ApplyChangeSets is permitted to advance migration again.
	// Reset after both backends have successfully committed; see
	// migrationAdvancedThisCommit for the AppHash continuity invariant
	// this preserves.
	cs.migrationAdvancedThisCommit = false

	if sto558Trace {
		cs.tracePostCommit(cosmosVersion, flatkvVersion)
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

// tracePostCommit emits one structured log entry per committed block, dumping
// the per-store hashes that contribute to AppHash. When STO-558 recurs the
// failing height is named in the consensus panic; one grep over this log
// answers "which store diverged" in seconds without rerunning the chain.
//
// Cheap to produce (a few short hex strings per block) and only enabled by
// STO558_TRACE=1, so safe to leave compiled in.
func (cs *CompositeCommitStore) tracePostCommit(cosmosVersion, flatkvVersion int64) {
	hexShort := func(b []byte) string {
		if len(b) == 0 {
			return ""
		}
		s := hex.EncodeToString(b)
		if len(s) > 16 {
			return s[:16]
		}
		return s
	}

	version := cosmosVersion
	if version < 0 {
		version = flatkvVersion
	}

	appendLattice := cs.shouldAppendLatticeHash()

	// Pack per-module hashes into a single grep-friendly string so the
	// log line stays one record per block: "auth:abc..;bank:def..;...".
	var storesBuf string
	if cs.memIAVL != nil {
		if ci := cs.memIAVL.LastCommitInfo(); ci != nil {
			for i, si := range ci.StoreInfos {
				if i > 0 {
					storesBuf += ";"
				}
				storesBuf += si.Name + ":" + hexShort(si.CommitId.Hash)
			}
		}
	}
	var latticeHashShort string
	if cs.flatKV != nil {
		latticeHashShort = hexShort(cs.flatKV.CommittedRootHash())
	}

	logger.Info("STO558 post-commit hashes",
		"version", version,
		"cosmosVersion", cosmosVersion,
		"flatkvVersion", flatkvVersion,
		"appendLattice", appendLattice,
		"latticeHash", latticeHashShort,
		"latched", cs.latticeAppendLatched.Load(),
		"stores", storesBuf,
	)
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

// GetLatestVersion returns the highest committed version.
func (cs *CompositeCommitStore) GetLatestVersion() (int64, error) {
	if cs.memIAVL != nil {
		return cs.memIAVL.GetLatestVersion()
	} else if cs.flatKV != nil {
		return cs.flatKV.GetLatestVersion()
	} else {
		return 0, errors.New("no backend configured")
	}
}

// shouldAppendLatticeHash reports whether LastCommitInfo and
// WorkingCommitInfo should append the synthetic evm_lattice StoreInfo to
// the commit info.
//
// The composite store contributes an evm_lattice entry to every commit
// info whenever the flatkv backend is participating in the AppHash. The
// one exception is the brief NotStarted window of a live
// MemiavlOnly -> MigrateEVM transition: between the time flatkv is
// opened (LoadVersion seeds it to memiavl's version) and the first
// post-transition commit (which advances the migration boundary), the
// chain's stored AppHash at the just-loaded height still reflects the
// memiavl-only era. Adding an evm_lattice entry at that height would
// silently change the AppHash that Tendermint already accepted and
// fail the handshake.
//
// To preserve continuity exactly through that window — and not a moment
// longer — the gate consults the on-disk migration metadata on flatkv:
//
//   - flatKV == nil (MemiavlOnly): never append; flatkv is not part of
//     the merkle root at all.
//   - WriteMode != MigrateEVM (EVMMigrated, MigrateAllButBank,
//     AllMigratedButBank, MigrateBank, FlatKVOnly, TestOnlyDualWrite):
//     always append. These modes either entered with the lattice baked
//     into their genesis or descend from a flatkv-bearing predecessor
//     that already committed it; there is no memiavl-only prior
//     AppHash to be inconsistent with. By design no operator will jump
//     a memiavl-only chain straight into one of these modes.
//   - MigrateEVM: append iff the migration has progressed past
//     MigrationNotStarted. We treat the boundary as "started" if
//     MigrationBoundaryKey is present and decodes to any status other
//     than MigrationNotStarted, OR if MigrationVersionKey is present.
//     The latter is what survives a completion block: the manager
//     atomically deletes MigrationBoundaryKey and writes
//     MigrationVersionKey, so checking both keys covers the entire
//     post-NotStarted lifecycle.
//
// The result is sticky once true. After the very first observation
// that the gate has opened, latticeAppendLatched is set and subsequent
// calls return immediately without re-reading flatkv. This both avoids
// per-call DB work on the hot commit-info path and guarantees a
// consistent answer across the completion block on which the on-disk
// signal hops from MigrationBoundaryKey to MigrationVersionKey.
func (cs *CompositeCommitStore) shouldAppendLatticeHash() bool {
	if cs.flatKV == nil {
		if sto558Trace {
			logger.Info("STO558 shouldAppendLatticeHash decision",
				"decision", false, "reason", "flatKV-nil")
		}
		return false
	}
	if cs.latticeAppendLatched.Load() {
		return true
	}
	if cs.config.WriteMode != config.MigrateEVM {
		cs.latticeAppendLatched.Store(true)
		if sto558Trace {
			logger.Info("STO558 shouldAppendLatticeHash decision",
				"decision", true, "reason", "writeMode-not-MigrateEVM",
				"writeMode", cs.config.WriteMode, "latched", true)
		}
		return true
	}
	if boundaryBytes, ok := cs.flatKV.Get(
		migration.MigrationStore, []byte(migration.MigrationBoundaryKey),
	); ok {
		boundary, err := migration.DeserializeMigrationBoundary(boundaryBytes)
		if err != nil {
			// Consensus-critical: a corrupt boundary record means we
			// cannot tell whether the lattice should be in the AppHash.
			// Failing loud is the only safe option.
			panic(fmt.Sprintf(
				"composite: failed to deserialize migration boundary from flatkv MigrationStore: %v", err))
		}
		if boundary.Status() != migration.MigrationNotStarted {
			cs.latticeAppendLatched.Store(true)
			if sto558Trace {
				logger.Info("STO558 shouldAppendLatticeHash decision",
					"decision", true, "reason", "boundary-not-NotStarted",
					"boundaryStatus", boundary.Status(), "latched", true)
			}
			return true
		}
		if sto558Trace {
			logger.Info("STO558 shouldAppendLatticeHash boundary check",
				"boundaryFound", true, "boundaryStatus", boundary.Status())
		}
	} else if sto558Trace {
		logger.Info("STO558 shouldAppendLatticeHash boundary check", "boundaryFound", false)
	}
	if _, ok := cs.flatKV.Get(
		migration.MigrationStore, []byte(migration.MigrationVersionKey),
	); ok {
		cs.latticeAppendLatched.Store(true)
		if sto558Trace {
			logger.Info("STO558 shouldAppendLatticeHash decision",
				"decision", true, "reason", "MigrationVersionKey-present", "latched", true)
		}
		return true
	}
	if sto558Trace {
		logger.Info("STO558 shouldAppendLatticeHash decision",
			"decision", false, "reason", "MigrateEVM-but-NotStarted-no-versionKey")
	}
	return false
}

// appendEvmLatticeHash returns a new CommitInfo with the EVM lattice hash
// appended, without mutating the original.
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

	if cs.shouldAppendLatticeHash() {
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

	if cs.shouldAppendLatticeHash() {
		return cs.appendEvmLatticeHash(ci, cs.flatKV.CommittedRootHash())
	}
	return ci
}

// GetChildStoreByName returns the underlying child store by module name.
// Panics if the store name is not supported by the current write mode.
//
// The reserved migration.MigrationStore tree is always rejected,
// regardless of mode: it is owned by the migration workflow.
func (cs *CompositeCommitStore) GetChildStoreByName(name string) types.CommitKVStore {
	if name == migration.MigrationStore {
		panic(fmt.Errorf(
			"CompositeCommitStore.GetChildStoreByName: store %q is reserved",
			name,
		))
	} else if cs.config.WriteMode == config.MemiavlOnly {
		// In MemiavlOnly mode, check to see if the tree exists. Required to support legacy test apps
		// that use non-standard store names.
		if cs.memIAVL.GetChildStoreByName(name) == nil {
			panic(fmt.Errorf(
				"CompositeCommitStore.GetChildStoreByName: store %q is not in keys.MemIAVLStoreKeys",
				name,
			))
		}
	} else if cs.config.WriteMode != config.FlatKVOnly {
		// FlatKV only mode can support arbitrary store names. Otherwise, require the store to be in the canonical list.
		if !keys.IsMemIAVLStoreKey(name) {
			panic(fmt.Errorf(
				"CompositeCommitStore.GetChildStoreByName: store %q is not in keys.MemIAVLStoreKeys",
				name,
			))
		}
	}

	return migration.NewRouterCommitKVStore(
		cs.router,
		name,
		cs.Version,
		func(start, end []byte, ascending bool) (db.Iterator, error) {
			return cs.iterate(name, start, end, ascending)
		},
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
			if memIAVLExporter != nil {
				_ = memIAVLExporter.Close()
			}
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
			if memIAVLImporter != nil {
				_ = memIAVLImporter.Close()
			}
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
	return cs.iterate(store, start, end, ascending)
}

// iterate builds a single iterator over store by stitching together one
// iterator per active backend.
//
// Both memiavl and flatkv expose the same per-store Iterator contract and
// matching key semantics, so iteration no longer goes through the router:
// composite asks each non-nil backend for an iterator and merges the results.
// A backend that does not hold store contributes nothing -- memiavl returns a
// nil iterator for an unknown store and flatkv returns an empty one -- so an
// absent store iterates as an empty (no-op) range rather than an error. This
// matches the long-term flatkv-only end state, where every module lives in a
// single backend and "unsupported store" ceases to exist.
//
// During a migration a key lives in exactly one backend at any committed
// version (migrated keys are deleted from memiavl as they are copied into
// flatkv), so the merged stream has no duplicates. flatkv is placed last so
// that, should a duplicate ever occur, the newer flatkv value wins.
func (cs *CompositeCommitStore) iterate(store string, start []byte, end []byte, ascending bool) (db.Iterator, error) {
	if store == "" {
		return nil, fmt.Errorf("store name cannot be empty")
	}
	if store == migration.MigrationStore {
		return nil, fmt.Errorf("iteration from the %q store is not permitted", migration.MigrationStore)
	}

	// flatkv is appended after memiavl so it is the rightmost (winning) child.
	children := make([]db.Iterator, 0, 2)
	if cs.memIAVL != nil {
		memIter, err := cs.memIAVL.Iterator(store, start, end, ascending)
		if err != nil {
			return nil, fmt.Errorf("failed to build memiavl iterator: %w", err)
		}
		// memiavl returns a nil iterator for a store it does not hold; skip it.
		if memIter != nil {
			children = append(children, memIter)
		}
	}
	if cs.flatKV != nil {
		flatIter, err := cs.flatKV.Iterator(store, start, end, ascending)
		if err != nil {
			closeIterators(children)
			return nil, fmt.Errorf("failed to build flatkv iterator: %w", err)
		}
		if flatIter != nil {
			children = append(children, flatIter)
		}
	}

	// Zero children yields a valid, empty iterator (an absent store is a no-op).
	// NewMergingIterator takes ownership of children and closes all of them if
	// construction fails, so we must not close them again here (Pebble's Close is
	// not idempotent and a double close could corrupt its iterator pool).
	merged, err := iterators.NewMergingIterator(ascending, children...)
	if err != nil {
		return nil, fmt.Errorf("failed to merge backend iterators: %w", err)
	}
	// The merged iterator reports the union of child domains; present the
	// caller's logical [start, end) instead, per the dbm.Iterator contract.
	return iterators.NewDomainIterator(merged, start, end)
}

// closeIterators best-effort closes a set of iterators, used to release any
// already-built children when a later step of iterator construction fails.
func closeIterators(iters []db.Iterator) {
	for _, it := range iters {
		if it != nil {
			_ = it.Close()
		}
	}
}
