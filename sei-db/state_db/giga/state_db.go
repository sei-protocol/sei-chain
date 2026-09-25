package giga

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashvault"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

var logger = seilog.NewLogger("db", "state-db", "giga")

var _ gigatypes.StateDB = (*StateDB)(nil)

// StateDB writes a committed block to the state WAL, the state commit store (SC) and the EVM state
// store (SS), serves current-block reads from SC, and records SC's block hashes in the hash vault.
//
// It opens all four stores, brings them onto one height, and closes them. SC and SS run without a WAL
// of their own, so every block either of them replays is read from the WAL here.
type StateDB struct {
	// The state WAL a committed block is written to.
	wal statewal.StateWAL

	// The state commit store, which both receives writes and serves current-block reads.
	sc *flatkv.CommitStore

	// ss is nil when the EVM state store is disabled.
	ss *evm.EVMStateStore

	// The hash vault SC's block hashes are recorded in.
	vault *hashvault.PebbleHashVault

	// The checkpoint schedule SC and SS take their snapshot boundaries from.
	checkpointer *controller.CheckpointScheduler

	// Splits a commit into the three stores it writes. Driven only by CommitStateChanges, which
	// callers serialize.
	commitPhases *metrics.PhaseTimer
}

// commitPhaseTimerName prefixes the instruments the commit phase breakdown is published on.
const commitPhaseTimerName = "giga_state_commit"

// gigaMeterName is the OTel meter this package's instruments are created on.
const gigaMeterName = "seidb_giga"

// NewStateDB opens SC, SS, the state WAL and the hash vault, and puts SC and SS on one checkpoint schedule
// and one height: the WAL's head, or rollbackTo when it is not 0. The returned StateDB commits the block
// after that height, and its hash vault holds the hash of the block SC is on.
func NewStateDB(
	ctx context.Context,
	flatkvCfg *flatkvconfig.Config,
	ssCfg config.StateStoreConfig,
	checkpointCfg config.CheckpointConfig,
	hashVaultCfg hashvault.HashVaultConfig,
	// The height to roll back to, or 0 to load the latest block possible. Data after rollbackTo target
	// may be permanently deleted. Returns an error if not possible to roll back to requested block height.
	rollbackTo uint64,
) (_ *StateDB, retErr error) {
	ssCfg.DisableInternalWAL = true

	if err := recoverStores(flatkvCfg, ssCfg, hashVaultCfg, rollbackTo); err != nil {
		return nil, fmt.Errorf("recover the state DB's stores: %w", err)
	}

	var err error
	var ss *evm.EVMStateStore
	var sc *flatkv.CommitStore
	var vault *hashvault.PebbleHashVault
	var wal statewal.StateWAL
	defer func() {
		if retErr == nil {
			return
		}
		if err := closeStores(ss, sc, vault, wal); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close a partially opened state DB: %w", err))
		}
	}()

	if ss, err = openSS(ssCfg); err != nil {
		return nil, fmt.Errorf("open the state DB: %w", err)
	}

	if sc, err = openSC(ctx, flatkvCfg); err != nil {
		return nil, fmt.Errorf("open the state DB: %w", err)
	}

	if vault, err = hashvault.NewPebbleHashVault(ctx, hashVaultCfg); err != nil {
		return nil, fmt.Errorf("open the state DB: %w", err)
	}
	// The vault must be SC's first listener, and registering it before SC is reachable from outside this
	// StateDB is what makes it first. SC hands a hash to its listeners one at a time in registration order
	// and stops at the first that refuses it, and the vault returns only once the hash is flushed, so no
	// later listener sees a hash the vault has not recorded or has refused.
	if _, err := sc.RegisterHashListener(hashVaultListener(vault)); err != nil {
		return nil, fmt.Errorf("register the hash vault on the state commit store: %w", err)
	}

	if wal, err = flatkv.OpenStateWAL(flatkvCfg); err != nil {
		return nil, fmt.Errorf("open state WAL: %w", err)
	}

	checkpointer := startCheckpointSchedule(checkpointCfg, sc, ss)

	if err := catchUpToWAL(ctx, sc, ss, wal); err != nil {
		return nil, fmt.Errorf("catch the state DB up to its WAL: %w", err)
	}
	if rollbackTo > 0 {
		if err := matchHeight(sc, ss, wal, int64(rollbackTo)); err != nil { //nolint:gosec // a WAL block number
			return nil, fmt.Errorf("cannot roll back to %d: %w", rollbackTo, err)
		}
	}

	if err := recordLoadedBlockHash(sc, vault); err != nil {
		return nil, fmt.Errorf("record the loaded block's hash in the hash vault: %w", err)
	}

	return &StateDB{
		wal:          wal,
		sc:           sc,
		ss:           ss,
		vault:        vault,
		checkpointer: checkpointer,
		commitPhases: metrics.NewPhaseTimerFactory(otel.Meter(gigaMeterName), commitPhaseTimerName).
			RecordLatencies().Build(),
	}, nil
}

// openSC opens SC with no WAL of its own, on the version its files hold: the working copy, or the
// snapshot a rollback has just repointed it at. It replays nothing, so it comes up at or below the
// WAL's head and catchUpTo carries it forward from there.
func openSC(ctx context.Context, flatkvCfg *flatkvconfig.Config) (*flatkv.CommitStore, error) {
	sc, err := flatkv.NewCommitStore(ctx, flatkvCfg, nil)
	if err != nil {
		return nil, fmt.Errorf("open state commit store: %w", err)
	}
	// Every readonly-* directory under the store is deleted, so this has to run before the process
	// opens a read-only view of its own: after that, the ones a crashed process left are no longer
	// the only ones there.
	if err := sc.CleanupOrphanedReadOnlyDirs(); err != nil {
		return nil, errors.Join(
			fmt.Errorf("clean up orphaned state commit read-only dirs: %w", err), sc.Close())
	}
	if err := sc.LoadWorkingCopy(); err != nil {
		return nil, errors.Join(fmt.Errorf("load the state commit store: %w", err), sc.Close())
	}
	return sc, nil
}

// openSS opens the EVM state store and its snapshot manager, returning nil when the store is disabled.
func openSS(ssCfg config.StateStoreConfig) (*evm.EVMStateStore, error) {
	if !ssCfg.Enable {
		return nil, nil
	}
	ss, err := evm.NewEVMStateStore(ssCfg.EVMDBDirectory, ssCfg)
	if err != nil {
		return nil, fmt.Errorf("open EVM state store: %w", err)
	}
	snapshotRoot := utils.GetStateStoreSnapshotsSiblingPath(ssCfg.EVMDBDirectory)
	if err := ss.StartSnapshots(snapshotRoot, ssCfg, nil); err != nil {
		return nil, errors.Join(fmt.Errorf("start EVM state store snapshot manager: %w", err), ss.Close())
	}
	return ss, nil
}

// startCheckpointSchedule puts SC and SS on one snapshot cadence, and returns it. ss is nil when SS is
// disabled. It runs before either store is on a height, so the blocks SC replays offer themselves to the
// schedule as live commits do.
func startCheckpointSchedule(
	cfg config.CheckpointConfig,
	sc *flatkv.CommitStore,
	ss *evm.EVMStateStore,
) *controller.CheckpointScheduler {
	checkpointer := controller.NewCheckpointScheduler(cfg)
	sc.SetCheckpointScheduler(checkpointer)
	if ss != nil {
		ss.SetCheckpointScheduler(checkpointer)
	}
	return checkpointer
}

// Close closes SC, SS, the hash vault and the state WAL, reporting every failure rather than stopping at
// the first.
func (s *StateDB) Close() error {
	return closeStores(s.ss, s.sc, s.vault, s.wal)
}

// closeStores closes whichever of the stores are not nil, reporting every failure rather than stopping at
// the first. The hash vault closes after SC, which hands it the hashes of the blocks it drains, and the
// WAL closes last, since SC replays through it.
//
// How long each store took is logged, since each drains its own write queue and waits on the
// compactions behind it, and those dominate the time a shutdown takes.
func closeStores(
	ss *evm.EVMStateStore,
	sc *flatkv.CommitStore,
	vault *hashvault.PebbleHashVault,
	wal statewal.StateWAL,
) error {
	var errs error
	var timer utils.CloseTimer
	if ss != nil {
		if err := timer.Close("ss", ss.Close); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close EVM state store: %w", err))
		}
	}
	if sc != nil {
		if err := timer.Close("sc", sc.Close); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close state commit store: %w", err))
		}
	}
	if vault != nil {
		closeVault := func() error { return vault.Close(context.Background()) }
		if err := timer.Close("hashvault", closeVault); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close hash vault: %w", err))
		}
	}
	if wal != nil {
		if err := timer.Close("wal", wal.Close); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close state WAL: %w", err))
		}
	}
	logger.Info("Closed the state DB", timer.Fields()...)
	return errs
}

// SC returns the state commit store.
func (s *StateDB) SC() *flatkv.CommitStore { return s.sc }

// SS returns the EVM state store, or nil when it is disabled.
func (s *StateDB) SS() *evm.EVMStateStore { return s.ss }

// WAL returns the state WAL. It is the one this StateDB opened, and is not replaced for the StateDB's
// lifetime.
func (s *StateDB) WAL() statewal.StateWAL { return s.wal }

// CheckpointScheduler returns the schedule SC and SS take their snapshot boundaries from.
func (s *StateDB) CheckpointScheduler() *controller.CheckpointScheduler { return s.checkpointer }

// PrunableStores returns the opened stores that can join a prune cycle.
func (s *StateDB) PrunableStores() []controller.PrunableStore {
	stores := make([]controller.PrunableStore, 0, 4)
	if s.sc != nil {
		stores = append(stores, s.sc)
	}
	if s.wal != nil {
		stores = append(stores, s.wal)
	}
	if s.ss != nil {
		stores = append(stores, s.ss)
	}
	if s.vault != nil {
		stores = append(stores, s.vault)
	}
	return stores
}

// CommitStateChanges writes a block to the state WAL, the state commit store and the EVM state store,
// in that order. Callers must not run two commits at once.
//
// The time each of the three takes is published under commitPhaseTimerName, split by phase, which is
// what makes a slow store on the commit path attributable to that store.
func (s *StateDB) CommitStateChanges(blockNum int64, changeset []*proto.NamedChangeSet) error {
	if blockNum < 0 {
		// The WAL numbers blocks with a uint64, so a negative height converts to a block far in the
		// future that the WAL has no way to recognize as a mistake.
		return fmt.Errorf("commit block %d: block number must not be negative", blockNum)
	}

	// Ends the phase in flight, so the gap until the next commit is not charged to the last store.
	defer s.commitPhases.Reset()

	// No need to flush WAL, since this WAL isn't used for crash recoverability safety (that's the BlockDB's job).
	s.commitPhases.SetPhase("write_state_wal")
	if err := s.wal.Write(uint64(blockNum), changeset); err != nil {
		return fmt.Errorf("write block %d to state WAL: %w", blockNum, err)
	}

	s.commitPhases.SetPhase("commit_sc")
	if err := s.sc.CommitStateChanges(blockNum, changeset); err != nil {
		return fmt.Errorf("commit block %d to live state DB: %w", blockNum, err)
	}
	// SS takes the block asynchronously and is not waited on: the WAL is written first, so a shutdown
	// that loses the queue leaves SS behind the WAL, which is the gap catchUpTo replays on the next open.
	// What is timed here is therefore the wait to hand the block over, not the write itself.
	if s.ss != nil {
		s.commitPhases.SetPhase("enqueue_ss")
		if err := s.ss.CommitBlock(blockNum, changeset); err != nil {
			return fmt.Errorf("commit block %d to the EVM state store: %w", blockNum, err)
		}
	}

	return nil
}

func (s *StateDB) OpenView() gigatypes.StateView {
	return s.sc.OpenView()
}

// OpenViewAt panics. Serving a past height requires the historical state DB, which is not wired into
// StateDB.
func (s *StateDB) OpenViewAt(blockNum int64) (gigatypes.StateView, bool) {
	panic(fmt.Sprintf(
		"giga: OpenViewAt(%d) is not implemented: the historical state DB is not wired in", blockNum))
}

// RegisterHashListener forwards to the state commit store, which is the layer that hashes blocks and
// so is the layer that dispatches them.
func (s *StateDB) RegisterHashListener(listener gigatypes.HashListener) (lthash.BlockHash, error) {
	mostRecentHash, err := s.sc.RegisterHashListener(listener)
	if err != nil {
		return mostRecentHash, fmt.Errorf("register hash listener on the state commit store: %w", err)
	}
	return mostRecentHash, nil
}

// GetBlockHeight returns the version SC is on.
func (s *StateDB) GetBlockHeight() uint64 {
	return uint64(s.sc.Version()) //nolint:gosec // a committed version is never negative
}

// GetBlockHash returns the hash the hash vault holds for blockNumber.
func (s *StateDB) GetBlockHash(blockNumber uint64) ([32]byte, gigatypes.BlockHashStatus, error) {
	hash, status, err := s.vault.Get(blockNumber)
	if err != nil {
		return hash, status, fmt.Errorf("get the hash of block %d: %w", blockNumber, err)
	}
	return hash, status, nil
}

// PruneBlockHashesBelow permits the hash vault to delete the hashes of blocks below blockNumber.
func (s *StateDB) PruneBlockHashesBelow(blockNumber uint64) error {
	s.vault.PruneBelow(blockNumber)
	return nil
}
