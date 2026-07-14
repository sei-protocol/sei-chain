// Package composite provides a unified commit store that coordinates
// between Cosmos (memiavl) and EVM (flatkv) committers.
package composite

import (
	"context"
	"errors"
	"fmt"
	"math"
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

	// currentWriteMode is the write mode actually driving routing and
	// mode-dependent gating. It equals the configured WriteMode unless the
	// configured mode is types.Auto, in which case it is derived from
	// the migration metadata persisted in flatkv (see
	// migration.DeriveWriteMode) during LoadVersion and advanced at
	// runtime by SetWriteMode. Written only between blocks (LoadVersion /
	// SetWriteMode); read unsynchronized on the commit path, matching the
	// pre-existing config-read contract.
	currentWriteMode types.WriteMode

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

	// memiavlHashExcluded is a sticky one-way flag mirroring
	// latticeAppendLatched on the memiavl side: once the bank migration's
	// completion (migration version Version3_FlatKVOnly) has been
	// observed, memiavl's per-store infos are permanently excluded from
	// the commit info without re-reading flatkv. See
	// shouldIncludeMemiavlInfos for the gating rules.
	memiavlHashExcluded atomic.Bool

	// migrationBatchSize is the governance-controlled number of keys to
	// migrate per block, pushed in via SetMigrationBatchSize (the app reads
	// the NumKeysToMigratePerBlock gov param each BeginBlock and forwards it
	// here through the rootmulti store). 0 means the migration is paused; it
	// is the sole source of the per-block batch size (there is no node-local
	// config fallback).
	//
	// Atomic because SetMigrationBatchSize (consensus goroutine, between
	// blocks) and the build paths that read it must not tear; it mirrors
	// the between-blocks-write / unsynchronized-read contract used by the
	// other sticky flags on this struct.
	migrationBatchSize atomic.Int64

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

	alignFlatKVSnapshotWithMemIAVL(&cfg)

	var memIAVL *memiavl.CommitStore
	if cfg.WriteMode != types.FlatKVOnly {
		memIAVL = memiavl.NewCommitStore(homeDir, cfg.MemIAVLConfig)
	}

	// Under types.Auto flatkv is lazy: the directory exists on disk only
	// once a MigrateEVM transition has materialized it (see SetWriteMode
	// / materializeFlatKV), so its presence on the file tree is the
	// signal that flatkv participates. An absent directory means the
	// store is effectively MemiavlOnly and no flatkv instance is needed —
	// constructing one here would create the directory as a side effect
	// (flatkv.NewCommitStore initializes its data directories eagerly)
	// and destroy the signal.
	cfg.FlatKVConfig.DataDir = utils.GetFlatKVPath(homeDir)
	openFlatKV := cfg.WriteMode != types.MemiavlOnly
	if cfg.WriteMode == types.Auto && !utils.DirExists(cfg.FlatKVConfig.DataDir) {
		openFlatKV = false
	}

	var flatKV flatkv.Store
	if openFlatKV {
		fkv, err := flatkv.NewCommitStore(ctx, &cfg.FlatKVConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create FlatKV commit store: %w", err)
		}
		flatKV = fkv
	}

	return &CompositeCommitStore{
		memIAVL:          memIAVL,
		flatKV:           flatKV,
		homeDir:          homeDir,
		config:           cfg,
		currentWriteMode: cfg.WriteMode,
		ctx:              ctx,
	}, nil
}

// alignFlatKVSnapshotWithMemIAVL keeps the two backends' snapshot cadence in
// sync. FlatKV has no independently-exposed snapshot knobs in app.toml, so it
// derives its snapshot-interval / keep-recent from memIAVL's sc-* keys. This is
// the single place both backends are constructed from the same config, so it is
// where the alignment is enforced.
//
// This derivation is intentionally unconditional across write modes, including
// FlatKVOnly — where NewCompositeCommitStore never constructs a memIAVL store.
// The sc-* keys are the only operator-visible snapshot-cadence knobs now that
// the flatkv.* keys are hidden from the app.toml template, so they must govern
// FlatKV's cadence in every mode; otherwise FlatKVOnly would have no
// template-visible way to tune it. It is harmless when memIAVL is absent: the
// sc-* defaults match FlatKV's own in-code defaults, and only cfg.FlatKVConfig
// is read when building the FlatKVOnly store.
//
// FlatKV mirrors memIAVL's *effective* cadence: a zero memIAVL value is first
// resolved to the same default Options.FillDefaults would apply at OpenDB
// (interval 0 -> DefaultSnapshotInterval, keep-recent 0 -> DefaultSnapshotKeepRecent),
// then assigned to FlatKV unconditionally. Resolving-then-assigning (rather than
// skipping on a zero and letting FlatKV keep its own in-code default) keeps the
// two backends in true lockstep without relying on FlatKV's default happening to
// equal memIAVL's healed default. That reliance is fragile — the defaults are
// only kept equal by hand — and it breaks for an upgrading node whose old
// app.toml still carries an explicit state-commit.flatkv.snapshot-keep-recent
// (rendered by the old template) alongside sc-keep-recent = 0: skipping would
// leave FlatKV pinned to the stale explicit value while memIAVL healed to a
// different default. Note that mirroring a raw 0 is never correct here (0 means
// "disable auto-snapshots" for FlatKV), which is why the zero is resolved first.
func alignFlatKVSnapshotWithMemIAVL(cfg *config.StateCommitConfig) {
	interval := cfg.MemIAVLConfig.SnapshotInterval
	if interval == 0 {
		interval = memiavl.DefaultSnapshotInterval
	}
	keepRecent := cfg.MemIAVLConfig.SnapshotKeepRecent
	if keepRecent == 0 {
		keepRecent = memiavl.DefaultSnapshotKeepRecent
	}
	cfg.FlatKVConfig.SnapshotInterval = interval
	cfg.FlatKVConfig.SnapshotKeepRecent = keepRecent
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
//
// Keyed on the configured (not effective) mode: Initialize runs before
// LoadVersion, when the effective mode is not yet derivable. types.Auto
// therefore takes the strict canonical-store-names branch below, which is
// the desired behavior since the mode may become mixed at any time.
func validateInitialStores(mode types.WriteMode, initialStores []string) error {
	for _, s := range initialStores {
		if s == migration.MigrationStore {
			return fmt.Errorf(
				"composite.Initialize: reserved store name %q is owned by the composite store",
				migration.MigrationStore,
			)
		}
	}
	if mode == types.MemiavlOnly || mode == types.FlatKVOnly {
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
func (cs *CompositeCommitStore) LoadVersion(
	targetVersion int64,
	readOnly bool,
) (committer types.Committer, retErr error) {
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

	if cs.flatKV != nil && !cs.readOnlyTargetPredatesFlatKV(targetVersion, readOnly) {
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
		if err := ro.resolveCurrentWriteMode(false); err != nil {
			return nil, fmt.Errorf("failed to resolve effective write mode for read-only handle: %w", err)
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

	if err := cs.resolveCurrentWriteMode(true); err != nil {
		return nil, fmt.Errorf("failed to resolve write mode: %w", err)
	}

	if err := cs.buildRouter(); err != nil {
		return nil, err
	}

	return cs, nil
}

// readOnlyTargetPredatesFlatKV reports whether a read-only LoadVersion at
// targetVersion should skip flatkv entirely because the version predates
// flatkv's history: under types.Auto the chain ran memiavl-only before the
// MigrateEVM transition seeded flatkv, so at such heights ALL consensus
// data (including evm) lives in memiavl and a memiavl-only handle serves
// both reads and proofs completely. Without the skip, the flatkv load
// fails ("no snapshot found ...") and the whole historical query errors —
// queries that succeeded when the node was configured memiavl_only.
//
// Keyed on flatkv's persisted earliest-history record
// (Store.EarliestVersion, written by the seeding SetInitialVersion), NOT
// on the load failing: "no snapshot at target" is also what pruned
// in-history versions produce, and silently serving those from
// post-migration memiavl (with migrated keys deleted) would fabricate
// nonexistence answers. Pruned/corrupt in-history versions therefore
// still fail loudly. Restricted to types.Auto: fixed-mode handles cannot
// re-derive an effective MemiavlOnly mode, and their fail-loud behavior
// is pre-existing and pinned.
func (cs *CompositeCommitStore) readOnlyTargetPredatesFlatKV(targetVersion int64, readOnly bool) bool {
	if !readOnly || cs.config.WriteMode != types.Auto || targetVersion <= 0 {
		return false
	}
	earliest := cs.flatKV.EarliestVersion()
	if earliest <= 0 || targetVersion >= earliest {
		return false
	}
	logger.Info("read-only target predates flatkv history; serving memiavl only",
		"targetVersion", targetVersion, "flatkvEarliestVersion", earliest)
	return true
}

// resolveCurrentWriteMode sets cs.currentWriteMode after the backends have been
// opened. For a fixed configured mode this is a copy; for types.Auto the
// mode is derived from the migration metadata persisted in flatkv. A nil
// flatkv under types.Auto means the backend was never materialized
// (lazy-open found no directory), which is definitionally MemiavlOnly.
//
// closeIdleFlatKV maintains the lazy-flatkv invariant: under types.Auto,
// flatKV != nil iff the effective mode is past MemiavlOnly (flatkv is
// consensus-visible). If flatkv is open but derivation still says
// MemiavlOnly — a MigrateEVM transition created the directory but crashed
// before its first commit advanced the migration boundary — the handle is
// closed again and the transition trigger is expected to re-fire
// (transitions must be level-triggered; see SetWriteMode). The writable
// LoadVersion path passes true; the read-only path passes false because
// its clone is also tracked by LoadVersion's error-cleanup defer (closing
// here would double-close) and an idle clone is released at handle Close
// anyway.
func (cs *CompositeCommitStore) resolveCurrentWriteMode(closeIdleFlatKV bool) error {
	if cs.config.WriteMode != types.Auto {
		cs.currentWriteMode = cs.config.WriteMode
		return nil
	}
	if cs.flatKV == nil {
		cs.currentWriteMode = types.MemiavlOnly
		return nil
	}
	derived, err := migration.DeriveWriteMode(cs.flatKV)
	if err != nil {
		return fmt.Errorf("failed to derive write mode: %w", err)
	}
	if derived == types.MemiavlOnly && closeIdleFlatKV {
		logger.Info("flatkv directory exists but no migration has started; " +
			"closing flatkv until a MigrateEVM transition materializes it")
		if err := cs.flatKV.Close(); err != nil {
			return fmt.Errorf("failed to close non-participating flatkv: %w", err)
		}
		cs.flatKV = nil
	}
	logger.Debug("derived effective write mode from migration metadata", "mode", derived)
	cs.currentWriteMode = derived
	return nil
}

// buildRouter constructs the migration router against the currently-opened
// backends and assigns it to cs.router. Must be called after memIAVL and
// flatKV (if any) have been opened via LoadVersion and after the effective
// mode has been resolved.
func (cs *CompositeCommitStore) buildRouter() error {
	routerCtx, cancel := context.WithCancel(cs.ctx)
	router, err := migration.BuildRouter(
		routerCtx, cs.currentWriteMode, cs.memIAVL, cs.flatKV, int(cs.migrationBatchSize.Load()))
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

// SetMigrationBatchSize records the governance-controlled migration batch
// size and pushes it into the live router. Only a migration router acts on it
// (every other router treats it as a no-op), so this is safe to call in any
// write mode. A batch size of 0 pauses the migration.
//
// This is the single chokepoint feeding the router builders and the
// MigrationManager, so it normalizes the value here: a negative rate is
// meaningless and is clamped to 0 (paused). The lower layers therefore trust
// the batch size to be non-negative and do no validation of their own.
//
// Must be called between blocks (the app calls it from BeginBlock, before any
// ApplyChangeSets). The router's threadSafeRouter wrapper serializes the push
// against concurrent reads.
func (cs *CompositeCommitStore) SetMigrationBatchSize(batchSize int) error {
	if batchSize < 0 {
		batchSize = 0
	}
	cs.migrationBatchSize.Store(int64(batchSize))
	if cs.router != nil {
		cs.router.SetMigrationBatchSize(batchSize)
	}
	return nil
}

// GetMigrationBatchSize returns the governance-controlled migration batch size
// most recently pushed via SetMigrationBatchSize (0 when never set / paused).
// It is intended for observability and tests.
func (cs *CompositeCommitStore) GetMigrationBatchSize() int {
	return int(cs.migrationBatchSize.Load())
}

// GetWriteMode returns the effective write mode currently driving routing.
// Under types.Auto this is the mode derived from migration metadata (and
// advanced by SetWriteMode); under a fixed configuration it equals the
// configured mode. Callers that gate consensus-relevant transitions on it
// must observe it between blocks for the same reasons SetWriteMode documents.
func (cs *CompositeCommitStore) GetWriteMode() types.WriteMode {
	return cs.currentWriteMode
}

// ConfiguredWriteMode reports the write mode set by configuration, before any
// Auto derivation. It is types.Auto for an auto store (whose effective
// GetWriteMode is derived from migration metadata) and the pinned mode for any
// fixed configuration. The migration kick-off needs this to tell an auto store
// resting in memiavl_only (which it may advance to migrate_evm) apart from a
// node pinned to fixed memiavl_only — the two report the same effective mode,
// but only the former is allowed to transition at runtime.
func (cs *CompositeCommitStore) ConfiguredWriteMode() types.WriteMode {
	return cs.config.WriteMode
}

// SetWriteMode transitions the effective write mode at runtime. Only legal
// when the configured mode is types.Auto; with any fixed configuration the
// write mode cannot change without a restart.
//
// Safety: only adjacent forward steps along the migration chain are
// permitted, and the current mode's work must be complete before it can
// be left — a migration mode may advance to its completion steady-state
// only once its migration version bump has been persisted. Setting the
// already-active mode is a no-op. Everything else (skipping steps, moving
// backward, exiting a migration mid-flight, targeting Auto or
// TestOnlyDualWrite) returns an error and leaves the store untouched.
//
// Must be called between blocks — after Commit has completed (all flushes
// done, every write buffer popped) and before the next block's first
// ApplyChangeSets. It is not synchronized against concurrent commits, and
// calling it between GetWorkingHash and Commit would advance migration
// state after the AppHash was already reported to Tendermint. The
// rootmulti.SetWriteMode entry point additionally enforces empty write
// buffers at the call.
//
// Trigger requirements — because migration writes feed the AppHash, all
// nodes must perform the same transition at the same height, and the
// transition is NOT persisted until the next block commits (a node
// crashing in that window restarts in the previous mode). The trigger
// must therefore be:
//
//   - deterministic: derived from chain state (e.g. height), never from
//     per-node operator input;
//   - level-triggered, evaluated on every commit:
//     `if height >= H && currentMode == <predecessor> { SetWriteMode(<target>) }`.
//     This self-heals the crash window: a reverted node notices the lag
//     at its next commit and re-fires; same-mode calls are no-ops, so
//     re-firing against already-persisted state is harmless.
//
// Do NOT key the trigger on a one-shot "applied" marker written into app
// state during block H: the marker becomes durable with H's commit while
// the mode switch is memory-only, so a crash immediately after the commit
// leaves the node stuck in the old mode with the trigger consumed.
func (cs *CompositeCommitStore) SetWriteMode(targetWriteMode types.WriteMode) error {
	if cs.config.WriteMode != types.Auto {
		return fmt.Errorf(
			"write mode is fixed at %q by configuration; runtime switching requires write mode %q",
			cs.config.WriteMode, types.Auto)
	}
	if cs.router == nil {
		return errors.New("SetWriteMode called before LoadVersion")
	}
	if targetWriteMode == cs.currentWriteMode {
		return nil
	}

	if err := types.ValidateTransition(cs.currentWriteMode, targetWriteMode); err != nil {
		return fmt.Errorf("write mode transition rejected: %w", err)
	}

	// The current mode's work must be finished before stepping forward.
	complete, err := migration.IsModeComplete(cs.flatKV, cs.currentWriteMode)
	if err != nil {
		return fmt.Errorf("failed to check completion of write mode %q: %w", cs.currentWriteMode, err)
	}
	if !complete {
		return fmt.Errorf("cannot transition %q -> %q: the %q migration is not complete",
			cs.currentWriteMode, targetWriteMode, cs.currentWriteMode)
	}

	// The MemiavlOnly -> MigrateEVM edge is where flatkv comes into
	// existence under lazy-open (the constructor skips it while its
	// directory is absent). Materialize it before the router that routes
	// to it is built.
	materialized := false
	if cs.flatKV == nil {
		if err := cs.materializeFlatKV(); err != nil {
			return fmt.Errorf("failed to materialize flatkv for write mode %q: %w", targetWriteMode, err)
		}
		materialized = true
	}

	prev := cs.currentWriteMode
	cs.currentWriteMode = targetWriteMode
	if err := cs.buildRouter(); err != nil {
		// buildRouter leaves the previous router installed on failure.
		cs.currentWriteMode = prev
		if materialized {
			// Restore the lazy-flatkv invariant (flatKV open iff the
			// effective mode is past MemiavlOnly). The on-disk directory
			// remains; the re-fired transition re-opens it idempotently.
			if closeErr := cs.flatKV.Close(); closeErr != nil {
				logger.Error("failed to close flatkv while rolling back write mode transition",
					"err", closeErr)
			}
			cs.flatKV = nil
		}
		return fmt.Errorf("failed to build router for write mode %q: %w", targetWriteMode, err)
	}
	cs.migrationAdvancedThisCommit = false
	logger.Info("write mode transitioned", "from", prev, "to", targetWriteMode)
	return nil
}

// materializeFlatKV creates, opens, and seeds the flatkv backend. Called
// from SetWriteMode on the MemiavlOnly -> MigrateEVM edge when flatkv was
// never opened (lazy-open found no directory at construction, or the
// directory was created by an interrupted transition and closed again by
// resolveCurrentWriteMode). Seeding mirrors the migration-entry seeding in
// LoadVersion and is idempotent: a flatkv left at a non-zero version by an
// interrupted transition is not re-seeded.
// newFlatKVInstance constructs (but does not open) a flatkv commit store
// rooted at this store's home directory, mirroring the constructor's
// configuration.
func (cs *CompositeCommitStore) newFlatKVInstance() (flatkv.Store, error) {
	flatKVConfig := cs.config.FlatKVConfig
	flatKVConfig.DataDir = utils.GetFlatKVPath(cs.homeDir)
	created, err := flatkv.NewCommitStore(cs.ctx, &flatKVConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create FlatKV commit store: %w", err)
	}
	return created, nil
}

func (cs *CompositeCommitStore) materializeFlatKV() error {
	created, err := cs.newFlatKVInstance()
	if err != nil {
		return err
	}
	// An interrupted earlier transition may have left crash artifacts in
	// the pre-existing directory; clean them like process startup does.
	if err := created.CleanupOrphanedReadOnlyDirs(); err != nil {
		_ = created.Close()
		return fmt.Errorf("failed to clean up FlatKV crash artifacts: %w", err)
	}
	loaded, err := created.LoadVersion(0, false)
	if err != nil {
		_ = created.Close()
		return fmt.Errorf("failed to load FlatKV: %w", err)
	}
	if cs.memIAVL.Version() > 0 && loaded.Version() == 0 {
		seedTo := cs.memIAVL.Version() + 1
		logger.Info("seeding flatkv initial version to match memiavl",
			"memiavlVersion", cs.memIAVL.Version(), "flatkvInitialVersion", seedTo)
		if err := loaded.SetInitialVersion(seedTo); err != nil {
			_ = loaded.Close()
			return fmt.Errorf("failed to seed flatkv to memiavl version %d: %w",
				cs.memIAVL.Version(), err)
		}
	}
	cs.flatKV = loaded
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
	if cs.currentWriteMode.IsMigrationMode() {
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
//   - flatKV == nil (configured MemiavlOnly, or types.Auto before a
//     MigrateEVM transition materializes flatkv): never append; flatkv
//     is not part of the merkle root at all.
//   - effective MemiavlOnly with flatKV open: defensive. On writable
//     stores this state no longer exists (lazy-open under types.Auto
//     closes a non-participating flatkv in resolveCurrentWriteMode);
//     it remains reachable on read-only handles opened during the
//     interrupted-transition window, which keep their idle clone.
//     Never append and never latch.
//   - effective mode != MigrateEVM (EVMMigrated, MigrateAllButBank,
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
		return false
	}
	if cs.latticeAppendLatched.Load() {
		return true
	}
	if cs.currentWriteMode == types.MemiavlOnly {
		// Defensive: writable stores close a non-participating flatkv in
		// resolveCurrentWriteMode, so this is reachable only on read-only
		// handles opened during an interrupted MemiavlOnly -> MigrateEVM
		// transition (directory created, boundary never advanced). flatkv
		// is open but not part of the AppHash; never latch.
		return false
	}
	if cs.currentWriteMode != types.MigrateEVM {
		cs.latticeAppendLatched.Store(true)
		return true
	}
	started, err := migrationStarted(cs.flatKV)
	if err != nil {
		// Consensus-critical: a corrupt boundary record means we
		// cannot tell whether the lattice should be in the AppHash.
		// Failing loud is the only safe option.
		panic(fmt.Sprintf(
			"composite: failed to read migration metadata from flatkv MigrationStore: %v", err))
	}
	if started {
		cs.latticeAppendLatched.Store(true)
		return true
	}
	return false
}

// migrationStarted reports whether the migration metadata visible through
// the given flatkv handle shows the EVM migration progressed past
// MigrationNotStarted: the boundary record decodes to any non-NotStarted
// status, or the version key exists (which is what survives a completion
// block — the manager atomically deletes the boundary and writes the
// version key). This is the moment flatkv joins the AppHash. Works
// against both live stores (latest + pending writes) and read-only clones
// (metadata as-of their loaded version).
func migrationStarted(flatKV flatkv.Store) (bool, error) {
	if boundaryBytes, ok := flatKV.Get(
		migration.MigrationStore, []byte(migration.MigrationBoundaryKey),
	); ok {
		boundary, err := migration.DeserializeMigrationBoundary(boundaryBytes)
		if err != nil {
			return false, fmt.Errorf("failed to deserialize migration boundary: %w", err)
		}
		if boundary.Status() != migration.MigrationNotStarted {
			return true, nil
		}
	}
	if _, ok := flatKV.Get(
		migration.MigrationStore, []byte(migration.MigrationVersionKey),
	); ok {
		return true, nil
	}
	return false, nil
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

// shouldIncludeMemiavlInfos reports whether memiavl's per-store infos
// belong in the commit info (and therefore the AppHash). Memiavl
// participates until the final (bank) migration completes, i.e. until the
// persisted migration version reaches Version3_FlatKVOnly; from that
// commit on, a store with memiavl still open must produce the same commit
// info as a configured flatkv_only store (memIAVL == nil), or the two
// configurations fork.
//
// The gate is keyed on persisted migration metadata, not on
// currentWriteMode: write-mode derivation auto-advances on restart
// (version 3 on disk derives FlatKVOnly) while live peers stay in
// MigrateBank until their transition trigger fires, so a mode-keyed
// answer would differ between a live and a restarted node at the same
// height. The metadata read sees pending (uncommitted) writes, so the
// block that completes the migration already excludes memiavl from its
// own working hash — deterministically on every node, since migration
// advances are part of block execution.
//
// The per-call metadata read is scoped to the only states that can
// observe version 3: MigrateBank (the migration that produces it) and
// FlatKVOnly (which the SetWriteMode gate only admits once version 3 is
// persisted). Every earlier mode includes memiavl unconditionally, and
// the result is one-way latched once exclusion is observed (mirroring
// latticeAppendLatched).
func (cs *CompositeCommitStore) shouldIncludeMemiavlInfos() bool {
	if cs.memIAVL == nil {
		return false
	}
	if cs.flatKV == nil {
		return true
	}
	if cs.memiavlHashExcluded.Load() {
		return false
	}
	if cs.currentWriteMode != types.MigrateBank && cs.currentWriteMode != types.FlatKVOnly {
		return true
	}
	if cs.currentWriteMode == types.FlatKVOnly {
		// Reachable only with memiavl open, i.e. types.Auto after the
		// runtime FlatKVOnly transition, whose gate requires the bank
		// migration complete.
		cs.memiavlHashExcluded.Store(true)
		return false
	}
	complete, err := migration.IsModeComplete(cs.flatKV, types.MigrateBank)
	if err != nil {
		// Consensus-critical: if the migration version cannot be read we
		// cannot tell whether memiavl belongs in the AppHash. Failing
		// loud is the only safe option (mirrors the corrupt-boundary
		// panic in shouldAppendLatticeHash).
		panic(fmt.Sprintf(
			"composite: failed to read migration version for commit-info gating: %v", err))
	}
	if complete {
		cs.memiavlHashExcluded.Store(true)
		return false
	}
	return true
}

// WorkingCommitInfo returns the working commit info
func (cs *CompositeCommitStore) WorkingCommitInfo() *proto.CommitInfo {
	var ci *proto.CommitInfo
	if cs.shouldIncludeMemiavlInfos() {
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
	if cs.shouldIncludeMemiavlInfos() {
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
	} else if cs.currentWriteMode == types.MemiavlOnly {
		// In MemiavlOnly mode, check to see if the tree exists. Required to support legacy test apps
		// that use non-standard store names.
		if cs.memIAVL.GetChildStoreByName(name) == nil {
			panic(fmt.Errorf(
				"CompositeCommitStore.GetChildStoreByName: store %q is not in keys.MemIAVLStoreKeys",
				name,
			))
		}
	} else if cs.currentWriteMode != types.FlatKVOnly {
		// FlatKV only mode can support arbitrary store names. Otherwise, require the store to be in the canonical list.
		if !keys.IsMemIAVLStoreKey(name) {
			panic(fmt.Errorf(
				"CompositeCommitStore.GetChildStoreByName: store %q is not in keys.MemIAVLStoreKeys",
				name,
			))
		}
	}

	// The provider resolves cs.router at call time: SetWriteMode replaces
	// the router while views vended here stay cached by rootmulti, and a
	// captured router value would keep serving the pre-transition mode.
	return migration.NewRouterCommitKVStore(
		func() migration.Router { return cs.router },
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
		memIAVL:          cosmosCopy,
		homeDir:          cs.homeDir,
		config:           cs.config,
		currentWriteMode: cs.currentWriteMode,
		ctx:              cs.ctx,
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

	// Clear both sticky commit-info latches so the next LastCommitInfo /
	// WorkingCommitInfo re-derives from the rolled-back migration metadata.
	// Rollback can move the boundary backwards across a seam that either
	// flag latched on:
	//   - latticeAppendLatched: set after the migrate_evm activation; a
	//     rollback to a pre-activation height (memiavl-only AppHash, no
	//     evm_lattice) must stop appending the lattice.
	//   - memiavlHashExcluded: set after the bank migration completes
	//     (version 3); a rollback below completion must re-include memiavl's
	//     per-store infos, which the AppHash still carries at that height.
	// Without these resets the in-process post-rollback commit info (the hash
	// `seid rollback` prints and rootmulti caches in rs.lastCommitInfo)
	// diverges from the canonical AppHash for the target height. The gates
	// re-latch correctly on the next call against the rolled-back metadata.
	//
	// Note: currentWriteMode is not re-derived here. It only matters when a
	// rollback crosses a seam whose latest-derived mode differs from the
	// target's (e.g. a rollback all the way across a completed migration);
	// that in-process view self-heals on the next `seid start`, which
	// re-derives the mode from the rolled-back metadata.
	cs.latticeAppendLatched.Store(false)
	cs.memiavlHashExcluded.Store(false)

	// Rollback is offline (no commit cycle in flight); clear the per-block
	// migration-advance gate defensively.
	cs.migrationAdvancedThisCommit = false

	return nil
}

// exportNeedsMetadataGating reports whether the configured mode allows
// backend presence to diverge from AppHash participation at some version,
// in which case the exporter must consult migration metadata as-of the
// exported version instead of trusting nil-ness:
//
//   - Auto: flatkv may be open but pre-consensus (interrupted transition
//     windows, seeded snapshots), memiavl may be open past version 3.
//   - MigrateEVM: flatkv is open from LoadVersion but only joins the hash
//     once the boundary advances; older versions predate flatkv entirely.
//   - MigrateBank: memiavl leaves the hash at the version-3 commit while
//     remaining open.
//
// Every other fixed mode has presence == participation: configured
// MemiavlOnly never opens flatkv, FlatKVOnly never opens memiavl, and the
// remaining modes entered with both backends consensus-visible from their
// first commit (genesis-EVMMigrated chains hash an empty flatkv from
// block 1, so a pure metadata rule would wrongly exclude it).
func exportNeedsMetadataGating(mode types.WriteMode) bool {
	return mode == types.Auto || mode == types.MigrateEVM || mode == types.MigrateBank
}

// Exporter returns an exporter for state sync.
//
// Section selection follows the AppHash: a backend's section belongs in
// the snapshot iff that backend contributes to the AppHash at the
// exported version. Anything less and a restored node misses consensus
// state; anything more and nodes with different configurations produce
// different snapshot streams at the same height, splitting state-sync
// chunk hashes across the network.
func (cs *CompositeCommitStore) Exporter(version int64) (types.Exporter, error) {
	if version < 0 || version > math.MaxUint32 {
		return nil, fmt.Errorf("version %d out of range", version)
	}

	includeMemiavl := cs.memIAVL != nil
	includeFlatKV := cs.flatKV != nil

	if includeFlatKV && exportNeedsMetadataGating(cs.config.WriteMode) {
		// Distinguish a genuinely pre-flatkv-era version from an in-history
		// flatkv load failure using flatkv's persisted earliest-history
		// record, NOT the load failing — mirroring readOnlyTargetPredatesFlatKV.
		// "no snapshot at target" / version-mismatch is also what a pruned or
		// corrupt in-history version produces (flatkv prunes old snapshots and
		// truncates the WAL beneath them while EarliestVersion stays fixed at
		// the seeded value), so keying on the load failing would silently emit
		// a memiavl-only snapshot that drops consensus-visible flatkv state and
		// is byte-indistinguishable from a legitimate pre-era stream.
		earliest := cs.flatKV.EarliestVersion()
		if earliest > 0 && version < earliest {
			// Genuinely pre-flatkv era: every consensus value at this height
			// lived in memiavl, so omitting flatkv is correct.
			logger.Info("export version predates flatkv history; exporting memiavl only",
				"version", version, "flatkvEarliestVersion", earliest)
			includeFlatKV = false
		} else {
			// Evaluate the hash predicates against metadata as-of the
			// exported version: flatkv read-only clones replay the WAL to the
			// target version, so the boundary/version keys reflect historical
			// state, not the live store's.
			ro, err := cs.flatKV.LoadVersion(version, true)
			if err != nil {
				// In-history load failure (pruned snapshot/WAL, corruption, or
				// a transient fault) at a version where flatkv participates in
				// the AppHash. Silently omitting flatkv here would produce a
				// consensus-incomplete snapshot, so fail loud instead.
				return nil, fmt.Errorf("failed to load flatkv at export version %d (>= earliest %d): %w",
					version, earliest, err)
			}
			started, gateErr := migrationStarted(ro)
			var bankDone bool
			if gateErr == nil {
				bankDone, gateErr = migration.IsModeComplete(ro, types.MigrateBank)
			}
			closeErr := ro.Close()
			if gateErr != nil {
				return nil, fmt.Errorf("failed to read migration metadata for export gating: %w", gateErr)
			}
			if closeErr != nil {
				return nil, fmt.Errorf("failed to close export gating handle: %w", closeErr)
			}
			if cs.config.WriteMode == types.MigrateBank {
				// Fixed MigrateBank descends from a flatkv-bearing
				// predecessor; flatkv is in its hash at every version it
				// can serve. Only the memiavl side is version-dependent.
				started = true
			}
			includeFlatKV = started
			includeMemiavl = includeMemiavl && !bankDone
		}
	}

	var memIAVLExporter types.Exporter
	if includeMemiavl {
		var err error
		memIAVLExporter, err = cs.memIAVL.Exporter(version)
		if err != nil {
			return nil, fmt.Errorf("failed to create cosmos exporter: %w", err)
		}
	}

	var flatkvExporter types.Exporter
	if includeFlatKV {
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
		// flatkv_only: the FlatKV exporter is self-describing (it emits its
		// own keys.FlatKVStoreKey module header ahead of the nodes), so it
		// can be returned directly without the composite wrapper.
		return flatkvExporter, nil
	} else if flatkvExporter == nil {
		return memIAVLExporter, nil
	} else {
		return NewExporter(memIAVLExporter, flatkvExporter)
	}
}

// Importer returns an importer for state sync.
//
// The importer is stream-driven: the sections present in the snapshot
// dictate what gets imported. Under types.Auto on a fresh node, flatkv
// does not exist yet (lazy-open found no directory) — but the snapshot
// being restored may carry a flatkv section, which is itself the proof
// that flatkv participates at that height. A factory is therefore handed
// to the SnapshotImporter to materialize flatkv on demand when (and only
// when) the stream presents its section; the created instance is adopted
// as cs.flatKV so the post-restore LoadVersion opens it normally.
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

	var flatKVFactory func() (types.Importer, error)
	if cs.flatKV == nil && cs.config.WriteMode == types.Auto {
		flatKVFactory = func() (types.Importer, error) {
			created, err := cs.newFlatKVInstance()
			if err != nil {
				return nil, fmt.Errorf("failed to materialize flatkv for import: %w", err)
			}
			imp, err := created.Importer(version)
			if err != nil {
				_ = created.Close()
				return nil, fmt.Errorf("failed to create flatkv importer: %w", err)
			}
			cs.flatKV = created
			return imp, nil
		}
	}

	if memIAVLImporter == nil && flatKVImporter == nil && flatKVFactory == nil {
		return nil, fmt.Errorf("no importer created")
	}
	if memIAVLImporter == nil && flatKVFactory == nil {
		// flatkv_only: the FlatKV importer consumes its own
		// self-describing section directly.
		return flatKVImporter, nil
	}
	// Wrapped even when only memiavl is active: with no flatkv importer
	// and no factory, a flatkv section in the stream is a configuration
	// mismatch that SnapshotImporter rejects loudly (previously it was
	// fed into the memiavl importer as a bogus tree).
	return NewImporter(memIAVLImporter, flatKVImporter, flatKVFactory), nil
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
// Iterator construction is synchronized by each backend rather than by the
// router: memiavl captures a COW root under the tree lock, and flatkv pins its
// Pebble views plus pending-write snapshots under its store lock.
//
// During a migration a key lives in exactly one backend at any committed
// version (migrated keys are deleted from memiavl as they are copied into
// flatkv), so the merged stream has no duplicates. memiavl must be queried
// before flatkv: if a migration commit interleaves between the two iterator
// constructions, this order makes the worst case a duplicate key, which the
// merge dedupes with flatkv winning. The reverse order could miss the key.
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
