package giga

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashvault"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// What the stores hold on disk before any of them opens.
type recoverySurvey struct {
	walStored bool

	// Only meaningful when walStored is true.
	walFirst uint64

	// Only meaningful when walStored is true.
	walLast uint64

	// Only meaningful when vaultRecorded is true.
	vaultHead uint64

	vaultRecorded bool

	// The live state DB's snapshot versions, lowest first.
	scSnapshots []uint64
}

// Where each store is put before the stores open.
type recoveryPlan struct {
	// The height the open replays every store up to, or 0 when there is nothing to move or replay.
	head uint64

	// The live state DB is put on its newest snapshot at or below this when it holds state above it.
	scTarget uint64

	// The historical state DB is put on its newest snapshot at or below this when it holds state above it.
	ssTarget uint64

	// Whether the snapshots and WAL blocks above head are dropped.
	rollback bool

	// Whether an empty hash vault's refill was cut short by how far back the snapshots and WAL reach.
	emptyVaultRefillShortened bool

	// Whether an empty hash vault cannot be refilled, and records only the loaded block's hash.
	emptyVaultRefillUnreachable bool
}

// Puts each store where it belongs, as decided from what the stores hold on disk. Every store must be
// closed.
func recoverStores(
	flatkvCfg *flatkvconfig.Config,
	ssCfg config.StateStoreConfig,
	hashVaultCfg hashvault.HashVaultConfig,
	rollbackTo uint64,
) error {
	survey, err := surveyStores(flatkvCfg, hashVaultCfg)
	if err != nil {
		return fmt.Errorf("survey the stores: %w", err)
	}
	plan, err := planRecovery(survey, rollbackTo, hashVaultCfg.EmptyVaultRollbackBlocks)
	if err != nil {
		return fmt.Errorf("plan the recovery: %w", err)
	}
	logRecoveryPlan(plan, survey, hashVaultCfg.EmptyVaultRollbackBlocks)
	if err := applyRecoveryPlan(flatkvCfg, ssCfg, survey.walFirst, plan); err != nil {
		return fmt.Errorf("apply the recovery plan: %w", err)
	}
	return nil
}

// Reads what the state WAL, the hash vault and the live state DB's snapshots hold. Every store must be
// closed.
func surveyStores(flatkvCfg *flatkvconfig.Config, hashVaultCfg hashvault.HashVaultConfig) (recoverySurvey, error) {
	// This takes the WAL directory's exclusive lock, so it only works before the WAL opens.
	walStored, walFirst, walLast, err := statewal.GetRange(flatkv.StateWALConfig(flatkvCfg.DataDir))
	if err != nil {
		return recoverySurvey{}, fmt.Errorf("survey the state WAL: %w", err)
	}
	versions, err := flatkv.SnapshotVersions(flatkvCfg.DataDir)
	if err != nil {
		return recoverySurvey{}, fmt.Errorf("survey the state commit store's snapshots: %w", err)
	}
	scSnapshots := make([]uint64, len(versions))
	for i, version := range versions {
		scSnapshots[i] = uint64(version) //nolint:gosec // a snapshot version is never negative
	}
	_, vaultHead, vaultRecorded, err := hashvault.StoredRange(hashVaultCfg)
	if err != nil {
		return recoverySurvey{}, fmt.Errorf("survey the hash vault: %w", err)
	}
	return recoverySurvey{
		walStored:     walStored,
		walFirst:      walFirst,
		walLast:       walLast,
		vaultHead:     vaultHead,
		vaultRecorded: vaultRecorded,
		scSnapshots:   scSnapshots,
	}, nil
}

// Decides where each store is put: on the WAL's head, or on rollbackTo when it is not 0, which must be at
// or below the head. The live state DB goes lower when the hash vault is missing hashes only a replay can
// produce.
func planRecovery(
	survey recoverySurvey,
	rollbackTo uint64,
	emptyVaultRollbackBlocks uint64,
) (recoveryPlan, error) {
	if !survey.walStored {
		// An empty WAL says nothing about where state belongs, so it moves nothing.
		if rollbackTo > 0 {
			return recoveryPlan{}, fmt.Errorf("cannot roll back to %d: the state WAL is empty, so no "+
				"replay reaches the target", rollbackTo)
		}
		return recoveryPlan{}, nil
	}
	// A crash can lose the WAL's unflushed tail while the state above it survives. Those blocks are
	// re-executed, so that state is discarded.
	plan := recoveryPlan{head: survey.walLast}
	if rollbackTo > 0 {
		if survey.walLast < rollbackTo {
			return recoveryPlan{}, fmt.Errorf("cannot roll back to %d: the state WAL ends at %d, so no "+
				"replay reaches the target", rollbackTo, survey.walLast)
		}
		plan.head = rollbackTo
		plan.rollback = true
	}
	plan.scTarget = plan.head
	plan.ssTarget = plan.head

	if plan.head == 0 || plan.head < survey.walFirst {
		// No WAL block is left to replay once the plan is applied, so a lower SC would stay lower.
		return plan, nil
	}
	planHashVaultRefill(&plan, survey, emptyVaultRollbackBlocks)
	return plan, nil
}

// Lowers the live state DB's target so the replay produces the hashes the vault is missing. plan.head must
// be a height the WAL can replay up to.
func planHashVaultRefill(plan *recoveryPlan, survey recoverySurvey, emptyVaultRollbackBlocks uint64) {
	if survey.vaultRecorded {
		if survey.vaultHead < plan.head {
			// The rewind lands on the snapshot at or below the target, so a target of 1 still reaches the
			// state a vault holding only block 0 needs.
			plan.scTarget = max(survey.vaultHead, 1)
		}
		return
	}

	if emptyVaultRollbackBlocks == 0 {
		return
	}
	earliest, reachable := earliestSCRewindTarget(survey.scSnapshots, survey.walFirst)
	if !reachable {
		plan.emptyVaultRefillUnreachable = true
		return
	}
	target := earliest
	if emptyVaultRollbackBlocks < plan.head && plan.head-emptyVaultRollbackBlocks >= earliest {
		target = plan.head - emptyVaultRollbackBlocks
	} else {
		plan.emptyVaultRefillShortened = true
	}
	plan.scTarget = min(target, plan.head)
}

// Returns the lowest height the live state DB can be rewound to and replayed from a WAL starting at
// walFirst, and false when there is none.
func earliestSCRewindTarget(scSnapshots []uint64, walFirst uint64) (uint64, bool) {
	for _, version := range scSnapshots {
		if version+1 >= walFirst {
			// A rewind target of 0 is refused, and 1 lands on a snapshot at 0 just the same.
			return max(version, 1), true
		}
	}
	return 0, false
}

// Reports a plan that puts the live state DB below the head, and why.
func logRecoveryPlan(plan recoveryPlan, survey recoverySurvey, emptyVaultRollbackBlocks uint64) {
	switch {
	case plan.emptyVaultRefillUnreachable:
		logger.Warn("The hash vault is empty and no snapshot of the state commit store can be replayed "+
			"from the state WAL, so it records only the loaded block's hash",
			"walFirst", survey.walFirst, "walLast", survey.walLast)
	case plan.emptyVaultRefillShortened:
		logger.Warn("The hash vault is empty and the requested rewind reaches further back than the state "+
			"commit store's snapshots and the state WAL can replay, so it is shortened",
			"requestedBlocks", emptyVaultRollbackBlocks, "head", plan.head, "target", plan.scTarget)
	}
	if plan.scTarget < plan.head {
		logger.Info("Rewinding the state commit store so the replay refills the hash vault",
			"target", plan.scTarget, "head", plan.head,
			"vaultRecorded", survey.vaultRecorded, "vaultHead", survey.vaultHead)
	}
}

// Puts the live and historical state DBs on the plan's targets and, for a rollback, drops the snapshots and
// WAL blocks above the head. The stores must be closed, and walFirst is the WAL's lowest block when the
// plan was made. A refused target leaves the WAL uncut.
func applyRecoveryPlan(
	flatkvCfg *flatkvconfig.Config,
	ssCfg config.StateStoreConfig,
	walFirst uint64,
	plan recoveryPlan,
) error {
	if plan.head == 0 {
		return nil
	}
	//nolint:gosec // WAL block numbers never approach the int64 ceiling
	if _, err := flatkv.DiscardStateAbove(flatkvCfg.DataDir, int64(plan.scTarget), int64(walFirst)); err != nil {
		return plan.refusal(fmt.Errorf("the state commit store cannot reach %d: %w", plan.scTarget, err))
	}
	if err := discardSSAbove(ssCfg, walFirst, plan.ssTarget); err != nil {
		return plan.refusal(err)
	}
	if !plan.rollback {
		return nil
	}
	if err := dropSnapshotsAbove(flatkvCfg, ssCfg, plan.head); err != nil {
		return fmt.Errorf("cannot roll back to %d: %w", plan.head, err)
	}
	// Last, so that an interruption leaves the WAL still above the target and a restart comes back here. A
	// live WAL prunes only from its start, so the tail is cut through the directory, with no WAL open on it.
	if err := statewal.PruneAfter(flatkv.StateWALConfig(flatkvCfg.DataDir), plan.head); err != nil {
		return fmt.Errorf("cannot roll back to %d: truncate state WAL: %w", plan.head, err)
	}
	return nil
}

// Wraps a store's refusal of its target in the rollback it refused, or in the WAL head when no rollback
// was asked for.
func (p recoveryPlan) refusal(err error) error {
	if p.scTarget < p.head {
		err = fmt.Errorf("rewinding the state commit store to %d to refill the hash vault: %w", p.scTarget, err)
	}
	if p.rollback {
		return fmt.Errorf("cannot roll back to %d: %w", p.head, err)
	}
	return fmt.Errorf("cannot open on the state WAL's head %d: %w", p.head, err)
}

// Puts an enabled historical state DB on its newest snapshot at or below target when it holds state above
// it. The store must be closed, and a target the WAL cannot replay it back up to is refused.
func discardSSAbove(ssCfg config.StateStoreConfig, walFirst uint64, target uint64) error {
	if !ssCfg.Enable {
		return nil
	}
	snapshotRoot := utils.GetStateStoreSnapshotsSiblingPath(ssCfg.EVMDBDirectory)
	//nolint:gosec // WAL block numbers never approach the int64 ceiling
	if _, err := evm.DiscardStateAbove(ssCfg, snapshotRoot, int64(target), int64(walFirst)); err != nil {
		return fmt.Errorf("the EVM state store cannot reach %d: %w", target, err)
	}
	return nil
}
